package db

// StatsSummary holds activity counters for a single event.
type StatsSummary struct {
	Participants     int
	Answered         int
	Answers          int
	Questions        int
	QA               int
	Votes            int
	FeedbackAnswered int
}

// TotalStats holds global counters across all events.
type TotalStats struct {
	Events       int
	Participants int
	Answers      int
	Questions    int
	QA           int
	Votes        int
}

// EventStatsSummary pairs an event with its counters.
type EventStatsSummary struct {
	Event   Event
	Summary StatsSummary
}

func countQuery(query string, args ...any) (int, error) {
	var n int
	err := DB.QueryRow(query, args...).Scan(&n)
	return n, err
}

// EventStats gathers activity counters for one event.
// Answered counts distinct participants with at least one answer; FeedbackAnswered
// counts distinct participants with at least one answer to a feedback question.
func EventStats(eventID int64) (*StatsSummary, error) {
	s := &StatsSummary{}
	var err error
	if s.Participants, err = countQuery("SELECT COUNT(*) FROM participants WHERE event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.Answered, err = countQuery("SELECT COUNT(DISTINCT a.participant_id) FROM answers a JOIN questions q ON q.id=a.question_id WHERE q.event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.Answers, err = countQuery("SELECT COUNT(*) FROM answers a JOIN questions q ON q.id=a.question_id WHERE q.event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.Questions, err = countQuery("SELECT COUNT(*) FROM questions WHERE event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.QA, err = countQuery("SELECT COUNT(*) FROM qa_questions WHERE event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.Votes, err = countQuery("SELECT COUNT(*) FROM qa_votes v JOIN qa_questions q ON q.id=v.qa_id WHERE q.event_id=?", eventID); err != nil {
		return nil, err
	}
	if s.FeedbackAnswered, err = countQuery("SELECT COUNT(DISTINCT a.participant_id) FROM answers a JOIN questions q ON q.id=a.question_id WHERE q.event_id=? AND q.is_feedback=1", eventID); err != nil {
		return nil, err
	}
	return s, nil
}

// GlobalStats gathers global counters plus a per-event summary.
func GlobalStats() (*TotalStats, []EventStatsSummary, error) {
	t := &TotalStats{}
	totals := []struct {
		dst   *int
		query string
	}{
		{&t.Events, "SELECT COUNT(*) FROM events"},
		{&t.Participants, "SELECT COUNT(*) FROM participants"},
		{&t.Answers, "SELECT COUNT(*) FROM answers"},
		{&t.Questions, "SELECT COUNT(*) FROM questions"},
		{&t.QA, "SELECT COUNT(*) FROM qa_questions"},
		{&t.Votes, "SELECT COUNT(*) FROM qa_votes"},
	}
	for _, c := range totals {
		if err := DB.QueryRow(c.query).Scan(c.dst); err != nil {
			return nil, nil, err
		}
	}
	events, err := ListEvents()
	if err != nil {
		return nil, nil, err
	}
	summaries := make([]EventStatsSummary, 0, len(events))
	for _, ev := range events {
		s, err := EventStats(ev.ID)
		if err != nil {
			return nil, nil, err
		}
		summaries = append(summaries, EventStatsSummary{Event: ev, Summary: *s})
	}
	return t, summaries, nil
}

// ResponseRate returns answered/participants as a percentage, or 0 when there
// are no participants yet.
func ResponseRate(participants, answered int) float64 {
	if participants <= 0 {
		return 0
	}
	return float64(answered) / float64(participants) * 100
}
