package session

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewManager builds an scs.SessionManager backed by pgxstore.
// Cookies are HttpOnly + SameSite=Lax (D-13); Secure is derived from secureCookies
// (true in production behind HTTPS; false when running over http:// for dev/e2e).
// Lifetime: 12h; IdleTimeout: 1h.
func NewManager(pool *pgxpool.Pool, secureCookies bool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = pgxstore.New(pool)
	sm.Lifetime = 12 * time.Hour
	sm.IdleTimeout = 1 * time.Hour
	sm.Cookie.Name = "mitigador_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = secureCookies
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
