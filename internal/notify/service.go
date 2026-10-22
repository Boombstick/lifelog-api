package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	UpsertDevice(ctx context.Context, device *Device) error
	DeleteDevice(ctx context.Context, userID uuid.UUID, deviceID string) error
	SetDeviceEnabled(ctx context.Context, userID uuid.UUID, deviceID string, enabled bool) (bool, error)
	Devices(ctx context.Context, userID uuid.UUID) ([]Device, error)
	PushEnabled(ctx context.Context, userID uuid.UUID) (bool, error)
	SetPushEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error

	Enqueue(ctx context.Context, userIDs []uuid.UUID, channels []string, payload []byte) (int, error)
}

const (
	testLimit  = 5
	testWindow = time.Hour
)

type Service struct {
	repo    Repository
	wake    func()
	limiter *windowLimiter
	now     func() time.Time

	channels []string

	vapidKey string
	guard    *EndpointGuard
}

func NewService(repo Repository, wake func()) *Service {
	if wake == nil {
		wake = func() {}
	}
	return &Service{
		repo:     repo,
		wake:     wake,
		limiter:  newWindowLimiter(testLimit, testWindow),
		now:      func() time.Time { return time.Now().UTC() },
		channels: []string{ChannelFCM},
	}
}

func (s *Service) EnableWebPush(publicKey string, guard EndpointGuard) {
	s.vapidKey = publicKey
	s.guard = &guard
	s.channels = []string{ChannelFCM, ChannelWebPush}
}

func (s *Service) VAPIDKey() (VAPIDKeyResponse, error) {
	if s.guard == nil {
		return VAPIDKeyResponse{}, ErrWebPushDisabled
	}
	return VAPIDKeyResponse{PublicKey: s.vapidKey}, nil
}

func (s *Service) RegisterDevice(ctx context.Context, userID uuid.UUID, rawDeviceID string, in RegisterInput) error {
	deviceID, err := parseDeviceID(rawDeviceID)
	if err != nil {
		return err
	}
	in, err = validateRegister(in, s.guard)
	if err != nil {
		return err
	}
	now := s.now()
	return s.repo.UpsertDevice(ctx, &Device{
		ID:         uuid.New(),
		UserID:     userID,
		DeviceID:   deviceID,
		Channel:    in.Channel,
		Token:      optional(in.Token),
		Endpoint:   optional(in.Endpoint),
		P256dh:     optional(in.P256dh),
		Auth:       optional(in.Auth),
		Platform:   in.Platform,
		Label:      in.Label,
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	})
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Service) DeleteDevice(ctx context.Context, userID uuid.UUID, rawDeviceID string) error {
	deviceID, err := parseDeviceID(rawDeviceID)
	if err != nil {
		return err
	}
	return s.repo.DeleteDevice(ctx, userID, deviceID)
}

func (s *Service) SetDeviceEnabled(ctx context.Context, userID uuid.UUID, rawDeviceID string, enabled bool) error {
	deviceID, err := parseDeviceID(rawDeviceID)
	if err != nil {
		return err
	}
	found, err := s.repo.SetDeviceEnabled(ctx, userID, deviceID, enabled)
	if err != nil {
		return err
	}
	if !found {
		return ErrDeviceNotFound
	}
	return nil
}

func (s *Service) Devices(ctx context.Context, userID uuid.UUID) (DevicesResponse, error) {
	devices, err := s.repo.Devices(ctx, userID)
	if err != nil {
		return DevicesResponse{}, err
	}
	items := make([]DeviceResponse, 0, len(devices))
	for _, d := range devices {
		items = append(items, DeviceResponse{
			DeviceID:   d.DeviceID,
			Channel:    d.Channel,
			Platform:   d.Platform,
			Label:      d.Label,
			Enabled:    d.Enabled,
			CreatedAt:  d.CreatedAt.UTC(),
			LastSeenAt: d.LastSeenAt.UTC(),
		})
	}
	return DevicesResponse{Items: items}, nil
}

func (s *Service) Settings(ctx context.Context, userID uuid.UUID) (SettingsResponse, error) {
	enabled, err := s.repo.PushEnabled(ctx, userID)
	if err != nil {
		return SettingsResponse{}, err
	}
	return SettingsResponse{PushEnabled: enabled}, nil
}

func (s *Service) SetSettings(ctx context.Context, userID uuid.UUID, pushEnabled bool) error {
	return s.repo.SetPushEnabled(ctx, userID, pushEnabled)
}

func (s *Service) Test(ctx context.Context, userID uuid.UUID) (TestResponse, error) {
	if !s.limiter.allow(userID, s.now()) {
		return TestResponse{}, ErrTooOften
	}
	message, users, err := buildMessage(NotifyInput{
		UserIDs: []string{userID.String()},
		Kind:    KindTest,
		Title:   "Проверка уведомлений",
		Body:    "Если вы это видите, уведомления работают",
		URL:     "/",
	}, s.now())
	if err != nil {
		return TestResponse{}, err
	}
	queued, err := s.enqueue(ctx, users, message)
	if err != nil {
		return TestResponse{}, err
	}
	return TestResponse{Devices: queued}, nil
}

func (s *Service) Notify(ctx context.Context, in NotifyInput) (QueuedResponse, error) {
	message, users, err := buildMessage(in, s.now())
	if err != nil {
		return QueuedResponse{}, err
	}
	queued, err := s.enqueue(ctx, users, message)
	if err != nil {
		return QueuedResponse{}, err
	}
	return QueuedResponse{Queued: queued}, nil
}

func (s *Service) enqueue(ctx context.Context, users []uuid.UUID, message Message) (int, error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("encode push payload: %w", err)
	}
	queued, err := s.repo.Enqueue(ctx, users, s.channels, payload)
	if err != nil {
		return 0, fmt.Errorf("enqueue push: %w", err)
	}
	enqueued.WithLabelValues(message.Kind).Add(float64(queued))
	if queued > 0 {
		s.wake()
	}
	return queued, nil
}

type windowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[uuid.UUID][]time.Time
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	return &windowLimiter{limit: limit, window: window, hits: map[uuid.UUID][]time.Time{}}
}

func (l *windowLimiter) allow(user uuid.UUID, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)

	if len(l.hits) > 1000 {
		for id, times := range l.hits {
			if len(times) == 0 || !times[len(times)-1].After(cutoff) {
				delete(l.hits, id)
			}
		}
	}

	recent := l.hits[user][:0]
	for _, t := range l.hits[user] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.limit {
		l.hits[user] = recent
		return false
	}
	l.hits[user] = append(recent, now)
	return true
}
