package trips

import (
	"strconv"
	"time"

	"github.com/google/uuid"
)

type TripStatus int

const (
	StatusPlanned   TripStatus = 1
	StatusActive    TripStatus = 2
	StatusCompleted TripStatus = 3
)

func (s TripStatus) String() string {
	switch s {
	case StatusPlanned:
		return "Planned"
	case StatusActive:
		return "Active"
	case StatusCompleted:
		return "Completed"
	}
	return strconv.Itoa(int(s))
}

func (s TripStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

type Source int

const (
	SourceBot Source = 1
	SourceApp Source = 2
)

type ActorMode int

const (
	ModeApp ActorMode = iota + 1

	ModeBot

	ModeInternal
)

type Actor struct {
	UserID     uuid.UUID
	AuthUserID uuid.UUID
	TelegramID *int64
	Username   string
	Mode       ActorMode
}

func (a Actor) Source() Source {
	if a.Mode == ModeBot {
		return SourceBot
	}
	return SourceApp
}

const (
	MaxUsernameLen         = 100
	MaxCityLen             = 120
	MaxTimezoneLen         = 64
	MaxDescriptionLen      = 250
	MaxCategoryNameLen     = 50
	MaxCaptionLen          = 500
	MaxOriginalFileNameLen = 255
	MaxContentTypeLen      = 64
)

const (
	OtherCategoryID   int64 = 10
	OtherCategoryName       = "Прочее"

	MaxPhotoBytes        int64 = 15 * 1024 * 1024
	MaxPhotoRequestBytes int64 = 20 * 1024 * 1024

	BuilderExpenseLimit = 2000

	USD = "USD"
)

type User struct {
	ID         uuid.UUID
	TelegramID *int64
	Username   string
	CreatedAt  time.Time
	AuthUserID *uuid.UUID
}

type Currency struct {
	Code   string
	Name   string
	NameRu string
	Symbol string
}

type Country struct {
	Code                string
	Name                string
	NameRu              string
	DefaultCurrencyCode string
	IsMultiCurrency     bool
	DefaultTimezone     string
	IsMultiTimezone     bool
}

type Category struct {
	ID       int64
	Name     string
	Keywords []string
}

type CategoryRef struct {
	ID   int64
	Name string
}

type Snapshot struct {
	ID           int64
	BaseCurrency string
	Rates        map[string]Decimal
	CreatedAt    time.Time
}

type Trip struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Name         string
	Budget       *Decimal
	StartDate    *Date
	EndDate      *Date
	Status       TripStatus
	City         *string
	CountryCode  *string
	CurrencyCode *string
	Timezone     *string
	Source       Source
	CreatedAt    time.Time
	UpdatedAt    time.Time

	CountryName *string
}

type TripSummary struct {
	Trip
	TotalUSD     Decimal
	ExpenseCount int
}

type Expense struct {
	ID            int64
	TripID        uuid.UUID
	Amount        Decimal
	AmountUSD     Decimal
	OccurredAt    Date
	Description   string
	CurrencyCode  string
	SnapshotID    int64
	CategoryID    int64
	CreatedAt     time.Time
	ClientID      *uuid.UUID
	DuplicateOfID *int64
	Source        Source
}

type ExpenseDetail struct {
	Expense
	CategoryName string
	TripUserID   uuid.UUID
	TripName     string
	TripCity     *string
}

type LookAlikeQuery struct {
	TripID       uuid.UUID
	Amount       Decimal
	CurrencyCode string
	OccurredAt   Date
	Source       Source
}

type ExpenseFilter struct {
	UserID uuid.UUID

	TripID     *uuid.UUID
	CategoryID *int64
}

type DateRange struct {
	From *Date
	To   *Date
}

type ReportRow struct {
	CategoryName string
	Amount       Decimal
	CurrencyCode string
}

type Photo struct {
	ID               uuid.UUID
	TripID           uuid.UUID
	StoredFileName   string
	OriginalFileName *string
	ContentType      string
	SizeBytes        int64
	Caption          *string
	CreatedAt        time.Time
}

type SeedData struct {
	Currencies []Currency
	Countries  []Country
	Categories []Category
}
