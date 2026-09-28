package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"meetup/db"
)

func TestSubmitAnswerValidation(t *testing.T) {
	if err := db.Init(filepath.Join(t.TempDir(), "answers.db")); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	defer db.Close()

	ev, err := db.CreateEvent("Answer Validation", "answers-1", "", "")
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	cases := []struct {
		name    string
		kind    string
		options []string
		value   string
		want    int
	}{
		{"poll accepts option", "poll", []string{"A", "B"}, "B", http.StatusOK},
		{"poll rejects other option", "poll", []string{"A", "B"}, "C", http.StatusBadRequest},
		{"multi accepts known options", "multi", []string{"A", "B"}, `["B","A"]`, http.StatusOK},
		{"multi rejects unknown option", "multi", []string{"A", "B"}, `["C"]`, http.StatusBadRequest},
		{"multi rejects duplicates", "multi", []string{"A", "B"}, `["A","A"]`, http.StatusBadRequest},
		{"ranking accepts full order", "ranking", []string{"A", "B"}, `["B","A"]`, http.StatusOK},
		{"ranking rejects partial order", "ranking", []string{"A", "B"}, `["A"]`, http.StatusBadRequest},
		{"yesno accepts yes", "yesno", nil, "yes", http.StatusOK},
		{"yesno rejects other", "yesno", nil, "maybe", http.StatusBadRequest},
		{"rating accepts in range", "rating", nil, "4", http.StatusOK},
		{"rating rejects out of range", "rating", nil, "6", http.StatusBadRequest},
		{"nps accepts in range", "nps", nil, "10", http.StatusOK},
		{"nps rejects out of range", "nps", nil, "11", http.StatusBadRequest},
		{"open accepts text", "open", nil, "hello", http.StatusOK},
		{"open rejects blank text", "open", nil, "   ", http.StatusBadRequest},
		{"wordcloud accepts text", "wordcloud", nil, "cloud", http.StatusOK},
		{"wordcloud rejects long text", "wordcloud", nil, strings.Repeat("x", 201), http.StatusBadRequest},
		{"unknown kind rejected", "bogus", nil, "x", http.StatusBadRequest},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := db.CreateQuestion(ev.ID, tc.kind, "live", "Question", tc.options, false, true, i+1, "", "")
			if err != nil {
				t.Fatalf("CreateQuestion: %v", err)
			}
			if err := db.ActivateQuestion(ev.ID, q.ID); err != nil {
				t.Fatalf("ActivateQuestion: %v", err)
			}
			body, _ := json.Marshal(AnswerRequest{QuestionID: q.ID, Value: tc.value})
			req := httptest.NewRequest(http.MethodPost, "/api/events/"+ev.Code+"/answers", strings.NewReader(string(body)))
			req.SetPathValue("code", ev.Code)
			rec := httptest.NewRecorder()
			SubmitAnswer(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusOK {
				n, err := db.CountAnswers(q.ID)
				if err != nil || n != 1 {
					t.Fatalf("stored answers = %d (%v), want 1", n, err)
				}
			}
		})
	}
}
