package internal

import (
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// RetryTransport retries requests that fail with 429 or a transient 502, 503
// or 504, waiting for Retry-After when the server sends one and backing off
// exponentially otherwise. Without it a single rate-limit response makes the
// caller give up, so an author can end up with no works and have that cached.
type RetryTransport struct {
	http.RoundTripper

	// Attempts is the total number of tries, including the first. Values
	// below 2 disable retries.
	Attempts int
	// Backoff is the first wait; it doubles on each retry. Defaults to 2s.
	Backoff time.Duration
	// MaxWait caps a single wait, including a long Retry-After. Defaults to 20s.
	MaxWait time.Duration
}

// RoundTrip sends the request, retrying retryable responses. The last response
// is returned unchanged once attempts run out.
func (t RetryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	backoff := t.Backoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}
	maxWait := t.MaxWait
	if maxWait <= 0 {
		maxWait = 20 * time.Second
	}

	for attempt := 1; ; attempt++ {
		req := r
		if attempt > 1 && r.GetBody != nil {
			body, err := r.GetBody()
			if err != nil {
				return nil, err
			}
			req = r.Clone(r.Context())
			req.Body = body
		}

		resp, err := t.RoundTripper.RoundTrip(req)

		// A request body we can't replay can only be sent once.
		replayable := r.Body == nil || r.Body == http.NoBody || r.GetBody != nil
		if err != nil || !retryableStatus(resp.StatusCode) || attempt >= t.Attempts || !replayable {
			return resp, err
		}

		wait := backoff
		if ra := retryAfter(resp); ra > 0 {
			wait = ra
		}
		wait = min(wait, maxWait)
		wait += time.Duration(rand.Int63n(int64(wait)/4 + 1)) // Jitter.
		backoff *= 2

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		select {
		case <-time.After(wait):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

// retryAfter parses a Retry-After header given in seconds.
func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
