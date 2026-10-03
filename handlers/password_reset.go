package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"meetup/db"
)

var (
	rateMu   sync.Mutex
	rateHits = map[string][]time.Time{}
)

func checkRateLimit(ip string) bool {
	rateMu.Lock()
	defer rateMu.Unlock()
	now := time.Now()
	hits := rateHits[ip]
	var keep []time.Time
	for _, t := range hits {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 5 {
		rateHits[ip] = keep
		return false
	}
	keep = append(keep, now)
	rateHits[ip] = keep
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ForgotPassword POST /api/auth/forgot {email|username}
func ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !checkRateLimit(clientIP(r)) {
		jsonError(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	var req struct {
		Email    string `json:"email"`
		Username string `json:"username"`
	}
	if err := decodeJSON(r, &req); err != nil {
		// support raw string also
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	identifier := strings.TrimSpace(req.Email)
	if identifier == "" {
		identifier = strings.TrimSpace(req.Username)
	}
	// generic response always
	genericOK := func() {
		writeJSON(w, http.StatusOK, map[string]string{"message": "If an account exists, a reset email has been sent."})
	}
	if identifier == "" {
		genericOK()
		return
	}
	user, err := db.GetUserByEmail(identifier)
	if err != nil {
		if err == sql.ErrNoRows {
			genericOK()
			return
		}
		genericOK()
		return
	}
	if user == nil {
		genericOK()
		return
	}
	// generate token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		genericOK()
		return
	}
	token := hex.EncodeToString(b)
	h := hashToken(token)
	exp := time.Now().Add(time.Hour)
	_ = db.CreatePasswordResetToken(h, user.ID, exp)
	// determine recipient email
	to := user.Email
	if to == "" {
		to = user.Username
	}
	// if username is not an email, we still try but if no @, skip send but still generic ok
	if !strings.Contains(to, "@") {
		genericOK()
		return
	}
	base := publicBaseURL(r.Host, "http")
	if r.TLS != nil {
		base = publicBaseURL(r.Host, "https")
	}
	// env PUBLIC_BASE_URL overrides
	// send async but ignore error for enumeration
	_ = SendPasswordResetEmail(to, token, base)
	genericOK()
}

// ResetPassword POST /api/auth/reset {token,password}
func ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		jsonError(w, "token required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	h := hashToken(req.Token)
	userID, _, err := db.GetValidPasswordResetToken(h)
	if err != nil {
		jsonError(w, "invalid or expired token", http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := db.UpdateUserPasswordHash(userID, string(hash)); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	_ = db.MarkPasswordResetTokenUsed(h)
	_ = db.DeletePasswordResetTokensForUser(userID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ValidateResetToken GET /api/auth/reset/validate?token=...
func ValidateResetToken(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		jsonError(w, "token required", http.StatusBadRequest)
		return
	}
	h := hashToken(token)
	_, _, err := db.GetValidPasswordResetToken(h)
	if err != nil {
		jsonError(w, "invalid or expired token", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"valid": true})
}
