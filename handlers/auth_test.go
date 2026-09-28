package handlers

import (
	"errors"
	"path/filepath"
	"testing"

	"meetup/db"
)

func TestProvisionOIDCUser(t *testing.T) {
	if err := db.Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	defer db.Close()

	// First user ever becomes admin.
	u, err := provisionOIDCUser("first@example.com")
	if err != nil {
		t.Fatalf("first user: %v", err)
	}
	if u == nil || u.Role != "admin" || u.Username != "first@example.com" {
		t.Fatalf("first user: got %+v, want admin first@example.com", u)
	}

	// Known user is returned unchanged.
	again, err := provisionOIDCUser("first@example.com")
	if err != nil {
		t.Fatalf("known user: %v", err)
	}
	if again == nil || again.ID != u.ID {
		t.Fatalf("known user: got %+v, want id %d", again, u.ID)
	}

	// Unknown user is rejected when not allowlisted.
	if _, err := provisionOIDCUser("stranger@example.com"); !errors.Is(err, errOIDCUnknownUser) {
		t.Fatalf("stranger: got %v, want errOIDCUnknownUser", err)
	}

	// Allowlisted user is provisioned (case-insensitive, whitespace-tolerant).
	t.Setenv("OIDC_ADMIN_EMAILS", " Second@Example.com , third@example.com ")
	u2, err := provisionOIDCUser("second@example.com")
	if err != nil {
		t.Fatalf("allowlisted user: %v", err)
	}
	if u2 == nil || u2.Role != "admin" || u2.Username != "second@example.com" {
		t.Fatalf("allowlisted user: got %+v, want admin second@example.com", u2)
	}

	// Still rejected: not on the list.
	if _, err := provisionOIDCUser("fourth@example.com"); !errors.Is(err, errOIDCUnknownUser) {
		t.Fatalf("non-allowlisted: got %v, want errOIDCUnknownUser", err)
	}

	// Empty email is invalid.
	if _, err := provisionOIDCUser("   "); err == nil || errors.Is(err, errOIDCUnknownUser) {
		t.Fatalf("empty email: got %v, want missing-email error", err)
	}
}
