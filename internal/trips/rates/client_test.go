package rates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientLatestParsesRatesKeepingScale(t *testing.T) {
	api := newFakeAPI(t)

	rates, err := api.client().Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	if got := api.lastPath.Load(); got != "/v6/"+testKey+"/latest/USD" {
		t.Errorf("path = %v", got)
	}
	want := map[string]string{"USD": "1", "AED": "3.6725", "THB": "33.0608", "JPY": "146.2100", "VND": "26150.5"}
	if len(rates) != len(want) {
		t.Fatalf("got %d rates, want %d: %v", len(rates), len(want), rates)
	}
	for code, text := range want {
		rate, ok := rates[code]
		if !ok {
			t.Errorf("missing %s", code)
			continue
		}
		if rate.String() != text {
			t.Errorf("%s = %s, want %s", code, rate.String(), text)
		}
	}
}

func TestClientLatestTrimsBaseURLSlash(t *testing.T) {
	api := newFakeAPI(t)
	client := NewClient(api.server.URL+"/", "6", testKey, api.server.Client())

	if _, err := client.Latest(context.Background()); err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got := api.lastPath.Load(); got != "/v6/"+testKey+"/latest/USD" {
		t.Errorf("path = %v", got)
	}
}

func TestClientLatestErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"server error", http.StatusInternalServerError, `oops ` + testKey, "provider answered 500"},
		{"invalid key", http.StatusForbidden, `{"result":"error","error-type":"invalid-key"}`, "provider answered 403 (error-type invalid-key)"},
		{"error inside 200", http.StatusOK, `{"result":"error","error-type":"quota-reached"}`, "no conversion_rates (error-type quota-reached)"},
		{"null rates", http.StatusOK, `{"result":"success","conversion_rates":null}`, "no conversion_rates"},
		{"empty rates", http.StatusOK, `{"result":"success","conversion_rates":{}}`, "no conversion_rates"},
		{"bad json", http.StatusOK, `<html>` + testKey, "invalid API response"},
		{"non-numeric rate", http.StatusOK, `{"conversion_rates":{"USD":"abc"}}`, "invalid API response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := NewClient(server.URL, "6", testKey, server.Client()).Latest(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks the key: %q", err)
			}
		})
	}
}

func TestClientLatestNetworkErrorHidesKey(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close()

	_, err := NewClient(url, "6", testKey, nil).Latest(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks the key: %q", err)
	}
	if !strings.Contains(err.Error(), "/v6/***/latest/USD") {
		t.Errorf("error should keep the redacted URL: %q", err)
	}
}

func TestClientLatestCanceledContext(t *testing.T) {
	api := newFakeAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := api.client().Latest(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled in the chain", err)
	}
	if err != nil && strings.Contains(err.Error(), testKey) {
		t.Errorf("error leaks the key: %q", err)
	}
}

func TestClientLatestWithoutKeyMakesNoRequest(t *testing.T) {
	api := newFakeAPI(t)

	_, err := NewClient(api.server.URL, "6", "", api.server.Client()).Latest(context.Background())
	if !errors.Is(err, errNotConfigured) {
		t.Errorf("err = %v, want errNotConfigured", err)
	}
	if api.calls.Load() != 0 {
		t.Errorf("made %d requests", api.calls.Load())
	}
}
