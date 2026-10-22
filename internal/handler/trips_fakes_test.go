package handler

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"lifelog-api/internal/token"
	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const tripsTestInternalToken = "internal-secret"

var (
	testAuthUserID = uuid.MustParse("0198a1b2-0000-7000-8000-000000000001")
	testAppUserID  = uuid.MustParse("0198a1b2-0000-7000-8000-0000000000a1")
	testBotUserID  = uuid.MustParse("0198a1b2-0000-7000-8000-0000000000b1")
)

type fakeTripsVerifier struct{}

func (fakeTripsVerifier) Parse(_ context.Context, raw string) (*token.Claims, error) {
	switch raw {
	case "good":
		return &token.Claims{Name: "Ann", RegisteredClaims: jwt.RegisteredClaims{Subject: testAuthUserID.String()}}, nil
	case "nameless":
		return &token.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: testAuthUserID.String()}}, nil
	case "bad-sub":
		return &token.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "not-a-uuid"}}, nil
	}
	return nil, errors.New("invalid token")
}

type fakeIdentity struct {
	mu          sync.Mutex
	err         error
	displayName string
	telegramID  int64
	authUserID  uuid.UUID
}

func (f *fakeIdentity) ByAuthUser(_ context.Context, authUserID uuid.UUID, displayName string) (trips.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.displayName, f.authUserID = displayName, authUserID
	if f.err != nil {
		return trips.Actor{}, f.err
	}
	return trips.Actor{UserID: testAppUserID, AuthUserID: authUserID, Username: displayName, Mode: trips.ModeApp}, nil
}

func (f *fakeIdentity) ByTelegram(_ context.Context, telegramID int64, authUserID uuid.UUID, displayName string) (trips.Actor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.displayName, f.telegramID, f.authUserID = displayName, telegramID, authUserID
	if f.err != nil {
		return trips.Actor{}, f.err
	}
	tg := telegramID
	return trips.Actor{UserID: testBotUserID, AuthUserID: authUserID, TelegramID: &tg, Username: displayName, Mode: trips.ModeBot}, nil
}

func (f *fakeIdentity) Identify(actor trips.Actor) trips.BotIdentifyResponse {
	return trips.BotIdentifyResponse{UserID: actor.UserID, TelegramID: actor.TelegramID, Username: actor.Username}
}

type fakeTripService struct {
	err       error
	actor     trips.Actor
	create    trips.CreateTripCommand
	arrive    trips.ArriveCommand
	currency  trips.SetCurrencyCommand
	timezone  trips.SetTimezoneCommand
	createOut trips.CreateTripResponse
	deletedID uuid.UUID
}

func (f *fakeTripService) History(_ context.Context, actor trips.Actor) (trips.TripHistoryResponse, error) {
	f.actor = actor
	return trips.TripHistoryResponse{Items: []trips.TripHistoryItem{}}, f.err
}

func (f *fakeTripService) Active(_ context.Context, actor trips.Actor) (trips.ActiveTripResponse, error) {
	f.actor = actor
	return trips.ActiveTripResponse{}, f.err
}

func (f *fakeTripService) Create(_ context.Context, actor trips.Actor, cmd trips.CreateTripCommand) (trips.CreateTripResponse, error) {
	f.actor, f.create = actor, cmd
	return f.createOut, f.err
}

func (f *fakeTripService) End(_ context.Context, actor trips.Actor, _ trips.EndTripCommand) (trips.EndTripResponse, error) {
	f.actor = actor
	return trips.EndTripResponse{}, f.err
}

func (f *fakeTripService) Arrive(_ context.Context, actor trips.Actor, cmd trips.ArriveCommand) (trips.ArriveResponse, error) {
	f.actor, f.arrive = actor, cmd
	return trips.ArriveResponse{}, f.err
}

func (f *fakeTripService) SetCurrency(_ context.Context, actor trips.Actor, cmd trips.SetCurrencyCommand) (trips.SetCurrencyResponse, error) {
	f.actor, f.currency = actor, cmd
	return trips.SetCurrencyResponse{}, f.err
}

func (f *fakeTripService) SetTimezone(_ context.Context, actor trips.Actor, cmd trips.SetTimezoneCommand) (trips.SetTimezoneResponse, error) {
	f.actor, f.timezone = actor, cmd
	return trips.SetTimezoneResponse{Timezone: "UTC+7"}, f.err
}

func (f *fakeTripService) Delete(_ context.Context, actor trips.Actor, tripID uuid.UUID) (trips.DeletedTrip, error) {
	f.actor, f.deletedID = actor, tripID
	return trips.DeletedTrip{TripID: tripID, Expenses: 2, Photos: 1}, f.err
}

type fakeExpenseService struct {
	err       error
	add       trips.AddExpenseCommand
	list      trips.ListExpensesQuery
	total     trips.DateRange
	deletedID int64
}

func (f *fakeExpenseService) Add(_ context.Context, _ trips.Actor, cmd trips.AddExpenseCommand) (trips.AddExpenseResponse, error) {
	f.add = cmd
	return trips.AddExpenseResponse{CategoryName: "Кафе и рестораны", Amount: cmd.Amount, Currency: "THB", ID: 241}, f.err
}

func (f *fakeExpenseService) List(_ context.Context, _ trips.Actor, q trips.ListExpensesQuery) (trips.ExpenseListResponse, error) {
	f.list = q
	return trips.ExpenseListResponse{Categories: []trips.ExpenseCategoryGroup{}, TotalUSD: trips.MustDecimal("0")}, f.err
}

func (f *fakeExpenseService) Total(_ context.Context, _ trips.Actor, r trips.DateRange) (trips.TotalResponse, error) {
	f.total = r
	return trips.TotalResponse{TotalExpense: trips.MustDecimal("470.00")}, f.err
}

func (f *fakeExpenseService) Duplicates(context.Context, trips.Actor) ([]trips.DuplicatePair, error) {
	return nil, f.err
}

func (f *fakeExpenseService) NotDuplicate(_ context.Context, _ trips.Actor, id int64) error {
	f.deletedID = id
	return f.err
}

func (f *fakeExpenseService) Delete(_ context.Context, _ trips.Actor, id int64) error {
	f.deletedID = id
	return f.err
}

type fakeReportService struct {
	builder trips.ReportBuilderQuery
}

func (f *fakeReportService) Report(context.Context, trips.Actor, trips.DateRange) (trips.ReportResponse, error) {
	return trips.ReportResponse{Result: []trips.ReportCategory{}, TotalAmounts: []trips.CurrencyAmount{}}, nil
}

func (f *fakeReportService) Builder(_ context.Context, _ trips.Actor, q trips.ReportBuilderQuery) (trips.ReportBuilderResponse, error) {
	f.builder = q
	return trips.ReportBuilderResponse{Groups: []trips.ReportGroup{}, Dimensions: []string{}}, nil
}

type fakePhotoService struct {
	err       error
	listTrip  *uuid.UUID
	add       trips.AddPhotoCommand
	addedBody []byte
	download  trips.PhotoDownload
}

func (f *fakePhotoService) List(_ context.Context, _ trips.Actor, tripID *uuid.UUID) (trips.PhotosResponse, error) {
	f.listTrip = tripID
	return trips.PhotosResponse{Result: []trips.PhotoDTO{}}, f.err
}

func (f *fakePhotoService) Add(_ context.Context, _ trips.Actor, cmd trips.AddPhotoCommand) (trips.AddPhotoResponse, error) {
	f.add = cmd
	f.addedBody, _ = io.ReadAll(cmd.Body)
	return trips.AddPhotoResponse{}, f.err
}

func (f *fakePhotoService) Open(context.Context, trips.Actor, uuid.UUID) (trips.PhotoDownload, error) {
	return f.download, f.err
}

func (f *fakePhotoService) Delete(context.Context, trips.Actor, uuid.UUID) error { return f.err }

type fakeDictionaryService struct {
	err             error
	actor           trips.Actor
	includeKeywords bool
	keywords        []string
	categoryID      int64
}

func (f *fakeDictionaryService) Countries(context.Context) (trips.CountriesResponse, error) {
	return trips.CountriesResponse{Result: []trips.CountryDTO{{Code: "TH", Name: "Thailand", NameRu: "Таиланд"}}}, f.err
}

func (f *fakeDictionaryService) Currencies(context.Context) (trips.CurrenciesResponse, error) {
	return trips.CurrenciesResponse{Result: []trips.CurrencyDTO{}}, f.err
}

func (f *fakeDictionaryService) Categories(_ context.Context, includeKeywords bool) ([]trips.CategoryDTO, error) {
	f.includeKeywords = includeKeywords
	return nil, f.err
}

func (f *fakeDictionaryService) CreateCategory(_ context.Context, actor trips.Actor, _ trips.CreateCategoryRequest) (int64, error) {
	f.actor = actor
	if actor.Mode == trips.ModeApp {
		return 0, trips.Forbidden(trips.MsgForbidden)
	}
	return 11, f.err
}

func (f *fakeDictionaryService) AddKeywords(_ context.Context, actor trips.Actor, id int64, keywords []string) error {
	f.actor, f.categoryID, f.keywords = actor, id, keywords
	if actor.Mode == trips.ModeApp {
		return trips.Forbidden(trips.MsgForbidden)
	}
	return f.err
}

type fakeSyncService struct {
	err      error
	actor    trips.Actor
	request  trips.SyncRequest
	response trips.SyncResponse
}

func (f *fakeSyncService) Process(_ context.Context, actor trips.Actor, req trips.SyncRequest) (trips.SyncResponse, error) {
	f.actor, f.request = actor, req
	return f.response, f.err
}

type fakeMergeService struct {
	calls   int
	request trips.MergeRequest
}

func (f *fakeMergeService) Merge(_ context.Context, req trips.MergeRequest) error {
	f.calls++
	f.request = req
	return nil
}

type tripsTestEnv struct {
	engine     *gin.Engine
	identity   *fakeIdentity
	trips      *fakeTripService
	expenses   *fakeExpenseService
	reports    *fakeReportService
	photos     *fakePhotoService
	dictionary *fakeDictionaryService
	sync       *fakeSyncService
	merge      *fakeMergeService
}

func newTripsTestEnv(t *testing.T, internalToken string) *tripsTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	env := &tripsTestEnv{
		engine:     gin.New(),
		identity:   &fakeIdentity{},
		trips:      &fakeTripService{},
		expenses:   &fakeExpenseService{},
		reports:    &fakeReportService{},
		photos:     &fakePhotoService{},
		dictionary: &fakeDictionaryService{},
		sync:       &fakeSyncService{},
		merge:      &fakeMergeService{},
	}
	NewTripsHandler(nil, TripsServices{
		Identity:   env.identity,
		Trips:      env.trips,
		Expenses:   env.expenses,
		Reports:    env.reports,
		Photos:     env.photos,
		Dictionary: env.dictionary,
		Sync:       env.sync,
		Merge:      env.merge,
	}, fakeTripsVerifier{}, internalToken).Register(env.engine)
	return env
}

type tripsRequest struct {
	method  string
	path    string
	body    io.Reader
	headers map[string]string
}

func (env *tripsTestEnv) do(r tripsRequest) *httptest.ResponseRecorder {
	req := httptest.NewRequest(r.method, r.path, r.body)
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	return rec
}

func bearer() map[string]string { return map[string]string{"Authorization": "Bearer good"} }

func botHeaders() map[string]string {
	return map[string]string{
		"X-Internal-Token": tripsTestInternalToken,
		"X-Telegram-Id":    "123456",
		"X-Auth-User-Id":   testAuthUserID.String(),

		"X-Telegram-Name": "%D0%98%D0%B2%D0%B0%D0%BD%20%D0%9F%D0%B5%D1%82%D1%80%D0%BE%D0%B2",
	}
}

func jsonBody(s string) io.Reader { return strings.NewReader(s) }

func withHeader(headers map[string]string, key, value string) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		out[k] = v
	}
	out[key] = value
	return out
}

type recorder = httptest.ResponseRecorder

func newRecorder() *recorder { return httptest.NewRecorder() }
