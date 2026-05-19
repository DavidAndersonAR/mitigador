package api_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

// testCookieJar wraps cookiejar.Jar and exposes Cookies by *http.Request.
type testCookieJar struct {
	jar *cookiejar.Jar
}

func newCookieJar(t *testing.T) *testCookieJar {
	t.Helper()
	j, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &testCookieJar{jar: j}
}

// SetCookies implements http.CookieJar (url-based).
func (j *testCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.jar.SetCookies(u, cookies)
}

// Cookies implements http.CookieJar (url-based).
func (j *testCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.jar.Cookies(u)
}

// sessionCookie retrieves the value of the mitigador_session cookie from the jar.
func sessionCookie(jar *testCookieJar, rawURL string) string {
	u, _ := url.Parse(rawURL)
	for _, c := range jar.Cookies(u) {
		if c.Name == "mitigador_session" {
			return c.Value
		}
	}
	return ""
}
