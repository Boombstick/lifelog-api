package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlowResponseIsRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(300 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"genres":[{"id":1,"name":"драма"}]}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test", RPS: 100, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	genres, err := client.Genres(context.Background(), "ru-RU")
	if err != nil {
		t.Fatalf("a slow first response must be retried, got %v", err)
	}
	if len(genres) != 1 || calls.Load() != 2 {
		t.Errorf("genres=%d calls=%d, want 1 genre after 2 calls", len(genres), calls.Load())
	}
}

func TestCallerDeadlineIsFatal(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test", RPS: 100, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := client.Genres(ctx, "ru-RU"); err == nil {
		t.Fatal("a cancelled caller must fail")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("gave up after %v, expected no backoff on a dead context", elapsed)
	}
	if calls.Load() != 1 {
		t.Errorf("calls=%d, want a single attempt", calls.Load())
	}
}

func TestClientErrorIsNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status_code":5}`))
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test", RPS: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SearchMulti(context.Background(), "x", 1)
	if err == nil || !Unavailable(err) || calls.Load() != 1 {
		t.Errorf("err=%v calls=%d, want one call and an outage error", err, calls.Load())
	}
}
