package token_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lifelog-api/internal/token"

	"github.com/google/uuid"
)

func countingJWKS(t *testing.T, signer *token.Signer, failures int64, delay time.Duration) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	hits := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		time.Sleep(delay)
		if n <= failures {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(signer.JWKS())
	}))
	t.Cleanup(server.Close)
	return server, hits
}

func parseWave(t *testing.T, verifier *token.Verifier, raw string, n int) {
	t.Helper()

	var wg sync.WaitGroup
	var accepted atomic.Int64
	for range n {
		wg.Go(func() {
			_, err := verifier.Parse(context.Background(), raw)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, token.ErrUnknownKey) {
				t.Errorf("Parse: %v, want ErrUnknownKey", err)
			}
		})
	}
	wg.Wait()
	if accepted.Load() != 0 {
		t.Errorf("%d tokens of an unknown key accepted", accepted.Load())
	}
}

func TestUnknownKidFetchesAreCoalescedAndSpacedOut(t *testing.T) {
	t.Parallel()

	known := newSigner(t, time.Hour)
	server, hits := countingJWKS(t, known, 0, 20*time.Millisecond)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)
	const interval = 300 * time.Millisecond
	verifier.SetUnknownKidInterval(interval)

	foreign, _, err := newSigner(t, time.Hour).Issue(uuid.New(), "user", "Тест", uuid.New())
	if err != nil {
		t.Fatal(err)
	}

	parseWave(t, verifier, foreign, 50)
	if got := hits.Load(); got != 1 {
		t.Fatalf("first wave: %d fetches, want 1", got)
	}

	parseWave(t, verifier, foreign, 50)
	if got := hits.Load(); got != 1 {
		t.Fatalf("second wave within the interval: %d fetches, want still 1", got)
	}

	time.Sleep(interval + 50*time.Millisecond)
	parseWave(t, verifier, foreign, 50)
	if got := hits.Load(); got != 2 {
		t.Fatalf("wave after the interval: %d fetches in total, want 2", got)
	}
}

func TestKnownKidIsNotThrottled(t *testing.T) {
	t.Parallel()

	known := newSigner(t, time.Hour)
	server, _ := countingJWKS(t, known, 0, 0)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)

	foreign, _, _ := newSigner(t, time.Hour).Issue(uuid.New(), "user", "Тест", uuid.New())
	if _, err := verifier.Parse(context.Background(), foreign); !errors.Is(err, token.ErrUnknownKey) {
		t.Fatalf("foreign token: %v", err)
	}
	raw, _, _ := known.Issue(uuid.New(), "user", "Тест", uuid.New())
	if _, err := verifier.Parse(context.Background(), raw); err != nil {
		t.Fatalf("known token right after a throttling fetch: %v", err)
	}
}

func TestWarmRetriesUntilKeysLoad(t *testing.T) {
	t.Parallel()

	known := newSigner(t, time.Hour)
	server, hits := countingJWKS(t, known, 2, 0)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)
	verifier.SetWarmDelays(10*time.Millisecond, 40*time.Millisecond)

	done := make(chan struct{})
	go func() {
		verifier.Warm(context.Background(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Warm did not finish")
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("%d fetches, want 3 (two failures and a success)", got)
	}

	raw, _, _ := known.Issue(uuid.New(), "user", "Тест", uuid.New())
	if _, err := verifier.Parse(context.Background(), raw); err != nil {
		t.Fatalf("Parse after Warm: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("Parse after Warm fetched again: %d fetches", got)
	}
}

func TestWarmStopsOnCancel(t *testing.T) {
	t.Parallel()

	known := newSigner(t, time.Hour)
	server, _ := countingJWKS(t, known, 1<<30, 0)
	verifier := token.NewVerifier(server.URL, "lifelog-auth", "lifelog", time.Hour)
	verifier.SetWarmDelays(10*time.Millisecond, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		verifier.Warm(ctx, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Warm ignored cancellation")
	}
}
