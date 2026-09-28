// Package handlers implements the HTTP API for the meetup app.
//
// Audience endpoints identify a device with an anonymous participant cookie so
// answers can be de-duplicated and votes counted without a login. Admin
// endpoints require a session cookie issued by the auth handlers.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"meetup/db"
	"meetup/live"
)

// Broker fans out "something changed" signals to every SSE connection of an
// event. Subscribers rebuild their own (participant-specific) state on wake-up.
var Broker = live.New()

// MediaDir is where uploaded presentations are stored. main sets it.
var MediaDir = "media"

const (
	sessionCookie     = "meetup_session"
	participantCookie = "meetup_participant"
	sessionTTL        = 30 * 24 * time.Hour
	participantTTL    = 365 * 24 * time.Hour
	maxBodyBytes      = 2 << 20
)

type ctxKey string

const userKey ctxKey = "user"

// ── JSON DTOs: the contract the embedded frontend is built against ──

type EventDTO struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	EventDate      string `json:"event_date"`
	Status         string `json:"status"`
	FeedbackOpen   bool   `json:"feedback_open"`
	Brand          string `json:"brand"`
	CreatedAt      string `json:"created_at,omitempty"`
	QuestionCount  int    `json:"question_count"`
	PendingQACount int    `json:"pending_qa_count"`
}

type QuestionDTO struct {
	ID          int64       `json:"id"`
	EventID     int64       `json:"event_id"`
	Kind        string      `json:"kind"`
	Mode        string      `json:"mode"`
	Prompt      string      `json:"prompt"`
	Options     []string    `json:"options"`
	Position    int         `json:"position"`
	Status      string      `json:"status"`
	ShowResults bool        `json:"show_results"`
	IsFeedback  bool        `json:"is_feedback"`
	Results     []db.Result `json:"results"`
	Total       int         `json:"total"`
	Respondents int         `json:"respondents"`
	NPS         *int        `json:"nps,omitempty"`
	MyAnswer    string      `json:"my_answer"`
	Answered    bool        `json:"answered"`
	CreatedAt   string      `json:"created_at,omitempty"`
}

type QADTO struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	Status    string `json:"status"`
	Votes     int    `json:"votes"`
	Voted     bool   `json:"voted"`
	CreatedAt string `json:"created_at,omitempty"`
}

type PresentationDTO struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Speaker   string `json:"speaker"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at,omitempty"`
}

type FeedbackDTO struct {
	Open      bool          `json:"open"`
	Questions []QuestionDTO `json:"questions"`
}

type StateDTO struct {
	Event          EventDTO     `json:"event"`
	ActiveQuestion *QuestionDTO `json:"active_question"`
	QA             []QADTO      `json:"qa"`
	Feedback       FeedbackDTO  `json:"feedback"`
}

type AnalyticsDTO struct {
	UmamiScriptURL  string `json:"umami_script_url"`
	UmamiWebsiteID  string `json:"umami_website_id"`
	TrackingEnabled bool   `json:"tracking_enabled"`
}

// ── Small HTTP helpers ──

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("writeJSON: %v", err)
		}
	}
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// eventByCode resolves the {code} path value to an event.
func eventByCode(r *http.Request) (*db.Event, error) {
	return db.GetEventByCode(r.PathValue("code"))
}

// ── Auth / identity ──

func withUser(ctx context.Context, u *db.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// currentUser returns the authenticated admin injected by AdminAuth, or nil.
func currentUser(r *http.Request) *db.User {
	u, _ := r.Context().Value(userKey).(*db.User)
	return u
}

func sessionUser(r *http.Request) *db.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	s, err := db.GetSession(c.Value)
	if err != nil || s == nil {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		_ = db.DeleteSession(s.Token)
		return nil
	}
	u, err := db.GetUserByID(s.UserID)
	if err != nil {
		return nil
	}
	return u
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// AuthMiddleware resolves a session when present and always continues.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := sessionUser(r); u != nil {
			r = r.WithContext(withUser(r.Context(), u))
		}
		next.ServeHTTP(w, r)
	})
}

// AdminAuth rejects requests without a valid session.
func AdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := sessionUser(r)
		if u == nil {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// participantID resolves the anonymous device cookie for an event, creating a
// participant row and setting the cookie when the device has none yet. Returns
// 0 when identity cannot be established (db failure); callers should still
// render aggregate state in that case.
func participantID(w http.ResponseWriter, r *http.Request, eventID int64) int64 {
	if c, err := r.Cookie(participantCookie); err == nil && c.Value != "" {
		if id, err := db.GetOrCreateParticipant(c.Value, eventID); err == nil {
			return id
		}
	}
	token := uuid.NewString()
	http.SetCookie(w, &http.Cookie{
		Name:     participantCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(participantTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	id, err := db.GetOrCreateParticipant(token, eventID)
	if err != nil {
		log.Printf("participant: %v", err)
		return 0
	}
	return id
}

// BroadcastEvent wakes every SSE subscriber of the event so they rebuild state.
func BroadcastEvent(eventID int64) {
	ev, err := db.GetEventByID(eventID)
	if err != nil || ev == nil {
		return
	}
	Broker.Broadcast(ev.Code, []byte("{}"))
}

// ── DTO conversion ──

func eventDTO(ev *db.Event, brand string) EventDTO {
	return EventDTO{
		ID:             ev.ID,
		Code:           ev.Code,
		Name:           ev.Name,
		Description:    ev.Description,
		EventDate:      ev.EventDate,
		Status:         ev.Status,
		FeedbackOpen:   ev.FeedbackOpen,
		Brand:          brand,
		CreatedAt:      ev.CreatedAt.Format(time.RFC3339),
		QuestionCount:  ev.QuestionCount,
		PendingQACount: ev.PendingQACount,
	}
}

func questionDTO(q db.Question, withResults bool) QuestionDTO {
	dto := QuestionDTO{
		ID:          q.ID,
		EventID:     q.EventID,
		Kind:        q.Kind,
		Mode:        q.Mode,
		Prompt:      q.Prompt,
		Options:     q.Options,
		Position:    q.Position,
		Status:      q.Status,
		ShowResults: q.ShowResults,
		IsFeedback:  q.IsFeedback,
		CreatedAt:   q.CreatedAt.Format(time.RFC3339),
	}
	if dto.Options == nil {
		dto.Options = []string{}
	}
	if withResults {
		st, err := db.GetQuestionStats(q.ID)
		if err != nil {
			log.Printf("question results %d: %v", q.ID, err)
			st = nil
		}
		if st == nil {
			st = &db.QuestionStats{Results: []db.Result{}}
		}
		if st.Results == nil {
			st.Results = []db.Result{}
		}
		dto.Results = st.Results
		dto.Total = st.Total
		dto.Respondents = st.Respondents
		dto.NPS = st.NPS
	} else {
		dto.Results = []db.Result{}
	}
	return dto
}

func qaDTO(q db.QAQuestion) QADTO {
	return QADTO{
		ID:        q.ID,
		Body:      q.Body,
		Author:    q.Author,
		Status:    q.Status,
		Votes:     q.Votes,
		Voted:     q.Voted,
		CreatedAt: q.CreatedAt.Format(time.RFC3339),
	}
}

func presentationDTO(p db.Presentation, eventCode string) PresentationDTO {
	return PresentationDTO{
		ID:        p.ID,
		Title:     p.Title,
		Speaker:   p.Speaker,
		Filename:  p.Filename,
		Size:      p.Size,
		URL:       "/api/events/" + eventCode + "/presentations/" + strconv.FormatInt(p.ID, 10) + "/download",
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}

func analyticsDTO(a *db.AnalyticsSettings) AnalyticsDTO {
	if a == nil {
		return AnalyticsDTO{}
	}
	return AnalyticsDTO{
		UmamiScriptURL:  a.UmamiScriptURL,
		UmamiWebsiteID:  a.UmamiWebsiteID,
		TrackingEnabled: a.TrackingEnabled,
	}
}

// ── State assembly ──

// BuildState renders the participant-specific view of an event. It is used by
// GET /state and by every SSE connection, so one audience member's answers and
// votes are never leaked to another.
func BuildState(ev *db.Event, participantID int64) StateDTO {
	brand, _ := db.GetBranding()
	state := StateDTO{
		Event:    eventDTO(ev, brand),
		QA:       []QADTO{},
		Feedback: FeedbackDTO{Open: ev.FeedbackOpen, Questions: []QuestionDTO{}},
	}

	if q, err := db.GetActiveQuestion(ev.ID); err == nil && q != nil {
		dto := questionDTO(*q, true)
		if participantID > 0 {
			if a, err := db.GetAnswer(q.ID, participantID); err == nil && a != nil {
				dto.Answered = true
				dto.MyAnswer = a.Value
			}
		}
		state.ActiveQuestion = &dto
	}

	qaRows, err := db.ListQAForParticipant(ev.ID, participantID)
	if err != nil {
		qaRows, err = db.ListQA(ev.ID, "approved")
	}
	if err == nil {
		for _, q := range qaRows {
			state.QA = append(state.QA, qaDTO(q))
		}
	}

	if fq, err := db.ListFeedbackQuestions(ev.ID); err == nil {
		for _, q := range fq {
			state.Feedback.Questions = append(state.Feedback.Questions, questionDTO(q, false))
		}
	}

	return state
}

var errNotFound = errors.New("not found")
