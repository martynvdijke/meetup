package handlers

import (
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"meetup/db"
	"meetup/qr"
)

// AnswerRequest is the body for SubmitAnswer.
type AnswerRequest struct {
	QuestionID int64  `json:"question_id"`
	Value      string `json:"value"`
}

// QARequest is the body for CreateQA.
type QARequest struct {
	Body   string `json:"body"`
	Author string `json:"author"`
}

// GetEvent returns the public event metadata.
// @Summary  Get event
// @Tags     public
// @Produce  json
// @Param    code path string true "Event code"
// @Success  200 {object} EventDTO
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code} [get]
func GetEvent(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	brand, _ := db.GetBranding()
	writeJSON(w, http.StatusOK, eventDTO(ev, brand))
}

// GetEventState returns the personalized event state.
// @Summary  Get event state
// @Tags     public
// @Produce  json
// @Param    code path string true "Event code"
// @Success  200 {object} StateDTO
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/state [get]
func GetEventState(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	pid := participantID(w, r, ev.ID)
	writeJSON(w, http.StatusOK, BuildState(ev, pid))
}

// EventStream streams personalized state via SSE.
// @Summary  Stream event state
// @Tags     public
// @Produce  text/event-stream
// @Description Server-sent events stream of personalized state. Each message is `event: state` with JSON StateDTO.
// @Param    code path string true "Event code"
// @Success  200 {string} string "text/event-stream"
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/stream [get]
func EventStream(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	// Read-only participant lookup: Set-Cookie cannot be delivered after SSE
	// headers flush, so creating a participant here would bind the stream to an
	// unreachable identity. The client fetches /state first, which creates it.
	var pid int64
	if c, err := r.Cookie(participantCookie); err == nil && c.Value != "" {
		if p, err := db.GetParticipantByToken(c.Value); err == nil && p.EventID == ev.ID {
			pid = p.ID
		}
	}
	code := ev.Code

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch, unsub := Broker.Subscribe(code)
	defer unsub()

	sendState := func() {
		state := BuildState(ev, pid)
		data, err := json.Marshal(state)
		if err != nil {
			log.Printf("sse marshal: %v", err)
			return
		}
		_, _ = w.Write([]byte("event: state\ndata: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	sendState()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			// Re-fetch event to ensure fresh data (e.g. status changed); keep same ID.
			if fresh, err := db.GetEventByID(ev.ID); err == nil && fresh != nil {
				ev = fresh
			}
			sendState()
		case <-ticker.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

// SubmitAnswer records an answer for a live or feedback question.
// @Summary  Submit answer
// @Tags     public
// @Accept   json
// @Produce  json
// @Param    code path string true "Event code"
// @Param    body body AnswerRequest true "answer"
// @Success  200 {object} map[string]bool
// @Failure  400 {object} map[string]string
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/answers [post]
func SubmitAnswer(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	var req AnswerRequest
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	val := strings.TrimSpace(req.Value)
	if val == "" {
		jsonError(w, "value is required", http.StatusBadRequest)
		return
	}
	if req.QuestionID == 0 {
		jsonError(w, "question_id is required", http.StatusBadRequest)
		return
	}
	q, err := db.GetQuestion(req.QuestionID)
	if err != nil || q == nil {
		jsonError(w, "question not found", http.StatusNotFound)
		return
	}
	if q.EventID != ev.ID {
		jsonError(w, "question not found", http.StatusNotFound)
		return
	}
	if q.Status != "live" && !q.IsFeedback {
		jsonError(w, "question is not live", http.StatusBadRequest)
		return
	}
	pid := participantID(w, r, ev.ID)
	if pid == 0 {
		jsonError(w, "could not identify participant", http.StatusBadRequest)
		return
	}
	if err := db.UpsertAnswer(q.ID, pid, val); err != nil {
		jsonError(w, "could not save answer", http.StatusInternalServerError)
		return
	}
	BroadcastEvent(ev.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ListPublicQA lists approved Q&A for the event.
// @Summary  List public Q&A
// @Tags     public
// @Produce  json
// @Param    code path string true "Event code"
// @Success  200 {array} QADTO
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/qa [get]
func ListPublicQA(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	pid := participantID(w, r, ev.ID)
	var rows []db.QAQuestion
	if pid != 0 {
		r2, err := db.ListQAForParticipant(ev.ID, pid)
		if err == nil {
			rows = r2
		} else {
			r2, _ := db.ListQA(ev.ID, "approved")
			rows = r2
		}
	} else {
		r2, _ := db.ListQA(ev.ID, "approved")
		rows = r2
	}
	if rows == nil {
		rows = []db.QAQuestion{}
	}
	out := make([]QADTO, 0, len(rows))
	for _, q := range rows {
		out = append(out, qaDTO(q))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateQA submits a new Q&A question for moderation.
// @Summary  Create Q&A question
// @Tags     public
// @Accept   json
// @Produce  json
// @Param    code path string true "Event code"
// @Param    body body QARequest true "question"
// @Success  200 {object} map[string]any
// @Failure  400 {object} map[string]string
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/qa [post]
func CreateQA(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	var req QARequest
	if err := decodeJSON(r, &req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		jsonError(w, "body is required", http.StatusBadRequest)
		return
	}
	if len([]rune(body)) > 500 {
		body = string([]rune(body)[:500])
	}
	author := strings.TrimSpace(req.Author)
	if len([]rune(author)) > 80 {
		author = string([]rune(author)[:80])
	}
	qa, err := db.CreateQA(ev.ID, body, author)
	if err != nil {
		jsonError(w, "could not create question", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": qa.ID, "status": "pending"})
}

// VoteQA toggles a vote for a Q&A question.
// @Summary  Vote Q&A
// @Tags     public
// @Produce  json
// @Param    code path string true "Event code"
// @Param    id path int true "Q&A ID"
// @Success  200 {object} map[string]any
// @Failure  400 {object} map[string]string
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/qa/{id}/vote [post]
func VoteQA(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	qaID := pathID(r, "id")
	if qaID == 0 {
		jsonError(w, "invalid qa id", http.StatusBadRequest)
		return
	}
	qa, err := db.GetQA(qaID)
	if err != nil || qa == nil {
		jsonError(w, "question not found", http.StatusNotFound)
		return
	}
	if qa.EventID != ev.ID {
		jsonError(w, "question not found", http.StatusNotFound)
		return
	}
	pid := participantID(w, r, ev.ID)
	if pid == 0 {
		jsonError(w, "could not identify participant", http.StatusBadRequest)
		return
	}
	votes, voted, err := db.ToggleVote(qaID, pid)
	if err != nil {
		jsonError(w, "could not vote", http.StatusInternalServerError)
		return
	}
	BroadcastEvent(ev.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "votes": votes, "voted": voted})
}

// ListPublicPresentations lists presentations for the event.
// @Summary  List presentations
// @Tags     public
// @Produce  json
// @Param    code path string true "Event code"
// @Success  200 {array} PresentationDTO
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/presentations [get]
func ListPublicPresentations(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	rows, err := db.ListPresentations(ev.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, []PresentationDTO{})
		return
	}
	if rows == nil {
		rows = []db.Presentation{}
	}
	out := make([]PresentationDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, presentationDTO(p, ev.Code))
	}
	writeJSON(w, http.StatusOK, out)
}

// DownloadPresentation serves a presentation file as attachment.
// @Summary  Download presentation
// @Tags     public
// @Produce  octet-stream
// @Param    code path string true "Event code"
// @Param    id path int true "Presentation ID"
// @Success  200 {file} file
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/presentations/{id}/download [get]
func DownloadPresentation(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	pid := pathID(r, "id")
	if pid == 0 {
		jsonError(w, "presentation not found", http.StatusNotFound)
		return
	}
	p, err := db.GetPresentation(pid)
	if err != nil || p == nil {
		jsonError(w, "presentation not found", http.StatusNotFound)
		return
	}
	if p.EventID != ev.ID {
		jsonError(w, "presentation not found", http.StatusNotFound)
		return
	}
	fpath := filepath.Join(MediaDir, p.Filename)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": p.Filename}))
	http.ServeFile(w, r, fpath)
}

// EventQR returns a QR code PNG for the event join URL.
// @Summary  Get event QR code
// @Tags     public
// @Produce  png
// @Param    code path string true "Event code"
// @Success  200 {file} file
// @Failure  404 {object} map[string]string
// @Router   /api/events/{code}/qr.png [get]
func EventQR(w http.ResponseWriter, r *http.Request) {
	ev, err := eventByCode(r)
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	base := os.Getenv("PUBLIC_BASE_URL")
	if base == "" {
		base = "http://localhost:6280"
	}
	base = strings.TrimRight(base, "/")
	payload := base + "/e/" + ev.Code
	png, err := qr.PNG(payload, 512)
	if err != nil {
		jsonError(w, "could not generate qr", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// PublicGetAnalyticsSettings returns public analytics settings.
// @Summary  Get analytics settings
// @Tags     public
// @Produce  json
// @Success  200 {object} AnalyticsDTO
// @Router   /api/settings/analytics [get]
func PublicGetAnalyticsSettings(w http.ResponseWriter, r *http.Request) {
	a, err := db.GetAnalyticsSettings()
	if err != nil {
		writeJSON(w, http.StatusOK, analyticsDTO(nil))
		return
	}
	writeJSON(w, http.StatusOK, analyticsDTO(a))
}
