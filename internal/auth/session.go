package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookie = "metery_session"
	sessionMaxAge = 30 * 24 * time.Hour
)

type SessionManager struct {
	secret []byte
}

func NewSessionManager(secret []byte) *SessionManager {
	return &SessionManager{secret: secret}
}

func (s *SessionManager) Set(w http.ResponseWriter, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    userID + "." + s.sign(userID),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionMaxAge.Seconds()),
	})
}

func (s *SessionManager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func (s *SessionManager) UserID(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	id, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(id))) {
		return ""
	}
	return id
}

func (s *SessionManager) sign(data string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
