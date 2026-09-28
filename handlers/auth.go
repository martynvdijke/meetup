package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"meetup/db"
)

// ── setup & auth ──

// SetupStatus reports whether first-run setup is required.
// @Summary  Setup status
// @Tags     auth
// @Produce  json
// @Success  200 {object} map[string]bool
// @Router   /api/setup/status [get]
func SetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := db.CountUsers()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": n == 0})
}

// Setup creates the initial admin account.
// @Summary  Initial setup
// @Tags     auth
// @Accept   json
// @Produce  json
// @Param    body body map[string]string true "username and password"
// @Success  200 {object} map[string]bool
// @Failure  400 {object} map[string]string
// @Failure  403 {object} map[string]string
// @Router   /api/setup [post]
func Setup(w http.ResponseWriter, r *http.Request) {
	n, err := db.CountUsers()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		jsonError(w, "already set up", http.StatusForbidden)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 {
		jsonError(w, "username must be at least 3 characters", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := db.CreateUser(req.Username, string(hash), "admin"); err != nil {
		jsonError(w, "failed to create user", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Login authenticates with username/password.
// @Summary  Login
// @Tags     auth
// @Accept   json
// @Produce  json
// @Param    body body map[string]string true "username and password"
// @Success  200 {object} map[string]any
// @Failure  401 {object} map[string]string
// @Router   /api/auth/login [post]
func Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	u, err := db.GetUserByUsername(req.Username)
	if err != nil || u == nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	token := uuid.NewString()
	if err := db.CreateSession(token, u.ID, time.Now().Add(sessionTTL)); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"user": map[string]string{
			"username": u.Username,
			"role":     u.Role,
		},
	})
}

// Logout clears the session.
// @Summary  Logout
// @Tags     auth
// @Produce  json
// @Security CookieAuth
// @Success  200 {object} map[string]bool
// @Router   /api/auth/logout [post]
func Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = db.DeleteSession(c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Me returns the current authenticated user.
// @Summary  Current user
// @Tags     auth
// @Produce  json
// @Security CookieAuth
// @Success  200 {object} map[string]any
// @Failure  401 {object} map[string]string
// @Router   /api/auth/me [get]
func Me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		// Also try resolving from cookie directly (when AuthMiddleware not used).
		u = sessionUser(r)
	}
	if u == nil {
		jsonError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]string{
			"username": u.Username,
			"role":     u.Role,
		},
	})
}

// ── OIDC ──

func oidcEnabled() (bool, string, string, string) {
	e := strings.TrimSpace(os.Getenv("OIDC_ENABLED"))
	enabled := e == "true" || e == "1" || strings.EqualFold(e, "yes")
	issuer := strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL"))
	clientID := strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID"))
	secret := strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
	if !enabled {
		return false, issuer, clientID, secret
	}
	if issuer == "" || clientID == "" || secret == "" {
		return false, issuer, clientID, secret
	}
	return true, issuer, clientID, secret
}

func oidcScopes() []string {
	s := strings.TrimSpace(os.Getenv("OIDC_SCOPES"))
	if s == "" {
		return []string{"openid", "email", "profile", "groups"}
	}
	return strings.Fields(s)
}

func oidcRedirectURL() string {
	if v := strings.TrimSpace(os.Getenv("OIDC_REDIRECT_URL")); v != "" {
		return v
	}
	return "http://localhost:6280/api/auth/oidc/callback"
}

type oidcCache struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
}

var (
	oidcOnce   sync.Once
	oidcCached *oidcCache
	oidcErr    error
	oidcMu     sync.Mutex
)

func getOIDCCached(ctx context.Context) (*oidcCache, error) {
	// sync.Once for lazy init; protect with mutex for re-init check.
	// If issuer/client changes a restart is expected, so Once is sufficient.
	oidcMu.Lock()
	defer oidcMu.Unlock()
	if oidcCached != nil {
		return oidcCached, oidcErr
	}
	// Use Once to satisfy spec requirement.
	oidcOnce.Do(func() {
		_, issuer, clientID, secret := oidcEnabled()
		// caller already validated enabled; issuer/clientID/secret non-empty
		redirect := oidcRedirectURL()
		scopes := oidcScopes()
		p, err := oidc.NewProvider(ctx, issuer)
		if err != nil {
			oidcErr = err
			return
		}
		oidcCached = &oidcCache{
			provider: p,
			verifier: p.Verifier(&oidc.Config{ClientID: clientID}),
			oauth: &oauth2.Config{
				ClientID:     clientID,
				ClientSecret: secret,
				Endpoint:     p.Endpoint(),
				RedirectURL:  redirect,
				Scopes:       scopes,
			},
		}
	})
	return oidcCached, oidcErr
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// OIDCStatus reports whether OIDC login is enabled.
// @Summary  OIDC status
// @Tags     auth
// @Produce  json
// @Success  200 {object} map[string]any
// @Router   /api/auth/oidc/status [get]
func OIDCStatus(w http.ResponseWriter, r *http.Request) {
	enabled, _, _, _ := oidcEnabled()
	if enabled {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "login_url": "/api/auth/oidc/login"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

// OIDCLogin redirects to the OIDC provider.
// @Summary  OIDC login
// @Tags     auth
// @Produce  json
// @Success  302 "redirect"
// @Failure  404 {object} map[string]string
// @Router   /api/auth/oidc/login [get]
func OIDCLogin(w http.ResponseWriter, r *http.Request) {
	enabled, _, _, _ := oidcEnabled()
	if !enabled {
		jsonError(w, "oidc disabled", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cached, err := getOIDCCached(ctx)
	if err != nil || cached == nil {
		jsonError(w, "oidc discovery failed", http.StatusInternalServerError)
		return
	}
	state := randHex(16)
	http.SetCookie(w, &http.Cookie{
		Name:     "meetup_oidc_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, cached.oauth.AuthCodeURL(state), http.StatusFound)
}

// OIDCCallback handles the OIDC callback.
// @Summary  OIDC callback
// @Tags     auth
// @Produce  json
// @Success  302 "redirect"
// @Failure  400 {object} map[string]string
// @Router   /api/auth/oidc/callback [get]
func OIDCCallback(w http.ResponseWriter, r *http.Request) {
	enabled, _, _, _ := oidcEnabled()
	if !enabled {
		jsonError(w, "oidc disabled", http.StatusNotFound)
		return
	}
	cookie, err := r.Cookie("meetup_oidc_state")
	if err != nil || cookie.Value == "" {
		jsonError(w, "invalid state", http.StatusBadRequest)
		return
	}
	qState := r.URL.Query().Get("state")
	if qState == "" || subtle.ConstantTimeCompare([]byte(qState), []byte(cookie.Value)) != 1 {
		jsonError(w, "invalid state", http.StatusBadRequest)
		return
	}
	// Clear state cookie
	http.SetCookie(w, &http.Cookie{Name: "meetup_oidc_state", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		jsonError(w, "missing code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cached, err := getOIDCCached(ctx)
	if err != nil || cached == nil {
		jsonError(w, "oidc discovery failed", http.StatusInternalServerError)
		return
	}
	token, err := cached.oauth.Exchange(ctx, code)
	if err != nil {
		jsonError(w, "oidc exchange failed", http.StatusBadGateway)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		jsonError(w, "missing id_token", http.StatusBadGateway)
		return
	}
	idToken, err := cached.verifier.Verify(ctx, raw)
	if err != nil {
		jsonError(w, "invalid id_token", http.StatusUnauthorized)
		return
	}
	var claims struct {
		Email             string `json:"email"`
		PreferredUsername string `json:"preferred_username"`
		Sub               string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		jsonError(w, "invalid claims", http.StatusBadGateway)
		return
	}
	email := strings.TrimSpace(claims.Email)
	if email == "" {
		email = strings.TrimSpace(claims.PreferredUsername)
	}
	if email == "" {
		email = strings.TrimSpace(claims.Sub)
	}
	if email == "" {
		jsonError(w, "missing email", http.StatusBadGateway)
		return
	}
	// Find-or-create user
	u, err := db.GetUserByUsername(email)
	if err != nil || u == nil {
		if _, err := db.CreateUser(email, "", "admin"); err != nil {
			// If race (already exists), try fetch again
			u2, err2 := db.GetUserByUsername(email)
			if err2 != nil || u2 == nil {
				jsonError(w, "failed to create user", http.StatusInternalServerError)
				return
			}
			u = u2
		} else {
			u, _ = db.GetUserByUsername(email)
		}
	}
	if u == nil {
		jsonError(w, "failed to provision user", http.StatusInternalServerError)
		return
	}
	sessToken := uuid.NewString()
	if err := db.CreateSession(sessToken, u.ID, time.Now().Add(sessionTTL)); err != nil {
		jsonError(w, "failed to create session", http.StatusInternalServerError)
		return
	}
	setSessionCookie(w, sessToken)
	http.Redirect(w, r, "/admin", http.StatusFound)
}

// OIDCLogout clears the session and redirects to admin.
// @Summary  OIDC logout
// @Tags     auth
// @Produce  json
// @Success  302 "redirect"
// @Router   /api/auth/oidc/logout [get]
func OIDCLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = db.DeleteSession(c.Value)
	}
	clearSessionCookie(w)
	// Also clear OIDC state cookie
	http.SetCookie(w, &http.Cookie{Name: "meetup_oidc_state", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/admin", http.StatusFound)
}
