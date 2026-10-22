package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func b64(t *testing.T, s string) []byte {
	t.Helper()
	out, err := base64.RawURLEncoding.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return out
}

func TestEncryptMatchesRFC8291(t *testing.T) {
	t.Parallel()
	asPrivate, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt(
		[]byte("When I grow up, I want to be a watermelon"),
		b64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		b64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
		b64(t, "DGv6ra1nlYgDCS1FRnbzlw"),
		asPrivate)
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if enc := base64.RawURLEncoding.EncodeToString(got); enc != want {
		t.Fatalf("body\n got %s\nwant %s", enc, want)
	}
}

type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return browser{key: key, auth: auth}
}

func (b browser) p256dh() string {
	return base64.RawURLEncoding.EncodeToString(b.key.PublicKey().Bytes())
}
func (b browser) authSecret() string { return base64.RawURLEncoding.EncodeToString(b.auth) }

func (b browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt := body[:16]
	if rs := binary.BigEndian.Uint32(body[16:20]); rs != recordSize {
		t.Fatalf("record size %d", rs)
	}
	idLen := int(body[20])
	asPublic := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]

	remote, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := b.key.ECDH(remote)
	if err != nil {
		t.Fatal(err)
	}
	prkKey, _ := hkdf.Extract(sha256.New, shared, b.auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(b.key.PublicKey().Bytes())+string(asPublic), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		t.Fatalf("missing last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func testVAPID(t *testing.T) *VAPID {
	t.Helper()
	public, private, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	vapid, err := LoadVAPID(public, private, "mailto:noreply@lifel0g.ru")
	if err != nil {
		t.Fatal(err)
	}
	return vapid
}

type capturedPush struct {
	header http.Header
	body   []byte
}

func pushService(t *testing.T, status int, extra http.Header) (*httptest.Server, chan capturedPush) {
	t.Helper()
	got := make(chan capturedPush, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- capturedPush{header: r.Header.Clone(), body: body}
		for k, v := range extra {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, got
}

func webPushTarget(endpoint string, b browser) Target {
	return Target{ID: uuid.New(), UserID: uuid.New(), Channel: ChannelWebPush,
		Endpoint: endpoint, P256dh: b.p256dh(), Auth: b.authSecret()}
}

func TestWebPushSendShape(t *testing.T) {
	t.Parallel()
	vapid := testVAPID(t)
	server, got := pushService(t, http.StatusCreated, nil)
	sender := NewWebPushSender(vapid, EndpointGuard{AllowTest: true}, "https://lifel0g.ru/")
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	sender.now = func() time.Time { return now }
	b := newBrowser(t)

	message := Message{Kind: KindReminder, Title: "Через 30 минут: Встреча", Body: "19:00 · Бангкок",
		URL: "/calendar", CollapseKey: "event-1", ExpiresAt: now.Add(90 * time.Second)}
	if err := sender.Send(context.Background(), webPushTarget(server.URL+"/push/abc", b), message); err != nil {
		t.Fatal(err)
	}
	push := <-got

	for header, want := range map[string]string{
		"TTL": "90", "Content-Encoding": "aes128gcm", "Content-Type": "application/octet-stream", "Urgency": "high",
	} {
		if value := push.header.Get(header); value != want {
			t.Errorf("%s = %q, want %q", header, value, want)
		}
	}
	if topic := push.header.Get("Topic"); len(topic) != 32 {
		t.Errorf("Topic = %q", topic)
	}

	auth := push.header.Get("Authorization")
	token, key, ok := strings.Cut(strings.TrimPrefix(auth, "vapid t="), ", k=")
	if !strings.HasPrefix(auth, "vapid t=") || !ok || key != vapid.PublicKey() {
		t.Fatalf("Authorization = %q", auth)
	}
	public, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), b64(t, key))
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return public, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithTimeFunc(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("VAPID JWT: %v", err)
	}
	if claims["aud"] != server.URL || claims["sub"] != "mailto:noreply@lifel0g.ru" {
		t.Errorf("claims = %v", claims)
	}
	if exp, _ := claims.GetExpirationTime(); exp == nil || exp.Sub(now) > 24*time.Hour {
		t.Errorf("exp = %v", exp)
	}

	var decoded declarativePush
	if err := json.Unmarshal(b.decrypt(t, push.body), &decoded); err != nil {
		t.Fatal(err)
	}
	n := decoded.Notification
	if decoded.WebPush != 8030 || n.Title != message.Title || n.Body != message.Body ||
		n.Navigate != "https://lifel0g.ru/?open=%2Fcalendar" || n.Tag != "event-1" ||
		n.Data["url"] != "/calendar" || n.Data["kind"] != KindReminder || n.Lang != "ru" {
		t.Fatalf("payload = %+v", decoded)
	}
}

func TestWebPushOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		header    http.Header
		gone      bool
		retry     bool
		wantAfter time.Duration
	}{
		{"accepted", http.StatusCreated, nil, false, false, 0},
		{"gone", http.StatusGone, nil, true, false, 0},
		{"not found", http.StatusNotFound, nil, true, false, 0},
		{"throttled", http.StatusTooManyRequests, http.Header{"Retry-After": {"120"}}, false, true, 2 * time.Minute},
		{"unavailable", http.StatusServiceUnavailable, nil, false, true, 0},
		{"bad jwt", http.StatusForbidden, nil, false, false, 0},
		{"too large", http.StatusRequestEntityTooLarge, nil, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server, _ := pushService(t, tt.status, tt.header)
			sender := NewWebPushSender(testVAPID(t), EndpointGuard{AllowTest: true}, "https://lifel0g.ru")
			target := webPushTarget(server.URL+"/secret-path", newBrowser(t))
			err := sender.Send(context.Background(), target,
				Message{Kind: KindTest, Title: "t", URL: "/", ExpiresAt: time.Now().Add(time.Hour)})

			var retry *RetryableError
			switch {
			case tt.status < 300:
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			case tt.gone:
				if !errors.Is(err, ErrTokenGone) {
					t.Fatalf("err = %v, want gone", err)
				}
			case tt.retry:
				if !errors.As(err, &retry) || retry.After != tt.wantAfter {
					t.Fatalf("err = %v, want retry after %v", err, tt.wantAfter)
				}
			default:
				if err == nil || errors.Is(err, ErrTokenGone) || errors.As(err, &retry) {
					t.Fatalf("err = %v, want permanent", err)
				}
			}
			if strings.Contains(err.Error(), "secret-path") {
				t.Fatalf("error leaks the endpoint: %v", err)
			}
		})
	}
}

func TestWebPushNetworkErrorHidesEndpoint(t *testing.T) {
	t.Parallel()
	server, _ := pushService(t, http.StatusCreated, nil)
	endpoint := server.URL + "/secret-path"
	server.Close()
	sender := NewWebPushSender(testVAPID(t), EndpointGuard{AllowTest: true}, "https://lifel0g.ru")
	err := sender.Send(context.Background(), webPushTarget(endpoint, newBrowser(t)),
		Message{Kind: KindTest, Title: "t", ExpiresAt: time.Now().Add(time.Hour)})
	var retry *RetryableError
	if !errors.As(err, &retry) || strings.Contains(err.Error(), "secret-path") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkerRemovesGoneWebPushDevice(t *testing.T) {
	t.Parallel()
	server, _ := pushService(t, http.StatusGone, nil)
	sender := NewWebPushSender(testVAPID(t), EndpointGuard{AllowTest: true}, "https://lifel0g.ru")
	target := webPushTarget(server.URL+"/x", newBrowser(t))
	payload, _ := json.Marshal(Message{Kind: KindTest, Title: "t", ExpiresAt: time.Now().Add(time.Hour)})
	outbox := newFakeOutbox(Job{ID: 7, Payload: payload, Target: target})
	worker := NewWorker(outbox, map[string]Sender{ChannelWebPush: sender}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := worker.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(outbox.devicesGone) != 1 || outbox.devicesGone[0] != target.ID {
		t.Fatalf("devices removed = %v", outbox.devicesGone)
	}
}

func TestWebPushRefusesUnknownEndpointAtSend(t *testing.T) {
	t.Parallel()
	sender := NewWebPushSender(testVAPID(t), EndpointGuard{}, "https://lifel0g.ru")
	err := sender.Send(context.Background(), webPushTarget("http://127.0.0.1:9095/internal/notify", newBrowser(t)),
		Message{Kind: KindTest, Title: "t", ExpiresAt: time.Now().Add(time.Hour)})
	if !errors.Is(err, ErrTokenGone) {
		t.Fatalf("err = %v", err)
	}
}

func TestPayloadIsCutToFit(t *testing.T) {
	t.Parallel()
	sender := NewWebPushSender(testVAPID(t), EndpointGuard{}, "https://lifel0g.ru")
	out, err := sender.payload(Message{Kind: KindDigest, Title: strings.Repeat("З", 120),
		Body: strings.Repeat("Длинная сводка. ", 200), URL: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxPushPayload {
		t.Fatalf("payload %d bytes", len(out))
	}
	var decoded declarativePush
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(decoded.Notification.Body, "…") || decoded.Notification.Navigate != "https://lifel0g.ru/" {
		t.Fatalf("notification = %+v", decoded.Notification)
	}
}

func TestEndpointGuard(t *testing.T) {
	t.Parallel()
	prod := EndpointGuard{}
	for _, ok := range []string{
		"https://fcm.googleapis.com/fcm/send/abc:APA91b",
		"https://fcm.googleapis.com/wp/abc",
		"https://web.push.apple.com/QGuQyavXutnMH",
		"https://updates.push.services.mozilla.com/wpush/v2/gAAAA",
		"https://wns2-par02p.notify.windows.com/w/?token=BQYAAA",
		"https://FCM.googleapis.com:443/fcm/send/x",
	} {
		if _, err := prod.Check(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"",
		"http://fcm.googleapis.com/fcm/send/x",
		"https://fcm.googleapis.com:8443/fcm/send/x",
		"https://user:pass@fcm.googleapis.com/x",
		"https://evil.com/fcm.googleapis.com",
		"https://fcm.googleapis.com.evil.com/x",
		"https://notpush.apple.com/x",
		"https://127.0.0.1/x",
		"https://notify:9095/internal/notify",
		"https://fcm.googleapis.com/" + strings.Repeat("a", maxEndpointLen),
	} {
		if _, err := prod.Check(bad); !errors.Is(err, ErrBadEndpoint) {
			t.Errorf("%q accepted (err %v)", bad, err)
		}
	}
	if _, err := prod.Check("https://evil.com/x"); func() bool { h, ok := RefusedHost(err); return !ok || h != "evil.com" }() {
		t.Errorf("refused host not reported: %v", err)
	}

	test := EndpointGuard{AllowTest: true}
	if _, err := test.Check("http://127.0.0.1:18080/push/1"); err != nil {
		t.Errorf("test guard refused localhost: %v", err)
	}
	if _, err := test.Check("ftp://127.0.0.1/x"); !errors.Is(err, ErrBadEndpoint) {
		t.Errorf("test guard accepted ftp")
	}
}

func TestRefusePrivate(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"127.0.0.1:443", "[::1]:443", "10.1.2.3:443", "172.18.0.5:443",
		"192.168.1.1:443", "169.254.169.254:80", "100.64.0.1:443", "0.0.0.0:443", "[::ffff:127.0.0.1]:443", "[fd00::1]:443"} {
		if err := refusePrivate("tcp", address, nil); !errors.Is(err, errPrivateAddress) {
			t.Errorf("%s allowed", address)
		}
	}
	for _, address := range []string{"142.250.74.106:443", "[2a00:1450:4001:80b::200a]:443", "17.188.143.40:443"} {
		if err := refusePrivate("tcp", address, nil); err != nil {
			t.Errorf("%s refused: %v", address, err)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Error("8.8.8.8 not public")
	}
}

func TestEndpointGuardHTTPClient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/landed", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	_, err := EndpointGuard{}.HTTPClient(2 * time.Second).Get(server.URL + "/push")
	if !errors.Is(err, errPrivateAddress) {
		t.Fatalf("prod client reached loopback: %v", err)
	}

	response, err := EndpointGuard{AllowTest: true}.HTTPClient(2 * time.Second).Get(server.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("status %d: the redirect was followed", response.StatusCode)
	}
}

func TestLoadVAPID(t *testing.T) {
	t.Parallel()
	public, private, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	otherPublic, _, _ := GenerateVAPIDKeys()

	if _, err := LoadVAPID(public, private, "mailto:noreply@lifel0g.ru"); err != nil {
		t.Fatalf("valid pair: %v", err)
	}

	std := base64.StdEncoding.EncodeToString(b64(t, private))
	if _, err := LoadVAPID(public, std, "https://lifel0g.ru"); err != nil {
		t.Fatalf("padded private key: %v", err)
	}
	if _, err := LoadVAPID(otherPublic, private, "mailto:noreply@lifel0g.ru"); err == nil {
		t.Fatal("mismatched pair accepted")
	}
	if _, err := LoadVAPID(public, "not-a-key", "mailto:noreply@lifel0g.ru"); err == nil {
		t.Fatal("garbage private key accepted")
	}
	for _, subject := range []string{"", "noreply@lifel0g.ru", "mailto:a@localhost", "mailto:a@host.local",
		"https://localhost", "http://lifel0g.ru", "mailto:not an address"} {
		if _, err := LoadVAPID(public, private, subject); err == nil {
			t.Errorf("subject %q accepted", subject)
		}
	}
}

func TestTopicAndUrgency(t *testing.T) {
	t.Parallel()
	if topic("") != "" {
		t.Error("empty collapse key must give no Topic")
	}
	if got := topic("cal-" + strings.Repeat("x", 100)); len(got) != 32 || strings.Trim(got, "0123456789abcdef") != "" {
		t.Errorf("topic = %q", got)
	}
	if urgency(KindReminder) != "high" || urgency(KindDigest) != "normal" {
		t.Error("urgency")
	}
}
