package handlers

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"

	"meetup/db"
)

type Mailer interface {
	Send(to, subject, htmlBody string) error
}

type SMTPMailer struct{}

func EffectiveEmailSettings() (*db.EmailSettings, error) {
	s, err := db.GetEmailSettings()
	if err != nil {
		return nil, err
	}
	// Env overrides DB, mirroring otel pattern
	if v := strings.TrimSpace(os.Getenv("SMTP_HOST")); v != "" {
		s.Host = v
	}
	if v := strings.TrimSpace(os.Getenv("SMTP_PORT")); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			s.Port = p
		}
	}
	if v := strings.TrimSpace(os.Getenv("SMTP_USER")); v != "" {
		s.User = v
	}
	if v := strings.TrimSpace(os.Getenv("SMTP_PASS")); v != "" {
		s.Pass = v
	}
	if v := strings.TrimSpace(os.Getenv("SMTP_FROM")); v != "" {
		s.From = v
	}
	if v := strings.TrimSpace(os.Getenv("SMTP_TLS")); v != "" {
		s.TLS = v
	}
	return s, nil
}

var DefaultMailer Mailer = &SMTPMailer{}

func (m *SMTPMailer) Send(to, subject, htmlBody string) error {
	s, err := EffectiveEmailSettings()
	if err != nil {
		return err
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("email not configured")
	}
	from := s.From
	if from == "" {
		from = s.User
	}
	if from == "" {
		from = "noreply@example.com"
	}
	port := s.Port
	if port == 0 {
		if strings.EqualFold(s.TLS, "ssl") {
			port = 465
		} else {
			port = 587
		}
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(port))
	// Build message
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=UTF-8"
	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}

	// 465 implicit TLS
	if port == 465 || strings.EqualFold(s.TLS, "ssl") {
		tlsCfg := &tls.Config{ServerName: s.Host}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return err
		}
		defer conn.Close()
		c, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			return err
		}
		defer c.Quit()
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
		if err := c.Mail(from); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(msg.String()))
		if err != nil {
			w.Close()
			return err
		}
		w.Close()
		return c.Quit()
	}
	// Standard STARTTLS or plain
	if auth != nil {
		return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg.String()))
	}
	// No auth: try send without auth
	return smtp.SendMail(addr, nil, from, []string{to}, []byte(msg.String()))
}

func SendPasswordResetEmail(to, token, baseURL string) error {
	link := strings.TrimRight(baseURL, "/") + "/reset-password?token=" + token
	html := fmt.Sprintf(`<p>You requested a password reset. Click the link below to reset your password. This link expires in 1 hour.</p><p><a href="%s">%s</a></p><p>If you did not request this, ignore this email.</p>`, link, link)
	return DefaultMailer.Send(to, "Password reset", html)
}

func publicBaseURL(rHost string, rScheme string) string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	scheme := rScheme
	if scheme == "" {
		scheme = "http"
	}
	if rHost == "" {
		return ""
	}
	return scheme + "://" + rHost
}
