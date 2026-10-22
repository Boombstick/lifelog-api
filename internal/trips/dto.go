package trips

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ErrorResponse struct {
	Error string `json:"error"`
}

type ValidationProblem struct {
	Type    string              `json:"type"`
	Title   string              `json:"title"`
	Status  int                 `json:"status"`
	Errors  map[string][]string `json:"errors"`
	TraceID string              `json:"traceId"`
}

const (
	ValidationProblemType  = "https://tools.ietf.org/html/rfc9110#section-15.5.1"
	ValidationProblemTitle = "Запрос не прошёл проверку"
)

type EmptyObject struct{}

type CountryDTO struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	NameRu string `json:"name_ru"`
}

type CountriesResponse struct {
	Result []CountryDTO `json:"result"`
}

type CurrencyDTO struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	NameRu string `json:"name_ru"`
	Symbol string `json:"symbol"`
}

type CurrenciesResponse struct {
	Result []CurrencyDTO `json:"result"`
}

type CategoryDTO struct {
	Name     string   `json:"name"`
	ID       int64    `json:"id"`
	Keywords []string `json:"keywords"`
}

type CreateCategoryRequest struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
}

type AddKeywordsRequest struct {
	Keywords []string `json:"keywords"`
}

type TripHistoryItem struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Status       TripStatus `json:"status"`
	IsActive     bool       `json:"is_active"`
	Budget       *Decimal   `json:"budget"`
	StartDate    *Date      `json:"start_date"`
	EndDate      *Date      `json:"end_date"`
	DurationDays int        `json:"duration_days"`
	CountryCode  *string    `json:"country_code"`
	CountryName  *string    `json:"country_name"`
	City         *string    `json:"city"`
	CurrencyCode *string    `json:"currency_code"`
	TotalUSD     Decimal    `json:"total_usd"`
	ExpenseCount int        `json:"expense_count"`
}

type TripHistoryResponse struct {
	Items []TripHistoryItem `json:"items"`
}

type ActiveTripDTO struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Budget       *Decimal  `json:"budget"`
	StartDate    *Date     `json:"start_date"`
	DurationDays int       `json:"duration_days"`
	CountryCode  *string   `json:"country_code"`
	CountryName  *string   `json:"country_name"`
	City         *string   `json:"city"`
	CurrencyCode *string   `json:"currency_code"`
	Timezone     *string   `json:"timezone"`
}

type ActiveTripResponse struct {
	HasActiveTrip bool           `json:"has_active_trip"`
	Trip          *ActiveTripDTO `json:"trip"`
}

type CreateTripRequest struct {
	Name         string   `json:"name"`
	CountryName  *string  `json:"country_name"`
	City         *string  `json:"city"`
	Budget       *Decimal `json:"budget"`
	CurrencyCode *string  `json:"currency_code"`

	StartDate *string `json:"start_date"`
}

const MsgStartDateFormat = "Дата начала поездки должна быть в формате ГГГГ-ММ-ДД"

func (r CreateTripRequest) ParseStartDate() (date *Date, ok bool) {
	if r.StartDate == nil || strings.TrimSpace(*r.StartDate) == "" {
		return nil, true
	}
	parsed, err := ParseDate(strings.TrimSpace(*r.StartDate))
	if err != nil {
		return nil, false
	}
	return &parsed, true
}

type CreateTripResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	CountryName  *string   `json:"country_name"`
	City         *string   `json:"city"`
	CurrencyCode *string   `json:"currency_code"`
	StartDate    *Date     `json:"start_date"`
}

type EndTripResponse struct {
	Name      string `json:"name"`
	StartDate *Date  `json:"start_date"`
	EndDate   *Date  `json:"end_date"`
}

type ArriveRequest struct {
	CountryName string  `json:"country_name"`
	City        *string `json:"city"`
}

type ArriveResponse struct {
	TripID          uuid.UUID `json:"trip_id"`
	CountryName     string    `json:"country_name"`
	City            *string   `json:"city"`
	CurrencyName    string    `json:"currency_name"`
	CurrencyCode    string    `json:"currency_code"`
	IsMultiCurrency bool      `json:"is_multi_currency"`
	IsMultiTimezone bool      `json:"is_multi_timezone"`
	StartedNewTrip  bool      `json:"started_new_trip"`
}

type SetCurrencyRequest struct {
	Currency string     `json:"currency"`
	TripID   *uuid.UUID `json:"trip_id"`
}

type SetCurrencyResponse struct {
	CurrencyCode string `json:"currency_code"`
	CurrencyName string `json:"currency_name"`
}

type SetTimezoneRequest struct {
	Latitude   Float      `json:"latitude"`
	Longtitude Float      `json:"longtitude"`
	TripID     *uuid.UUID `json:"trip_id"`
}

type SetTimezoneResponse struct {
	Timezone string `json:"timezone"`
}

type PhotoDTO struct {
	ID        uuid.UUID `json:"id"`
	TripID    uuid.UUID `json:"trip_id"`
	FileName  *string   `json:"file_name"`
	Caption   *string   `json:"caption"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

type PhotosResponse struct {
	Result []PhotoDTO `json:"result"`
}

type AddPhotoResponse struct {
	ID        uuid.UUID `json:"id"`
	TripID    uuid.UUID `json:"trip_id"`
	FileName  *string   `json:"file_name"`
	Caption   *string   `json:"caption"`
	CreatedAt time.Time `json:"created_at"`
}

type AddExpenseRequest struct {
	Amount      Decimal        `json:"amount"`
	Currency    *string        `json:"currency"`
	Description string         `json:"description"`
	Date        *DateTimeInput `json:"date"`
}

type AddExpenseResponse struct {
	CategoryName string  `json:"category_name"`
	Amount       Decimal `json:"amount"`
	Currency     string  `json:"currency"`
	ID           int64   `json:"id"`
}

type ExpenseListItem struct {
	ID            int64   `json:"id"`
	Amount        Decimal `json:"amount"`
	CurrencyCode  string  `json:"currency_code"`
	AmountUSD     Decimal `json:"amount_usd"`
	Description   string  `json:"description"`
	OccurredAt    Date    `json:"occurred_at"`
	DuplicateOfID *int64  `json:"duplicate_of_id"`
}

type ExpenseCategoryGroup struct {
	CategoryID   int64             `json:"category_id"`
	CategoryName string            `json:"category_name"`
	TotalUSD     Decimal           `json:"total_usd"`
	Count        int               `json:"count"`
	Items        []ExpenseListItem `json:"items"`
}

type ExpenseListResponse struct {
	Categories []ExpenseCategoryGroup `json:"categories"`
	TotalUSD   Decimal                `json:"total_usd"`
	Count      int                    `json:"count"`
}

type TotalResponse struct {
	TotalExpense Decimal `json:"total_expense"`
}

type CurrencyAmount struct {
	Currency string  `json:"currency"`
	Amount   Decimal `json:"amount"`
}

type ReportCategory struct {
	CategoryName string           `json:"category_name"`
	Amounts      []CurrencyAmount `json:"amounts"`
}

type ReportResponse struct {
	Result       []ReportCategory `json:"result"`
	TotalAmounts []CurrencyAmount `json:"total_amounts"`
	From         Date             `json:"from"`
	To           Date             `json:"to"`
}

type ReportExpense struct {
	ID           int64   `json:"id"`
	OccurredAt   Date    `json:"occurred_at"`
	Description  string  `json:"description"`
	Amount       Decimal `json:"amount"`
	CurrencyCode string  `json:"currency_code"`
	AmountUSD    Decimal `json:"amount_usd"`
	CategoryName string  `json:"category_name"`
	TripName     string  `json:"trip_name"`
	City         string  `json:"city"`
}

type ReportGroup struct {
	TripID       *uuid.UUID       `json:"trip_id"`
	TripName     *string          `json:"trip_name"`
	City         *string          `json:"city"`
	CategoryID   *int64           `json:"category_id"`
	CategoryName *string          `json:"category_name"`
	Count        int              `json:"count"`
	TotalUSD     Decimal          `json:"total_usd"`
	Amounts      []CurrencyAmount `json:"amounts"`
	Expenses     []ReportExpense  `json:"expenses"`
}

type ReportBuilderResponse struct {
	Groups     []ReportGroup `json:"groups"`
	Dimensions []string      `json:"dimensions"`
	TotalCount int           `json:"total_count"`
	TotalUSD   Decimal       `json:"total_usd"`
	From       *Date         `json:"from"`
	To         *Date         `json:"to"`
	Truncated  bool          `json:"truncated"`
}

type ExpenseDTO struct {
	ID            int64      `json:"id"`
	Amount        Decimal    `json:"amount"`
	CurrencyCode  string     `json:"currency_code"`
	AmountUSD     Decimal    `json:"amount_usd"`
	Description   string     `json:"description"`
	OccurredAt    Date       `json:"occurred_at"`
	DuplicateOfID *int64     `json:"duplicate_of_id"`
	TripID        uuid.UUID  `json:"trip_id"`
	ClientID      *uuid.UUID `json:"client_id"`
	CategoryID    int64      `json:"category_id"`
	CategoryName  string     `json:"category_name"`
}

type DuplicatePair struct {
	Expense     ExpenseDTO `json:"expense"`
	DuplicateOf ExpenseDTO `json:"duplicate_of"`
}

type TripDTO struct {
	ID           uuid.UUID   `json:"id"`
	Name         string      `json:"name"`
	Budget       *Decimal    `json:"budget"`
	StartDate    *Date       `json:"start_date"`
	DurationDays int         `json:"duration_days"`
	CountryCode  *string     `json:"country_code"`
	CountryName  *string     `json:"country_name"`
	City         *string     `json:"city"`
	CurrencyCode *string     `json:"currency_code"`
	Timezone     *string     `json:"timezone"`
	Status       TripStatus  `json:"status"`
	EndDate      *Date       `json:"end_date"`
	ClientIDs    []uuid.UUID `json:"client_ids"`
}

type SyncRequest struct {
	Operations *[]*SyncOperation `json:"operations"`
}

type SyncOperation struct {
	OpID       uuid.UUID       `json:"op_id"`
	Type       string          `json:"type"`
	OccurredAt *DateTimeInput  `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

const (
	OpTripCreate      = "trip.create"
	OpTripArrive      = "trip.arrive"
	OpTripEnd         = "trip.end"
	OpTripSetCurrency = "trip.setCurrency"
	OpTripSetTimezone = "trip.setTimezone"
	OpTripDelete      = "trip.delete"
	OpExpenseAdd      = "expense.add"
)

const (
	SyncStatusOK       = "ok"
	SyncStatusConflict = "conflict"
	SyncStatusBlocked  = "blocked"
	SyncStatusError    = "error"
)

type SyncTripRef struct {
	ID       *uuid.UUID `json:"id"`
	ClientID *uuid.UUID `json:"client_id"`
}

type SyncTripCreatePayload struct {
	ClientID     *uuid.UUID `json:"client_id"`
	Name         *string    `json:"name"`
	CountryName  *string    `json:"country_name"`
	City         *string    `json:"city"`
	Budget       *Decimal   `json:"budget"`
	CurrencyCode *string    `json:"currency_code"`
	Resolution   *string    `json:"resolution"`
	SameAsTripID *uuid.UUID `json:"same_as_trip_id"`
}

type SyncTripArrivePayload struct {
	Trip            *SyncTripRef `json:"trip"`
	CountryName     *string      `json:"country_name"`
	City            *string      `json:"city"`
	NewTripClientID *uuid.UUID   `json:"new_trip_client_id"`
}

type SyncTripEndPayload struct {
	Trip *SyncTripRef `json:"trip"`
}

type SyncTripSetCurrencyPayload struct {
	Trip         *SyncTripRef `json:"trip"`
	CurrencyCode *string      `json:"currency_code"`
}

type SyncTripSetTimezonePayload struct {
	Trip      *SyncTripRef `json:"trip"`
	Latitude  *Float       `json:"latitude"`
	Longitude *Float       `json:"longitude"`
}

type SyncTripDeletePayload struct {
	Trip *SyncTripRef `json:"trip"`
}

type SyncExpenseAddPayload struct {
	ClientID    *uuid.UUID   `json:"client_id"`
	Trip        *SyncTripRef `json:"trip"`
	Amount      *Decimal     `json:"amount"`
	Currency    *string      `json:"currency"`
	Description *string      `json:"description"`
}

type SyncConflict struct {
	Kind     string  `json:"kind"`
	Existing TripDTO `json:"existing"`
}

const SyncConflictSimilarActiveTrip = "similarActiveTrip"

type SyncResult struct {
	OpID         uuid.UUID       `json:"op_id"`
	Status       string          `json:"status"`
	Trip         *TripDTO        `json:"trip,omitempty"`
	Expense      *ExpenseDTO     `json:"expense,omitempty"`
	Arrival      *ArriveResponse `json:"arrival,omitempty"`
	ClosedTripID *uuid.UUID      `json:"closed_trip_id,omitempty"`

	DeletedTripID *uuid.UUID    `json:"deleted_trip_id,omitempty"`
	Conflict      *SyncConflict `json:"conflict,omitempty"`
	Error         *string       `json:"error,omitempty"`
	Retryable     *bool         `json:"retryable,omitempty"`
}

type SyncResponse struct {
	Results    []json.RawMessage `json:"results"`
	ActiveTrip *TripDTO          `json:"active_trip"`
}

type MergeRequest struct {
	From uuid.UUID `json:"from"`
	Into uuid.UUID `json:"into"`
}

type BotIdentifyResponse struct {
	UserID     uuid.UUID `json:"user_id"`
	TelegramID *int64    `json:"telegram_id"`
	Username   string    `json:"username"`
}
