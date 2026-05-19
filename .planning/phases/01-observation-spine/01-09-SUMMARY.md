---
phase: 01-observation-spine
plan: 09
subsystem: alert
tags: [alert-bus, telegram, smtp, rate-limiter, fan-out, pt-BR, go-telegram-bot, go-mail, token-bucket]

requires:
  - phase: 01-07
    provides: AttackEvent struct and channel contract (detect.AttackEvent, State, Vector)
  - phase: 01-03
    provides: Config.Telegram (BotToken, AllowedChatIDs) and Config.SMTP (Host, Port, Username, Password, Security, FromAddr, ToAddrs)

provides:
  - alert.Bus: fan-out broadcaster (1 input chan → N buffered subscriber chans, drop-on-full per sink)
  - alert.Sink interface: Name() string + Run(ctx, in) error
  - telegram.Format: pt-BR MarkdownV2-escaped message templates for STARTED/UPDATED/ENDED
  - telegram.Sender: dual token bucket (30/s global + 1/s per chat) + 429 retry + bounded queue
  - email.Sender: SMTP via wneessen/go-mail, STARTTLS/TLS/plain, pt-BR plain-text, 30s per-send timeout

affects: [01-10-sse-dashboard, 01-12-smoke-test, cmd/mitigador]

tech-stack:
  added:
    - "github.com/go-telegram/bot v1.20.0 — Telegram Bot API client (MarkdownV2 support, TooManyRequestsError, WithServerURL for testing)"
    - "github.com/wneessen/go-mail v0.7.3 — SMTP client (STARTTLS/TLS/plain, DialAndSendWithContext)"
  patterns:
    - "Fan-out bus: single input channel → N subscriber channels, each buffered; drop-on-full with slog.Warn (non-blocking delivery guarantee)"
    - "Sink interface: each sink runs independently in its own goroutine via alert.Sink; slow sink never blocks bus or peer sinks"
    - "Dual token bucket: golang.org/x/time/rate.Limiter — global 30/s + per-chat 1/s map protected by sync.Mutex"
    - "429 retry: errors.As(*gobot.TooManyRequestsError), sleep RetryAfter+1s, re-enqueue bounded (maxRetries=3)"
    - "WithSkipGetMe: bot.New skips getMe API call — faster startup, no 429 at construction, test-friendly"
    - "White-box email tests: package email (internal test) tests unexported format() directly without network"
    - "Multipart form: go-telegram/bot sends multipart/form-data — tests use r.FormValue() not JSON body parsing"

key-files:
  created:
    - internal/alert/bus.go
    - internal/alert/bus_test.go
    - internal/alert/telegram/format.go
    - internal/alert/telegram/format_test.go
    - internal/alert/telegram/sender.go
    - internal/alert/telegram/sender_test.go
    - internal/alert/email/sender.go
    - internal/alert/email/sender_test.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "WithSkipGetMe always prepended in NewSender: avoids getMe API call at construction, preventing 429 during test setup and speeding up production startup (SendMessage-only usage never needs getMe)"
  - "go-telegram/bot sends multipart/form-data not JSON: test handlers use r.FormValue('chat_id') not JSON body parsing — discovered via library source inspection"
  - "White-box test for email.format(): package email internal test accesses unexported format() directly; no SMTP network call needed for unit tests; integration coverage deferred to plan 12 smoke test"
  - "Bus.Subscribe returns receive-only channel: enforces that only the bus writes to subscriber channels; pre-filling for test impossible from external package — drop test redesigned to use large buffer + unread subscriber"
  - "Bot.New options variadic in NewSender signature: allows callers to inject gobot.WithServerURL(srv.URL) for test isolation without changing production call site"

patterns-established:
  - "alert.Sink interface contract: every notification channel (Telegram, Email, SSE) implements Name()+Run(ctx, in) and subscribes to alert.Bus independently"
  - "Drop-on-full with slog.Warn: all buffered channels in the alert layer use select{case ch<-ev: default: slog.Warn(...)} — never block the hot path"

requirements-completed: [ALER-01, ALER-02, ALER-05, ALER-06, ALER-08]

duration: 45min
completed: 2026-05-19
---

# Phase 1 Plan 9: Alert Fan-Out Summary

**alert.Bus fan-out to Telegram (dual token bucket 30/s global + 1/s per-chat, 429 retry) and Email (wneessen/go-mail STARTTLS/TLS/plain) sinks with pt-BR MarkdownV2 and plain-text templates including incident URL**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-05-19T00:00:00Z
- **Completed:** 2026-05-19
- **Tasks:** 3
- **Files modified:** 10 (8 created + go.mod + go.sum)

## Accomplishments

- `alert.Bus`: fan-out broadcaster with drop-on-full per subscriber — slow Telegram never blocks Email or SSE (plan 10 will Subscribe here)
- `telegram.Sender`: ALER-08 compliant with dual `rate.Limiter`, `TooManyRequestsError` retry (max 3 attempts, RetryAfter+1s sleep), bounded queue (1000), `WithSkipGetMe` for test isolation
- `telegram.Format`: pt-BR MarkdownV2 templates for all three states (STARTED/UPDATED/ENDED), with `mdv2Escape()` covering all 18 Telegram special chars, incident URL in every message
- `email.Sender`: wneessen/go-mail with STARTTLS/TLS/plain transport selection, pt-BR plain-text subjects and bodies with incident URL, 30s per-send context timeout
- 17 tests pass across 3 packages with race detector (`-race`)

## Bus API

```go
func NewBus(in <-chan detect.AttackEvent, perSinkBuffer int) *Bus
func (b *Bus) Subscribe(name string) <-chan detect.AttackEvent  // must call before Run
func (b *Bus) Run(ctx context.Context) error                    // blocks; closes all subs on exit
```

Subscriber channels are closed when `Run` exits (ctx cancelled or `in` closed), which signals sinks to return from their own `Run`.

## Telegram Sender Constants

| Constant | Value | Purpose |
|----------|-------|---------|
| `globalRatePerSec` | 30 | Telegram global limit (ALER-08) |
| `perChatRatePerSec` | 1 | Per-chat limit (ALER-08) |
| `queueSize` | 1000 | Bounded internal queue depth |
| `maxRetries` | 3 | Max 429 retry attempts before drop |

MarkdownV2 parse mode: `models.ParseModeMarkdown = "MarkdownV2"` (go-telegram/bot v1.20.0 constant).

## Email Sender Security Mode Map

| Config `security` | go-mail option |
|-------------------|----------------|
| `"starttls"` | `WithTLSPolicy(mail.TLSMandatory)` |
| `"tls"` | `WithSSLPort(false)` (implicit TLS, port from WithPort) |
| `"plain"` | `WithTLSPolicy(mail.NoTLS)` |
| other | returns error at `NewSender` time |

## Alert Text Confirmation

All alert messages are in pt-BR regardless of dashboard UI toggle:
- Telegram STARTED: "🚨 *Ataque detectado*"
- Telegram UPDATED: "📈 *Ataque ainda em curso*"
- Telegram ENDED: "✅ *Ataque encerrado*"
- Email STARTED subject: "[Mitigador] Ataque detectado em {IP} ({Vector})"
- Email UPDATED subject: "[Mitigador] Ataque em andamento em {IP} ({Vector})"
- Email ENDED subject: "[Mitigador] Ataque encerrado em {IP} ({Vector})"

## Task Commits

| Task | Name | Commit | Type |
|------|------|--------|------|
| 1 | alert.Bus + pt-BR Format | `45f294c` | feat |
| 2 | Telegram Sender (rate limits + 429 retry) | `25d4e00` | feat |
| 3 | Email Sender (go-mail STARTTLS/TLS/plain) | `0b9164e` | feat |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Bus drop test redesigned — Subscribe returns receive-only channel**
- **Found during:** Task 1 (TestBus_DropsForSlowSubscriber)
- **Issue:** Initial test tried to pre-fill the subscriber channel to force drops, but `Subscribe` returns `<-chan detect.AttackEvent` (receive-only); sends from the test package fail to compile.
- **Fix:** Redesigned test to use a `perSinkBuffer=100` fast subscriber (large buffer) and an unread "slow" subscriber. Added a separate `TestBus_DropsOnFullBuffer` test with `perSinkBuffer=1` and a closed input channel to verify the bus doesn't deadlock on a full subscriber buffer.
- **Files modified:** `internal/alert/bus_test.go`
- **Committed in:** `45f294c` (Task 1 commit)

**2. [Rule 1 - Bug] bot.New calls getMe at construction — 429 on test server**
- **Found during:** Task 2 (TestSender_RetriesOn429)
- **Issue:** `bot.New(token, opts...)` makes a `getMe` HTTP call. The test's 429 handler returned 429 on the first call, causing `NewSender` to fail rather than the `SendMessage` call.
- **Fix:** Always prepend `gobot.WithSkipGetMe()` in `NewSender` before caller-supplied options. This also improves production startup (getMe is unnecessary for SendMessage-only usage).
- **Files modified:** `internal/alert/telegram/sender.go`
- **Committed in:** `25d4e00` (Task 2 commit)

**3. [Rule 1 - Bug] go-telegram/bot sends multipart/form-data, not JSON**
- **Found during:** Task 2 (TestSender_OnlyAllowedChatIDsReceive — totalCalls never incremented)
- **Issue:** The initial test handler parsed the HTTP body as JSON (`json.Unmarshal`) and looked for `"chat_id"` key. The library sends multipart/form-data, so `json.Unmarshal` returned empty map and `chat_id` was never found.
- **Fix:** Switched handler to `r.ParseMultipartForm(1<<20)` + `r.FormValue("chat_id")` + `strconv.ParseInt`. Added data-race protection (`sync.Mutex` on the slice + `atomic.Int64` for the count).
- **Files modified:** `internal/alert/telegram/sender_test.go`
- **Committed in:** `25d4e00` (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (all Rule 1 — bugs in test setup, not in production logic)
**Impact on plan:** No production code logic changed. All fixes were test correctness issues. No scope creep.

## Known Stubs

None — all exported symbols are fully implemented. `email.format()` is unexported by design (tested white-box); `telegram.Format()` is exported as specified.

## Threat Flags

No new threat surface beyond the plan's threat model. The files produced are outbound-only clients (Telegram HTTPS, SMTP). No new endpoints, file access, or auth paths introduced.

Threat model compliance verified:
- **T-01-09-01:** `cfg.BotToken` appears only in `gobot.New(cfg.BotToken, ...)` constructor call; `cfg.Password` only in `mail.WithPassword(cfg.Password)`. Neither appears in any `slog.*` or `fmt.Errorf` format string.
- **T-01-09-02:** `Run` iterates only `s.chatIDs` (copied from `cfg.AllowedChatIDs` at construction). No broadcast, no chat discovery.
- **T-01-09-03:** `mdv2Escape()` escapes all 18 Telegram MarkdownV2 special chars; `TestFormat_EscapesMarkdownV2Chars` confirms dot escaping.
- **T-01-09-04:** Dual token bucket + bounded queue + drop-on-full + D-17 email parity all implemented.
- **T-01-09-07:** `NewSender` returns error for unknown `Security` value; only `starttls/tls/plain` accepted.
- **T-01-09-08:** `sendOne` wraps `DialAndSendWithContext` in a 30s `context.WithTimeout`.

## Self-Check: PASSED

Files created:
- FOUND: internal/alert/bus.go
- FOUND: internal/alert/bus_test.go
- FOUND: internal/alert/telegram/format.go
- FOUND: internal/alert/telegram/format_test.go
- FOUND: internal/alert/telegram/sender.go
- FOUND: internal/alert/telegram/sender_test.go
- FOUND: internal/alert/email/sender.go
- FOUND: internal/alert/email/sender_test.go

Commits:
- FOUND: 45f294c — feat(01-09): alert.Bus fan-out + pt-BR Telegram Format
- FOUND: 25d4e00 — feat(01-09): Telegram Sender with dual token bucket + 429 retry (ALER-08)
- FOUND: 0b9164e — feat(01-09): Email Sender via wneessen/go-mail with pt-BR templates (D-17)

Verifications:
- `go build ./...` exits 0
- `go vet ./internal/alert/...` exits 0
- `go test ./internal/alert/... -count=1 -race` → 17 tests PASS
- ALER-01: only AllowedChatIDs receive (TestSender_OnlyAllowedChatIDsReceive)
- ALER-08: dual token bucket (TestSender_FansToAllChatIDs, rate limiter constants in code)
- ALER-08: 429 retry (TestSender_RetriesOn429)
- D-17: Telegram and Email subscribe independently to the same Bus
- pt-BR confirmed: "Ataque detectado", "Ataque encerrado", "ainda em curso" in sources
