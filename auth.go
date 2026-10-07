package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const sessionCookie = "mb_session"

// auth implements an optional single shared password. Sessions are an HMAC
// over a per-run random secret, so they all expire when the server restarts.
type auth struct {
	passwordHash [32]byte
	token        string
}

func newAuth(password string) *auth {
	secret := make([]byte, 32)
	rand.Read(secret)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("session"))
	return &auth{
		passwordHash: sha256.Sum256([]byte(password)),
		token:        hex.EncodeToString(mac.Sum(nil)),
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	return err == nil && hmac.Equal([]byte(c.Value), []byte(a.token))
}

func (a *auth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The login page needs the web font before anyone is logged in.
		if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/static/fonts/") || a.valid(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func (a *auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	given := sha256.Sum256([]byte(r.FormValue("password")))
	if subtle.ConstantTimeCompare(given[:], a.passwordHash[:]) != 1 {
		time.Sleep(500 * time.Millisecond) // slow down guessing
		http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    a.token,
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
