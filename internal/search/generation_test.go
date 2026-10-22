package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeClient(t *testing.T, handler http.HandlerFunc, opts Options) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	opts.Addresses = []string{server.URL}
	opts.IndexPrefix = "test"
	client, err := NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestEnsureIndicesCreatesVersionedIndexBehindAlias(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		created = map[string]map[string]any{}
	)
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodHead:

			if r.URL.Path != "/test_films" {
				w.WriteHeader(http.StatusNotFound)
			}
		case http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			created[strings.TrimPrefix(r.URL.Path, "/")] = body
			fmt.Fprint(w, `{"acknowledged":true}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}, Options{})
	client.now = func() time.Time { return time.Date(2026, 10, 19, 23, 35, 0, 0, time.FixedZone("MSK", 3*3600)) }

	if err := client.EnsureIndices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 {
		t.Fatalf("created %v, want only the people index", created)
	}
	body, ok := created["test_people_20261019t203500"]
	if !ok {
		t.Fatalf("created %v, want a UTC-stamped name", created)
	}
	aliases, _ := body["aliases"].(map[string]any)
	if _, ok := aliases["test_people"]; !ok {
		t.Errorf("alias missing from the create body: %v", body["aliases"])
	}
	if _, ok := body["mappings"]; !ok {
		t.Error("mapping missing from the create body")
	}
}

func TestBulkReturnsRefusedDocuments(t *testing.T) {
	t.Parallel()

	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, `{"errors":true,"items":[
			{"index":{"_id":"1","status":201}},
			{"index":{"_id":"2","status":400,"error":{"type":"mapper_parsing_exception","reason":"bad year"}}}]}`)
	}, Options{})

	failed, err := client.IndexFilms(context.Background(), []FilmDoc{{ID: 1}, {ID: 2}})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []BulkFailure{{ID: "2", Type: "mapper_parsing_exception", Reason: "bad year"}}
	if fmt.Sprint(failed) != fmt.Sprint(want) {
		t.Errorf("failed = %v", failed)
	}
}

func TestBulkTransportFailureIsAnError(t *testing.T) {
	t.Parallel()

	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"type":"boom","reason":"down"}}`)
	}, Options{})

	if _, err := client.IndexPeople(context.Background(), []PersonDoc{{ID: 1}}); err == nil {
		t.Fatal("a failed bulk request must be an error")
	}
}

func TestRatingBulkSkipsMissingDocuments(t *testing.T) {
	t.Parallel()

	answer := `{"errors":true,"items":[{"update":{"_id":"1","status":404,"error":{"type":"document_missing_exception","reason":"missing"}}}]}`
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		fmt.Fprint(w, answer)
	}, Options{})
	if err := client.UpdateFilmRatings(context.Background(), []LocalRating{{FilmID: 1, Rating: 7, Votes: 1}}); err != nil {
		t.Errorf("missing document: %v", err)
	}

	answer = `{"errors":true,"items":[{"update":{"_id":"1","status":400,"error":{"type":"x","reason":"y"}}}]}`
	if err := client.UpdateFilmRatings(context.Background(), []LocalRating{{FilmID: 1, Rating: 7, Votes: 1}}); err == nil {
		t.Error("a refused update must be an error")
	}
}

func slowCluster(t *testing.T, delay time.Duration, timeout time.Duration) *Client {
	t.Helper()
	release := make(chan struct{})
	client := fakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"hits":{"total":{"value":0},"hits":[]}}`)
	}, Options{ReadTimeout: timeout})

	t.Cleanup(func() { close(release) })
	return client
}

func TestReadDeadlineIsErrTimeout(t *testing.T) {
	t.Parallel()

	client := slowCluster(t, 5*time.Second, 50*time.Millisecond)
	started := time.Now()
	_, err := client.SearchFilms(context.Background(), Query{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("read took %s past a 50ms deadline", elapsed)
	}

	if _, err := client.FilmBounds(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Errorf("bounds: err = %v, want ErrTimeout", err)
	}
}

func TestCancelledCallerIsNotErrTimeout(t *testing.T) {
	t.Parallel()

	client := slowCluster(t, 5*time.Second, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := client.SearchPeople(ctx, Query{Text: "нолан"})
	if err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want a plain context error", err)
	}
}

func TestReadWithinDeadlineSucceeds(t *testing.T) {
	t.Parallel()

	client := slowCluster(t, 0, time.Second)
	if _, err := client.SearchFilms(context.Background(), Query{}); err != nil {
		t.Fatal(err)
	}
}
