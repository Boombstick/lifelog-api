package notify

import (
	"crypto/ecdh"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	ChannelFCM = "fcm"

	ChannelWebPush = "webpush"
)

const (
	KindReminder = "calendar.reminder"
	KindDigest   = "calendar.digest"
	KindTest     = "test"
)

const (
	MaxRecipients = 1000

	MaxDevicesPerUser = 20
	maxTokenLen       = 4096
	maxLabelRunes     = 100
	maxTitleRunes     = 120
	maxBodyRunes      = 1000
	maxURLLen         = 512
	maxCollapse       = 100
	maxTTL            = 28 * 24 * time.Hour
)

var (
	ErrBadDeviceID       = errors.New("Некорректный идентификатор устройства")
	ErrBadChannel        = errors.New("Неизвестный канал доставки")
	ErrEmptyToken        = errors.New("Укажите токен устройства")
	ErrTokenTooLong      = errors.New("Токен устройства слишком длинный")
	ErrBadPlatform       = errors.New("Неизвестная платформа")
	ErrDeviceNotFound    = errors.New("Устройство не найдено")
	ErrTooOften          = errors.New("Слишком часто, попробуйте позже")
	ErrNoRecipients      = errors.New("Укажите получателей")
	ErrTooManyRecipients = errors.New("Не больше 1000 получателей за раз")
	ErrBadRecipient      = errors.New("Некорректный идентификатор получателя")
	ErrBadKind           = errors.New("Неизвестный вид уведомления")
	ErrEmptyTitle        = errors.New("Укажите заголовок")
	ErrBadURL            = errors.New("Адрес перехода должен начинаться с /")
	ErrBadTTL            = errors.New("Срок жизни уведомления — от 1 секунды до 28 дней")
	ErrBadCollapseKey    = errors.New("Слишком длинный ключ схлопывания")
	ErrBadEndpoint       = errors.New("Некорректный адрес подписки")
	ErrBadKeys           = errors.New("Некорректные ключи подписки")
	ErrWebPushDisabled   = errors.New("Уведомления в браузере на сервере не настроены")
)

type Device struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID `gorm:"type:uuid"`
	DeviceID   string
	Channel    string
	Token      *string
	Endpoint   *string
	P256dh     *string
	Auth       *string
	Platform   string
	Label      string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastSeenAt time.Time
}

func (Device) TableName() string { return "push_devices" }

type DeviceResponse struct {
	DeviceID   string    `json:"device_id"`
	Channel    string    `json:"channel"`
	Platform   string    `json:"platform"`
	Label      string    `json:"label"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type DevicesResponse struct {
	Items []DeviceResponse `json:"items"`
}

type SettingsResponse struct {
	PushEnabled bool `json:"push_enabled"`
}

type TestResponse struct {
	Devices int `json:"devices"`
}

type QueuedResponse struct {
	Queued int `json:"queued"`
}

type RegisterInput struct {
	Channel  string
	Token    string
	Endpoint string
	P256dh   string
	Auth     string
	Platform string
	Label    string
}

type NotifyInput struct {
	UserIDs     []string
	Kind        string
	Title       string
	Body        string
	URL         string
	CollapseKey string

	TTLSeconds *int
}

type Message struct {
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
	CollapseKey string    `json:"collapse_key,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type Target struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Channel  string
	Token    string
	Endpoint string
	P256dh   string
	Auth     string
}

type VAPIDKeyResponse struct {
	PublicKey string `json:"public_key"`
}

var platforms = map[string]bool{"android": true, "web": true, "ios": true}

func parseDeviceID(raw string) (string, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrBadDeviceID
	}
	return id.String(), nil
}

func validateRegister(in RegisterInput, guard *EndpointGuard) (RegisterInput, error) {
	in.Channel = strings.TrimSpace(in.Channel)
	in.Token = strings.TrimSpace(in.Token)
	in.Endpoint = strings.TrimSpace(in.Endpoint)
	in.P256dh = strings.TrimSpace(in.P256dh)
	in.Auth = strings.TrimSpace(in.Auth)
	in.Platform = strings.ToLower(strings.TrimSpace(in.Platform))
	in.Label = cut(strings.TrimSpace(in.Label), maxLabelRunes)

	switch in.Channel {
	case ChannelFCM:
		if in.Token == "" {
			return in, ErrEmptyToken
		}
		if len(in.Token) > maxTokenLen {
			return in, ErrTokenTooLong
		}
		in.Endpoint, in.P256dh, in.Auth = "", "", ""
		if in.Platform == "" {
			in.Platform = "android"
		}
	case ChannelWebPush:
		if guard == nil {
			return in, ErrWebPushDisabled
		}
		if _, err := guard.Check(in.Endpoint); err != nil {
			return in, err
		}
		if !validSubscriptionKeys(in.P256dh, in.Auth) {
			return in, ErrBadKeys
		}
		in.Token = ""
		if in.Platform == "" {
			in.Platform = "web"
		}
	default:
		return in, ErrBadChannel
	}
	if !platforms[in.Platform] {
		return in, ErrBadPlatform
	}
	return in, nil
}

func validSubscriptionKeys(p256dh, auth string) bool {
	if len(p256dh) > 200 || len(auth) > 100 {
		return false
	}
	key, err := decodeBase64(p256dh)
	if err != nil {
		return false
	}
	if _, err := ecdh.P256().NewPublicKey(key); err != nil {
		return false
	}
	secret, err := decodeBase64(auth)
	return err == nil && len(secret) == 16
}

var defaultTTL = map[string]time.Duration{
	KindReminder: time.Hour,
	KindDigest:   12 * time.Hour,
	KindTest:     time.Hour,
}

func buildMessage(in NotifyInput, now time.Time) (Message, []uuid.UUID, error) {
	if len(in.UserIDs) == 0 {
		return Message{}, nil, ErrNoRecipients
	}
	if len(in.UserIDs) > MaxRecipients {
		return Message{}, nil, ErrTooManyRecipients
	}
	seen := make(map[uuid.UUID]bool, len(in.UserIDs))
	users := make([]uuid.UUID, 0, len(in.UserIDs))
	for _, raw := range in.UserIDs {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return Message{}, nil, ErrBadRecipient
		}
		if !seen[id] {
			seen[id] = true
			users = append(users, id)
		}
	}

	ttl, known := defaultTTL[in.Kind]
	if !known {
		return Message{}, nil, ErrBadKind
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Message{}, nil, ErrEmptyTitle
	}
	url := strings.TrimSpace(in.URL)
	if url == "" {
		url = "/"
	}

	if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") || len(url) > maxURLLen {
		return Message{}, nil, ErrBadURL
	}
	if len(in.CollapseKey) > maxCollapse {
		return Message{}, nil, ErrBadCollapseKey
	}
	if in.TTLSeconds != nil {
		seconds := *in.TTLSeconds
		if seconds < 1 || int64(seconds) > int64(maxTTL/time.Second) {
			return Message{}, nil, ErrBadTTL
		}
		ttl = time.Duration(seconds) * time.Second
	}

	return Message{
		Kind:        in.Kind,
		Title:       cut(title, maxTitleRunes),
		Body:        cut(strings.TrimSpace(in.Body), maxBodyRunes),
		URL:         url,
		CollapseKey: in.CollapseKey,
		ExpiresAt:   now.Add(ttl),
	}, users, nil
}

func cut(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func maskToken(token string) string {
	const keep = 6
	if len(token) <= keep {
		return "…"
	}
	return "…" + token[len(token)-keep:]
}
