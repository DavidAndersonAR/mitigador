package telegram

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	gobot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"golang.org/x/time/rate"

	"github.com/mitigador/mitigador/internal/config"
	"github.com/mitigador/mitigador/internal/detect"
)

const (
	globalRatePerSec  = 30
	perChatRatePerSec = 1
	queueSize         = 1000
	maxRetries        = 3
)

// outbound is a pending message to be sent to a single chat.
type outbound struct {
	chatID   int64
	text     string
	attempts int
}

// Sender implements alert.Sink for Telegram.
// It fans out each AttackEvent to every configured AllowedChatID, obeying a
// dual token bucket: ≤30 msg/s globally and ≤1 msg/s per chat (ALER-08).
// On HTTP 429, it sleeps RetryAfter+1 seconds and re-enqueues (max 3 attempts).
type Sender struct {
	bot       *gobot.Bot
	chatIDs   []int64
	appURL    string
	global    *rate.Limiter
	perChatMu sync.Mutex
	perChat   map[int64]*rate.Limiter
	queue     chan outbound
}

// NewSender creates a Telegram Sender. Extra bot.Option values (e.g.
// gobot.WithServerURL) can be passed for testing against a fake server.
// gobot.WithSkipGetMe is always prepended so bot.New does not make a network
// call at construction time (avoids 429 during test server setup and speeds
// up production startup — GetMe is not needed for SendMessage-only usage).
func NewSender(cfg config.Telegram, appBaseURL string, opts ...gobot.Option) (*Sender, error) {
	allOpts := append([]gobot.Option{gobot.WithSkipGetMe()}, opts...)
	b, err := gobot.New(cfg.BotToken, allOpts...)
	if err != nil {
		return nil, err
	}
	return &Sender{
		bot:     b,
		chatIDs: append([]int64(nil), cfg.AllowedChatIDs...),
		appURL:  appBaseURL,
		global:  rate.NewLimiter(rate.Limit(globalRatePerSec), globalRatePerSec),
		perChat: make(map[int64]*rate.Limiter),
		queue:   make(chan outbound, queueSize),
	}, nil
}

// Name satisfies alert.Sink.
func (s *Sender) Name() string { return "telegram" }

// limiterFor returns (creating if absent) the per-chat rate limiter.
func (s *Sender) limiterFor(chatID int64) *rate.Limiter {
	s.perChatMu.Lock()
	defer s.perChatMu.Unlock()
	lim, ok := s.perChat[chatID]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(perChatRatePerSec), perChatRatePerSec)
		s.perChat[chatID] = lim
	}
	return lim
}

// Run satisfies alert.Sink. It starts a worker goroutine that drains the
// internal queue, then loops reading events from in. Exits when ctx is
// cancelled or in is closed.
func (s *Sender) Run(ctx context.Context, in <-chan detect.AttackEvent) error {
	go s.worker(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-in:
			if !ok {
				return nil
			}
			text := Format(ev, s.appURL)
			if text == "" {
				continue
			}
			for _, chatID := range s.chatIDs {
				select {
				case s.queue <- outbound{chatID: chatID, text: text}:
				default:
					slog.Warn("telegram: queue full, dropping message",
						"chat_id", chatID,
						"incident_id", ev.IncidentID,
					)
				}
			}
		}
	}
}

// worker reads from the internal queue and sends each message, respecting rate
// limits and retrying on 429.
func (s *Sender) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case out := <-s.queue:
			if err := s.send(ctx, out); err != nil {
				slog.Error("telegram: send failed",
					"chat_id", out.chatID,
					"attempts", out.attempts,
					"err", err.Error(),
				)
			}
		}
	}
}

// send applies both rate limiters, then issues the API call.
// On 429, it sleeps RetryAfter+1 seconds and re-enqueues with incremented
// attempt count. On the 4th attempt (maxRetries exceeded), it logs and drops.
func (s *Sender) send(ctx context.Context, out outbound) error {
	// Global bucket — max 30 msg/s across all chats.
	if err := s.global.Wait(ctx); err != nil {
		return err
	}
	// Per-chat bucket — max 1 msg/s to the same chat.
	if err := s.limiterFor(out.chatID).Wait(ctx); err != nil {
		return err
	}

	_, err := s.bot.SendMessage(ctx, &gobot.SendMessageParams{
		ChatID:    out.chatID,
		Text:      out.text,
		ParseMode: models.ParseModeMarkdown, // "MarkdownV2" in the Telegram API
	})
	if err == nil {
		return nil
	}

	var tooMany *gobot.TooManyRequestsError
	if errors.As(err, &tooMany) {
		wait := time.Duration(tooMany.RetryAfter+1) * time.Second
		slog.Warn("telegram: 429 Too Many Requests, backing off",
			"retry_after_sec", tooMany.RetryAfter,
			"chat_id", out.chatID,
			"attempts", out.attempts,
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		if out.attempts+1 >= maxRetries {
			return errors.New("telegram: max retries reached after 429")
		}
		out.attempts++
		select {
		case s.queue <- out:
		default:
			slog.Warn("telegram: queue full on 429 retry, dropping",
				"chat_id", out.chatID,
				"attempts", out.attempts,
			)
		}
		return nil
	}

	return err
}
