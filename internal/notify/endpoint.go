package notify

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

const maxEndpointLen = 2048

var (
	pushHosts = map[string]bool{
		"fcm.googleapis.com":        true,
		"push.services.mozilla.com": true,
	}
	pushHostSuffixes = []string{
		".push.apple.com",
		".push.services.mozilla.com",
		".notify.windows.com",

		".push.yandex.ru",
	}
)

type EndpointGuard struct {
	AllowTest bool
}

type errEndpointHost struct{ host string }

func (e errEndpointHost) Error() string { return "push endpoint host not allowed: " + e.host }

func (g EndpointGuard) Check(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > maxEndpointLen {
		return nil, ErrBadEndpoint
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrBadEndpoint
	}
	if g.AllowTest {
		if parsed.Scheme != "https" && parsed.Scheme != "http" {
			return nil, ErrBadEndpoint
		}
		return parsed, nil
	}
	if parsed.Scheme != "https" || (parsed.Port() != "" && parsed.Port() != "443") {
		return nil, ErrBadEndpoint
	}
	host := strings.ToLower(parsed.Hostname())
	if !knownPushHost(host) {
		return nil, fmt.Errorf("%w: %w", ErrBadEndpoint, errEndpointHost{host: host})
	}
	return parsed, nil
}

func knownPushHost(host string) bool {
	if pushHosts[host] {
		return true
	}
	for _, suffix := range pushHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func RefusedHost(err error) (string, bool) {
	var refused errEndpointHost
	if errors.As(err, &refused) {
		return refused.host, true
	}
	return "", false
}

var errPrivateAddress = errors.New("push endpoint resolves to a private address")

func (g EndpointGuard) HTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	if !g.AllowTest {
		dialer.Control = refusePrivate
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func refusePrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !publicAddress(ip.Unmap()) {
		return errPrivateAddress
	}
	return nil
}

var sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")

func publicAddress(ip netip.Addr) bool {
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified() &&
		!ip.IsInterfaceLocalMulticast() && !sharedAddressSpace.Contains(ip)
}
