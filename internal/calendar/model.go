package calendar

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindFilmRelease Kind = "film_release"
	KindSeasonStart Kind = "season_start"
	KindOwnEvent    Kind = "own_event"
	KindGoogleEvent Kind = "google_event"
)

var (
	ErrNotFound    = errors.New("Событие не найдено")
	ErrBadTime     = errors.New("Конец не может быть раньше начала")
	ErrBadLead     = errors.New("Напоминание — не раньше чем за 30 дней до начала")
	ErrBadDigest   = errors.New("Время сводки — в формате ЧЧ:ММ")
	ErrTitleNeeded = errors.New("Укажите название события")
	ErrBadTimezone = errors.New("Неизвестный часовой пояс")

	ErrGoogleDisabled     = errors.New("Импорт из Google Календаря не настроен")
	ErrGoogleNotLinked    = errors.New("Google Календарь не подключён")
	ErrGoogleBadState     = errors.New("Ссылка входа в Google недействительна или устарела")
	ErrGoogleScopeMissing = errors.New("Доступ к событиям календаря не выдан")

	ErrGoogleRevoked = errors.New("Доступ к Google отозван, подключите календарь заново")
)

type Event struct {
	ID       uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index:idx_calendar_events_user_start,priority:1"`
	Title    string    `gorm:"size:255;not null"`
	Details  string    `gorm:"size:2000"`
	Location string    `gorm:"size:255"`
	StartsAt time.Time `gorm:"not null;index:idx_calendar_events_user_start,priority:2"`

	EndsAt *time.Time

	AllDay bool `gorm:"not null;default:false"`

	Timezone  string `gorm:"size:64"`
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time `gorm:"index"`
}

func (Event) TableName() string { return "calendar_events" }

type Settings struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`

	DigestAt string `gorm:"size:5;not null"`

	Timezone string `gorm:"size:64;not null"`

	Enabled bool `gorm:"not null"`

	LeadMinutes int `gorm:"not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Settings) TableName() string { return "calendar_settings" }

type Delivery struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`

	Ref    string    `gorm:"size:128;primaryKey"`
	Slot   string    `gorm:"size:64;primaryKey"`
	SentAt time.Time `gorm:"not null"`
}

func (Delivery) TableName() string { return "calendar_deliveries" }

type GoogleLink struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey"`

	RefreshToken []byte `gorm:"not null"`
	Email        string `gorm:"size:255"`

	Revoked    bool `gorm:"not null;default:false"`
	LastSyncAt *time.Time
	LastError  string `gorm:"size:500"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (GoogleLink) TableName() string { return "calendar_google_links" }

type GoogleState struct {
	Hash      string    `gorm:"size:64;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	ExpiresAt time.Time `gorm:"not null;index"`
}

func (GoogleState) TableName() string { return "calendar_google_states" }

type ImportedEvent struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey;index:idx_calendar_imported_user_start,priority:1"`

	GoogleID string    `gorm:"size:1024;primaryKey"`
	Title    string    `gorm:"size:1024;not null"`
	Details  string    `gorm:"type:text"`
	Location string    `gorm:"size:1024"`
	StartsAt time.Time `gorm:"not null;index:idx_calendar_imported_user_start,priority:2"`
	EndsAt   *time.Time
	AllDay   bool   `gorm:"not null;default:false"`
	Link     string `gorm:"size:1024"`
}

func (ImportedEvent) TableName() string { return "calendar_imported_events" }
