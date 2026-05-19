package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/netsampler/goflow2/v2/producer"
	"github.com/netsampler/goflow2/v2/utils"

	"github.com/mitigador/mitigador/internal/config"
)

// Start launches three UDP listeners (NetFlow v9 on cfg.NetFlow, IPFIX on cfg.IPFIX,
// sFlow v5 on cfg.SFlow), all routed through the same prod.
//
// Goflow2 v2.2.6 API confirmed via source inspection:
//   - utils.NewNetFlowPipe(*PipeConfig) *NetFlowPipe  — handles NetFlow v5/v9 and IPFIX (v10)
//   - utils.NewSFlowPipe(*PipeConfig) *SFlowPipe      — handles sFlow v5
//   - utils.PipeConfig.Producer                       — field that receives producer.ProducerInterface
//   - utils.NewUDPReceiver(*UDPReceiverConfig)        — Workers, Sockets, QueueSize fields
//   - recv.Start(addr, port, decodeFunc)              — binds UDP socket, starts workers
//   - recv.Stop()                                     — closes socket and drains workers
//
// Note on ReceiveBufferBytes: goflow2 v2.2.6's UDPReceiverConfig does NOT expose an
// SO_RCVBUF setting. cfg.ReceiveBufferBytes is used to size the internal dispatch queue
// (QueueSize ≈ ReceiveBufferBytes / 9000 bytes per packet, clamped to [1000, 1000000]).
// Operators wanting larger kernel socket buffers must set net.core.rmem_max via sysctl
// and document this in the systemd unit (deploy/systemd/mitigador.service).
//
// Returns when ctx is canceled; releases all three sockets cleanly.
func Start(ctx context.Context, cfg config.Ingest, prod producer.ProducerInterface) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// Approximate queue size from ReceiveBufferBytes: assume ~9000 bytes/packet.
	// Clamp between 1000 and 1000000 to stay within sane bounds.
	queueSize := cfg.ReceiveBufferBytes / 9000
	if queueSize < 1000 {
		queueSize = 1000
	}
	if queueSize > 1000000 {
		queueSize = 1000000
	}

	startListener := func(name string, listenCfg config.IngestPort, pipe utils.FlowPipe) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("ingest: starting listener",
				"proto", name,
				"addr", listenCfg.ListenAddr,
				"port", listenCfg.ListenPort,
			)
			if err := runListener(ctx, name, listenCfg.ListenAddr, listenCfg.ListenPort, queueSize, pipe); err != nil {
				errCh <- fmt.Errorf("%s listener: %w", name, err)
			}
		}()
	}

	// NetFlow v9 — port cfg.NetFlow.ListenPort (default 2055) (TELE-01)
	startListener("netflow",
		config.IngestPort{ListenAddr: cfg.NetFlow.ListenAddr, ListenPort: cfg.NetFlow.ListenPort},
		utils.NewNetFlowPipe(&utils.PipeConfig{Producer: prod}))
	// IPFIX (NetFlow v10) — port cfg.IPFIX.ListenPort (default 4739) (TELE-02)
	// NetFlowPipe handles both v9 and v10 (IPFIX); the version byte selects the decoder.
	startListener("ipfix",
		config.IngestPort{ListenAddr: cfg.IPFIX.ListenAddr, ListenPort: cfg.IPFIX.ListenPort},
		utils.NewNetFlowPipe(&utils.PipeConfig{Producer: prod}))
	// sFlow v5 — port cfg.SFlow.ListenPort (default 6343) (TELE-03)
	startListener("sflow",
		config.IngestPort{ListenAddr: cfg.SFlow.ListenAddr, ListenPort: cfg.SFlow.ListenPort},
		utils.NewSFlowPipe(&utils.PipeConfig{Producer: prod}))

	wg.Wait()
	close(errCh)

	// Return the first error encountered, if any.
	for e := range errCh {
		return e
	}
	return nil
}

// runListener binds a single UDP port, registers the pipe's DecodeFlow as the handler,
// and blocks until ctx is done. It stops the receiver before returning.
func runListener(ctx context.Context, name, addr string, port, queueSize int, pipe utils.FlowPipe) error {
	recv, err := utils.NewUDPReceiver(&utils.UDPReceiverConfig{
		Workers:   1,
		Sockets:   1,
		QueueSize: queueSize,
	})
	if err != nil {
		return fmt.Errorf("new UDP receiver: %w", err)
	}

	if err := recv.Start(addr, port, pipe.DecodeFlow); err != nil {
		return fmt.Errorf("start %s:%d: %w", addr, port, err)
	}

	<-ctx.Done()

	if err := recv.Stop(); err != nil {
		slog.Warn("ingest: error stopping listener", "proto", name, "err", err.Error())
	}
	slog.Info("ingest: listener stopped", "proto", name)
	return nil
}
