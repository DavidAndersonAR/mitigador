package session

import (
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewManager builds an scs.SessionManager backed by pgxstore.
// Cookies are HttpOnly + Secure + SameSite=Lax (D-13).
// Lifetime: 12h; IdleTimeout: 1h.
func NewManager(pool *pgxpool.Pool) *scs.SessionManager {
	sm := scs.New()
	sm.Store = pgxstore.New(pool)
	sm.Lifetime = 12 * time.Hour
	sm.IdleTimeout = 1 * time.Hour
	sm.Cookie.Name = "mitigador_session"
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = true
	sm.Cookie.SameSite = http.SameSiteLaxMode
	sm.Cookie.Path = "/"
	return sm
}
