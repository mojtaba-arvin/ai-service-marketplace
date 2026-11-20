package marketplace

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieSessionAttributes(t *testing.T) {
	h := &Handler{CookieSecure:true}
	rec := httptest.NewRecorder()
	now := time.Now()
	if err := h.setSessionCookies(rec, SessionTokens{
		SessionID:"session-1", AccessToken:"access-secret", RefreshToken:"refresh-secret",
		AccessExpiresAt:now.Add(15*time.Minute), RefreshExpiresAt:now.Add(24*time.Hour),
	}); err != nil { t.Fatal(err) }
	cookies := rec.Result().Cookies()
	if len(cookies) != 3 { t.Fatalf("expected three cookies, got %d", len(cookies)) }
	for _, c := range cookies {
		if !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
			t.Fatalf("unsafe cookie attributes for %q: %+v", c.Name, c)
		}
		if (c.Name == accessCookieName || c.Name == refreshCookieName) && !c.HttpOnly {
			t.Fatalf("session credential %q must be HttpOnly", c.Name)
		}
		if c.Name == csrfCookieName && c.HttpOnly {
			t.Fatal("CSRF cookie must be readable by same-origin JavaScript")
		}
	}
}

func TestCookieSessionRequiresCSRFAndSameOrigin(t *testing.T) {
	h := &Handler{TestMode:true, OTPCode:"123456"}
	request := func(origin, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/session/revoke", nil)
		req.AddCookie(&http.Cookie{Name:accessCookieName,Value:"test-session"})
		req.AddCookie(&http.Cookie{Name:csrfCookieName,Value:"csrf-value"})
		if csrf != "" { req.Header.Set("X-CSRF-Token",csrf) }
		if origin != "" { req.Header.Set("Origin",origin) }
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec,req)
		return rec
	}
	if got := request("",""); got.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF token: got %d",got.Code)
	}
	if got := request("https://attacker.example","csrf-value"); got.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation: got %d",got.Code)
	}
	if got := request("http://wrong.example","csrf-value"); got.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin mutation: got %d",got.Code)
	}
	if got := request("http://example.com","csrf-value"); got.Code != http.StatusNotImplemented {
		t.Fatalf("same-origin request should pass CSRF and reach revoke handler: got %d",got.Code)
	}
}

func TestCookieSessionAuthenticatesWithoutBearerHeader(t *testing.T) {
	h := &Handler{TestMode:true}
	req := httptest.NewRequest(http.MethodGet,"/v1/auth/me",nil)
	req.AddCookie(&http.Cookie{Name:accessCookieName,Value:"test-session"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec,req)
	if rec.Code != http.StatusOK { t.Fatalf("cookie authentication status=%d body=%s",rec.Code,rec.Body.String()) }
}
