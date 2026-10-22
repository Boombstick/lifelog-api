package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	upserted *Device
	enqueued [][]uuid.UUID
	payloads [][]byte
	channels [][]string
	queued   int
	found    bool
}

func (f *fakeRepo) UpsertDevice(_ context.Context, d *Device) error { f.upserted = d; return nil }
func (f *fakeRepo) DeleteDevice(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakeRepo) SetDeviceEnabled(context.Context, uuid.UUID, string, bool) (bool, error) {
	return f.found, nil
}
func (f *fakeRepo) Devices(context.Context, uuid.UUID) ([]Device, error) {
	token := "secret-token"
	return []Device{{DeviceID: "d", Channel: ChannelFCM, Token: &token, Enabled: true}}, nil
}
func (f *fakeRepo) PushEnabled(context.Context, uuid.UUID) (bool, error)  { return true, nil }
func (f *fakeRepo) SetPushEnabled(context.Context, uuid.UUID, bool) error { return nil }
func (f *fakeRepo) Enqueue(_ context.Context, users []uuid.UUID, channels []string, payload []byte) (int, error) {
	f.enqueued = append(f.enqueued, users)
	f.channels = append(f.channels, channels)
	f.payloads = append(f.payloads, payload)
	return f.queued, nil
}

func TestRegisterDeviceValidation(t *testing.T) {
	t.Parallel()
	service := NewService(&fakeRepo{}, nil)
	user := uuid.New()
	device := uuid.NewString()

	tests := []struct {
		name     string
		deviceID string
		in       RegisterInput
		want     error
	}{
		{"bad device id", "phone-1", RegisterInput{Channel: "fcm", Token: "t"}, ErrBadDeviceID},
		{"webpush off on this server", device, RegisterInput{Channel: "webpush", Endpoint: "https://fcm.googleapis.com/wp/x"}, ErrWebPushDisabled},
		{"unknown channel", device, RegisterInput{Channel: "sms", Token: "t"}, ErrBadChannel},
		{"empty token", device, RegisterInput{Channel: "fcm", Token: "  "}, ErrEmptyToken},
		{"long token", device, RegisterInput{Channel: "fcm", Token: strings.Repeat("x", 5000)}, ErrTokenTooLong},
		{"bad platform", device, RegisterInput{Channel: "fcm", Token: "t", Platform: "symbian"}, ErrBadPlatform},
		{"ok", device, RegisterInput{Channel: "fcm", Token: "t", Platform: "Android", Label: "Pixel"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.RegisterDevice(context.Background(), user, tt.deviceID, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegisterDeviceNormalises(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	service := NewService(repo, nil)
	id := uuid.New()
	err := service.RegisterDevice(context.Background(), uuid.New(), strings.ToUpper(id.String()),
		RegisterInput{Channel: "fcm", Token: " tok ", Label: strings.Repeat("я", 150)})
	if err != nil {
		t.Fatal(err)
	}
	d := repo.upserted
	if d.DeviceID != id.String() || *d.Token != "tok" || d.Platform != "android" || !d.Enabled {
		t.Errorf("device %+v", d)
	}
	if n := len([]rune(d.Label)); n != maxLabelRunes {
		t.Errorf("label not cut: %d runes", n)
	}
}

func TestDevicesNeverExposeToken(t *testing.T) {
	t.Parallel()
	list, err := NewService(&fakeRepo{}, nil).Devices(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "secret-token") || strings.Contains(string(raw), "token") {
		t.Errorf("device list leaks the token: %s", raw)
	}
}

func TestSetDeviceEnabledNotFound(t *testing.T) {
	t.Parallel()
	err := NewService(&fakeRepo{found: false}, nil).SetDeviceEnabled(context.Background(), uuid.New(), uuid.NewString(), false)
	if !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestTestPushIsLimited(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{queued: 2}
	woken := 0
	service := NewService(repo, func() { woken++ })
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	user := uuid.New()

	for i := range testLimit {
		got, err := service.Test(context.Background(), user)
		if err != nil || got.Devices != 2 {
			t.Fatalf("test %d: %+v, %v", i+1, got, err)
		}
	}
	if _, err := service.Test(context.Background(), user); !errors.Is(err, ErrTooOften) {
		t.Fatalf("sixth test within the hour must be refused, got %v", err)
	}

	if _, err := service.Test(context.Background(), uuid.New()); err != nil {
		t.Fatalf("other user: %v", err)
	}

	now = now.Add(testWindow + time.Second)
	if _, err := service.Test(context.Background(), user); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	if woken != testLimit+2 {
		t.Errorf("worker woken %d times", woken)
	}

	var message Message
	if err := json.Unmarshal(repo.payloads[0], &message); err != nil {
		t.Fatal(err)
	}
	if message.Kind != KindTest || message.Title != "Проверка уведомлений" {
		t.Errorf("test message %+v", message)
	}
}

func TestNotifyValidation(t *testing.T) {
	t.Parallel()
	user := uuid.NewString()
	ttl := func(v int) *int { return &v }
	many := make([]string, MaxRecipients+1)
	for i := range many {
		many[i] = uuid.NewString()
	}

	tests := []struct {
		name string
		in   NotifyInput
		want error
	}{
		{"no users", NotifyInput{Kind: KindDigest, Title: "x"}, ErrNoRecipients},
		{"too many", NotifyInput{UserIDs: many, Kind: KindDigest, Title: "x"}, ErrTooManyRecipients},
		{"bad user", NotifyInput{UserIDs: []string{"42"}, Kind: KindDigest, Title: "x"}, ErrBadRecipient},
		{"bad kind", NotifyInput{UserIDs: []string{user}, Kind: "promo", Title: "x"}, ErrBadKind},
		{"no title", NotifyInput{UserIDs: []string{user}, Kind: KindDigest}, ErrEmptyTitle},
		{"absolute url", NotifyInput{UserIDs: []string{user}, Kind: KindDigest, Title: "x", URL: "https://evil.example"}, ErrBadURL},
		{"protocol-relative url", NotifyInput{UserIDs: []string{user}, Kind: KindDigest, Title: "x", URL: "//evil.example"}, ErrBadURL},
		{"negative ttl", NotifyInput{UserIDs: []string{user}, Kind: KindDigest, Title: "x", TTLSeconds: ttl(-1)}, ErrBadTTL},
		{"huge ttl", NotifyInput{UserIDs: []string{user}, Kind: KindDigest, Title: "x", TTLSeconds: ttl(29 * 24 * 3600)}, ErrBadTTL},
		{"ok", NotifyInput{UserIDs: []string{user, user}, Kind: KindReminder, Title: "x", URL: "/calendar", TTLSeconds: ttl(600)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{queued: 1}
			_, err := NewService(repo, nil).Notify(context.Background(), tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if tt.want == nil && len(repo.enqueued[0]) != 1 {
				t.Errorf("duplicate recipients must collapse: %v", repo.enqueued[0])
			}
		})
	}
}

func TestNotifyLimitsAreInclusive(t *testing.T) {
	t.Parallel()
	user := uuid.NewString()
	ttl := func(v int) *int { return &v }
	base := func() NotifyInput { return NotifyInput{UserIDs: []string{user}, Kind: KindDigest, Title: "x"} }
	maxTTLSeconds := int(maxTTL / time.Second)

	tests := []struct {
		name   string
		modify func(*NotifyInput)
		want   error
	}{
		{"url at limit", func(in *NotifyInput) { in.URL = "/" + strings.Repeat("a", maxURLLen-1) }, nil},
		{"url over limit", func(in *NotifyInput) { in.URL = "/" + strings.Repeat("a", maxURLLen) }, ErrBadURL},
		{"collapse key at limit", func(in *NotifyInput) { in.CollapseKey = strings.Repeat("k", maxCollapse) }, nil},
		{"collapse key over limit", func(in *NotifyInput) { in.CollapseKey = strings.Repeat("k", maxCollapse+1) }, ErrBadCollapseKey},
		{"ttl at limit", func(in *NotifyInput) { in.TTLSeconds = ttl(maxTTLSeconds) }, nil},
		{"ttl over limit", func(in *NotifyInput) { in.TTLSeconds = ttl(maxTTLSeconds + 1) }, ErrBadTTL},
		{"zero ttl", func(in *NotifyInput) { in.TTLSeconds = ttl(0) }, ErrBadTTL},
		{"one second ttl", func(in *NotifyInput) { in.TTLSeconds = ttl(1) }, nil},

		{"overflowing ttl", func(in *NotifyInput) { in.TTLSeconds = ttl(10_000_000_000) }, ErrBadTTL},
		{"nil uuid recipient", func(in *NotifyInput) { in.UserIDs = []string{uuid.Nil.String()} }, ErrBadRecipient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base()
			tt.modify(&in)
			if _, _, err := buildMessage(in, time.Now()); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegisterLimitsAreInclusive(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	service := webPushService(&fakeRepo{})
	device := uuid.NewString()
	tests := []struct {
		name string
		in   RegisterInput
		want error
	}{
		{"token at limit", RegisterInput{Channel: "fcm", Token: strings.Repeat("x", maxTokenLen)}, nil},
		{"token over limit", RegisterInput{Channel: "fcm", Token: strings.Repeat("x", maxTokenLen+1)}, ErrTokenTooLong},

		{"auth of 12 bytes", RegisterInput{Channel: "webpush", Endpoint: "https://fcm.googleapis.com/wp/a", P256dh: b.p256dh(), Auth: "AAAAAAAAAAAAAAAA"}, ErrBadKeys},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := service.RegisterDevice(context.Background(), uuid.New(), device, tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRegisterFCMDropsWebPushFields(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{}
	err := NewService(repo, nil).RegisterDevice(context.Background(), uuid.New(), uuid.NewString(),
		RegisterInput{Channel: "fcm", Token: "t", Endpoint: "https://fcm.googleapis.com/wp/a", P256dh: "p", Auth: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if d := repo.upserted; d.Endpoint != nil || d.P256dh != nil || d.Auth != nil {
		t.Fatalf("device kept web push fields: %+v", d)
	}
}

func TestNotifyWithNobodyToReachDoesNotWakeWorker(t *testing.T) {
	t.Parallel()
	woken := 0
	service := NewService(&fakeRepo{queued: 0}, func() { woken++ })
	got, err := service.Notify(context.Background(), NotifyInput{UserIDs: []string{uuid.NewString()}, Kind: KindDigest, Title: "x"})
	if err != nil || got.Queued != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if woken != 0 {
		t.Fatalf("worker woken %d times for an empty enqueue", woken)
	}
}

func TestCutKeepsTextAtLimit(t *testing.T) {
	t.Parallel()
	exact := strings.Repeat("т", maxTitleRunes)
	if got := cut(exact, maxTitleRunes); got != exact {
		t.Fatalf("text of exactly %d runes was cut to %q", maxTitleRunes, got)
	}
	if got := cut(exact+"x", maxTitleRunes); len([]rune(got)) != maxTitleRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("over the limit: %q", got)
	}
}

func TestBuildMessageCutsAndDefaults(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	message, _, err := buildMessage(NotifyInput{
		UserIDs: []string{uuid.NewString()},
		Kind:    KindDigest,
		Title:   strings.Repeat("т", 300),
		Body:    strings.Repeat("б", 3000),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(message.Title)) != maxTitleRunes || len([]rune(message.Body)) != maxBodyRunes {
		t.Errorf("not cut: %d / %d", len([]rune(message.Title)), len([]rune(message.Body)))
	}
	if message.URL != "/" || !message.ExpiresAt.Equal(now.Add(12*time.Hour)) {
		t.Errorf("defaults: url %q expires %v", message.URL, message.ExpiresAt)
	}
}

func TestMaskToken(t *testing.T) {
	t.Parallel()
	if got := maskToken("abcdefghijkl"); got != "…ghijkl" {
		t.Errorf("got %q", got)
	}
	if got := maskToken("abc"); got != "…" {
		t.Errorf("short: %q", got)
	}
}

func webPushService(repo *fakeRepo) *Service {
	service := NewService(repo, nil)
	service.EnableWebPush("BPUBLIC", EndpointGuard{})
	return service
}

func TestRegisterWebPush(t *testing.T) {
	t.Parallel()
	b := newBrowser(t)
	device := uuid.NewString()
	endpoint := "https://fcm.googleapis.com/wp/abc"

	tests := []struct {
		name string
		in   RegisterInput
		want error
	}{
		{"foreign host", RegisterInput{Channel: "webpush", Endpoint: "https://evil.example/x", P256dh: b.p256dh(), Auth: b.authSecret()}, ErrBadEndpoint},
		{"internal address", RegisterInput{Channel: "webpush", Endpoint: "http://notify:9095/internal/notify", P256dh: b.p256dh(), Auth: b.authSecret()}, ErrBadEndpoint},
		{"no keys", RegisterInput{Channel: "webpush", Endpoint: endpoint}, ErrBadKeys},
		{"key not on the curve", RegisterInput{Channel: "webpush", Endpoint: endpoint, P256dh: "BAAA" + strings.Repeat("A", 83), Auth: b.authSecret()}, ErrBadKeys},
		{"short auth", RegisterInput{Channel: "webpush", Endpoint: endpoint, P256dh: b.p256dh(), Auth: "AAAA"}, ErrBadKeys},
		{"ok", RegisterInput{Channel: "webpush", Endpoint: endpoint, P256dh: b.p256dh(), Auth: b.authSecret(), Label: "Chrome"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			err := webPushService(repo).RegisterDevice(context.Background(), uuid.New(), device, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				return
			}
			d := repo.upserted
			if d.Channel != ChannelWebPush || d.Token != nil || *d.Endpoint != endpoint ||
				*d.P256dh != b.p256dh() || *d.Auth != b.authSecret() || d.Platform != "web" {
				t.Fatalf("device %+v", d)
			}
		})
	}
}

func TestVAPIDKeyAndChannels(t *testing.T) {
	t.Parallel()
	off := NewService(&fakeRepo{}, nil)
	if _, err := off.VAPIDKey(); !errors.Is(err, ErrWebPushDisabled) {
		t.Fatalf("vapid key without config: %v", err)
	}
	repo := &fakeRepo{queued: 1}
	on := webPushService(repo)
	if key, err := on.VAPIDKey(); err != nil || key.PublicKey != "BPUBLIC" {
		t.Fatalf("vapid key: %+v, %v", key, err)
	}
	if _, err := on.Test(context.Background(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(repo.channels[0], ","); got != "fcm,webpush" {
		t.Fatalf("channels = %s", got)
	}
}
