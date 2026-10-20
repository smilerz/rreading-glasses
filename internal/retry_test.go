package internal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func retryClient(attempts int) *http.Client {
	return &http.Client{Transport: RetryTransport{
		RoundTripper: http.DefaultTransport,
		Attempts:     attempts,
		Backoff:      time.Millisecond,
		MaxWait:      10 * time.Millisecond,
	}}
}

func TestRetryTransportRetriesRateLimits(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		if hits.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	resp, err := retryClient(5).Post(srv.URL, "application/json", strings.NewReader(`{"query":"q"}`))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.EqualValues(t, 3, hits.Load())
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{`{"query":"q"}`, `{"query":"q"}`, `{"query":"q"}`}, bodies, "the body is resent on every attempt")
}

func TestRetryTransportGivesUp(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	resp, err := retryClient(3).Get(srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "the last response is returned unchanged")
	assert.EqualValues(t, 3, hits.Load())
}

func TestRetryTransportLeavesOtherStatusesAlone(t *testing.T) {
	t.Parallel()

	for _, code := range []int{http.StatusOK, http.StatusNotFound, http.StatusUnauthorized, http.StatusInternalServerError} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.WriteHeader(code)
		}))

		resp, err := retryClient(5).Get(srv.URL)
		require.NoError(t, err)
		_ = resp.Body.Close()
		srv.Close()

		assert.Equal(t, code, resp.StatusCode)
		assert.EqualValues(t, 1, hits.Load(), "status %d must not be retried", code)
	}
}

func TestRetryTransportDisabled(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	for _, attempts := range []int{0, 1} {
		hits.Store(0)
		resp, err := retryClient(attempts).Get(srv.URL)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.EqualValues(t, 1, hits.Load(), "attempts=%d", attempts)
	}
}

func TestRetryTransportStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: RetryTransport{RoundTripper: http.DefaultTransport, Attempts: 5, MaxWait: time.Minute}}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = client.Do(req)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "a cancelled context ends the wait")
}
