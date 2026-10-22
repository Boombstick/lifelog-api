package safedial

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func ips(values ...string) []net.IP {
	out := make([]net.IP, 0, len(values))
	for _, v := range values {
		out = append(out, net.ParseIP(v))
	}
	return out
}

func TestUsableDropsLoopbackAndPrefersIPv4(t *testing.T) {
	got := Usable(ips("::1", "2600:9000::1", "127.0.0.1", "3.174.2.25", "0.0.0.0"))
	if len(got) != 2 || got[0].String() != "3.174.2.25" || got[1].String() != "2600:9000::1" {
		t.Fatalf("usable = %v", got)
	}
}

func lookupOf(values ...string) Lookup {
	return func(context.Context, string) ([]net.IP, error) { return ips(values...), nil }
}

func TestLoopbackAnswerFallsBack(t *testing.T) {
	d := &Dialer{
		Resolve:   lookupOf("::1", "127.0.0.1"),
		Fallbacks: []Named{{Name: "doh", Lookup: lookupOf("3.174.2.25")}},
	}
	got, err := d.resolve(context.Background(), "api.themoviedb.org")
	if err != nil || len(got) != 1 || got[0].String() != "3.174.2.25" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
}

func TestLoopbackFromFirstFallbackTriesNext(t *testing.T) {
	d := &Dialer{
		Resolve: lookupOf("127.0.0.1"),
		Fallbacks: []Named{
			{Name: "first", Lookup: lookupOf("127.0.0.1")},
			{Name: "second", Lookup: func(context.Context, string) ([]net.IP, error) { return nil, errors.New("timeout") }},
			{Name: "third", Lookup: lookupOf("143.204.238.8")},
		},
	}
	got, err := d.resolve(context.Background(), "api.themoviedb.org")
	if err != nil || len(got) != 1 || got[0].String() != "143.204.238.8" {
		t.Fatalf("resolve = %v, %v", got, err)
	}
}

func TestAllResolversLoopbackIsUnresolved(t *testing.T) {
	d := &Dialer{
		Resolve:   lookupOf("::1"),
		Fallbacks: []Named{{Name: "doh", Lookup: lookupOf("127.0.0.1")}},
	}
	_, err := d.resolve(context.Background(), "api.themoviedb.org")
	if !errors.Is(err, ErrUnresolved) || !strings.Contains(err.Error(), "адрес не получен") || !strings.Contains(err.Error(), "doh") {
		t.Fatalf("err = %v", err)
	}
}

func TestSystemErrorStillTriesFallbacks(t *testing.T) {
	d := &Dialer{
		Resolve:   func(context.Context, string) ([]net.IP, error) { return nil, errors.New("no such host") },
		Fallbacks: []Named{{Name: "doh", Lookup: lookupOf("143.204.238.8")}},
	}
	if got, err := d.resolve(context.Background(), "api.themoviedb.org"); err != nil || len(got) != 1 {
		t.Fatalf("resolve = %v, %v", got, err)
	}
}

func TestHonestAnswerSkipsFallback(t *testing.T) {
	called := false
	d := &Dialer{
		Resolve:   lookupOf("3.174.2.25"),
		Fallbacks: []Named{{Name: "doh", Lookup: func(context.Context, string) ([]net.IP, error) { called = true; return nil, nil }}},
	}
	if _, err := d.resolve(context.Background(), "api.themoviedb.org"); err != nil || called {
		t.Fatalf("err = %v, fallback called = %v", err, called)
	}
}

func TestDialUsesResolvedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))

	d := &Dialer{}
	client := &http.Client{Transport: d.Transport()}
	resp, err := client.Get("http://127.0.0.1:" + port)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestCloudflareDoHParsesAndCaches(t *testing.T) {
	hits := 0
	gotSubnet := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotSubnet = r.URL.Query().Get("edns_client_subnet")
		if r.Header.Get("Accept") != "application/dns-json" {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"Answer":[{"type":5,"TTL":60,"data":"x.cloudfront.net."},{"type":1,"TTL":60,"data":"3.174.2.25"}]}`))
	}))
	defer server.Close()

	client := server.Client()
	client.Transport = rewriteTo(server.URL, client.Transport)
	lookup := CloudflareDoH(client)
	defer func() {
		if gotSubnet != "" {
			t.Errorf("cloudflare sent a subnet: %q", gotSubnet)
		}
	}()
	for range 2 {
		got, err := lookup(context.Background(), "api.themoviedb.org")
		if err != nil || len(got) != 1 || got[0].String() != "3.174.2.25" {
			t.Fatalf("lookup = %v, %v", got, err)
		}
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (cached)", hits)
	}
}

func rewriteTo(base string, next http.RoundTripper) http.RoundTripper {
	return roundTripper(func(r *http.Request) (*http.Response, error) {
		target := strings.TrimPrefix(base, "https://")
		r = r.Clone(r.Context())
		r.URL.Host = target
		r.Host = target
		return next.RoundTrip(r)
	})
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGoogleDoHSendsSubnetAndSkipsCachingLoopback(t *testing.T) {
	var subnets []string
	answer := "127.0.0.1"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subnets = append(subnets, r.URL.Query().Get("edns_client_subnet"))
		_, _ = w.Write([]byte(`{"Answer":[{"type":1,"TTL":60,"data":"` + answer + `"}]}`))
	}))
	defer server.Close()
	client := server.Client()
	client.Transport = rewriteTo(server.URL, client.Transport)
	lookup := GoogleDoH(client, EuropeanSubnet)

	if got, _ := lookup(context.Background(), "api.themoviedb.org"); len(Usable(got)) != 0 {
		t.Fatalf("first = %v", got)
	}
	answer = "143.204.238.8"
	if got, err := lookup(context.Background(), "api.themoviedb.org"); err != nil || got[0].String() != "143.204.238.8" {
		t.Fatalf("second = %v, %v", got, err)
	}
	if len(subnets) != 2 || subnets[0] != EuropeanSubnet {
		t.Fatalf("subnets = %v", subnets)
	}
}
