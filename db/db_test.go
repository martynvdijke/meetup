package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tmpDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.db")
	if err := Init(p); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Close() })
	return p
}

func TestMigrateIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.db")
	if err := Init(p); err != nil {
		t.Fatalf("Init1: %v", err)
	}
	if err := migrate(); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	Close()
	if err := Init(p); err != nil {
		t.Fatalf("Init2: %v", err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	tmpDB(t)
	n, _ := CountUsers()
	if n != 0 {
		t.Fatalf("want 0 got %d", n)
	}
	id, err := CreateUser("alice", "hash1", "admin")
	if err != nil {
		t.Fatal(err)
	}
	u, err := GetUserByUsername("alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != id || u.PasswordHash != "hash1" {
		t.Fatalf("mismatch %+v", u)
	}
	u2, err := GetUserByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if u2.Username != "alice" {
		t.Fatalf("username %q", u2.Username)
	}
	n, _ = CountUsers()
	if n != 1 {
		t.Fatalf("count %d", n)
	}
	exp := time.Now().Add(time.Hour)
	if err := CreateSession("tok1", id, exp); err != nil {
		t.Fatal(err)
	}
	s, err := GetSession("tok1")
	if err != nil {
		t.Fatal(err)
	}
	if s.UserID != id {
		t.Fatalf("user mismatch")
	}
	if err := DeleteSession("tok1"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetSession("tok1"); err == nil {
		t.Fatal("expected error after delete")
	}
	// expired sessions
	CreateSession("tok2", id, time.Now().Add(-time.Hour))
	CreateSession("tok3", id, time.Now().Add(time.Hour))
	DeleteExpiredSessions()
	if _, err := GetSession("tok2"); err == nil {
		t.Fatal("expired should be deleted")
	}
	if _, err := GetSession("tok3"); err != nil {
		t.Fatal("tok3 should remain")
	}
}

func TestEvents(t *testing.T) {
	tmpDB(t)
	ev, err := CreateEvent("E1", "CODE1", "desc", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Code != "CODE1" {
		t.Fatalf("code %q", ev.Code)
	}
	ok, _ := EventCodeExists("CODE1")
	if !ok {
		t.Fatal("should exist")
	}
	ok, _ = EventCodeExists("")
	if ok {
		t.Fatal("empty should be false")
	}
	ok, _ = EventCodeExists("NOPE")
	if ok {
		t.Fatal("should not exist")
	}
	ev2, _ := GetEventByCode("CODE1")
	if ev2.ID != ev.ID {
		t.Fatal("get by code mismatch")
	}
	if ev2.QuestionCount != 0 || ev2.PendingQACount != 0 {
		t.Fatalf("counts %+v", ev2)
	}
	// create question to bump count
	CreateQuestion(ev.ID, "poll", "live", "Q1", []string{"A", "B"}, false, true, 0, "", "")
	ev3, _ := GetEventByID(ev.ID)
	if ev3.QuestionCount != 1 {
		t.Fatalf("question count %d", ev3.QuestionCount)
	}
	// qa pending count
	CreateQA(ev.ID, "q body", "anon")
	ev4, _ := GetEventByID(ev.ID)
	if ev4.PendingQACount != 1 {
		t.Fatalf("pending %d", ev4.PendingQACount)
	}
	// update
	updated, err := UpdateEvent(ev.ID, map[string]any{"name": "E1-updated", "feedback_open": true, "unknown": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "E1-updated" || !updated.FeedbackOpen {
		t.Fatalf("update failed %+v", updated)
	}
	// update with empty should not error
	_, err = UpdateEvent(ev.ID, map[string]any{"bogus": 1})
	if err != nil {
		t.Fatal(err)
	}
	evs, _ := ListEvents()
	if len(evs) != 1 {
		t.Fatalf("list %d", len(evs))
	}
	if err := DeleteEvent(ev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetEventByID(ev.ID); err == nil {
		t.Fatal("should be deleted")
	}
}

func TestQuestionsActivateAndResults(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	q1, err := CreateQuestion(ev.ID, "poll", "live", "Pick?", []string{"A", "B", "C"}, false, true, 1, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(q1.Options) != 3 || q1.Options[0] != "A" {
		t.Fatalf("options %+v", q1.Options)
	}
	q2, _ := CreateQuestion(ev.ID, "poll", "live", "Pick2?", []string{"X", "Y"}, false, true, 2, "", "")
	// activate q1
	if err := ActivateQuestion(ev.ID, q1.ID); err != nil {
		t.Fatal(err)
	}
	active, _ := GetActiveQuestion(ev.ID)
	if active == nil || active.ID != q1.ID {
		t.Fatalf("active should be q1 got %+v", active)
	}
	// activate q2 closes q1
	if err := ActivateQuestion(ev.ID, q2.ID); err != nil {
		t.Fatal(err)
	}
	active, _ = GetActiveQuestion(ev.ID)
	if active.ID != q2.ID {
		t.Fatalf("active should be q2")
	}
	q1r, _ := GetQuestion(q1.ID)
	if q1r.Status != "closed" {
		t.Fatalf("q1 should be closed %q", q1r.Status)
	}
	// wrong event should error
	ev2, _ := CreateEvent("E2", "C2", "", "")
	if err := ActivateQuestion(ev2.ID, q1.ID); err == nil {
		t.Fatal("should error mismatched event")
	}
	// GetQuestionStats zero-count in option order
	p1, _ := GetOrCreateParticipant("part1", ev.ID)
	p2, _ := GetOrCreateParticipant("part2", ev.ID)
	UpsertAnswer(q2.ID, p1, "X")
	// q2 options X,Y ; only X voted -> Y should be 0
	q2r, _ := GetQuestion(q2.ID)
	if len(q2r.Results) != 2 {
		t.Fatalf("results len %d %+v", len(q2r.Results), q2r.Results)
	}
	if q2r.Results[0].Label != "X" || q2r.Results[0].Count != 1 {
		t.Fatalf("result0 %+v", q2r.Results[0])
	}
	if q2r.Results[1].Label != "Y" || q2r.Results[1].Count != 0 {
		t.Fatalf("result1 %+v", q2r.Results[1])
	}
	if q2r.Total != 1 {
		t.Fatalf("total %d", q2r.Total)
	}
	// add another vote
	UpsertAnswer(q2.ID, p2, "X")
	q2r2, _ := GetQuestion(q2.ID)
	if q2r2.Total != 2 {
		t.Fatalf("total2 %d", q2r2.Total)
	}
	// open kind raw values
	q3, _ := CreateQuestion(ev.ID, "open", "live", "Say?", nil, false, true, 3, "", "")
	UpsertAnswer(q3.ID, p1, "hello")
	UpsertAnswer(q3.ID, p2, "world")
	UpsertAnswer(q3.ID, p1, "hello-updated")
	// actually p1 upsert keeps count 2; test below separately
	st, _ := GetQuestionStats(q3.ID)
	if st.Total != 2 {
		t.Fatalf("open total %d", st.Total)
	}
	if len(st.Results) != 2 {
		t.Fatalf("open results %v", st.Results)
	}
	// no active when none live
	CloseQuestion(ev.ID, q2.ID)
	if a, _ := GetActiveQuestion(ev.ID); a != nil {
		t.Fatalf("should be nil active %+v", a)
	}
	// delete
	if err := DeleteQuestion(q1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetQuestion(q1.ID); err == nil {
		t.Fatal("deleted should error")
	}
	// UpdateQuestion whitelist
	_, err = UpdateQuestion(q2.ID, map[string]any{"prompt": "new prompt", "bogus": "x"})
	if err != nil {
		t.Fatal(err)
	}
	q2u, _ := GetQuestion(q2.ID)
	if q2u.Prompt != "new prompt" {
		t.Fatalf("prompt %q", q2u.Prompt)
	}
	// update options
	_, err = UpdateQuestion(q2.ID, map[string]any{"options": []string{"P", "Q"}})
	if err != nil {
		t.Fatal(err)
	}
	q2u2, _ := GetQuestion(q2.ID)
	if len(q2u2.Options) != 2 || q2u2.Options[0] != "P" {
		t.Fatalf("opts %+v", q2u2.Options)
	}
}

func TestAnswersUpsert(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	q, _ := CreateQuestion(ev.ID, "poll", "live", "Q?", []string{"A", "B"}, false, true, 0, "", "")
	pid, _ := GetOrCreateParticipant("tokA", ev.ID)
	UpsertAnswer(q.ID, pid, "A")
	UpsertAnswer(q.ID, pid, "B")
	c, _ := CountAnswers(q.ID)
	if c != 1 {
		t.Fatalf("count %d", c)
	}
	a, _ := GetAnswer(q.ID, pid)
	if a.Value != "B" {
		t.Fatalf("value %q", a.Value)
	}
}

func TestParticipants(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	id1, _ := GetOrCreateParticipant("tok1", ev.ID)
	id2, _ := GetOrCreateParticipant("tok1", ev.ID)
	if id1 != id2 {
		t.Fatal("should return same id")
	}
	p, _ := GetParticipantByToken("tok1")
	if p.Token != "tok1" || p.EventID != ev.ID {
		t.Fatalf("participant %+v", p)
	}
}

func TestQA(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	qa1, _ := CreateQA(ev.ID, "Q1 body", "a1")
	qa2, _ := CreateQA(ev.ID, "Q2 body", "a2")
	// votes
	p1, _ := GetOrCreateParticipant("p1", ev.ID)
	p2, _ := GetOrCreateParticipant("p2", ev.ID)
	cnt, voted, _ := ToggleVote(qa1.ID, p1)
	if cnt != 1 || !voted {
		t.Fatalf("vote1 %d %v", cnt, voted)
	}
	ToggleVote(qa1.ID, p2)
	// qa1 has 2, qa2 has 0 -> ordering votes desc
	list, _ := ListQA(ev.ID)
	if list[0].ID != qa1.ID {
		t.Fatalf("ordering %+v", list)
	}
	// toggle off
	cnt, voted, _ = ToggleVote(qa1.ID, p1)
	if cnt != 1 || voted {
		t.Fatalf("toggle off %d %v", cnt, voted)
	}
	cnt, voted, _ = ToggleVote(qa1.ID, p2)
	if cnt != 0 || voted {
		t.Fatalf("toggle off2")
	}
	// status update & count pending
	n, _ := CountPendingQA(ev.ID)
	if n != 2 {
		t.Fatalf("pending %d", n)
	}
	UpdateQAStatus(qa1.ID, "approved")
	n, _ = CountPendingQA(ev.ID)
	if n != 1 {
		t.Fatalf("pending after %d", n)
	}
	// ListQA filtered
	filt, _ := ListQA(ev.ID, "approved")
	if len(filt) != 1 || filt[0].ID != qa1.ID {
		t.Fatalf("filtered %+v", filt)
	}
	// ListQAForParticipant returns only approved
	ToggleVote(qa1.ID, p1) // p1 votes qa1
	lp, _ := ListQAForParticipant(ev.ID, p1)
	if len(lp) != 1 || !lp[0].Voted {
		t.Fatalf("lp %+v", lp)
	}
	if lp[0].Votes != 1 {
		t.Fatalf("votes %d", lp[0].Votes)
	}
	// participant with no vote
	lp2, _ := ListQAForParticipant(ev.ID, p2)
	if len(lp2) != 1 || lp2[0].Voted {
		t.Fatalf("lp2 voted should be false %+v", lp2)
	}
	// delete
	DeleteQA(qa2.ID)
	if _, err := GetQA(qa2.ID); err == nil {
		t.Fatal("deleted qa should error")
	}
	_ = qa2
}

func TestSettings(t *testing.T) {
	tmpDB(t)
	s, _ := GetAnalyticsSettings()
	if s.TrackingEnabled {
		t.Fatal("default should be disabled")
	}
	UpdateAnalyticsSettings("https://example.com/script.js", "wid123", true)
	s2, _ := GetAnalyticsSettings()
	if s2.UmamiScriptURL != "https://example.com/script.js" || s2.UmamiWebsiteID != "wid123" || !s2.TrackingEnabled {
		t.Fatalf("settings %+v", s2)
	}
	b, _ := GetBranding()
	if b != "" {
		t.Fatalf("brand default %q", b)
	}
	UpdateBranding("MyBrand")
	b2, _ := GetBranding()
	if b2 != "MyBrand" {
		t.Fatalf("brand %q", b2)
	}
}

func TestOTelSettings(t *testing.T) {
	tmpDB(t)
	s, err := GetOTelSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Endpoint != "" || s.ServiceName != "" || s.Headers != "" {
		t.Fatalf("defaults %+v", s)
	}
	if err := UpdateOTelSettings("http://collector:4318", "meetup-db", "Authorization=Bearer%20x"); err != nil {
		t.Fatal(err)
	}
	s2, err := GetOTelSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s2.Endpoint != "http://collector:4318" || s2.ServiceName != "meetup-db" || s2.Headers != "Authorization=Bearer%20x" {
		t.Fatalf("round trip %+v", s2)
	}
	if err := UpdateOTelSettings("", "", ""); err != nil {
		t.Fatal(err)
	}
	s3, _ := GetOTelSettings()
	if s3.Endpoint != "" || s3.ServiceName != "" || s3.Headers != "" {
		t.Fatalf("clear %+v", s3)
	}
}

func TestValidateAnswer(t *testing.T) {
	opts := []string{"A", "B", "C"}
	cases := []struct {
		name    string
		kind    string
		options []string
		raw     string
		want    string
		wantErr bool
	}{
		{"poll valid", "poll", opts, "B", "B", false},
		{"poll invalid", "poll", opts, "Z", "", true},
		{"multi valid", "multi", opts, `["A","C"]`, `["A","C"]`, false},
		{"multi empty", "multi", opts, `[]`, "", true},
		{"multi unknown", "multi", opts, `["A","Z"]`, "", true},
		{"multi dup", "multi", opts, `["A","A"]`, "", true},
		{"multi bad json", "multi", opts, `A`, "", true},
		{"ranking valid", "ranking", opts, `["C","A","B"]`, `["C","A","B"]`, false},
		{"ranking missing", "ranking", opts, `["A","B"]`, "", true},
		{"ranking dup", "ranking", opts, `["A","A","B"]`, "", true},
		{"yesno yes", "yesno", nil, "Yes", "yes", false},
		{"yesno bad", "yesno", nil, "maybe", "", true},
		{"rating valid", "rating", nil, "5", "5", false},
		{"rating bad", "rating", nil, "0", "", true},
		{"nps valid", "nps", nil, "10", "10", false},
		{"nps bad", "nps", nil, "11", "", true},
		{"open trimmed", "open", nil, " hello ", "hello", false},
		{"open empty", "open", nil, "   ", "", true},
		{"wordcloud too long", "wordcloud", nil, strings.Repeat("x", 201), "", true},
		{"unknown kind", "bogus", nil, "x", "", true},
	}
	for _, tc := range cases {
		got, err := ValidateAnswer(tc.kind, tc.options, tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: want error, got %q", tc.name, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestGetQuestionStats(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	p1, _ := GetOrCreateParticipant("p1", ev.ID)
	p2, _ := GetOrCreateParticipant("p2", ev.ID)
	p3, _ := GetOrCreateParticipant("p3", ev.ID)

	// multi: counts per option, ballots as total
	qm, _ := CreateQuestion(ev.ID, "multi", "live", "Pick some", []string{"A", "B", "C"}, false, true, 1, "", "")
	UpsertAnswer(qm.ID, p1, `["A","C"]`)
	UpsertAnswer(qm.ID, p2, `["A"]`)
	sm, err := GetQuestionStats(qm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sm.Total != 2 || sm.Respondents != 2 {
		t.Fatalf("multi total %d respondents %d", sm.Total, sm.Respondents)
	}
	wantMulti := []int{2, 0, 1}
	for i, want := range wantMulti {
		if sm.Results[i].Label != []string{"A", "B", "C"}[i] || sm.Results[i].Count != want {
			t.Fatalf("multi result %d %+v", i, sm.Results[i])
		}
	}
	if sm.NPS != nil {
		t.Fatalf("multi nps should be nil")
	}

	// ranking: Borda n-r with best rank 1, avg rank, ordered by score
	qr, _ := CreateQuestion(ev.ID, "ranking", "live", "Rank", []string{"A", "B", "C"}, false, true, 2, "", "")
	UpsertAnswer(qr.ID, p1, `["A","B","C"]`)
	UpsertAnswer(qr.ID, p2, `["C","A","B"]`)
	sr, _ := GetQuestionStats(qr.ID)
	if sr.Total != 2 || sr.Respondents != 2 {
		t.Fatalf("ranking total %d respondents %d", sr.Total, sr.Respondents)
	}
	wantRank := []struct {
		label   string
		score   float64
		avgRank float64
	}{{"A", 3, 1.5}, {"C", 2, 2}, {"B", 1, 2.5}}
	for i, want := range wantRank {
		r := sr.Results[i]
		if r.Label != want.label || r.Score != want.score || r.AvgRank != want.avgRank {
			t.Fatalf("ranking result %d %+v want %+v", i, r, want)
		}
	}

	// nps: promoters 9-10, detractors 0-6
	qn, _ := CreateQuestion(ev.ID, "nps", "live", "NPS", nil, false, true, 3, "", "")
	UpsertAnswer(qn.ID, p1, "10")
	UpsertAnswer(qn.ID, p2, "9")
	UpsertAnswer(qn.ID, p3, "6")
	sn, _ := GetQuestionStats(qn.ID)
	if sn.NPS == nil || *sn.NPS != 33 {
		t.Fatalf("nps %v", sn.NPS)
	}
	if len(sn.Results) != 11 || sn.Results[10].Count != 1 || sn.Results[0].Count != 0 {
		t.Fatalf("nps distribution %+v", sn.Results)
	}
	if sn.Total != 3 || sn.Respondents != 3 {
		t.Fatalf("nps total %d respondents %d", sn.Total, sn.Respondents)
	}

	// yesno without explicit options defaults to yes/no
	qy, _ := CreateQuestion(ev.ID, "yesno", "live", "YN", nil, false, true, 4, "", "")
	UpsertAnswer(qy.ID, p1, "yes")
	sy, _ := GetQuestionStats(qy.ID)
	if len(sy.Results) != 2 || sy.Results[0].Label != "yes" || sy.Results[0].Count != 1 || sy.Results[1].Label != "no" || sy.Results[1].Count != 0 {
		t.Fatalf("yesno results %+v", sy.Results)
	}

	// unknown question returns empty stats
	missing, err := GetQuestionStats(9999)
	if err != nil || missing == nil || len(missing.Results) != 0 {
		t.Fatalf("missing stats %+v err %v", missing, err)
	}
}

func TestPresentations(t *testing.T) {
	tmpDB(t)
	ev, _ := CreateEvent("E", "C1", "", "")
	pr, err := CreatePresentation(ev.ID, "Title", "Speaker", "file.pdf", 1234)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "Title" || pr.Size != 1234 {
		t.Fatalf("pres %+v", pr)
	}
	list, _ := ListPresentations(ev.ID)
	if len(list) != 1 {
		t.Fatalf("list %d", len(list))
	}
	got, _ := GetPresentation(pr.ID)
	if got.Filename != "file.pdf" {
		t.Fatalf("filename %q", got.Filename)
	}
	if err := DeletePresentation(pr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := GetPresentation(pr.ID); err == nil {
		t.Fatal("should be deleted")
	}
}

func TestEnsureColumnMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy questions table without the media columns.
	if _, err := raw.Exec(`CREATE TABLE questions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id INTEGER NOT NULL,
		kind TEXT NOT NULL,
		mode TEXT NOT NULL DEFAULT 'live',
		prompt TEXT NOT NULL,
		options TEXT NOT NULL DEFAULT '[]',
		position INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'draft',
		show_results INTEGER NOT NULL DEFAULT 1,
		is_feedback INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT INTO questions (event_id, kind, prompt) VALUES (1, 'poll', 'legacy question');`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Init(path); err != nil {
		t.Fatalf("init legacy: %v", err)
	}
	defer Close()

	q, err := GetQuestion(1)
	if err != nil || q == nil {
		t.Fatalf("legacy question missing: %v", err)
	}
	if q.MediaURL != "" || q.MediaType != "" {
		t.Fatalf("expected empty media, got %q %q", q.MediaURL, q.MediaType)
	}
	// ensureColumn must be idempotent across restarts.
	if err := Init(path); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if q, err := GetQuestion(1); err != nil || q == nil {
		t.Fatalf("question lost after second init: %v", err)
	}
}

func TestQuestionMediaRoundTrip(t *testing.T) {
	tmpDB(t)
	ev, err := CreateEvent("Media Event", "media-event", "", "")
	if err != nil {
		t.Fatal(err)
	}
	q, err := CreateQuestion(ev.ID, "poll", "live", "Pick", []string{"A", "B"}, false, true, 1, "/media/abc.png", "image")
	if err != nil {
		t.Fatal(err)
	}
	if q.MediaURL != "/media/abc.png" || q.MediaType != "image" {
		t.Fatalf("media not stored: %+v", q)
	}
	updated, err := UpdateQuestion(q.ID, map[string]any{"media_url": "https://example.com/clip.mp4", "media_type": "video"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.MediaURL != "https://example.com/clip.mp4" || updated.MediaType != "video" {
		t.Fatalf("media not updated: %+v", updated)
	}
	cleared, err := UpdateQuestion(q.ID, map[string]any{"media_url": "", "media_type": ""})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.MediaURL != "" || cleared.MediaType != "" {
		t.Fatalf("media not cleared: %+v", cleared)
	}
}
