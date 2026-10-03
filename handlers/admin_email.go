package handlers

import (
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"meetup/db"
)

type EmailSettingsDTO struct {
	Host       string `json:"smtp_host"`
	Port       int    `json:"smtp_port"`
	User       string `json:"smtp_user"`
	HasPassword bool  `json:"has_password"`
	From       string `json:"smtp_from"`
	TLS        string `json:"smtp_tls"`
}

func emailDTO(s *db.EmailSettings) EmailSettingsDTO {
	return EmailSettingsDTO{
		Host: s.Host, Port: s.Port, User: s.User, HasPassword: s.Pass != "", From: s.From, TLS: s.TLS,
	}
}

func AdminGetEmailSettings(w http.ResponseWriter, r *http.Request) {
	s, err := db.GetEmailSettings()
	if err != nil {
		jsonError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	// apply env overrides for display? Show effective host etc but not pass
	eff, _ := EffectiveEmailSettings()
	// Use effective host/port etc but keep has_password from effective
	dto := emailDTO(eff)
	// Also indicate source? simple
	writeJSON(w, http.StatusOK, dto)
	_ = s
}

func AdminUpdateEmailSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"smtp_host"`
		Port *int   `json:"smtp_port"`
		User string `json:"smtp_user"`
		Pass *string `json:"smtp_pass"`
		From string `json:"smtp_from"`
		TLS  string `json:"smtp_tls"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	req.From = strings.TrimSpace(req.From)
	req.TLS = strings.TrimSpace(req.TLS)
	if req.TLS != "" && req.TLS != "starttls" && req.TLS != "ssl" {
		jsonError(w, "smtp_tls must be '', 'starttls' or 'ssl'", http.StatusBadRequest)
		return
	}
	if req.Host != "" {
		// basic validation
		if strings.Contains(req.Host, " ") {
			jsonError(w, "invalid host", http.StatusBadRequest)
			return
		}
	}
	s, err := db.GetEmailSettings()
	if err != nil {
		jsonError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	s.Host = req.Host
	if req.Port != nil {
		if *req.Port < 0 || *req.Port > 65535 {
			jsonError(w, "invalid port", http.StatusBadRequest)
			return
		}
		s.Port = *req.Port
	}
	s.User = req.User
	if req.Pass != nil {
		if *req.Pass != "" {
			s.Pass = *req.Pass
		}
		// blank keeps existing
	}
	s.From = req.From
	s.TLS = req.TLS
	if err := db.UpdateEmailSettings(*s); err != nil {
		jsonError(w, "failed to save", http.StatusInternalServerError)
		return
	}
	eff, _ := EffectiveEmailSettings()
	writeJSON(w, http.StatusOK, emailDTO(eff))
}

func AdminTestEmailSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	_ = decodeJSON(r, &req)
	to := strings.TrimSpace(req.To)
	// if no to, try current user email
	if to == "" {
		if u := currentUser(r); u != nil {
			if u.Email != "" && strings.Contains(u.Email, "@") {
				to = u.Email
			} else if strings.Contains(u.Username, "@") {
				to = u.Username
			}
		}
	}
	if to == "" {
		jsonError(w, "recipient required", http.StatusBadRequest)
		return
	}
	s, err := EffectiveEmailSettings()
	if err != nil {
		jsonError(w, "failed to get settings", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(s.Host) == "" {
		jsonError(w, "email not configured", http.StatusBadRequest)
		return
	}
	// quick smtp dial check: if user set, try auth; otherwise just send test
	err = DefaultMailer.Send(to, "Meetup test email", "<p>This is a test email from Meetup.</p>")
	if err != nil {
		// provide clearer message
		if strings.Contains(err.Error(), "email not configured") {
			jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		// smtp auth error etc
		_ = smtp.PlainAuth
		jsonError(w, "failed to send: "+err.Error(), http.StatusBadGateway)
		return
	}
	// also reference strconv to avoid unused
	_ = strconv.Itoa(s.Port)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
