package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"meetup/db"
)

// StatsTotalsDTO reports global counters.
type StatsTotalsDTO struct {
	Events       int `json:"events"`
	Participants int `json:"participants"`
	Answers      int `json:"answers"`
	Questions    int `json:"questions"`
	QA           int `json:"qa"`
	Votes        int `json:"votes"`
}

// EventStatsSummaryDTO is one event's row in the stats overview.
type EventStatsSummaryDTO struct {
	ID               int64   `json:"id"`
	Code             string  `json:"code"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	Participants     int     `json:"participants"`
	Answered         int     `json:"answered"`
	ResponseRate     float64 `json:"response_rate"`
	Answers          int     `json:"answers"`
	Questions        int     `json:"questions"`
	QA               int     `json:"qa"`
	Votes            int     `json:"votes"`
	FeedbackAnswered int     `json:"feedback_answered"`
	FeedbackRate     float64 `json:"feedback_rate"`
}

// GlobalStatsDTO is the whole-instance statistics payload.
type GlobalStatsDTO struct {
	Totals StatsTotalsDTO         `json:"totals"`
	Events []EventStatsSummaryDTO `json:"events"`
}

// EventStatsDTO is the detailed statistics payload for one event.
type EventStatsDTO struct {
	Event     EventStatsSummaryDTO `json:"event"`
	Questions []QuestionDTO        `json:"questions"`
	Feedback  []QuestionDTO        `json:"feedback"`
}

func eventStatsSummaryDTO(ev db.Event, s db.StatsSummary) EventStatsSummaryDTO {
	return EventStatsSummaryDTO{
		ID:               ev.ID,
		Code:             ev.Code,
		Name:             ev.Name,
		Status:           ev.Status,
		Participants:     s.Participants,
		Answered:         s.Answered,
		ResponseRate:     db.ResponseRate(s.Participants, s.Answered),
		Answers:          s.Answers,
		Questions:        s.Questions,
		QA:               s.QA,
		Votes:            s.Votes,
		FeedbackAnswered: s.FeedbackAnswered,
		FeedbackRate:     db.ResponseRate(s.Participants, s.FeedbackAnswered),
	}
}

func buildEventStats(ev *db.Event) (*EventStatsDTO, error) {
	summary, err := db.EventStats(ev.ID)
	if err != nil {
		return nil, err
	}
	questions, err := db.ListQuestions(ev.ID)
	if err != nil {
		return nil, err
	}
	feedback, err := db.ListFeedbackQuestions(ev.ID)
	if err != nil {
		return nil, err
	}
	dto := &EventStatsDTO{
		Event:     eventStatsSummaryDTO(*ev, *summary),
		Questions: make([]QuestionDTO, 0, len(questions)),
		Feedback:  make([]QuestionDTO, 0, len(feedback)),
	}
	for _, q := range questions {
		dto.Questions = append(dto.Questions, questionDTO(q, true))
	}
	for _, q := range feedback {
		dto.Feedback = append(dto.Feedback, questionDTO(q, true))
	}
	return dto, nil
}

// AdminGetStats returns global statistics plus per-event summaries.
// @Summary  Global statistics
// @Tags     admin
// @Produce  json
// @Security CookieAuth
// @Success  200 {object} GlobalStatsDTO
// @Failure  500 {object} map[string]string
// @Router   /api/admin/stats [get]
func AdminGetStats(w http.ResponseWriter, r *http.Request) {
	totals, events, err := db.GlobalStats()
	if err != nil {
		jsonError(w, "could not load stats", http.StatusInternalServerError)
		return
	}
	dto := GlobalStatsDTO{
		Totals: StatsTotalsDTO{
			Events:       totals.Events,
			Participants: totals.Participants,
			Answers:      totals.Answers,
			Questions:    totals.Questions,
			QA:           totals.QA,
			Votes:        totals.Votes,
		},
		Events: make([]EventStatsSummaryDTO, 0, len(events)),
	}
	for _, es := range events {
		dto.Events = append(dto.Events, eventStatsSummaryDTO(es.Event, es.Summary))
	}
	writeJSON(w, http.StatusOK, dto)
}

// AdminGetEventStats returns detailed statistics for one event.
// @Summary  Event statistics
// @Tags     admin
// @Produce  json
// @Security CookieAuth
// @Param    id path int true "Event ID"
// @Success  200 {object} EventStatsDTO
// @Failure  500 {object} map[string]string
// @Router   /api/admin/events/{id}/stats [get]
func AdminGetEventStats(w http.ResponseWriter, r *http.Request) {
	ev, err := db.GetEventByID(pathID(r, "id"))
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}
	dto, err := buildEventStats(ev)
	if err != nil {
		jsonError(w, "could not load stats", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// AdminStreamEventStats streams event statistics over SSE whenever the event changes.
// @Summary  Stream event statistics
// @Tags     admin
// @Produce  text/event-stream
// @Security CookieAuth
// @Param    id path int true "Event ID"
// @Success  200 {object} EventStatsDTO
// @Failure  500 {object} map[string]string
// @Router   /api/admin/events/{id}/stats/stream [get]
func AdminStreamEventStats(w http.ResponseWriter, r *http.Request) {
	ev, err := db.GetEventByID(pathID(r, "id"))
	if err != nil || ev == nil {
		jsonError(w, "event not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ch, unsub := Broker.Subscribe(ev.Code)
	defer unsub()

	sendStats := func() {
		dto, err := buildEventStats(ev)
		if err != nil {
			log.Printf("sse stats: %v", err)
			return
		}
		data, err := json.Marshal(dto)
		if err != nil {
			log.Printf("sse stats marshal: %v", err)
			return
		}
		_, _ = w.Write([]byte("event: stats\ndata: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	sendStats()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			if fresh, err := db.GetEventByID(ev.ID); err == nil && fresh != nil {
				ev = fresh
			}
			sendStats()
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
