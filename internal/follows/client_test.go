package follows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAuth struct {
	calls      []string
	tokens     []string
	candidates []uuid.UUID
	answer     []uuid.UUID
	status     int
}

func (f *fakeAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	f.tokens = append(f.tokens, r.Header.Get("X-Internal-Token"))
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/internal/follows/check":
		var in struct {
			Candidates []uuid.UUID `json:"candidates"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.candidates = in.Candidates
		_ = json.NewEncoder(w).Encode(map[string]any{"followers": f.answer})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/follows/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"following": f.answer})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/internal/blocks/"):
		_ = json.NewEncoder(w).Encode(map[string]any{"blocked": f.answer})
	default:
		http.NotFound(w, r)
	}
}

func newFakeAuth(t *testing.T) (*fakeAuth, *Client) {
	t.Helper()
	fake := &fakeAuth{answer: []uuid.UUID{uuid.New()}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	return fake, NewClient(server.URL+"/", "secret", 2*time.Second)
}

func TestClientCallsTheRightEndpoints(t *testing.T) {
	fake, client := newFakeAuth(t)
	user, target := uuid.New(), uuid.New()
	candidates := []uuid.UUID{uuid.New(), uuid.New()}

	if got, err := client.Following(context.Background(), user); err != nil || !slices.Equal(got, fake.answer) {
		t.Fatalf("Following = %v, %v", got, err)
	}
	if got, err := client.Blocked(context.Background(), user); err != nil || !slices.Equal(got, fake.answer) {
		t.Fatalf("Blocked = %v, %v", got, err)
	}
	if got, err := client.FollowersAmong(context.Background(), target, candidates); err != nil || !slices.Equal(got, fake.answer) {
		t.Fatalf("FollowersAmong = %v, %v", got, err)
	}

	want := []string{
		"GET /internal/follows/" + user.String(),
		"GET /internal/blocks/" + user.String(),
		"POST /internal/follows/check",
	}
	if !slices.Equal(fake.calls, want) {
		t.Fatalf("calls %v, want %v", fake.calls, want)
	}
	if !slices.Equal(fake.candidates, candidates) {
		t.Fatalf("candidates sent %v, want %v", fake.candidates, candidates)
	}
	for _, token := range fake.tokens {
		if token != "secret" {
			t.Fatalf("service token %q, want it on every call", token)
		}
	}
}

func TestClientSkipsCallWithoutCandidates(t *testing.T) {
	fake, client := newFakeAuth(t)
	got, err := client.FollowersAmong(context.Background(), uuid.New(), nil)
	if err != nil || got != nil || len(fake.calls) != 0 {
		t.Fatalf("got %v, %v after %d calls; want no call", got, err, len(fake.calls))
	}
}

func TestClientFailuresAreUnavailable(t *testing.T) {
	fake, client := newFakeAuth(t)
	fake.status = http.StatusBadGateway
	if _, err := client.Following(context.Background(), uuid.New()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("502: %v", err)
	}

	down := NewClient("http://127.0.0.1:1", "secret", time.Second)
	if _, err := down.Blocked(context.Background(), uuid.New()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("refused connection: %v", err)
	}
}
