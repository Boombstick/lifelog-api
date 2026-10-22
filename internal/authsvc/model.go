package authsvc

import (
	"errors"
	"time"

	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type User struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Login       *string   `gorm:"size:64"`
	Email       *string   `gorm:"size:254"`
	DisplayName string    `gorm:"size:128"`
	Role        Role      `gorm:"size:16;not null;default:user"`

	IsPrivate bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"not null"`
}

func (User) TableName() string { return "auth_users" }

func (u *User) IsSilent() bool { return u.Login == nil && u.Email == nil }

type Password struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Hash      string    `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (Password) TableName() string { return "auth_passwords" }

type TelegramLink struct {
	UserID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	TelegramID int64     `gorm:"not null"`
	LinkedAt   time.Time `gorm:"not null"`
}

func (TelegramLink) TableName() string { return "auth_telegram_links" }

type Session struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID      uuid.UUID `gorm:"type:uuid;index;not null"`
	FamilyID    uuid.UUID `gorm:"type:uuid;index;not null"`
	RefreshHash string    `gorm:"size:64;not null"`
	IssuedAt    time.Time `gorm:"not null"`
	ExpiresAt   time.Time `gorm:"index;not null"`
	UsedAt      *time.Time
	RevokedAt   *time.Time
	UserAgent   string  `gorm:"size:256"`
	DeviceID    *string `gorm:"size:64"`
}

type Client struct {
	UserAgent string
	DeviceID  string
}

func deviceRef(id string) *string {
	if id == "" {
		return nil
	}
	id = textutil.Truncate(id, 64)
	return &id
}

func (Session) TableName() string { return "auth_sessions" }

type LinkNonce struct {
	Code      string    `gorm:"size:64;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null"`
	UsedAt    *time.Time
}

func (LinkNonce) TableName() string { return "auth_link_nonces" }

const (
	MergeStatusPending = "pending"
	MergeStatusDone    = "done"
)

type Merge struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	FromUser  uuid.UUID `gorm:"type:uuid;not null"`
	IntoUser  uuid.UUID `gorm:"type:uuid;not null"`
	Status    string    `gorm:"size:16;not null;index"`
	CreatedAt time.Time `gorm:"not null"`
	DoneAt    *time.Time
}

func (Merge) TableName() string { return "auth_merges" }

var (
	ErrLoginTaken         = errors.New("логин занят")
	ErrInvalidCredentials = errors.New("неверный логин или пароль")
	ErrLoginLocked        = errors.New("слишком много попыток входа, попробуйте через 15 минут")
	ErrUserNotFound       = errors.New("пользователь не найден")
	ErrNoPassword         = errors.New("у пользователя не задан пароль")
	ErrRefreshInvalid     = errors.New("refresh-токен недействителен")
	ErrRefreshReused      = errors.New("refresh-токен уже использован")
	ErrNonceInvalid       = errors.New("ссылка устарела или уже использована — нажмите «Привязать телеграм» в приложении ещё раз")
	ErrWeakPassword       = errors.New("пароль короче восьми символов")
	ErrSessionNotFound    = errors.New("сессия не найдена")
	ErrSelfSubscription   = errors.New("нельзя подписаться на себя")
	ErrSubscriptionAbsent = errors.New("подписки нет")
)

var (
	ErrTelegramAlreadyLinked = errors.New("к аккаунту уже привязан Telegram")
	ErrTelegramTaken         = errors.New("этот Telegram уже привязан к другому аккаунту")
	ErrTelegramNotLinked     = errors.New("Telegram не привязан")
)

type DeviceSession struct {
	FamilyID  uuid.UUID `json:"family_id"`
	UserAgent string    `json:"user_agent"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
	Current   bool      `json:"current"`
}

const (
	SubscriptionActive  = "active"
	SubscriptionPending = "pending"
)

type Subscription struct {
	FollowerID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TargetID   uuid.UUID `gorm:"type:uuid;primaryKey;index"`
	Status     string    `gorm:"size:16;not null"`
	CreatedAt  time.Time `gorm:"not null"`
	DecidedAt  *time.Time
}

func (Subscription) TableName() string { return "auth_subscriptions" }

func (s Subscription) Active() bool { return s.Status == SubscriptionActive }

type SubscriptionView struct {
	UserID      uuid.UUID `json:"user_id"`
	Login       string    `json:"login"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
