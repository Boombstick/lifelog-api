package safedial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ErrUnresolved = errors.New("адрес не получен: DNS отдаёт localhost")

var (
	loopbackAnswers = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "external_dns_localhost_total",
		Help: "DNS answers for external hosts that pointed at this machine (expected for TMDB from Russia)",
	}, []string{"host"})
	unresolved = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "external_dns_unresolved_total",
		Help: "External hosts no resolver could give a usable address for",
	}, []string{"host"})
)

const EuropeanSubnet = "85.214.0.0/16"

type Lookup func(ctx context.Context, host string) ([]net.IP, error)

type Named struct {
	Name   string
	Lookup Lookup
}

type Dialer struct {
	Resolve Lookup

	Fallbacks []Named
	Logger    *slog.Logger
	Timeout   time.Duration

	mu     sync.Mutex
	logged map[string]bool
}

func New(logger *slog.Logger) *Dialer {
	return &Dialer{
		Fallbacks: []Named{
			{Name: "Google DoH", Lookup: GoogleDoH(nil, EuropeanSubnet)},
			{Name: "Cloudflare DoH", Lookup: CloudflareDoH(nil)},
		},
		Logger:  logger,
		Timeout: 10 * time.Second,
	}
}

func (d *Dialer) Transport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = d.DialContext
	return transport
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: d.Timeout}
	if net.ParseIP(host) != nil || strings.EqualFold(host, "localhost") {
		return dialer.DialContext(ctx, network, address)
	}

	ips, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

func (d *Dialer) resolve(ctx context.Context, host string) ([]net.IP, error) {
	lookup := d.Resolve
	if lookup == nil {
		lookup = systemLookup
	}
	answers, err := lookup(ctx, host)
	if err == nil {
		if usable := Usable(answers); len(usable) > 0 {
			return usable, nil
		}
		loopbackAnswers.WithLabelValues(host).Inc()
	}

	var failures []string
	if err != nil {
		failures = append(failures, "системный DNS: "+err.Error())
	} else {
		failures = append(failures, fmt.Sprintf("системный DNS: %v", answers))
	}
	for _, fallback := range d.Fallbacks {
		ips, ferr := fallback.Lookup(ctx, host)
		if usable := Usable(ips); ferr == nil && len(usable) > 0 {
			d.noteFallback(host, fallback.Name)
			return usable, nil
		}
		if ferr != nil {
			failures = append(failures, fallback.Name+": "+ferr.Error())
		} else {
			failures = append(failures, fmt.Sprintf("%s: %v", fallback.Name, ips))
		}
	}

	unresolved.WithLabelValues(host).Inc()
	err = fmt.Errorf("%w: %s (%s)", ErrUnresolved, host, strings.Join(failures, "; "))
	d.logger().Error("адрес внешнего сервиса не получен", "host", host, "error", err.Error())
	return nil, err
}

func (d *Dialer) noteFallback(host, resolver string) {
	key := host + "|" + resolver
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.logged[key] {
		return
	}
	if d.logged == nil {
		d.logged = map[string]bool{}
	}
	d.logged[key] = true
	d.logger().Info("DNS отдаёт localhost для этой страны, адрес взят через запасной резолвер",
		"host", host, "resolver", resolver)
}

func (d *Dialer) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

func Usable(ips []net.IP) []net.IP {
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		out = append(out, ip)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].To4() != nil && out[j].To4() == nil
	})
	return out
}

func systemLookup(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func GoogleDoH(client *http.Client, subnet string) Lookup {
	extra := url.Values{}
	if subnet != "" {
		extra.Set("edns_client_subnet", subnet)
	}
	return jsonDoH(client, "https://8.8.8.8/resolve", extra)
}

func CloudflareDoH(client *http.Client) Lookup {
	return jsonDoH(client, "https://1.1.1.1/dns-query", nil)
}

func jsonDoH(client *http.Client, endpoint string, extra url.Values) Lookup {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	var mu sync.Mutex
	cache := map[string]dohEntry{}
	return func(ctx context.Context, host string) ([]net.IP, error) {
		mu.Lock()
		if entry, ok := cache[host]; ok && time.Now().Before(entry.expires) {
			mu.Unlock()
			return entry.ips, nil
		}
		mu.Unlock()

		query := url.Values{}
		for k, v := range extra {
			query[k] = v
		}
		query.Set("name", host)
		query.Set("type", "A")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/dns-json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ответил %d", resp.StatusCode)
		}
		var body struct {
			Answer []struct {
				Type int    `json:"type"`
				TTL  int    `json:"TTL"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return nil, err
		}
		var ips []net.IP
		ttl := 300
		for _, answer := range body.Answer {
			if answer.Type != 1 {
				continue
			}
			if ip := net.ParseIP(answer.Data); ip != nil {
				ips = append(ips, ip)
				if answer.TTL > 0 && answer.TTL < ttl {
					ttl = answer.TTL
				}
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("не знает %s", host)
		}
		if len(Usable(ips)) > 0 {
			mu.Lock()
			cache[host] = dohEntry{ips: ips, expires: time.Now().Add(time.Duration(ttl) * time.Second)}
			mu.Unlock()
		}
		return ips, nil
	}
}

type dohEntry struct {
	ips     []net.IP
	expires time.Time
}
