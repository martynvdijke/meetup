package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type Event struct {
	ID             int64
	Code           string
	Name           string
	Description    string
	EventDate      string
	Status         string
	FeedbackOpen   bool
	CreatedAt      time.Time
	QuestionCount  int
	PendingQACount int
}

type Presentation struct {
	ID        int64
	EventID   int64
	Title     string
	Speaker   string
	Filename  string
	Size      int64
	CreatedAt time.Time
}

type Result struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	// Score and AvgRank are only set for ranking questions.
	Score   float64 `json:"score,omitempty"`
	AvgRank float64 `json:"avg_rank,omitempty"`
}

// QuestionStats is the aggregate result set for one question.
type QuestionStats struct {
	Results     []Result
	Total       int  // responses (ballots) for multi/ranking, selections for the rest
	Respondents int  // distinct participants who answered
	NPS         *int // set for kind "nps"
}

type Question struct {
	ID          int64
	EventID     int64
	Kind        string
	Mode        string
	Prompt      string
	Options     []string
	Position    int
	Status      string
	ShowResults bool
	IsFeedback  bool
	MediaURL    string
	MediaType   string
	CreatedAt   time.Time
	Results     []Result `json:"results"`
	Total       int      `json:"total"`
	Respondents int      `json:"respondents"`
	NPS         *int     `json:"nps,omitempty"`
}

type QAQuestion struct {
	ID        int64
	EventID   int64
	Body      string
	Author    string
	Status    string
	Votes     int
	Voted     bool
	CreatedAt time.Time
}

type AnalyticsSettings struct {
	UmamiScriptURL  string
	UmamiWebsiteID  string
	TrackingEnabled bool
}

// OTelSettings is the admin-managed OpenTelemetry configuration. Environment
// variables take precedence over these values.
type OTelSettings struct {
	Endpoint    string
	ServiceName string
	Headers     string
}

type Participant struct {
	ID        int64
	EventID   int64
	Token     string
	CreatedAt time.Time
}

type Answer struct {
	ID            int64
	QuestionID    int64
	ParticipantID int64
	Value         string
	CreatedAt     time.Time
}

var DB *sql.DB

func Init(path string) error {
	var err error
	// WAL + per-connection busy_timeout let many readers run concurrently with
	// a single writer; SQLite serialises writers itself.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
	DB, err = sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	// Multiple connections: WAL gives concurrent readers + one writer, and
	// modernc applies busy_timeout per connection so writers queue instead of
	// failing with SQLITE_BUSY.
	DB.SetMaxOpenConns(8)
	DB.SetMaxIdleConns(8)
	DB.SetConnMaxIdleTime(5 * time.Minute)
	if err := migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

func Close() error {
	if DB == nil {
		return nil
	}
	err := DB.Close()
	DB = nil
	return err
}

func migrate() error {
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'admin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			event_date TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			feedback_open INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS presentations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			speaker TEXT DEFAULT '',
			filename TEXT NOT NULL,
			size INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS questions (
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
			media_url TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS participants (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token TEXT UNIQUE NOT NULL,
			event_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS answers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			question_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			value TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(question_id, participant_id)
		);
		CREATE TABLE IF NOT EXISTS qa_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			body TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS qa_votes (
			qa_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(qa_id, participant_id)
		);
		CREATE TABLE IF NOT EXISTS settings (
			id INTEGER PRIMARY KEY CHECK(id=1),
			umami_script_url TEXT NOT NULL DEFAULT '',
			umami_website_id TEXT NOT NULL DEFAULT '',
			tracking_enabled INTEGER NOT NULL DEFAULT 0,
			brand TEXT NOT NULL DEFAULT '',
			otel_endpoint TEXT NOT NULL DEFAULT '',
			otel_service_name TEXT NOT NULL DEFAULT '',
			otel_headers TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT OR IGNORE INTO settings(id) VALUES(1)")
	if err != nil {
		return err
	}
	// Additive migrations for databases created before these columns existed.
	if err := ensureColumn("questions", "media_url", "media_url TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := ensureColumn("questions", "media_type", "media_type TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range []struct{ name, ddl string }{
		{"otel_endpoint", "otel_endpoint TEXT NOT NULL DEFAULT ''"},
		{"otel_service_name", "otel_service_name TEXT NOT NULL DEFAULT ''"},
		{"otel_headers", "otel_headers TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn("settings", col.name, col.ddl); err != nil {
			return err
		}
	}
	return nil
}

// ensureColumn adds a column to an existing table if it is missing. It is
// idempotent and safe to call on every startup.
func ensureColumn(table, column, ddl string) error {
	rows, err := DB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = DB.Exec("ALTER TABLE " + table + " ADD COLUMN " + ddl)
	return err
}

// helpers

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

func parseTimePragmatic(s string) time.Time {
	t, _ := parseTime(s)
	return t
}

// users/auth

func CountUsers() (int, error) {
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func CreateUser(username, passwordHash, role string) (int64, error) {
	if role == "" {
		role = "admin"
	}
	res, err := DB.Exec("INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)", username, passwordHash, role)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetUserByUsername(username string) (*User, error) {
	u := &User{}
	var ca string
	err := DB.QueryRow("SELECT id, username, password_hash, role, created_at FROM users WHERE username=?", username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &ca)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTimePragmatic(ca)
	return u, nil
}

func GetUserByID(id int64) (*User, error) {
	u := &User{}
	var ca string
	err := DB.QueryRow("SELECT id, username, password_hash, role, created_at FROM users WHERE id=?", id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &ca)
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTimePragmatic(ca)
	return u, nil
}

func CreateSession(token string, userID int64, expires time.Time) error {
	_, err := DB.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expires.UTC().Format("2006-01-02 15:04:05"))
	return err
}

func GetSession(token string) (*Session, error) {
	s := &Session{}
	var ea string
	err := DB.QueryRow("SELECT token, user_id, expires_at FROM sessions WHERE token=?", token).Scan(&s.Token, &s.UserID, &ea)
	if err != nil {
		return nil, err
	}
	s.ExpiresAt = parseTimePragmatic(ea)
	return s, nil
}

func DeleteSession(token string) error {
	_, err := DB.Exec("DELETE FROM sessions WHERE token=?", token)
	return err
}

func DeleteExpiredSessions() error {
	_, err := DB.Exec("DELETE FROM sessions WHERE expires_at <= datetime('now')")
	return err
}

// events

func scanEventRow(row *sql.Row) (*Event, error) {
	var e Event
	var fo int
	var ca string
	err := row.Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.EventDate, &e.Status, &fo, &ca)
	if err != nil {
		return nil, err
	}
	e.FeedbackOpen = fo == 1
	e.CreatedAt = parseTimePragmatic(ca)
	// populate counts
	_ = DB.QueryRow("SELECT COUNT(*) FROM questions WHERE event_id=?", e.ID).Scan(&e.QuestionCount)
	_ = DB.QueryRow("SELECT COUNT(*) FROM qa_questions WHERE event_id=? AND status='pending'", e.ID).Scan(&e.PendingQACount)
	return &e, nil
}

func scanEventRows(rows *sql.Rows) (*Event, error) {
	var e Event
	var fo int
	var ca string
	err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.EventDate, &e.Status, &fo, &ca)
	if err != nil {
		return nil, err
	}
	e.FeedbackOpen = fo == 1
	e.CreatedAt = parseTimePragmatic(ca)
	_ = DB.QueryRow("SELECT COUNT(*) FROM questions WHERE event_id=?", e.ID).Scan(&e.QuestionCount)
	_ = DB.QueryRow("SELECT COUNT(*) FROM qa_questions WHERE event_id=? AND status='pending'", e.ID).Scan(&e.PendingQACount)
	return &e, nil
}

func CreateEvent(name, code, description, eventDate string) (*Event, error) {
	res, err := DB.Exec("INSERT INTO events (name, code, description, event_date) VALUES (?, ?, ?, ?)", name, code, description, eventDate)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetEventByID(id)
}

func ListEvents() ([]Event, error) {
	rows, err := DB.Query("SELECT id, code, name, description, event_date, status, feedback_open, created_at, (SELECT COUNT(*) FROM questions WHERE event_id=events.id) as qc, (SELECT COUNT(*) FROM qa_questions WHERE event_id=events.id AND status='pending') as pc FROM events ORDER BY created_at DESC, id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var fo int
		var ca string
		if err := rows.Scan(&e.ID, &e.Code, &e.Name, &e.Description, &e.EventDate, &e.Status, &fo, &ca, &e.QuestionCount, &e.PendingQACount); err != nil {
			return nil, err
		}
		e.FeedbackOpen = fo == 1
		e.CreatedAt = parseTimePragmatic(ca)
		out = append(out, e)
	}
	return out, rows.Err()
}

func GetEventByID(id int64) (*Event, error) {
	row := DB.QueryRow("SELECT id, code, name, description, event_date, status, feedback_open, created_at FROM events WHERE id=?", id)
	return scanEventRow(row)
}

func GetEventByCode(code string) (*Event, error) {
	row := DB.QueryRow("SELECT id, code, name, description, event_date, status, feedback_open, created_at FROM events WHERE code=?", code)
	e, err := scanEventRow(row)
	if err != nil {
		return nil, err
	}
	// populate question results not needed for events
	return e, nil
}

func UpdateEvent(id int64, fields map[string]any) (*Event, error) {
	allowed := map[string]string{
		"name":          "name",
		"description":   "description",
		"event_date":    "event_date",
		"status":        "status",
		"feedback_open": "feedback_open",
	}
	var sets []string
	var args []any
	for k, v := range fields {
		col, ok := allowed[k]
		if !ok {
			continue
		}
		sets = append(sets, col+"=?")
		if col == "feedback_open" {
			switch val := v.(type) {
			case bool:
				args = append(args, btoi(val))
			case int:
				args = append(args, val)
			case int64:
				args = append(args, val)
			default:
				args = append(args, v)
			}
		} else {
			args = append(args, v)
		}
	}
	if len(sets) > 0 {
		args = append(args, id)
		_, err := DB.Exec("UPDATE events SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
		if err != nil {
			return nil, err
		}
	}
	return GetEventByID(id)
}

func DeleteEvent(id int64) error {
	_, err := DB.Exec("DELETE FROM events WHERE id=?", id)
	return err
}

func EventCodeExists(code string) (bool, error) {
	if code == "" {
		return false, nil
	}
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM events WHERE code=?", code).Scan(&n)
	return n > 0, err
}

// presentations

func CreatePresentation(eventID int64, title, speaker, filename string, size int64) (*Presentation, error) {
	res, err := DB.Exec("INSERT INTO presentations (event_id, title, speaker, filename, size) VALUES (?, ?, ?, ?, ?)", eventID, title, speaker, filename, size)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetPresentation(id)
}

func ListPresentations(eventID int64) ([]Presentation, error) {
	rows, err := DB.Query("SELECT id, event_id, title, speaker, filename, size, created_at FROM presentations WHERE event_id=? ORDER BY created_at ASC, id ASC", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Presentation
	for rows.Next() {
		var p Presentation
		var ca string
		if err := rows.Scan(&p.ID, &p.EventID, &p.Title, &p.Speaker, &p.Filename, &p.Size, &ca); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTimePragmatic(ca)
		out = append(out, p)
	}
	return out, rows.Err()
}

func GetPresentation(id int64) (*Presentation, error) {
	var p Presentation
	var ca string
	err := DB.QueryRow("SELECT id, event_id, title, speaker, filename, size, created_at FROM presentations WHERE id=?", id).Scan(&p.ID, &p.EventID, &p.Title, &p.Speaker, &p.Filename, &p.Size, &ca)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTimePragmatic(ca)
	return &p, nil
}

func DeletePresentation(id int64) error {
	_, err := DB.Exec("DELETE FROM presentations WHERE id=?", id)
	return err
}

// questions

func scanQuestionRows(rows *sql.Rows) (*Question, error) {
	var q Question
	var opts, ca string
	var sr, fb int
	err := rows.Scan(&q.ID, &q.EventID, &q.Kind, &q.Mode, &q.Prompt, &opts, &q.Position, &q.Status, &sr, &fb, &q.MediaURL, &q.MediaType, &ca)
	if err != nil {
		return nil, err
	}
	q.ShowResults = sr == 1
	q.IsFeedback = fb == 1
	q.CreatedAt = parseTimePragmatic(ca)
	_ = json.Unmarshal([]byte(opts), &q.Options)
	if q.Options == nil {
		q.Options = []string{}
	}
	return &q, nil
}

func fillQuestionStats(q *Question) {
	st, err := GetQuestionStats(q.ID)
	if err != nil || st == nil {
		q.Results = []Result{}
		return
	}
	q.Results = st.Results
	q.Total = st.Total
	q.Respondents = st.Respondents
	q.NPS = st.NPS
}

func scanQuestionRow(row *sql.Row) (*Question, error) {
	var q Question
	var opts, ca string
	var sr, fb int
	err := row.Scan(&q.ID, &q.EventID, &q.Kind, &q.Mode, &q.Prompt, &opts, &q.Position, &q.Status, &sr, &fb, &q.MediaURL, &q.MediaType, &ca)
	if err != nil {
		return nil, err
	}
	q.ShowResults = sr == 1
	q.IsFeedback = fb == 1
	q.CreatedAt = parseTimePragmatic(ca)
	_ = json.Unmarshal([]byte(opts), &q.Options)
	if q.Options == nil {
		q.Options = []string{}
	}
	fillQuestionStats(&q)
	return &q, nil
}

func CreateQuestion(eventID int64, kind, mode, prompt string, options []string, isFeedback, showResults bool, position int, mediaURL, mediaType string) (*Question, error) {
	if mode == "" {
		mode = "live"
	}
	b, _ := json.Marshal(options)
	if b == nil {
		b = []byte("[]")
	}
	res, err := DB.Exec("INSERT INTO questions (event_id, kind, mode, prompt, options, position, show_results, is_feedback, media_url, media_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", eventID, kind, mode, prompt, string(b), position, btoi(showResults), btoi(isFeedback), mediaURL, mediaType)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetQuestion(id)
}

func ListQuestions(eventID int64) ([]Question, error) {
	rows, err := DB.Query("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, media_url, media_type, created_at FROM questions WHERE event_id=? AND is_feedback=0 ORDER BY position ASC, id ASC", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		fillQuestionStats(&out[i])
	}
	return out, nil
}

func ListFeedbackQuestions(eventID int64) ([]Question, error) {
	rows, err := DB.Query("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, media_url, media_type, created_at FROM questions WHERE event_id=? AND is_feedback=1 ORDER BY position ASC, id ASC", eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		fillQuestionStats(&out[i])
	}
	return out, nil
}

func GetQuestion(id int64) (*Question, error) {
	row := DB.QueryRow("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, media_url, media_type, created_at FROM questions WHERE id=?", id)
	return scanQuestionRow(row)
}

func UpdateQuestion(id int64, fields map[string]any) (*Question, error) {
	allowed := map[string]string{
		"prompt":       "prompt",
		"options":      "options",
		"position":     "position",
		"show_results": "show_results",
		"is_feedback":  "is_feedback",
		"kind":         "kind",
		"mode":         "mode",
		"status":       "status",
		"media_url":    "media_url",
		"media_type":   "media_type",
	}
	var sets []string
	var args []any
	for k, v := range fields {
		col, ok := allowed[k]
		if !ok {
			continue
		}
		switch col {
		case "options":
			// expect []string
			var s string
			switch val := v.(type) {
			case []string:
				b, _ := json.Marshal(val)
				s = string(b)
			case string:
				s = val
			default:
				b, _ := json.Marshal(v)
				s = string(b)
			}
			sets = append(sets, col+"=?")
			args = append(args, s)
		case "show_results", "is_feedback":
			switch val := v.(type) {
			case bool:
				sets = append(sets, col+"=?")
				args = append(args, btoi(val))
			case int:
				sets = append(sets, col+"=?")
				args = append(args, val)
			default:
				sets = append(sets, col+"=?")
				args = append(args, v)
			}
		default:
			sets = append(sets, col+"=?")
			args = append(args, v)
		}
	}
	if len(sets) > 0 {
		args = append(args, id)
		_, err := DB.Exec("UPDATE questions SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
		if err != nil {
			return nil, err
		}
	}
	return GetQuestion(id)
}

func DeleteQuestion(id int64) error {
	_, err := DB.Exec("DELETE FROM questions WHERE id=?", id)
	return err
}

func ActivateQuestion(eventID, questionID int64) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRow("SELECT COUNT(*) FROM questions WHERE id=? AND event_id=?", questionID, eventID).Scan(&exists)
	if err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("question not found")
	}
	_, err = tx.Exec("UPDATE questions SET status='closed' WHERE event_id=? AND status='live'", eventID)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE questions SET status='live' WHERE id=? AND event_id=?", questionID, eventID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func CloseQuestion(eventID, questionID int64) error {
	res, err := DB.Exec("UPDATE questions SET status='closed' WHERE id=? AND event_id=?", questionID, eventID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("question not found")
	}
	return nil
}

func GetActiveQuestion(eventID int64) (*Question, error) {
	row := DB.QueryRow("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, media_url, media_type, created_at FROM questions WHERE event_id=? AND status='live' LIMIT 1", eventID)
	q, err := scanQuestionRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return q, err
}

// ValidQuestionKind reports whether kind is a supported question type.
func ValidQuestionKind(kind string) bool {
	switch kind {
	case "poll", "multi", "ranking", "yesno", "rating", "nps", "open", "wordcloud":
		return true
	}
	return false
}

// ValidateAnswer checks and normalizes a raw client answer for a question kind.
// The returned value is the canonical string stored in the answers table.
func ValidateAnswer(kind string, options []string, raw string) (string, error) {
	switch kind {
	case "poll":
		v := strings.TrimSpace(raw)
		for _, o := range options {
			if v == o {
				return o, nil
			}
		}
		return "", fmt.Errorf("answer must be one of the options")
	case "multi":
		vals, err := parseAnswerArray(raw)
		if err != nil {
			return "", fmt.Errorf("answer must be a JSON array of options")
		}
		if len(vals) == 0 {
			return "", fmt.Errorf("select at least one option")
		}
		seen := map[string]bool{}
		out := make([]string, 0, len(vals))
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if seen[v] {
				return "", fmt.Errorf("duplicate option in answer")
			}
			if !containsString(options, v) {
				return "", fmt.Errorf("unknown option in answer")
			}
			seen[v] = true
			out = append(out, v)
		}
		b, _ := json.Marshal(out)
		return string(b), nil
	case "ranking":
		vals, err := parseAnswerArray(raw)
		if err != nil {
			return "", fmt.Errorf("answer must be a JSON array of options")
		}
		if len(vals) != len(options) {
			return "", fmt.Errorf("answer must rank every option")
		}
		seen := map[string]bool{}
		for _, v := range vals {
			if !containsString(options, v) || seen[v] {
				return "", fmt.Errorf("answer must rank every option exactly once")
			}
			seen[v] = true
		}
		b, _ := json.Marshal(vals)
		return string(b), nil
	case "yesno":
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "yes" || v == "no" {
			return v, nil
		}
		return "", fmt.Errorf("answer must be yes or no")
	case "rating":
		v := strings.TrimSpace(raw)
		if v == "1" || v == "2" || v == "3" || v == "4" || v == "5" {
			return v, nil
		}
		return "", fmt.Errorf("answer must be a rating from 1 to 5")
	case "nps":
		v := strings.TrimSpace(raw)
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 10 {
			return "", fmt.Errorf("answer must be a score from 0 to 10")
		}
		return v, nil
	case "open":
		return validateTextAnswer(raw, 500)
	case "wordcloud":
		return validateTextAnswer(raw, 200)
	}
	return "", fmt.Errorf("unknown question kind")
}

func validateTextAnswer(raw string, max int) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", fmt.Errorf("answer is required")
	}
	if utf8.RuneCountInString(v) > max {
		return "", fmt.Errorf("answer is too long")
	}
	return v, nil
}

func parseAnswerArray(raw string) ([]string, error) {
	var vals []string
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return nil, err
	}
	return vals, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// GetQuestionStats aggregates the answers for a single question.
// Results are ordered by option order for choice kinds, by Borda score for
// ranking, by score value for nps and by count for open/wordcloud.
func GetQuestionStats(questionID int64) (*QuestionStats, error) {
	var optsStr, kind string
	err := DB.QueryRow("SELECT options, kind FROM questions WHERE id=?", questionID).Scan(&optsStr, &kind)
	if err != nil {
		if err == sql.ErrNoRows {
			return &QuestionStats{Results: []Result{}}, nil
		}
		return nil, err
	}
	var opts []string
	_ = json.Unmarshal([]byte(optsStr), &opts)

	stats := &QuestionStats{}
	if err := DB.QueryRow("SELECT COUNT(DISTINCT participant_id) FROM answers WHERE question_id=?", questionID).Scan(&stats.Respondents); err != nil {
		return nil, err
	}

	switch kind {
	case "multi":
		return multiStats(questionID, opts, stats)
	case "ranking":
		return rankingStats(questionID, opts, stats)
	case "nps":
		return npsStats(questionID, stats)
	}

	rows, err := DB.Query("SELECT value, COUNT(*) as cnt FROM answers WHERE question_id=? GROUP BY value ORDER BY cnt DESC LIMIT 100", questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	countMap := map[string]int{}
	var raw []Result
	for rows.Next() {
		var val string
		var cnt int
		if err := rows.Scan(&val, &cnt); err != nil {
			return nil, err
		}
		countMap[val] = cnt
		raw = append(raw, Result{Label: val, Count: cnt})
		stats.Total += cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch kind {
	case "poll", "rating", "yesno":
		if kind == "yesno" && len(opts) == 0 {
			opts = []string{"yes", "no"}
		}
		ordered := make([]Result, 0, len(opts)+len(raw))
		seen := map[string]bool{}
		for _, o := range opts {
			ordered = append(ordered, Result{Label: o, Count: countMap[o]})
			seen[o] = true
		}
		// Append any legacy values that are not among the options.
		for _, r := range raw {
			if !seen[r.Label] {
				ordered = append(ordered, r)
			}
		}
		stats.Results = ordered
	default:
		if raw == nil {
			raw = []Result{}
		}
		stats.Results = raw
	}
	return stats, nil
}

// multiStats counts how many respondents selected each option. Total is the
// number of ballots, so bar percentages read as "% of respondents".
func multiStats(questionID int64, opts []string, stats *QuestionStats) (*QuestionStats, error) {
	rows, err := DB.Query("SELECT value FROM answers WHERE question_id=?", questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	ballots := 0
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			return nil, err
		}
		var chosen []string
		if err := json.Unmarshal([]byte(val), &chosen); err != nil {
			continue
		}
		ballots++
		for _, c := range chosen {
			counts[c]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stats.Total = ballots
	stats.Results = orderedResults(opts, counts)
	return stats, nil
}

// rankingStats computes Borda points (n-rank, best rank = 1) and the average
// rank per option, ordered by score descending.
func rankingStats(questionID int64, opts []string, stats *QuestionStats) (*QuestionStats, error) {
	rows, err := DB.Query("SELECT value FROM answers WHERE question_id=?", questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := len(opts)
	type rankAcc struct {
		score   float64
		rankSum float64
		count   int
	}
	accs := map[string]*rankAcc{}
	ballots := 0
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			return nil, err
		}
		var order []string
		if err := json.Unmarshal([]byte(val), &order); err != nil {
			continue
		}
		ballots++
		for i, label := range order {
			a := accs[label]
			if a == nil {
				a = &rankAcc{}
				accs[label] = a
			}
			a.count++
			a.score += float64(n - (i + 1))
			a.rankSum += float64(i + 1)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Build in option order first so score ties stay deterministic.
	results := make([]Result, 0, len(accs))
	seen := map[string]bool{}
	for _, o := range opts {
		if a := accs[o]; a != nil {
			results = append(results, rankResult(o, a.score, a.rankSum, a.count))
			seen[o] = true
		}
	}
	var extras []string
	for label := range accs {
		if !seen[label] {
			extras = append(extras, label)
		}
	}
	sort.Strings(extras)
	for _, label := range extras {
		a := accs[label]
		results = append(results, rankResult(label, a.score, a.rankSum, a.count))
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	stats.Total = ballots
	stats.Results = results
	return stats, nil
}

func rankResult(label string, score, rankSum float64, count int) Result {
	r := Result{Label: label, Count: count, Score: score}
	if count > 0 {
		r.AvgRank = rankSum / float64(count)
	}
	return r
}

// npsStats returns the 0..10 distribution plus the net promoter score.
func npsStats(questionID int64, stats *QuestionStats) (*QuestionStats, error) {
	rows, err := DB.Query("SELECT value, COUNT(*) FROM answers WHERE question_id=? GROUP BY value", questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := [11]int{}
	for rows.Next() {
		var val string
		var cnt int
		if err := rows.Scan(&val, &cnt); err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n < 0 || n > 10 {
			continue
		}
		counts[n] += cnt
		stats.Total += cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	stats.Results = make([]Result, 0, 11)
	for score := 0; score <= 10; score++ {
		stats.Results = append(stats.Results, Result{Label: strconv.Itoa(score), Count: counts[score]})
	}
	if stats.Total > 0 {
		promoters := counts[9] + counts[10]
		detractors := counts[0] + counts[1] + counts[2] + counts[3] + counts[4] + counts[5] + counts[6]
		nps := int(math.Round(100 * float64(promoters-detractors) / float64(stats.Total)))
		stats.NPS = &nps
	}
	return stats, nil
}

// orderedResults lists the given options in order (zero counts included),
// followed by any extra counted values.
func orderedResults(opts []string, counts map[string]int) []Result {
	ordered := make([]Result, 0, len(opts))
	seen := map[string]bool{}
	for _, o := range opts {
		ordered = append(ordered, Result{Label: o, Count: counts[o]})
		seen[o] = true
	}
	var extras []string
	for label := range counts {
		if !seen[label] {
			extras = append(extras, label)
		}
	}
	sort.Strings(extras)
	for _, label := range extras {
		ordered = append(ordered, Result{Label: label, Count: counts[label]})
	}
	return ordered
}

// answers

func UpsertAnswer(questionID, participantID int64, value string) error {
	_, err := DB.Exec("INSERT INTO answers (question_id, participant_id, value) VALUES (?, ?, ?) ON CONFLICT(question_id, participant_id) DO UPDATE SET value=excluded.value, created_at=CURRENT_TIMESTAMP", questionID, participantID, value)
	return err
}

func GetAnswer(questionID, participantID int64) (*Answer, error) {
	var a Answer
	var ca string
	err := DB.QueryRow("SELECT id, question_id, participant_id, value, created_at FROM answers WHERE question_id=? AND participant_id=?", questionID, participantID).Scan(&a.ID, &a.QuestionID, &a.ParticipantID, &a.Value, &ca)
	if err != nil {
		return nil, err
	}
	a.CreatedAt = parseTimePragmatic(ca)
	return &a, nil
}

func CountAnswers(questionID int64) (int, error) {
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM answers WHERE question_id=?", questionID).Scan(&n)
	return n, err
}

// participants

func GetOrCreateParticipant(token string, eventID int64) (int64, error) {
	var id int64
	err := DB.QueryRow("SELECT id FROM participants WHERE token=?", token).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := DB.Exec("INSERT INTO participants (token, event_id) VALUES (?, ?)", token, eventID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetParticipantByToken(token string) (*Participant, error) {
	var p Participant
	var ca string
	err := DB.QueryRow("SELECT id, event_id, token, created_at FROM participants WHERE token=?", token).Scan(&p.ID, &p.EventID, &p.Token, &ca)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTimePragmatic(ca)
	return &p, nil
}

// qa

func CreateQA(eventID int64, body, author string) (*QAQuestion, error) {
	res, err := DB.Exec("INSERT INTO qa_questions (event_id, body, author) VALUES (?, ?, ?)", eventID, body, author)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetQA(id)
}

func ListQA(eventID int64, statuses ...string) ([]QAQuestion, error) {
	var rows *sql.Rows
	var err error
	if len(statuses) == 0 {
		rows, err = DB.Query(`
			SELECT q.id, q.event_id, q.body, q.author, q.status, q.created_at, COUNT(v.qa_id) as votes
			FROM qa_questions q LEFT JOIN qa_votes v ON v.qa_id=q.id
			WHERE q.event_id=?
			GROUP BY q.id ORDER BY votes DESC, q.created_at ASC`, eventID)
	} else {
		ph := strings.Repeat("?,", len(statuses))
		ph = ph[:len(ph)-1]
		args := []any{eventID}
		for _, s := range statuses {
			args = append(args, s)
		}
		query := fmt.Sprintf(`
			SELECT q.id, q.event_id, q.body, q.author, q.status, q.created_at, COUNT(v.qa_id) as votes
			FROM qa_questions q LEFT JOIN qa_votes v ON v.qa_id=q.id
			WHERE q.event_id=? AND q.status IN (%s)
			GROUP BY q.id ORDER BY votes DESC, q.created_at ASC`, ph)
		rows, err = DB.Query(query, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QAQuestion
	for rows.Next() {
		var q QAQuestion
		var ca string
		if err := rows.Scan(&q.ID, &q.EventID, &q.Body, &q.Author, &q.Status, &ca, &q.Votes); err != nil {
			return nil, err
		}
		q.CreatedAt = parseTimePragmatic(ca)
		out = append(out, q)
	}
	return out, rows.Err()
}

func ListQAForParticipant(eventID, participantID int64) ([]QAQuestion, error) {
	rows, err := DB.Query(`
		SELECT q.id, q.event_id, q.body, q.author, q.status, q.created_at, COUNT(v.qa_id) as votes,
		       CASE WHEN EXISTS(SELECT 1 FROM qa_votes WHERE qa_id=q.id AND participant_id=?) THEN 1 ELSE 0 END as voted
		FROM qa_questions q LEFT JOIN qa_votes v ON v.qa_id=q.id
		WHERE q.event_id=? AND q.status='approved'
		GROUP BY q.id ORDER BY votes DESC, q.created_at ASC`, participantID, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QAQuestion
	for rows.Next() {
		var q QAQuestion
		var ca string
		var voted int
		if err := rows.Scan(&q.ID, &q.EventID, &q.Body, &q.Author, &q.Status, &ca, &q.Votes, &voted); err != nil {
			return nil, err
		}
		q.CreatedAt = parseTimePragmatic(ca)
		q.Voted = voted == 1
		out = append(out, q)
	}
	return out, rows.Err()
}

func GetQA(id int64) (*QAQuestion, error) {
	var q QAQuestion
	var ca string
	err := DB.QueryRow(`
		SELECT q.id, q.event_id, q.body, q.author, q.status, q.created_at, COUNT(v.qa_id) as votes
		FROM qa_questions q LEFT JOIN qa_votes v ON v.qa_id=q.id
		WHERE q.id=? GROUP BY q.id`, id).Scan(&q.ID, &q.EventID, &q.Body, &q.Author, &q.Status, &ca, &q.Votes)
	if err != nil {
		return nil, err
	}
	q.CreatedAt = parseTimePragmatic(ca)
	return &q, nil
}

func UpdateQAStatus(id int64, status string) error {
	_, err := DB.Exec("UPDATE qa_questions SET status=? WHERE id=?", status, id)
	return err
}

func DeleteQA(id int64) error {
	_, err := DB.Exec("DELETE FROM qa_questions WHERE id=?", id)
	return err
}

func ToggleVote(qaID, participantID int64) (int, bool, error) {
	// try insert
	res, err := DB.Exec("INSERT OR IGNORE INTO qa_votes (qa_id, participant_id) VALUES (?, ?)", qaID, participantID)
	if err != nil {
		return 0, false, err
	}
	n, _ := res.RowsAffected()
	var voted bool
	if n == 1 {
		voted = true
	} else {
		// already exists, delete
		_, err = DB.Exec("DELETE FROM qa_votes WHERE qa_id=? AND participant_id=?", qaID, participantID)
		if err != nil {
			return 0, false, err
		}
		voted = false
	}
	var cnt int
	err = DB.QueryRow("SELECT COUNT(*) FROM qa_votes WHERE qa_id=?", qaID).Scan(&cnt)
	return cnt, voted, err
}

func CountPendingQA(eventID int64) (int, error) {
	var n int
	err := DB.QueryRow("SELECT COUNT(*) FROM qa_questions WHERE event_id=? AND status='pending'", eventID).Scan(&n)
	return n, err
}

// settings

func GetAnalyticsSettings() (*AnalyticsSettings, error) {
	var s AnalyticsSettings
	var en int
	err := DB.QueryRow("SELECT umami_script_url, umami_website_id, tracking_enabled FROM settings WHERE id=1").Scan(&s.UmamiScriptURL, &s.UmamiWebsiteID, &en)
	if err != nil {
		return nil, err
	}
	s.TrackingEnabled = en == 1
	return &s, nil
}

func UpdateAnalyticsSettings(url, websiteID string, enabled bool) error {
	_, err := DB.Exec("UPDATE settings SET umami_script_url=?, umami_website_id=?, tracking_enabled=?, updated_at=CURRENT_TIMESTAMP WHERE id=1", url, websiteID, btoi(enabled))
	return err
}

// GetOTelSettings returns the admin-managed OpenTelemetry configuration.
func GetOTelSettings() (*OTelSettings, error) {
	var s OTelSettings
	err := DB.QueryRow("SELECT otel_endpoint, otel_service_name, otel_headers FROM settings WHERE id=1").Scan(&s.Endpoint, &s.ServiceName, &s.Headers)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpdateOTelSettings stores the OpenTelemetry configuration. The values are
// applied on the next restart; environment variables still take precedence.
func UpdateOTelSettings(endpoint, serviceName, headers string) error {
	_, err := DB.Exec("UPDATE settings SET otel_endpoint=?, otel_service_name=?, otel_headers=?, updated_at=CURRENT_TIMESTAMP WHERE id=1", endpoint, serviceName, headers)
	return err
}

func GetBranding() (string, error) {
	var b string
	err := DB.QueryRow("SELECT brand FROM settings WHERE id=1").Scan(&b)
	return b, err
}

func UpdateBranding(brand string) error {
	_, err := DB.Exec("UPDATE settings SET brand=?, updated_at=CURRENT_TIMESTAMP WHERE id=1", brand)
	return err
}
