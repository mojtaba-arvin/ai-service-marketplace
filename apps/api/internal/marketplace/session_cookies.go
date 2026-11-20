package marketplace

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	accessCookieName = "marketplace_access"
	refreshCookieName = "marketplace_refresh"
	csrfCookieName = "marketplace_csrf"
)

// Cookie sessions keep credentials out of JavaScript storage. The CSRF token is
// intentionally readable by same-origin JavaScript and must be echoed in a header.
func (h *Handler) setSessionCookies(w http.ResponseWriter, tokens SessionTokens) error {
	csrf, err := sessionToken()
	if err != nil { return err }
	secure := h.CookieSecure
	set := func(name, value string, expires time.Time, httpOnly bool) {
		maxAge := int(time.Until(expires).Seconds())
		if maxAge < 0 { maxAge = 0 }
		http.SetCookie(w, &http.Cookie{
			Name:name, Value:value, Path:"/", Expires:expires, MaxAge:maxAge,
			HttpOnly:httpOnly, Secure:secure, SameSite:http.SameSiteLaxMode,
		})
	}
	set(accessCookieName, tokens.AccessToken, tokens.AccessExpiresAt, true)
	set(refreshCookieName, tokens.RefreshToken, tokens.RefreshExpiresAt, true)
	set(csrfCookieName, csrf, tokens.RefreshExpiresAt, false)
	return nil
}

func (h *Handler) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{accessCookieName, refreshCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{Name:name, Value:"", Path:"/", MaxAge:-1,
			Expires:time.Unix(1,0), HttpOnly:name!=csrfCookieName,
			Secure:h.CookieSecure, SameSite:http.SameSiteLaxMode})
	}
}

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil { return "" }
	return c.Value
}

func validSameOrigin(r *http.Request) bool {
	raw := strings.TrimSpace(r.Header.Get("Origin"))
	if raw == "" { return true } // non-browser clients do not send Origin
	origin, err := url.Parse(raw)
	if err != nil || origin.Host == "" || origin.User != nil { return false }
	if !strings.EqualFold(origin.Host, r.Host) { return false }
	return origin.Scheme == "http" || origin.Scheme == "https"
}

func validCSRFCookie(r *http.Request) bool {
	cookieToken := cookieValue(r, csrfCookieName)
	headerToken := r.Header.Get("X-CSRF-Token")
	if cookieToken == "" || headerToken == "" || len(cookieToken) != len(headerToken) { return false }
	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(headerToken)) != 1 { return false }
	return validSameOrigin(r)
}

func (h *Handler) enforceCookieCSRF(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions { return true }
	if cookieValue(r, accessCookieName) == "" && cookieValue(r, refreshCookieName) == "" { return true }
	if validCSRFCookie(r) { return true }
	writeError(w, http.StatusForbidden, "CSRF validation failed")
	return false
}
