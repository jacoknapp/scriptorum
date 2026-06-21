package httpapi

import (
	"testing"
	"time"
)

func TestCSRFManagerCleanupRemovesExpiredTokens(t *testing.T) {
	c := newCSRFManager()
	token := c.generateToken("user:alice")

	// Backdate the token past its TTL so cleanup evicts it.
	c.mu.Lock()
	info := c.tokens[token]
	info.created = time.Now().Add(-csrfTokenTTL - time.Minute)
	c.tokens[token] = info
	c.mu.Unlock()

	c.cleanup()

	if c.validateToken(token, "user:alice") {
		t.Fatal("expected expired token to be removed by cleanup")
	}
}

func TestRateLimiterCleanupEvictsStaleKeys(t *testing.T) {
	rl := newRateLimiter()
	if !rl.allow("1.2.3.4:/login", 10, time.Minute) {
		t.Fatal("expected first request to be allowed")
	}

	rl.mu.Lock()
	rl.requests["1.2.3.4:/login"] = []time.Time{time.Now().Add(-time.Hour)}
	rl.mu.Unlock()

	rl.cleanup(15 * time.Minute)

	rl.mu.RLock()
	_, exists := rl.requests["1.2.3.4:/login"]
	rl.mu.RUnlock()
	if exists {
		t.Fatal("expected stale rate-limit key to be evicted by cleanup")
	}
}
