package db

import "testing"

func TestEventStats(t *testing.T) {
	tmpDB(t)

	ev, err := CreateEvent("Stats Event", "STATS1", "", "")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}

	s, err := EventStats(ev.ID)
	if err != nil {
		t.Fatalf("event stats: %v", err)
	}
	if s.Participants != 0 || s.Answered != 0 || s.Answers != 0 || s.Questions != 0 || s.QA != 0 || s.Votes != 0 || s.FeedbackAnswered != 0 {
		t.Fatalf("expected zero stats, got %+v", *s)
	}

	p1, err := GetOrCreateParticipant("stats-token-1", ev.ID)
	if err != nil {
		t.Fatalf("participant 1: %v", err)
	}
	p2, err := GetOrCreateParticipant("stats-token-2", ev.ID)
	if err != nil {
		t.Fatalf("participant 2: %v", err)
	}

	q, err := CreateQuestion(ev.ID, "poll", "live", "Pick one", []string{"A", "B"}, false, true, 1, "", "")
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	if err := UpsertAnswer(q.ID, p1, "A"); err != nil {
		t.Fatalf("answer 1: %v", err)
	}
	if err := UpsertAnswer(q.ID, p2, "B"); err != nil {
		t.Fatalf("answer 2: %v", err)
	}

	fb, err := CreateQuestion(ev.ID, "open", "form", "How was it?", nil, true, false, 2, "", "")
	if err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	if err := UpsertAnswer(fb.ID, p1, "Great"); err != nil {
		t.Fatalf("feedback answer: %v", err)
	}

	qa, err := CreateQA(ev.ID, "A question?", "Someone")
	if err != nil {
		t.Fatalf("create qa: %v", err)
	}
	if _, _, err := ToggleVote(qa.ID, p1); err != nil {
		t.Fatalf("vote: %v", err)
	}

	s, err = EventStats(ev.ID)
	if err != nil {
		t.Fatalf("event stats: %v", err)
	}
	if s.Participants != 2 || s.Answered != 2 || s.Answers != 3 || s.Questions != 2 || s.QA != 1 || s.Votes != 1 || s.FeedbackAnswered != 1 {
		t.Fatalf("unexpected stats: %+v", *s)
	}

	// second event must not leak into the first event's counters
	if _, err := CreateEvent("Other Event", "STATS2", "", ""); err != nil {
		t.Fatalf("create second event: %v", err)
	}
	s2, err := EventStats(ev.ID)
	if err != nil {
		t.Fatalf("event stats again: %v", err)
	}
	if *s2 != *s {
		t.Fatalf("first event stats changed after creating another event: %+v vs %+v", *s2, *s)
	}
}

func TestGlobalStats(t *testing.T) {
	tmpDB(t)

	ev, err := CreateEvent("Global Event", "GLOBAL1", "", "")
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	if _, err := CreateEvent("Empty Event", "GLOBAL2", "", ""); err != nil {
		t.Fatalf("create second event: %v", err)
	}

	p, err := GetOrCreateParticipant("global-token", ev.ID)
	if err != nil {
		t.Fatalf("participant: %v", err)
	}
	q, err := CreateQuestion(ev.ID, "rating", "live", "Rate", nil, false, true, 1, "", "")
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	if err := UpsertAnswer(q.ID, p, "5"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := CreateQA(ev.ID, "Question?", "Someone"); err != nil {
		t.Fatalf("create qa: %v", err)
	}

	totals, summaries, err := GlobalStats()
	if err != nil {
		t.Fatalf("global stats: %v", err)
	}
	if totals.Events != 2 || totals.Participants != 1 || totals.Answers != 1 || totals.Questions != 1 || totals.QA != 1 || totals.Votes != 0 {
		t.Fatalf("unexpected totals: %+v", *totals)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 event summaries, got %d", len(summaries))
	}
	var found bool
	for _, es := range summaries {
		if es.Event.ID != ev.ID {
			continue
		}
		found = true
		if es.Summary.Participants != 1 || es.Summary.Answered != 1 || es.Summary.Answers != 1 || es.Summary.QA != 1 {
			t.Fatalf("unexpected event summary: %+v", es.Summary)
		}
	}
	if !found {
		t.Fatal("event summary for the active event not found")
	}
}

func TestResponseRate(t *testing.T) {
	cases := []struct {
		participants, answered int
		want                   float64
	}{
		{0, 0, 0},
		{0, 3, 0},
		{4, 0, 0},
		{2, 1, 50},
		{4, 3, 75},
		{2, 2, 100},
	}
	for _, c := range cases {
		if got := ResponseRate(c.participants, c.answered); got != c.want {
			t.Errorf("ResponseRate(%d, %d) = %v, want %v", c.participants, c.answered, got, c.want)
		}
	}
}
