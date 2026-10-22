package authsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lifelog-api/internal/randtoken"
	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

const (
	minPasswordLength = 8
	nonceTTL          = 3 * time.Minute
	refreshBytes      = 32
)

var decoyHash = func() string {
	hash, err := createHash(context.Background(), randtoken.New(refreshBytes))
	if err != nil {
		return ""
	}
	return hash
}()

type Repository interface {
	CreateUser(ctx context.Context, user *User) error
	CreateUserWithPassword(ctx context.Context, user *User, hash string) error
	DeleteExpired(ctx context.Context) (int64, error)
	UserByID(ctx context.Context, id uuid.UUID) (*User, error)
	UserByLogin(ctx context.Context, login string) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	UserByTelegram(ctx context.Context, telegramID int64) (*User, error)
	TelegramIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error)

	SetPassword(ctx context.Context, userID uuid.UUID, hash string) error
	PasswordOf(ctx context.Context, userID uuid.UUID) (*Password, error)

	LinkTelegram(ctx context.Context, userID uuid.UUID, telegramID int64) error
	UnlinkTelegram(ctx context.Context, userID uuid.UUID) error

	CreateSession(ctx context.Context, session *Session) error
	ActiveSessions(ctx context.Context, userID uuid.UUID) ([]DeviceSession, error)
	SessionByRefreshHash(ctx context.Context, hash string) (*Session, error)
	RotateSession(ctx context.Context, oldID uuid.UUID, next *Session) error
	RevokeFamily(ctx context.Context, familyID uuid.UUID) error
	RevokeUserFamily(ctx context.Context, userID, familyID uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
	RevokeOtherFamilies(ctx context.Context, userID, keep uuid.UUID) error
	RevokeDevice(ctx context.Context, userID uuid.UUID, deviceID string) error

	UpdateProfile(ctx context.Context, userID uuid.UUID, role Role, email *string, displayName string) error
	DemoteAdmins(ctx context.Context, keep uuid.UUID) (int64, error)

	ReplaceNonce(ctx context.Context, nonce *LinkNonce) error

	Subscribe(ctx context.Context, subscription *Subscription) error
	Unsubscribe(ctx context.Context, followerID, targetID uuid.UUID) error
	SetSubscriptionStatus(ctx context.Context, followerID, targetID uuid.UUID, status string) error
	SubscriptionsOf(ctx context.Context, followerID uuid.UUID) ([]SubscriptionView, error)
	SubscribersOf(ctx context.Context, targetID uuid.UUID) ([]SubscriptionView, error)
	SetPrivate(ctx context.Context, userID uuid.UUID, isPrivate bool) error
	SetDisplayName(ctx context.Context, userID uuid.UUID, name string) error
	Block(ctx context.Context, block *Block) error
	Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error
	BlockState(ctx context.Context, viewerID, otherID uuid.UUID) (blockedByOther, blockedByViewer bool, err error)
	BlocksOf(ctx context.Context, blockerID uuid.UUID) ([]BlockView, error)

	BlockRelations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)

	FollowingOf(ctx context.Context, followerID uuid.UUID) ([]uuid.UUID, error)
	FollowersAmong(ctx context.Context, targetID uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error)
	ConsumeNonce(ctx context.Context, code string) (*LinkNonce, error)

	AbsorbSilent(ctx context.Context, silent, into uuid.UUID) (*Merge, error)
	PendingMerges(ctx context.Context) ([]Merge, error)
	CompleteMerge(ctx context.Context, id uuid.UUID) error

	EmailCode(ctx context.Context, key string) (*EmailCode, error)
	SaveEmailCode(ctx context.Context, code *EmailCode) error
	DeleteEmailCode(ctx context.Context, key string) error

	TakeCodeAttempt(ctx context.Context, key string, maxAttempts int, now time.Time) (*EmailCode, error)

	HitRateCounter(ctx context.Context, key string, now time.Time, window time.Duration) (int, error)

	ResetRateCounter(ctx context.Context, key string) error
	CreateUserFromCode(ctx context.Context, key, codeHash string, user *User, passwordHash string) error
	ResetPasswordWithCode(ctx context.Context, key, codeHash string, userID uuid.UUID, passwordHash string) error
	AttachEmailWithCode(ctx context.Context, key, codeHash string, userID uuid.UUID, email string) error
}

type TokenIssuer interface {
	Issue(userID uuid.UUID, role, name string, family uuid.UUID) (string, time.Time, error)
}

type MergeNotifier interface {
	NotifyMerge(ctx context.Context, from, into uuid.UUID) error

	InvalidateFollows(ctx context.Context, userIDs ...uuid.UUID) error

	NotifyBlock(ctx context.Context, a, b uuid.UUID) error
}

type Service struct {
	repo       Repository
	tokens     TokenIssuer
	notifier   MergeNotifier
	logger     *slog.Logger
	refreshTTL time.Duration
	botName    string
	mailer     Mailer
	rootLogin  string
}

func NewService(repo Repository, tokens TokenIssuer, notifier MergeNotifier, logger *slog.Logger, refreshTTL time.Duration, botName string) *Service {
	return &Service{repo: repo, tokens: tokens, notifier: notifier, logger: logger, refreshTTL: refreshTTL, botName: botName, mailer: NewLogMailer(logger)}
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       uuid.UUID `json:"user_id"`
}

func (s *Service) Login(ctx context.Context, login, password string, client Client) (*TokenPair, error) {
	normalized := normalizeLogin(login)
	return s.limitedSignIn(ctx, normalized, func() (*TokenPair, error) {
		user, err := s.repo.UserByLogin(ctx, normalized)
		return s.passwordSignIn(ctx, user, err, password, client)
	})
}

func (s *Service) Refresh(ctx context.Context, refreshToken string, client Client) (*TokenPair, error) {
	session, err := s.repo.SessionByRefreshHash(ctx, randtoken.Hash(refreshToken))
	if err != nil {
		return nil, err
	}

	now := time.Now()
	switch {
	case session.RevokedAt != nil, session.ExpiresAt.Before(now):
		return nil, ErrRefreshInvalid
	case session.UsedAt != nil:

		return nil, s.refreshReused(ctx, session)
	}

	user, err := s.repo.UserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}

	access, expires, err := s.tokens.Issue(user.ID, string(user.Role), user.DisplayName, session.FamilyID)
	if err != nil {
		return nil, err
	}

	if client.DeviceID == "" && session.DeviceID != nil {
		client.DeviceID = *session.DeviceID
	}
	raw, next, err := s.newSession(user.ID, session.FamilyID, client)
	if err != nil {
		return nil, err
	}
	if err := s.repo.RotateSession(ctx, session.ID, next); err != nil {
		if errors.Is(err, ErrRefreshReused) {
			return nil, s.refreshReused(ctx, session)
		}
		return nil, err
	}

	return &TokenPair{AccessToken: access, RefreshToken: raw, ExpiresAt: expires, UserID: user.ID}, nil
}

func (s *Service) refreshReused(ctx context.Context, session *Session) error {
	if err := s.repo.RevokeFamily(ctx, session.FamilyID); err != nil {
		return err
	}
	s.logger.Warn("повторное использование refresh-токена", "user_id", session.UserID, "family_id", session.FamilyID)
	return ErrRefreshReused
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	session, err := s.repo.SessionByRefreshHash(ctx, randtoken.Hash(refreshToken))
	if err != nil {
		if errors.Is(err, ErrRefreshInvalid) {
			return nil
		}
		return err
	}
	return s.repo.RevokeFamily(ctx, session.FamilyID)
}

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*User, error) {
	return s.repo.UserByID(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string, keepFamily uuid.UUID) error {
	if len([]rune(next)) < minPasswordLength {
		return ErrWeakPassword
	}
	if root, err := s.isRoot(ctx, userID); err != nil {
		return err
	} else if root {
		return ErrManagedPassword
	}

	stored, err := s.repo.PasswordOf(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := compareHash(ctx, current, stored.Hash)
	if err != nil {
		return fmt.Errorf("проверка пароля: %w", err)
	}
	if !ok {
		return ErrInvalidCredentials
	}

	hash, err := createHash(ctx, next)
	if err != nil {
		return fmt.Errorf("хеширование пароля: %w", err)
	}
	if err := s.repo.SetPassword(ctx, userID, hash); err != nil {
		return err
	}
	return s.repo.RevokeOtherFamilies(ctx, userID, keepFamily)
}

func (s *Service) ResetPassword(ctx context.Context, targetID uuid.UUID, next string) error {
	if len([]rune(next)) < minPasswordLength {
		return ErrWeakPassword
	}
	if _, err := s.repo.UserByID(ctx, targetID); err != nil {
		return err
	}
	if root, err := s.isRoot(ctx, targetID); err != nil {
		return err
	} else if root {
		return ErrManagedPassword
	}

	hash, err := createHash(ctx, next)
	if err != nil {
		return fmt.Errorf("хеширование пароля: %w", err)
	}
	if err := s.repo.SetPassword(ctx, targetID, hash); err != nil {
		return err
	}
	return s.repo.RevokeAllForUser(ctx, targetID)
}

func (s *Service) issuePair(ctx context.Context, user *User, familyID uuid.UUID, client Client) (*TokenPair, error) {
	access, expires, err := s.tokens.Issue(user.ID, string(user.Role), user.DisplayName, familyID)
	if err != nil {
		return nil, err
	}

	raw, session, err := s.newSession(user.ID, familyID, client)
	if err != nil {
		return nil, err
	}

	if client.DeviceID != "" {
		if err := s.repo.RevokeDevice(ctx, user.ID, client.DeviceID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	return &TokenPair{AccessToken: access, RefreshToken: raw, ExpiresAt: expires, UserID: user.ID}, nil
}

func (s *Service) newSession(userID, familyID uuid.UUID, client Client) (string, *Session, error) {
	raw := randtoken.New(refreshBytes)

	now := time.Now()
	return raw, &Session{
		ID:          uuid.New(),
		UserID:      userID,
		FamilyID:    familyID,
		RefreshHash: randtoken.Hash(raw),
		IssuedAt:    now,
		ExpiresAt:   now.Add(s.refreshTTL),
		UserAgent:   textutil.Truncate(client.UserAgent, 256),
		DeviceID:    deviceRef(client.DeviceID),
	}, nil
}

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

func (s *Service) Sessions(ctx context.Context, userID uuid.UUID, currentFamily string) ([]DeviceSession, error) {
	sessions, err := s.repo.ActiveSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		sessions[i].Current = sessions[i].FamilyID.String() == currentFamily
	}
	return sessions, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID, familyID uuid.UUID) error {
	return s.repo.RevokeUserFamily(ctx, userID, familyID)
}

func (s *Service) RevokeAllSessions(ctx context.Context, userID uuid.UUID) error {
	return s.repo.RevokeAllForUser(ctx, userID)
}

func (s *Service) UserByLogin(ctx context.Context, login string) (*User, error) {
	return s.repo.UserByLogin(ctx, normalizeLogin(login))
}
