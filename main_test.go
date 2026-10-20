package main

import (
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"no header", "", "meetup.example", true},
		{"same origin", "https://meetup.example", "meetup.example", true},
		{"cross origin", "https://evil.example", "meetup.example", false},
		{"null origin", "null", "meetup.example", false},
		{"malformed", "not-a-url", "meetup.example", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://"+tc.host+"/api/events/x/answers", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := sameOrigin(req); got != tc.want {
				t.Fatalf("sameOrigin = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSameOriginHonoursForwardedHost(t *testing.T) {
	req := httptest.NewRequest("POST", "http://internal:8080/api/events/x/answers", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Host", "meetup.example")
	req.Header.Set("Origin", "https://meetup.example")
	if !sameOrigin(req) {
		t.Fatal("expected forwarded host to match the browser origin")
	}
}
