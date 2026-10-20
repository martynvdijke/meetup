package handlers

import (
	"net/http"
	"sync"
	"time"
)

const anonymousWriteLimit = 30

var (
	anonWriteMu   sync.Mutex
	anonWriteHits = map[string][]time.Time{}
)

// AllowAnonymousWrite applies a coarse per-IP budget to unauthenticated write
// endpoints (answers, Q&A, votes). It is intentionally separate from the
// password-reset limiter so heavy audience participation cannot lock an admin
// out of account recovery.
func AllowAnonymousWrite(r *http.Request) bool {
	ip := clientIP(r)
	now := time.Now()
	anonWriteMu.Lock()
	defer anonWriteMu.Unlock()
	hits := anonWriteHits[ip]
	keep := hits[:0]
	for _, t := range hits {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) >= anonymousWriteLimit {
		anonWriteHits[ip] = keep
		return false
	}
	anonWriteHits[ip] = append(keep, now)
	return true
}
