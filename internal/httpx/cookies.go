package httpx

import (
	"net/http"
	"time"
)

// Cookies sets and clears the API's httpOnly cookies with one consistent policy:
// HttpOnly (unreadable from JS, so XSS can't steal it), SameSite=Lax, Secure in
// production, and an optional parent Domain so the user/merchant/admin
// subdomains share a session.
type Cookies struct {
	Domain string
	Secure bool
}

func (c Cookies) Set(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, c.cookie(name, value, int(maxAge.Seconds())))
}

func (c Cookies) Clear(w http.ResponseWriter, name string) {
	http.SetCookie(w, c.cookie(name, "", -1))
}

func (c Cookies) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Domain:   c.Domain,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	}
}
