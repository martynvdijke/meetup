package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	CreatedAt   time.Time
	Results     []Result `json:"results"`
	Total       int      `json:"total"`
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
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}
	_, err = DB.Exec("INSERT OR IGNORE INTO settings(id) VALUES(1)")
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
	err := rows.Scan(&q.ID, &q.EventID, &q.Kind, &q.Mode, &q.Prompt, &opts, &q.Position, &q.Status, &sr, &fb, &ca)
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

func scanQuestionRow(row *sql.Row) (*Question, error) {
	var q Question
	var opts, ca string
	var sr, fb int
	err := row.Scan(&q.ID, &q.EventID, &q.Kind, &q.Mode, &q.Prompt, &opts, &q.Position, &q.Status, &sr, &fb, &ca)
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
	results, total, _ := QuestionResults(q.ID)
	q.Results = results
	q.Total = total
	return &q, nil
}

func CreateQuestion(eventID int64, kind, mode, prompt string, options []string, isFeedback, showResults bool, position int) (*Question, error) {
	if mode == "" {
		mode = "live"
	}
	b, _ := json.Marshal(options)
	if b == nil {
		b = []byte("[]")
	}
	res, err := DB.Exec("INSERT INTO questions (event_id, kind, mode, prompt, options, position, show_results, is_feedback) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", eventID, kind, mode, prompt, string(b), position, btoi(showResults), btoi(isFeedback))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return GetQuestion(id)
}

func ListQuestions(eventID int64) ([]Question, error) {
	rows, err := DB.Query("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, created_at FROM questions WHERE event_id=? AND is_feedback=0 ORDER BY position ASC, id ASC", eventID)
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
		results, total, _ := QuestionResults(out[i].ID)
		out[i].Results = results
		out[i].Total = total
	}
	return out, nil
}

func ListFeedbackQuestions(eventID int64) ([]Question, error) {
	rows, err := DB.Query("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, created_at FROM questions WHERE event_id=? AND is_feedback=1 ORDER BY position ASC, id ASC", eventID)
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
		results, total, _ := QuestionResults(out[i].ID)
		out[i].Results = results
		out[i].Total = total
	}
	return out, nil
}

func GetQuestion(id int64) (*Question, error) {
	row := DB.QueryRow("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, created_at FROM questions WHERE id=?", id)
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
	row := DB.QueryRow("SELECT id, event_id, kind, mode, prompt, options, position, status, show_results, is_feedback, created_at FROM questions WHERE event_id=? AND status='live' LIMIT 1", eventID)
	q, err := scanQuestionRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return q, err
}

func QuestionResults(questionID int64) ([]Result, int, error) {
	// get options and kind
	var optsStr, kind string
	err := DB.QueryRow("SELECT options, kind FROM questions WHERE id=?", questionID).Scan(&optsStr, &kind)
	if err != nil {
		if err == sql.ErrNoRows {
			return []Result{}, 0, nil
		}
		return nil, 0, err
	}
	var opts []string
	_ = json.Unmarshal([]byte(optsStr), &opts)

	rows, err := DB.Query("SELECT value, COUNT(*) as cnt FROM answers WHERE question_id=? GROUP BY value ORDER BY cnt DESC LIMIT 100", questionID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	countMap := map[string]int{}
	var total int
	var rawResults []Result
	for rows.Next() {
		var val string
		var cnt int
		if err := rows.Scan(&val, &cnt); err != nil {
			return nil, 0, err
		}
		countMap[val] = cnt
		rawResults = append(rawResults, Result{Label: val, Count: cnt})
		total += cnt
	}
	// For poll/rating include zero-count options in original order
	if kind == "poll" || kind == "rating" {
		// Need to return in option order with counts, plus any extra?
		// Spec: also include every option with count 0 if absent, in original options order
		// And GROUP BY ORDER BY count DESC already; but for poll/rating we want option order?
		// Spec says "in the original options order" for zero-count ones, but overall we should
		// return all options in original order with counts? Let's interpret: results include all options in original order, with counts.
		// However spec also says GROUP BY value ORDER BY count DESC. For poll/rating, include every option with 0 if absent in original order.
		// Approach: build ordered list from options, preserving option order, appending missing.
		var ordered []Result
		seen := map[string]bool{}
		for _, o := range opts {
			ordered = append(ordered, Result{Label: o, Count: countMap[o]})
			seen[o] = true
		}
		// Append any extra values not in options (shouldn't happen for poll but keep)
		for _, r := range rawResults {
			if !seen[r.Label] {
				ordered = append(ordered, r)
			}
		}
		return ordered, total, nil
	}
	// open/wordcloud: raw values top 100 count desc
	if rawResults == nil {
		rawResults = []Result{}
	}
	return rawResults, total, nil
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

func GetBranding() (string, error) {
	var b string
	err := DB.QueryRow("SELECT brand FROM settings WHERE id=1").Scan(&b)
	return b, err
}

func UpdateBranding(brand string) error {
	_, err := DB.Exec("UPDATE settings SET brand=?, updated_at=CURRENT_TIMESTAMP WHERE id=1", brand)
	return err
}
