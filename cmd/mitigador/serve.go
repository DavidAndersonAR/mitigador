package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/mitigador/mitigador/internal/aggregate"
	"github.com/mitigador/mitigador/internal/alert"
	"github.com/mitigador/mitigador/internal/alert/email"
	"github.com/mitigador/mitigador/internal/alert/telegram"
	"github.com/mitigador/mitigador/internal/api"
	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/detect"
	"github.com/mitigador/mitigador/internal/dns"
	"github.com/mitigador/mitigador/internal/flow"
	"github.com/mitigador/mitigador/internal/incident"
	"github.com/mitigador/mitigador/internal/ingest"
	"github.com/mitigador/mitigador/internal/netowner"
	"github.com/mitigador/mitigador/internal/session"
	pg "github.com/mitigador/mitigador/internal/storage/postgres"
	"github.com/mitigador/mitigador/internal/user"
	"github.com/mitigador/mitigador/internal/version"
)

func newServeCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Mitigador daemon (HTTP API + UDP listeners + detection)",
		Long:  "Boots all subsystems: config → migrate → pool → inventory → stores → aggregate → detect → alert bus → API → ingest listeners. Shuts down cleanly on SIGINT/SIGTERM within 30s.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return serve(cmd.Context(), *configPath)
		},
	}
}

func serve(rootCtx context.Context, configPath string) error {
	// 1) Config — fail fast.
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 2) Logger — set early so all subsequent logs use the configured handler.
	setupLogger(cfg.Log)
	slog.Info("mitigador starting", "version", version.String(), "config_path", configPath)

	// 3) Migrations — idempotent; run before acquiring pool to avoid partial state.
	if err := pg.Migrate(cfg.Postgres.DSN); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// 4) Pool.
	poolCtx, poolCancel := context.WithTimeout(rootCtx, 10*time.Second)
	pool, err := pg.NewPool(poolCtx, cfg.Postgres.DSN, cfg.Postgres.MaxConns, cfg.Postgres.MinConns)
	poolCancel()
	if err != nil {
		return fmt.Errorf("postgres pool: %w", err)
	}
	defer pool.Close()

	// 5) Stores.
	users := user.NewStore(pool)
	incidents := incident.NewStore(pool)

	// 6) Inventory + Health.
	inv, err := ingest.LoadInventory(rootCtx, pool)
	if err != nil {
		return fmt.Errorf("load inventory: %w", err)
	}
	if len(inv.All()) == 0 {
		slog.Warn("ingest: inventory is empty — no flows will be accepted (run `mitigador config sync --file domain.yaml`)")
	} else {
		slog.Info("ingest: inventory loaded", "count", len(inv.All()))
	}
	health := ingest.NewHealthTracker()

	// 7) Detection catalog.
	catalog, err := detect.LoadCatalog(rootCtx, pool)
	if err != nil {
		return fmt.Errorf("load detect catalog: %w", err)
	}
	slog.Info("detect: catalog loaded")

	// 8) Crash recovery — close orphan incidents older than 24h.
	cutoff := time.Now().Add(-24 * time.Hour)
	closed, err := incidents.CloseOrphans(rootCtx, cutoff)
	if err != nil {
		slog.Warn("incident.recovered: close orphans error", "err", err.Error())
	}
	if closed > 0 {
		slog.Info("incident.recovered: closed orphan incidents older than 24h", "count", closed)
	}

	// 9) Channel topology.
	//    flowChan: ChannelProducer → aggregate writer goroutine
	//    attackEvents: detect engine → alert bus → four subscribers
	flowChan := make(chan flow.Record, 8192)
	attackEvents := make(chan detect.AttackEvent, 256)

	// 10) Aggregate store + producer.
	store := aggregate.New(runtime.NumCPU())
	prod := ingest.NewChannelProducer(inv, health, flowChan)
	recentFlows := flow.NewRecentBuffer(500)
	dnsResolver := dns.NewResolver()

	// Optional ASN/owner enrichment (GeoLite2-ASN or db-ip ASN-Lite mmdb).
	var asnDB *netowner.MMDB
	if p := cfg.GeoIP.ASNPath; p != "" {
		db, err := netowner.OpenMMDB(p)
		if err != nil {
			slog.Warn("geoip: ASN mmdb not loaded — dashboard owner column falls back to CIDR table", "path", p, "err", err)
		} else {
			slog.Info("geoip: ASN mmdb loaded", "path", p)
			asnDB = db
			defer asnDB.Close()
		}
	}
	// Optional Country enrichment (GeoLite2-Country or db-ip Country-Lite mmdb).
	var countryDB *netowner.CountryMMDB
	if p := cfg.GeoIP.CountryPath; p != "" {
		db, err := netowner.OpenCountryMMDB(p)
		if err != nil {
			slog.Warn("geoip: Country mmdb not loaded — country chips will be omitted", "path", p, "err", err)
		} else {
			slog.Info("geoip: Country mmdb loaded", "path", p)
			countryDB = db
			defer countryDB.Close()
		}
	}
	netOwnerResolver := netowner.New(asnDB, countryDB)

	// 11) Detect engine.
	engine := detect.NewEngine(store, catalog, attackEvents)

	// 12) Alert bus — subscribe all sinks BEFORE bus.Run.
	bus := alert.NewBus(attackEvents, 256)
	tgSub := bus.Subscribe("telegram")
	mailSub := bus.Subscribe("email")
	sseSub := bus.Subscribe("sse")
	incSub := bus.Subscribe("incident")

	tgSender, err := telegram.NewSender(cfg.Telegram, cfg.HTTP.AppBaseURL)
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	emailSender, err := email.NewSender(cfg.SMTP, cfg.HTTP.AppBaseURL)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}

	// 13) Incident recorder.
	recorder := incident.NewRecorder(incidents, incSub)

	// 14) SSE broker.
	sseBroker := api.NewBroker(sseSub)

	// 15) Session manager + API server.
	// Secure cookies only when the operator-declared base URL is https — otherwise
	// browsers (and Go's cookiejar) silently drop the cookie on plain http.
	secureCookies := strings.HasPrefix(cfg.HTTP.AppBaseURL, "https://")
	sm := session.NewManager(pool, secureCookies)
	apiHandler := api.New(api.Deps{
		Pool:        pool,
		SM:          sm,
		Users:       users,
		Incidents:   incidents,
		Inventory:   inv,
		Health:      health,
		SSEBroker:   sseBroker,
		Store:       store,
		Catalog:     catalog,
		RecentFlows: recentFlows,
		DNS:         dnsResolver,
		NetOwner:    netOwnerResolver,
	})

	// 16) HTTP server.
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTP.ListenAddr, cfg.HTTP.ListenPort),
		Handler:           apiHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 17) Signal handling — cancel context on SIGINT/SIGTERM.
	ctx, cancel := signal.NotifyContext(rootCtx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// 18) errgroup — all goroutines share the signal context.
	// T-01-12-01: each g.Go body has a defer recover() to log panics; the errgroup
	// cancels all peers on the first non-nil return, causing a clean daemon restart
	// via systemd Restart=on-failure.
	g, gctx := errgroup.WithContext(ctx)

	// Flow → aggregate writer.
	// aggregate.Store.Update is concurrency-safe; this goroutine is the single writer.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("ingest writer: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		for {
			select {
			case <-gctx.Done():
				return nil
			case r, ok := <-flowChan:
				if !ok {
					return nil
				}
				store.Update(r.DstIP, r.Received.Unix(), r)
				recentFlows.Push(r)
			}
		}
	})

	// UDP listeners (NetFlow 2055 / IPFIX 4739 / sFlow 6343).
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("ingest listeners: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		slog.Info("ingest: starting listeners",
			"netflow_port", cfg.Ingest.NetFlow.ListenPort,
			"ipfix_port", cfg.Ingest.IPFIX.ListenPort,
			"sflow_port", cfg.Ingest.SFlow.ListenPort,
		)
		return ingest.Start(gctx, cfg.Ingest, prod)
	})

	// Detection engine (1Hz tick).
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("detect engine: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return engine.Run(gctx)
	})

	// Alert bus (fan-out to subscribers).
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("alert bus: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return bus.Run(gctx)
	})

	// Telegram sink.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("telegram sender: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return tgSender.Run(gctx, tgSub)
	})

	// Email sink.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("email sender: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return emailSender.Run(gctx, mailSub)
	})

	// Incident recorder.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("incident recorder: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return recorder.Run(gctx)
	})

	// SSE broker.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("sse broker: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		return sseBroker.Run(gctx)
	})

	// HTTP server — listen.
	g.Go(func() error {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("http server: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		slog.Info("http: listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})

	// HTTP server — graceful shutdown triggered by context cancellation.
	// PERS-01: 30-second deadline ensures in-flight incident writes complete.
	g.Go(func() error {
		<-gctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		slog.Info("http: shutting down gracefully")
		return srv.Shutdown(shutdownCtx)
	})

	// Periodic drop-counter monitor (15s ticker — T-01-12-01 observability).
	g.Go(func() error {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		var lastDrops uint64
		for {
			select {
			case <-gctx.Done():
				return nil
			case <-t.C:
				d := prod.Drops()
				if d > lastDrops {
					slog.Warn("ingest: flow records dropped (channel full)",
						"delta", d-lastDrops,
						"total", d,
					)
				}
				lastDrops = d

				if dd := engine.Dropped(); dd > 0 {
					slog.Warn("detect: attack events dropped (bus full)", "total", dd)
				}
			}
		}
	})

	// Wait for all goroutines to exit.
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	slog.Info("mitigador stopped")
	return nil
}

// setupLogger configures the global slog handler based on operator config.
// Called once at startup before any other subsystem logs.
// T-01-12-02: logs only the config path, not the full config struct (avoids leaking secrets).
func setupLogger(cfg config.Log) {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(handler))
}
