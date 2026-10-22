package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallSendsAndDecodes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/x" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get(InternalTokenHeader) != "secret" || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		_, _ = w.Write([]byte(`{"greeting":"hi ` + in.Name + `"}`))
	}))
	defer server.Close()

	var out struct {
		Greeting string `json:"greeting"`
	}
	err := Call(context.Background(), server.Client(), http.MethodPost, server.URL+"/internal/x", "secret",
		map[string]string{"name": "ann"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Greeting != "hi ann" {
		t.Errorf("greeting %q", out.Greeting)
	}
}

func TestCallWithoutBodySetsNoContentType(t *testing.T) {
	t.Parallel()
	var contentType, token string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		token = r.Header.Get(InternalTokenHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := Call(context.Background(), server.Client(), http.MethodGet, server.URL, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if contentType != "" || token != "" {
		t.Errorf("content type %q, token %q", contentType, token)
	}
}

func TestCallStatuses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		status int
		ok     []int
		fail   bool
	}{
		{"any 2xx by default", http.StatusNoContent, nil, false},
		{"3xx is not 2xx", http.StatusNotModified, nil, true},
		{"listed status", http.StatusAccepted, []int{http.StatusAccepted}, false},
		{"200 when only 202 is accepted", http.StatusOK, []int{http.StatusAccepted}, true},
		{"server error", http.StatusBadGateway, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			err := Call(context.Background(), server.Client(), http.MethodGet, server.URL, "t", nil, nil, tc.ok...)
			if tc.fail {
				statusErr, ok := errors.AsType[*StatusError](err)
				if !ok || statusErr.Status != tc.status {
					t.Fatalf("want StatusError %d, got %v", tc.status, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCallCapsErrorBodyAndReusesConnection(t *testing.T) {
	t.Parallel()
	var conns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 10_000)))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	client := Internal(5 * time.Second)
	for range 3 {
		err := Call(context.Background(), client, http.MethodGet, server.URL, "t", nil, nil)
		statusErr, ok := errors.AsType[*StatusError](err)
		if !ok {
			t.Fatalf("want StatusError, got %v", err)
		}
		if len(statusErr.Body) != errorBodyLimit {
			t.Errorf("error body %d bytes, want %d", len(statusErr.Body), errorBodyLimit)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("%d connections, want 1: the body was not drained", n)
	}
}

func TestCallDecodeError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()
	var out map[string]any
	err := Call(context.Background(), server.Client(), http.MethodGet, server.URL, "t", nil, &out)
	if err == nil {
		t.Fatal("bad JSON must be an error")
	}
	if _, ok := errors.AsType[*StatusError](err); ok {
		t.Error("a decode failure is not a status error")
	}
}

func TestInternalIdleTimeoutBelowServers(t *testing.T) {
	t.Parallel()
	client := Internal(3 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", client.Transport)
	}
	if transport.IdleConnTimeout <= 0 || transport.IdleConnTimeout >= time.Minute {
		t.Fatalf("IdleConnTimeout %v, want under a minute", transport.IdleConnTimeout)
	}
	if client.Timeout != 3*time.Second {
		t.Fatalf("Timeout %v", client.Timeout)
	}
}
