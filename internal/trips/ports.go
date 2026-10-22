package trips

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

type Store interface {
	InTx(ctx context.Context, fn func(tx Store) error) error

	UserStore
	DictionaryStore
	TripStore
	ExpenseStore
	PhotoRecordStore
	SyncStore
}

type UserStore interface {
	UserByAuthID(ctx context.Context, authUserID uuid.UUID) (*User, error)
	UserByTelegramID(ctx context.Context, telegramID int64) (*User, error)

	InsertUser(ctx context.Context, user *User) error
	SetUserAuthID(ctx context.Context, userID, authUserID uuid.UUID) error

	SetUserTelegramID(ctx context.Context, userID uuid.UUID, telegramID *int64) error

	DeleteUser(ctx context.Context, userID uuid.UUID) error
}

type DictionaryStore interface {
	Countries(ctx context.Context) ([]Country, error)

	Currencies(ctx context.Context) ([]Currency, error)

	CountryByName(ctx context.Context, name string) (*Country, error)

	CurrencyByCode(ctx context.Context, code string) (*Currency, error)

	Categories(ctx context.Context) ([]Category, error)
	CategoryByID(ctx context.Context, id int64) (*Category, error)

	InsertCategory(ctx context.Context, category *Category) error

	SetCategoryKeywords(ctx context.Context, id int64, keywords []string) error

	SeedDictionaries(ctx context.Context, data SeedData) error
}

type TripStore interface {
	ActiveTrips(ctx context.Context, userID uuid.UUID) ([]Trip, error)

	OwnedTrip(ctx context.Context, userID, tripID uuid.UUID) (*Trip, error)

	TripsOfUser(ctx context.Context, userID uuid.UUID) ([]Trip, error)

	TripHistory(ctx context.Context, userID uuid.UUID) ([]TripSummary, error)

	InsertTrip(ctx context.Context, trip *Trip) error

	UpdateTrip(ctx context.Context, trip *Trip) error

	DeleteTrip(ctx context.Context, userID, tripID uuid.UUID) (found bool, err error)

	TripIDByClientID(ctx context.Context, userID, clientID uuid.UUID) (*uuid.UUID, error)

	InsertTripClientID(ctx context.Context, userID, clientID, tripID uuid.UUID) error

	TripClientIDs(ctx context.Context, tripID uuid.UUID) ([]uuid.UUID, error)
}

type ExpenseStore interface {
	ExpenseByClientID(ctx context.Context, clientID uuid.UUID) (*ExpenseDetail, error)

	OwnedExpense(ctx context.Context, userID uuid.UUID, id int64) (*ExpenseDetail, error)

	LookAlike(ctx context.Context, q LookAlikeQuery) (*int64, error)

	InsertExpense(ctx context.Context, expense *Expense) error

	ListExpenses(ctx context.Context, filter ExpenseFilter) ([]ExpenseDetail, error)

	TotalAmount(ctx context.Context, userID uuid.UUID, r DateRange) (Decimal, error)

	ReportRows(ctx context.Context, userID uuid.UUID, r DateRange) ([]ReportRow, error)

	BuilderRows(ctx context.Context, userID uuid.UUID, r DateRange) ([]ExpenseDetail, error)

	FlaggedExpenses(ctx context.Context, userID uuid.UUID) ([]ExpenseDetail, error)

	ClearDuplicateFlag(ctx context.Context, userID uuid.UUID, id int64) (found bool, err error)

	DeleteExpense(ctx context.Context, userID uuid.UUID, id int64) (found bool, err error)

	DeleteExpensesOfTrip(ctx context.Context, tripID uuid.UUID) (int, error)
}

type PhotoRecordStore interface {
	TripPhotos(ctx context.Context, userID, tripID uuid.UUID) ([]Photo, error)
	OwnedPhoto(ctx context.Context, userID, photoID uuid.UUID) (*Photo, error)

	InsertPhoto(ctx context.Context, photo *Photo) error
	DeletePhoto(ctx context.Context, photoID uuid.UUID) error

	DeleteTripPhotos(ctx context.Context, tripID uuid.UUID) (int, error)
}

type SyncStore interface {
	SyncResult(ctx context.Context, userID, opID uuid.UUID) (json.RawMessage, error)

	SaveSyncResult(ctx context.Context, userID, opID uuid.UUID, result json.RawMessage, processedAt time.Time) error

	ReassignSyncData(ctx context.Context, fromUserID, toUserID uuid.UUID) error
}

type Rates interface {
	Latest(ctx context.Context) (*Snapshot, error)

	Closest(ctx context.Context, at time.Time) (*Snapshot, error)
}

type CategorySearch interface {
	Search(ctx context.Context, description string) (*CategoryRef, error)
}

type CategoryIndex interface {
	Ensure(ctx context.Context) error

	SyncAll(ctx context.Context, categories []Category) error

	Put(ctx context.Context, category Category) error

	AddKeywords(ctx context.Context, id int64, keywords []string) error
}

var ErrPhotoFileMissing = errors.New("trips: photo file is missing")

var ErrUnsafePhotoPath = errors.New("Недопустимый путь к файлу фотографии")

type PhotoStore interface {
	Save(ctx context.Context, name string, body io.Reader) error

	Open(ctx context.Context, name string) (io.ReadCloser, error)

	Delete(ctx context.Context, name string) error
}

type TimezoneLookup interface {
	Lookup(latitude, longitude float64) (string, error)

	BaseOffset(timezone string, year int) (time.Duration, error)
}

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type IdentityCache interface {
	ByAuth(authUserID uuid.UUID) (User, bool)
	PutByAuth(authUserID uuid.UUID, user User)
	ByTelegram(telegramID int64, authUserID uuid.UUID) (User, bool)
	PutByTelegram(telegramID int64, authUserID uuid.UUID, user User)

	InvalidateUser(userID uuid.UUID)
}
