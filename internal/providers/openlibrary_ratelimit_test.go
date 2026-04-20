package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestTokenBucketBasicFlow verifies that tokens are consumed and refilled.
func TestTokenBucketBasicFlow(t *testing.T) {
	tb := newTokenBucket(10, 2) // 10 tokens/sec, burst of 2
	ctx := context.Background()

	// Should get 2 tokens immediately (burst)
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("first token: %v", err)
	}
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("second token: %v", err)
	}
	// Third token should require waiting ~100ms (1/10 sec)
	start := time.Now()
	if err := tb.Wait(ctx); err != nil {
		t.Fatalf("third token: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < 50*time.Millisecond {
		t.Errorf("third token arrived too quickly: %v", elapsed)
	}
}

// TestTokenBucketRespectsContext verifies Wait returns on cancelled context.
func TestTokenBucketRespectsContext(t *testing.T) {
	tb := newTokenBucket(0.1, 0) // very slow refill, no burst
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := tb.Wait(ctx)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

// TestTokenBucketConcurrentAccess verifies the limiter is safe for concurrent use.
func TestTokenBucketConcurrentAccess(t *testing.T) {
	tb := newTokenBucket(100, 10) // fast enough for testing
	ctx := context.Background()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			_ = tb.Wait(ctx)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for concurrent token consumers")
		}
	}
}

// TestGetJSONRetries429WithBackoff verifies that getJSON retries on HTTP 429
// and eventually succeeds.
func TestGetJSONRetries429WithBackoff(t *testing.T) {
	// Use a very fast rate limiter for testing so waits are minimal.
	origLimiter := olRateLimiter
	olRateLimiter = newTokenBucket(1000, 100)
	defer func() { olRateLimiter = origLimiter }()

	var attempts int32
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			// First two attempts return 429
			return &http.Response{
				StatusCode: 429,
				Status:     "429 Too Many Requests",
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
				Header:     make(http.Header),
			}, nil
		}
		// Third attempt succeeds
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"docs":[]}`)),
			Header:     make(http.Header),
		}, nil
	})

	var out OLResp
	err := ol.getJSON(context.Background(), "https://openlibrary.org/search.json?q=test", "search", &out)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("expected 3 attempts (2 x 429 + 1 success), got %d", got)
	}
}

// TestGetJSONGivesUpAfterMaxRetries verifies that getJSON stops retrying
// after olMaxRetries 429 responses.
func TestGetJSONGivesUpAfterMaxRetries(t *testing.T) {
	origLimiter := olRateLimiter
	olRateLimiter = newTokenBucket(1000, 100)
	defer func() { olRateLimiter = origLimiter }()

	var attempts int32
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&attempts, 1)
		return &http.Response{
			StatusCode: 429,
			Status:     "429 Too Many Requests",
			Body:       io.NopCloser(strings.NewReader(`too many requests`)),
			Header:     make(http.Header),
		}, nil
	})

	var out OLResp
	err := ol.getJSON(context.Background(), "https://openlibrary.org/search.json?q=test", "search", &out)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention 429: %v", err)
	}
	// olMaxRetries + 1 initial attempt
	if got := atomic.LoadInt32(&attempts); got != int32(olMaxRetries+1) {
		t.Errorf("expected %d total attempts, got %d", olMaxRetries+1, got)
	}
}

// TestGetJSON429RespectsContextCancellation verifies that 429 retry loop
// stops when the context is cancelled.
func TestGetJSON429RespectsContextCancellation(t *testing.T) {
	origLimiter := olRateLimiter
	olRateLimiter = newTokenBucket(1000, 100)
	defer func() { olRateLimiter = origLimiter }()

	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 429,
			Status:     "429 Too Many Requests",
			Body:       io.NopCloser(strings.NewReader(`rate limited`)),
			Header:     make(http.Header),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var out OLResp
	err := ol.getJSON(ctx, "https://openlibrary.org/search.json?q=test", "search", &out)
	if err == nil {
		t.Fatal("expected context error")
	}
}

// TestOlBackoffExponential verifies the backoff durations increase.
func TestOlBackoffExponential(t *testing.T) {
	d0 := olBackoff(0)
	d1 := olBackoff(1)
	d2 := olBackoff(2)

	// Backoff(0) = 2s + jitter(0..2s), so between 2s and 4s
	if d0 < 2*time.Second || d0 > 4*time.Second {
		t.Errorf("backoff(0) = %v, want [2s, 4s]", d0)
	}
	// Backoff(1) = 4s + jitter(0..2s), so between 4s and 6s
	if d1 < 4*time.Second || d1 > 6*time.Second {
		t.Errorf("backoff(1) = %v, want [4s, 6s]", d1)
	}
	// Backoff(2) = 8s + jitter(0..2s), so between 8s and 10s
	if d2 < 8*time.Second || d2 > 10*time.Second {
		t.Errorf("backoff(2) = %v, want [8s, 10s]", d2)
	}
}

// TestRateLimiterPreventsRequestBursts validates that the rate limiter
// actually throttles request throughput.
func TestRateLimiterPreventsRequestBursts(t *testing.T) {
	tb := newTokenBucket(5, 2) // 5 tokens/sec, burst of 2
	ctx := context.Background()

	// Drain the burst
	_ = tb.Wait(ctx)
	_ = tb.Wait(ctx)

	// Now 5 more tokens should take ~1 second
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := tb.Wait(ctx); err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	// 5 tokens at 5/sec = ~1 second (allow some tolerance)
	if elapsed < 800*time.Millisecond {
		t.Errorf("5 tokens arrived in %v, expected ~1s", elapsed)
	}
}

// TestOpenLibraryContactEmailUserAgent verifies the OPENLIBRARY_CONTACT_EMAIL
// env var is incorporated into the User-Agent header.
func TestOpenLibraryContactEmailUserAgent(t *testing.T) {
	t.Setenv("OPENLIBRARY_USER_AGENT", "")
	t.Setenv("OPENLIBRARY_CONTACT_EMAIL", "admin@example.com")

	ua := openLibraryUserAgent()
	if !strings.Contains(ua, "mailto:admin@example.com") {
		t.Errorf("User-Agent should contain email: %q", ua)
	}
	if !strings.Contains(ua, "Scriptorum/1.0") {
		t.Errorf("User-Agent should contain app name: %q", ua)
	}
}

// TestGetJSONUsesRateLimiter verifies that getJSON calls go through the
// global rate limiter (requests should be throttled, not instantaneous).
func TestGetJSONUsesRateLimiter(t *testing.T) {
	// Set a very slow rate limiter to prove it's being used.
	origLimiter := olRateLimiter
	olRateLimiter = newTokenBucket(2, 1) // 2/sec, burst 1
	defer func() { olRateLimiter = origLimiter }()

	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"docs":[]}`)), Header: make(http.Header)}, nil
	})

	// First call uses the burst token (instant). Second must wait.
	var out OLResp
	_ = ol.getJSON(context.Background(), "https://openlibrary.org/search.json?q=a", "search", &out)

	start := time.Now()
	_ = ol.getJSON(context.Background(), "https://openlibrary.org/search.json?q=b", "search", &out)
	elapsed := time.Since(start)

	// At 2/sec with burst used up, second call should wait ~500ms
	if elapsed < 300*time.Millisecond {
		t.Errorf("second call completed too quickly (%v), rate limiter may not be engaged", elapsed)
	}
}
