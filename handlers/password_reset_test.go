package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"meetup/db"
)

type stubMailer struct{ sent []string }
func (s *stubMailer) Send(to, subject, html string) error { s.sent = append(s.sent, to); return nil }

func setupTestDB(t *testing.T) {
	t.Helper()
	if err := db.Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func(){ db.Close() })
}

func TestForgotEnumerationSafe(t *testing.T) {
	setupTestDB(t)
	stub := &stubMailer{}
	DefaultMailer = stub
	defer func(){ DefaultMailer = &SMTPMailer{} }()
	rateHits = map[string][]time.Time{}
	h := sha256.Sum256([]byte("tok"))
	_ = hex.EncodeToString(h[:])

	body, _ := json.Marshal(map[string]string{"email": "nobody@example.com"})
	req := httptest.NewRequest("POST", "/api/auth/forgot", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.1.1.1:1234"
	w := httptest.NewRecorder()
	ForgotPassword(w, req)
	if w.Code != 200 { t.Fatalf("code %d", w.Code) }
	if len(stub.sent)!=0 { t.Fatalf("should not send") }
}

func TestResetFlow(t *testing.T) {
	setupTestDB(t)
	stub := &stubMailer{}
	DefaultMailer = stub
	defer func(){ DefaultMailer = &SMTPMailer{} }()
	rateHits = map[string][]time.Time{}
	hash,_ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	uid,_ := db.CreateUser("admin@example.com", string(hash), "admin")
	// create token
	tok := "abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"
	h := sha256.Sum256([]byte(tok))
	hexH := hex.EncodeToString(h[:])
	_ = db.CreatePasswordResetToken(hexH, uid, time.Now().Add(time.Hour))
	// weak password should fail
	body,_ := json.Marshal(map[string]string{"token": tok, "password":"short"})
	req:=httptest.NewRequest("POST","/api/auth/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type","application/json")
	w:=httptest.NewRecorder()
	ResetPassword(w,req)
	if w.Code==200 { t.Fatal("weak should fail") }
	// success
	body,_ = json.Marshal(map[string]string{"token": tok, "password":"newpassword123"})
	req=httptest.NewRequest("POST","/api/auth/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type","application/json")
	w=httptest.NewRecorder()
	ResetPassword(w,req)
	if w.Code!=200 { t.Fatalf("reset %d %s", w.Code, w.Body.String()) }
	// reused should fail
	req=httptest.NewRequest("POST","/api/auth/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type","application/json")
	w=httptest.NewRecorder()
	ResetPassword(w,req)
	if w.Code==200 { t.Fatal("reuse should fail") }
	// login with new password
	u,_:=db.GetUserByUsername("admin@example.com")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("newpassword123"))!=nil { t.Fatal("password not updated") }
}

func TestExpiredToken(t *testing.T) {
	setupTestDB(t)
	DefaultMailer = &stubMailer{}
	hash,_ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	uid,_ := db.CreateUser("a@b.com", string(hash), "admin")
	h := sha256.Sum256([]byte("expiredtok"))
	hexH := hex.EncodeToString(h[:])
	_ = db.CreatePasswordResetToken(hexH, uid, time.Now().Add(-time.Hour))
	body,_ := json.Marshal(map[string]string{"token":"expiredtok","password":"newpassword123"})
	req:=httptest.NewRequest("POST","/api/auth/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type","application/json")
	w:=httptest.NewRecorder()
	ResetPassword(w,req)
	if w.Code==200 { t.Fatal("expired should fail") }
}

func TestValidateToken(t *testing.T){
	setupTestDB(t)
	hash,_ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	uid,_ := db.CreateUser("a@b.com", string(hash), "admin")
	h := sha256.Sum256([]byte("validtok"))
	hexH := hex.EncodeToString(h[:])
	_ = db.CreatePasswordResetToken(hexH, uid, time.Now().Add(time.Hour))
	req:=httptest.NewRequest("GET","/api/auth/reset/validate?token=validtok", nil)
	w:=httptest.NewRecorder()
	ValidateResetToken(w,req)
	if w.Code!=200 { t.Fatalf("validate %d", w.Code) }
	req=httptest.NewRequest("GET","/api/auth/reset/validate?token=bad", nil)
	w=httptest.NewRecorder()
	ValidateResetToken(w,req)
	if w.Code==200 { t.Fatal("bad should fail") }
}
