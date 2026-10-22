package trips

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type m4SyncFixture struct {
	store *m4Store
	rates *fakeRates
	files *fakeFiles
	svc   *SyncService
	actor Actor
}

func m4NewSync() *m4SyncFixture {
	store := m4NewStore()
	clock := fixedClock{testNow}
	tz := &fakeTZ{zone: "Asia/Bangkok", offset: 7 * time.Hour}
	rates := &fakeRates{snapshots: []Snapshot{
		{ID: 110, CreatedAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
			Rates: map[string]Decimal{"USD": MustDecimal("1.0"), "THB": MustDecimal("33.0608"), "RUB": MustDecimal("80")}},
		{ID: 111, CreatedAt: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
			Rates: map[string]Decimal{"USD": MustDecimal("1.0"), "THB": MustDecimal("35"), "RUB": MustDecimal("80")}},
	}}
	search := &fakeSearch{hits: map[string]CategoryRef{"кофе": {ID: 3, Name: "Кафе и рестораны"}}}
	dictionary := NewDictionaryService(store, &fakeIndex{}, quietLogger)
	files := newFakeFiles()
	tripService := NewTripService(store, tz, files, clock, quietLogger)
	expenses := NewExpenseService(store, dictionary, search, rates, clock, quietLogger)
	user := store.m4PutUser(User{Username: "u"})
	return &m4SyncFixture{
		store: store,
		rates: rates,
		files: files,
		svc:   NewSyncService(store, tripService, expenses, clock, quietLogger),
		actor: appActor(user.ID),
	}
}

func m4Op(t *testing.T, typ, occurredAt string, payload any) *SyncOperation {
	t.Helper()
	var raw json.RawMessage
	switch p := payload.(type) {
	case string:
		raw = json.RawMessage(p)
	case nil:
	default:
		data, err := json.Marshal(p)
		m4NoErr(t, err)
		raw = data
	}
	at, err := ParseDateTimeInput(occurredAt)
	m4NoErr(t, err)
	return &SyncOperation{OpID: uuid.New(), Type: typ, OccurredAt: &at, Payload: raw}
}

func (f *m4SyncFixture) run(t *testing.T, ops ...*SyncOperation) SyncResponse {
	t.Helper()
	resp, err := f.svc.Process(bg, f.actor, SyncRequest{Operations: &ops})
	m4NoErr(t, err)
	if len(resp.Results) != len(ops) {
		t.Fatalf("results: got %d, want %d", len(resp.Results), len(ops))
	}
	return resp
}

type m4TripView struct {
	ID           uuid.UUID   `json:"id"`
	Name         string      `json:"name"`
	Status       string      `json:"status"`
	StartDate    *Date       `json:"start_date"`
	EndDate      *Date       `json:"end_date"`
	CountryCode  *string     `json:"country_code"`
	City         *string     `json:"city"`
	CurrencyCode *string     `json:"currency_code"`
	Timezone     *string     `json:"timezone"`
	Budget       *Decimal    `json:"budget"`
	ClientIDs    []uuid.UUID `json:"client_ids"`
}

type m4ResultView struct {
	OpID    uuid.UUID   `json:"op_id"`
	Status  string      `json:"status"`
	Trip    *m4TripView `json:"trip"`
	Expense *struct {
		ID            int64      `json:"id"`
		Amount        Decimal    `json:"amount"`
		AmountUSD     Decimal    `json:"amount_usd"`
		CurrencyCode  string     `json:"currency_code"`
		OccurredAt    Date       `json:"occurred_at"`
		DuplicateOfID *int64     `json:"duplicate_of_id"`
		TripID        uuid.UUID  `json:"trip_id"`
		ClientID      *uuid.UUID `json:"client_id"`
		CategoryName  string     `json:"category_name"`
	} `json:"expense"`
	Arrival       *ArriveResponse `json:"arrival"`
	ClosedTripID  *uuid.UUID      `json:"closed_trip_id"`
	DeletedTripID *uuid.UUID      `json:"deleted_trip_id"`
	Conflict      *struct {
		Kind     string     `json:"kind"`
		Existing m4TripView `json:"existing"`
	} `json:"conflict"`
	Error     *string `json:"error"`
	Retryable *bool   `json:"retryable"`
}

func m4Result(t *testing.T, raw json.RawMessage) m4ResultView {
	t.Helper()
	var v m4ResultView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func m4WantStatus(t *testing.T, raw json.RawMessage, status string) m4ResultView {
	t.Helper()
	v := m4Result(t, raw)
	if v.Status != status {
		t.Fatalf("status: got %s, want %s (%s)", v.Status, status, raw)
	}
	return v
}

func m4WantError(t *testing.T, raw json.RawMessage, message string, retryable bool) {
	t.Helper()
	v := m4WantStatus(t, raw, SyncStatusError)
	if v.Error == nil || *v.Error != message {
		t.Fatalf("error: got %s, want %q", raw, message)
	}
	if v.Retryable == nil || *v.Retryable != retryable {
		t.Fatalf("retryable: got %s, want %v", raw, retryable)
	}
}

func m4Ref(clientID uuid.UUID) map[string]any { return map[string]any{"client_id": clientID} }

func TestSyncRejectsMalformedRequest(t *testing.T) {
	f := m4NewSync()
	good := func() *SyncOperation {
		return m4Op(t, OpTripCreate, "2026-09-16T10:00:00+07:00", map[string]any{"client_id": uuid.New(), "name": "x"})
	}
	withOp := func(mutate func(*SyncOperation)) []*SyncOperation {
		op := good()
		mutate(op)
		return []*SyncOperation{good(), op}
	}
	id := uuid.New()
	cases := []struct {
		name string
		ops  *[]*SyncOperation
		want string
	}{
		{"operations missing", nil, "Нет списка операций"},
		{"null element", &[]*SyncOperation{good(), nil}, "У каждой операции должен быть op_id"},
		{"zero opId", ptr(withOp(func(op *SyncOperation) { op.OpID = uuid.Nil })), "У каждой операции должен быть op_id"},
		{"blank type", ptr(withOp(func(op *SyncOperation) { op.OpID = id; op.Type = " " })), fmt.Sprintf("У операции %s не указан тип", id)},
		{"unknown type", ptr(withOp(func(op *SyncOperation) { op.OpID = id; op.Type = "trip.rename" })), fmt.Sprintf("У операции %s неизвестный тип 'trip.rename'", id)},
		{"no occurredAt", ptr(withOp(func(op *SyncOperation) { op.OpID = id; op.OccurredAt = nil })), fmt.Sprintf("У операции %s не указано время occurred_at", id)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.svc.Process(bg, f.actor, SyncRequest{Operations: c.ops})
			wantErr(t, err, KindValidation, c.want)
			m4Expect(t, "nothing applied", f.store.m4TripCount(), 0)
		})
	}
}

func TestSyncEmptyOperations(t *testing.T) {
	f := m4NewSync()
	resp := f.run(t)
	data, err := json.Marshal(resp)
	m4NoErr(t, err)
	m4Expect(t, "body", string(data), `{"results":[],"active_trip":null}`)
}

func TestSyncTripCreateAndReplay(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()

	op := m4Op(t, OpTripCreate, "2026-09-16T23:30:00-05:00", map[string]any{
		"client_id": clientID, "name": "Asia", "country_name": "Таиланд", "city": "Пхукет", "budget": "500",
	})

	first := f.run(t, op)
	v := m4WantStatus(t, first.Results[0], SyncStatusOK)
	if v.Trip == nil {
		t.Fatalf("no trip in %s", first.Results[0])
	}
	m4Expect(t, "start date", *v.Trip.StartDate, NewDate(2026, 9, 17))
	m4Expect(t, "status", v.Trip.Status, "Active")
	m4Expect(t, "country", *v.Trip.CountryCode, "TH")
	m4Expect(t, "budget", v.Trip.Budget.String(), "500")
	m4Expect(t, "client ids", len(v.Trip.ClientIDs), 1)
	m4Expect(t, "client id", v.Trip.ClientIDs[0], clientID)
	if v.ClosedTripID != nil || v.Arrival != nil || v.Expense != nil || v.Conflict != nil {
		t.Fatalf("unexpected members in %s", first.Results[0])
	}
	if first.ActiveTrip == nil || first.ActiveTrip.ID != v.Trip.ID {
		t.Fatal("activeTrip must be the created trip")
	}
	m4Expect(t, "source", f.store.trip(v.Trip.ID).Source, SourceApp)
	m4Expect(t, "stored results", f.store.m4SyncCount(), 1)

	trip := f.store.trip(v.Trip.ID)
	trip.Name = "renamed"
	f.store.putTrip(trip)

	second := f.run(t, op)
	if !bytes.Equal(second.Results[0], first.Results[0]) {
		t.Fatalf("replay differs:\n%s\n%s", second.Results[0], first.Results[0])
	}
	m4Expect(t, "trips after replay", f.store.m4TripCount(), 1)
}

func TestSyncTripCreateKnownClientID(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()
	payload := map[string]any{"client_id": clientID, "name": "Asia"}
	first := m4WantStatus(t, f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", payload)).Results[0], SyncStatusOK)

	second := m4WantStatus(t, f.run(t, m4Op(t, OpTripCreate, "2026-09-16T11:00:00Z", payload)).Results[0], SyncStatusOK)
	m4Expect(t, "same trip", second.Trip.ID, first.Trip.ID)
	if second.ClosedTripID != nil {
		t.Fatal("closedTripId must not be set")
	}
	m4Expect(t, "trips", f.store.m4TripCount(), 1)
}

func TestSyncSimilarConflictWritesNothingAndBlocks(t *testing.T) {
	f := m4NewSync()
	start := NewDate(2026, 9, 10)
	active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CountryCode: ptr("TH"), City: ptr("Пхукет"),
		CurrencyCode: ptr("THB"), StartDate: &start})

	offline, moved := uuid.New(), uuid.New()
	create := m4Op(t, OpTripCreate, "2026-09-16T10:00:00+07:00", map[string]any{
		"client_id": offline, "name": "offline", "country_name": "Thailand",
	})
	expense := m4Op(t, OpExpenseAdd, "2026-09-16T11:00:00+07:00", map[string]any{
		"client_id": uuid.New(), "trip": m4Ref(offline), "amount": 350, "description": "кофе",
	})
	arrive := m4Op(t, OpTripArrive, "2026-09-16T12:00:00+07:00", map[string]any{
		"trip": m4Ref(offline), "country_name": "Россия", "new_trip_client_id": moved,
	})

	end := m4Op(t, OpTripEnd, "2026-09-16T13:00:00+07:00", map[string]any{"trip": m4Ref(moved)})
	other := m4Op(t, OpExpenseAdd, "2026-09-16T14:00:00+07:00", map[string]any{
		"client_id": uuid.New(), "trip": map[string]any{"id": active.ID}, "amount": 100, "description": "кофе",
	})

	resp := f.run(t, create, expense, arrive, end, other)

	conflict := m4WantStatus(t, resp.Results[0], SyncStatusConflict)
	m4Expect(t, "kind", conflict.Conflict.Kind, SyncConflictSimilarActiveTrip)
	m4Expect(t, "existing", conflict.Conflict.Existing.ID, active.ID)
	m4Expect(t, "op_id", conflict.OpID, create.OpID)
	if conflict.Trip != nil || conflict.Error != nil {
		t.Fatalf("conflict carries extra members: %s", resp.Results[0])
	}
	for i, op := range []*SyncOperation{expense, arrive, end} {
		v := m4WantStatus(t, resp.Results[i+1], SyncStatusBlocked)
		m4Expect(t, "blocked opId", v.OpID, op.OpID)
		m4Expect(t, "blocked body", string(resp.Results[i+1]), fmt.Sprintf(`{"op_id":"%s","status":"blocked"}`, op.OpID))
	}
	m4WantStatus(t, resp.Results[4], SyncStatusOK)

	m4Expect(t, "trips", f.store.m4TripCount(), 1)
	m4Expect(t, "stored results (only the ok one)", f.store.m4SyncCount(), 1)
	if id, _ := f.store.TripIDByClientID(bg, f.actor.UserID, offline); id != nil {
		t.Fatal("conflict must not store the client id")
	}
	m4Expect(t, "active trip", resp.ActiveTrip.ID, active.ID)
}

func TestSyncStoredResultWinsOverBlocking(t *testing.T) {
	f := m4NewSync()
	start := NewDate(2026, 9, 10)
	f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", StartDate: &start})
	offline := uuid.New()

	create := m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": offline, "name": "offline"})
	applied := m4Op(t, OpTripEnd, "2026-09-16T11:00:00Z", map[string]any{"trip": m4Ref(offline)})
	stored := json.RawMessage(fmt.Sprintf(`{"op_id":"%s","status":"ok"}`, applied.OpID))
	m4NoErr(t, f.store.memStore.SaveSyncResult(bg, f.actor.UserID, applied.OpID, stored, testNow))

	resp := f.run(t, create, applied)
	m4WantStatus(t, resp.Results[0], SyncStatusConflict)
	m4Expect(t, "replayed", string(resp.Results[1]), string(stored))
}

func TestSyncResolutionSame(t *testing.T) {
	f := m4NewSync()
	start := NewDate(2026, 9, 10)
	existing := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", City: ptr("Бангкок"), StartDate: &start})
	offline := uuid.New()

	create := m4Op(t, OpTripCreate, "2026-09-16T10:00:00+07:00", map[string]any{
		"client_id": offline, "name": "offline", "country_name": "Таиланд", "city": "Пхукет",
		"currency_code": "usd", "budget": 700, "resolution": "same", "same_as_trip_id": existing.ID,
	})
	expense := m4Op(t, OpExpenseAdd, "2026-09-16T11:00:00+07:00", map[string]any{
		"client_id": uuid.New(), "trip": m4Ref(offline), "amount": 5, "description": "кофе",
	})
	resp := f.run(t, create, expense)

	v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	m4Expect(t, "trip", v.Trip.ID, existing.ID)
	m4Expect(t, "currency filled from payload", *v.Trip.CurrencyCode, "USD")
	m4Expect(t, "country filled", *v.Trip.CountryCode, "TH")
	m4Expect(t, "zone filled from the country", *v.Trip.Timezone, "Asia/Bangkok")
	m4Expect(t, "city kept", *v.Trip.City, "Бангкок")
	m4Expect(t, "budget filled", v.Trip.Budget.String(), "700")
	m4Expect(t, "alias", v.Trip.ClientIDs[0], offline)
	m4Expect(t, "no new trip", f.store.m4TripCount(), 1)

	e := m4WantStatus(t, resp.Results[1], SyncStatusOK)
	m4Expect(t, "expense trip", e.Expense.TripID, existing.ID)
	m4Expect(t, "expense currency", e.Expense.CurrencyCode, "USD")
}

func TestSyncResolutionSameErrors(t *testing.T) {
	f := m4NewSync()
	existing := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server"})

	resp := f.run(t,
		m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "name": "x", "resolution": "same",
		}),
		m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "name": "x", "resolution": "same", "same_as_trip_id": uuid.New(),
		}),
		m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "name": "x", "resolution": "same", "same_as_trip_id": existing.ID, "currency_code": "xyz",
		}),
	)
	m4WantError(t, resp.Results[0], "Для решения 'same' нужен same_as_trip_id", false)
	m4WantError(t, resp.Results[1], MsgTripNotFound, false)
	m4WantError(t, resp.Results[2], "Валюта xyz не найдена", false)
	if got := f.store.trip(existing.ID).CurrencyCode; got != nil {
		t.Fatalf("currency written despite the error: %v", *got)
	}
}

func TestSyncResolutionNewClosesActive(t *testing.T) {
	cases := []struct {
		name       string
		occurredAt string
		wantEnd    Date
		wantStart  Date
	}{
		{"on the new start date", "2026-09-15T10:00:00+07:00", NewDate(2026, 9, 15), NewDate(2026, 9, 15)},
		{"never before its own start", "2026-09-05T10:00:00+07:00", NewDate(2026, 9, 10), NewDate(2026, 9, 5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := m4NewSync()
			start := NewDate(2026, 9, 10)
			active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CountryCode: ptr("TH"), StartDate: &start})

			resp := f.run(t, m4Op(t, OpTripCreate, c.occurredAt, map[string]any{
				"client_id": uuid.New(), "name": "offline", "country_name": "Таиланд", "resolution": "new",
			}))
			v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
			m4Expect(t, "closed", *v.ClosedTripID, active.ID)
			m4Expect(t, "new start", *v.Trip.StartDate, c.wantStart)
			closed := f.store.trip(active.ID)
			m4Expect(t, "old status", closed.Status, StatusCompleted)
			m4Expect(t, "old end", *closed.EndDate, c.wantEnd)
			m4Expect(t, "active trip", resp.ActiveTrip.ID, v.Trip.ID)
		})
	}
}

func TestSyncTripSimilarity(t *testing.T) {
	cases := []struct {
		name        string
		country     *string
		city        *string
		payload     map[string]any
		wantConflit bool
	}{
		{"different countries", ptr("TH"), nil, map[string]any{"country_name": "Россия"}, false},
		{"different cities", ptr("TH"), ptr("Пхукет"), map[string]any{"country_name": "Таиланд", "city": "Бангкок"}, false},
		{"same city ignoring case and spaces", ptr("TH"), ptr("Пхукет"), map[string]any{"city": " пхукет "}, true},
		{"server trip without a country", nil, nil, map[string]any{"country_name": "Россия"}, true},
		{"payload without anything", ptr("TH"), ptr("Пхукет"), map[string]any{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := m4NewSync()
			start := NewDate(2026, 9, 10)
			active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CountryCode: c.country, City: c.city, StartDate: &start})
			payload := map[string]any{"client_id": uuid.New(), "name": "offline"}
			for k, v := range c.payload {
				payload[k] = v
			}
			resp := f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00+07:00", payload))
			if c.wantConflit {
				m4WantStatus(t, resp.Results[0], SyncStatusConflict)
				m4Expect(t, "server trip still active", f.store.trip(active.ID).Status, StatusActive)
				return
			}
			v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
			m4Expect(t, "auto-closed", *v.ClosedTripID, active.ID)
			m4Expect(t, "old end", *f.store.trip(active.ID).EndDate, NewDate(2026, 9, 16))
		})
	}
}

func TestSyncArriveMoveThenExpenseOnNewTrip(t *testing.T) {
	f := m4NewSync()
	first, moved := uuid.New(), uuid.New()
	resp := f.run(t,
		m4Op(t, OpTripCreate, "2026-09-10T10:00:00+07:00", map[string]any{
			"client_id": first, "name": "Asia", "country_name": "Таиланд", "city": "Пхукет",
		}),
		m4Op(t, OpTripArrive, "2026-09-16T10:00:00+03:00", map[string]any{
			"trip": m4Ref(first), "country_name": "Россия", "city": "Москва", "new_trip_client_id": moved,
		}),
		m4Op(t, OpExpenseAdd, "2026-09-16T12:00:00+03:00", map[string]any{
			"client_id": uuid.New(), "trip": m4Ref(moved), "amount": "800", "description": "кофе",
		}),
	)
	created := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	arrival := m4WantStatus(t, resp.Results[1], SyncStatusOK)
	if arrival.Arrival == nil || !arrival.Arrival.StartedNewTrip {
		t.Fatalf("arrival must start a new trip: %s", resp.Results[1])
	}
	m4Expect(t, "closed", *arrival.ClosedTripID, created.Trip.ID)
	m4Expect(t, "result trip", arrival.Trip.ID, arrival.Arrival.TripID)
	m4Expect(t, "new trip client id", arrival.Trip.ClientIDs[0], moved)

	m4Expect(t, "old end", *f.store.trip(created.Trip.ID).EndDate, NewDate(2026, 9, 16))

	expense := m4WantStatus(t, resp.Results[2], SyncStatusOK)
	m4Expect(t, "expense trip", expense.Expense.TripID, arrival.Arrival.TripID)
	m4Expect(t, "expense currency", expense.Expense.CurrencyCode, "RUB")
	m4Expect(t, "active", resp.ActiveTrip.ID, arrival.Arrival.TripID)
}

func TestSyncTripEnd(t *testing.T) {
	f := m4NewSync()
	start, end := NewDate(2026, 9, 1), NewDate(2026, 9, 5)
	done := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "done", Status: StatusCompleted, StartDate: &start, EndDate: &end})
	active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "active", Timezone: ptr("Asia/Bangkok"), StartDate: &start})

	resp := f.run(t,
		m4Op(t, OpTripEnd, "2026-09-16T10:00:00Z", map[string]any{"trip": map[string]any{"id": done.ID}}),

		m4Op(t, OpTripEnd, "2026-09-16T20:00:00Z", map[string]any{"trip": map[string]any{"id": active.ID}}),
	)
	v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	m4Expect(t, "completed end kept", *v.Trip.EndDate, end)
	v = m4WantStatus(t, resp.Results[1], SyncStatusOK)
	m4Expect(t, "status", v.Trip.Status, "Completed")
	m4Expect(t, "end in trip zone", *v.Trip.EndDate, NewDate(2026, 9, 17))
	if resp.ActiveTrip != nil {
		t.Fatal("no active trip is left")
	}
}

func TestSyncSetCurrencyAndTimezone(t *testing.T) {
	f := m4NewSync()
	trip := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "t"})
	ref := map[string]any{"id": trip.ID}

	resp := f.run(t,
		m4Op(t, OpTripSetCurrency, "2026-09-16T10:00:00Z", map[string]any{"trip": ref, "currency_code": "usd"}),
		m4Op(t, OpTripSetTimezone, "2026-09-16T10:00:00Z", map[string]any{"trip": ref, "latitude": "7.88", "longitude": 98.39}),
		m4Op(t, OpTripSetTimezone, "2026-09-16T10:00:00Z", map[string]any{"trip": ref, "latitude": 7.88}),
		m4Op(t, OpTripSetCurrency, "2026-09-16T10:00:00Z", map[string]any{"trip": ref}),
	)
	m4Expect(t, "currency", *m4WantStatus(t, resp.Results[0], SyncStatusOK).Trip.CurrencyCode, "USD")
	m4Expect(t, "timezone", *m4WantStatus(t, resp.Results[1], SyncStatusOK).Trip.Timezone, "Asia/Bangkok")
	m4WantError(t, resp.Results[2], "Нужны широта и долгота", false)
	m4WantError(t, resp.Results[3], "Укажите валюту", false)
}

func TestSyncExpenseAdd(t *testing.T) {
	f := m4NewSync()
	start := NewDate(2026, 9, 10)
	trip := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "t", CurrencyCode: ptr("THB"), Timezone: ptr("Asia/Bangkok"), StartDate: &start})

	bot := f.store.putExpense(Expense{TripID: trip.ID, Amount: MustDecimal("350.00"), CurrencyCode: "THB",
		OccurredAt: NewDate(2026, 9, 15), Source: SourceBot, CategoryID: 3, Description: "кофе"})

	clientID := uuid.New()
	payload := map[string]any{"client_id": clientID, "trip": map[string]any{"id": trip.ID}, "amount": 350, "description": " кофе "}

	resp := f.run(t, m4Op(t, OpExpenseAdd, "2026-09-15T20:00:00+07:00", payload))

	v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	e := v.Expense
	if e == nil || v.Trip != nil {
		t.Fatalf("want only an expense: %s", resp.Results[0])
	}
	m4Expect(t, "occurred at", e.OccurredAt, NewDate(2026, 9, 15))
	m4Expect(t, "amount usd via the closest snapshot", e.AmountUSD.String(), "10.000000")
	m4Expect(t, "category", e.CategoryName, "Кафе и рестораны")
	m4Expect(t, "client id", *e.ClientID, clientID)
	if e.DuplicateOfID == nil || *e.DuplicateOfID != bot.ID {
		t.Fatalf("look-alike not flagged: %s", resp.Results[0])
	}
	stored, err := f.store.OwnedExpense(bg, f.actor.UserID, e.ID)
	m4NoErr(t, err)
	m4Expect(t, "source", stored.Source, SourceApp)

	again := m4WantStatus(t, f.run(t, m4Op(t, OpExpenseAdd, "2026-09-15T20:00:00+07:00", payload)).Results[0], SyncStatusOK)
	m4Expect(t, "idempotent by client id", again.Expense.ID, e.ID)
	m4Expect(t, "inserts", f.store.calls["InsertExpense"], 1)
}

func TestSyncBotActorStillWritesAsApp(t *testing.T) {
	f := m4NewSync()
	f.actor.Mode = ModeBot
	resp := f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "x"}))
	v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	m4Expect(t, "source", f.store.trip(v.Trip.ID).Source, SourceApp)
}

func TestSyncOperationErrors(t *testing.T) {
	longText := strings.Repeat("я", MaxDescriptionLen+1)
	cases := []struct {
		name      string
		setup     func(f *m4SyncFixture, trip Trip)
		typ       string
		payload   func(trip Trip) any
		message   string
		retryable bool
	}{
		{name: "payload not an object", typ: OpTripEnd,
			payload: func(Trip) any { return `[1]` }, message: "Данные операции должны быть объектом"},
		{name: "payload missing", typ: OpTripEnd,
			payload: func(Trip) any { return nil }, message: "Данные операции должны быть объектом"},
		{name: "trip reference missing", typ: OpTripEnd,
			payload: func(Trip) any { return `{}` }, message: "Не указана поездка"},
		{name: "unknown client id", typ: OpTripEnd,
			payload: func(Trip) any { return map[string]any{"trip": m4Ref(uuid.New())} }, message: MsgTripNotFound},
		{name: "someone else's trip", typ: OpTripEnd,
			setup:   func(f *m4SyncFixture, _ Trip) {},
			payload: func(Trip) any { return map[string]any{"trip": map[string]any{"id": uuid.New()}} }, message: MsgTripNotFound},
		{name: "create without clientId", typ: OpTripCreate,
			payload: func(Trip) any { return map[string]any{"name": "x"} }, message: "Не указан client_id"},
		{name: "create without name", typ: OpTripCreate,
			payload: func(Trip) any { return map[string]any{"client_id": uuid.New(), "name": "  "} }, message: "Укажите название поездки"},
		{name: "unknown resolution", typ: OpTripCreate,
			payload: func(Trip) any { return map[string]any{"client_id": uuid.New(), "name": "x", "resolution": "maybe"} },
			message: "Неизвестное решение конфликта 'maybe'"},
		{name: "unknown country", typ: OpTripCreate,
			payload: func(Trip) any {
				return map[string]any{"client_id": uuid.New(), "name": "x", "country_name": "Атлантида"}
			},
			message: MsgCountryUnsupported},
		{name: "arrive without country", typ: OpTripArrive,
			payload: func(trip Trip) any { return map[string]any{"trip": map[string]any{"id": trip.ID}} },
			message: "Укажите страну"},
		{name: "arrive on a completed trip in another country", typ: OpTripArrive,
			setup: func(f *m4SyncFixture, trip Trip) {
				trip.Status = StatusCompleted
				trip.CountryCode = ptr("TH")
				f.store.putTrip(trip)
			},
			payload: func(trip Trip) any {
				return map[string]any{"trip": map[string]any{"id": trip.ID}, "country_name": "Россия"}
			},
			message: "Попытка закончить уже законченную поездку"},
		{name: "expense amount missing", typ: OpExpenseAdd,
			payload: func(trip Trip) any {
				return map[string]any{"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "description": "кофе"}
			}, message: "Укажите сумму"},
		{name: "expense description missing", typ: OpExpenseAdd,
			payload: func(trip Trip) any {
				return map[string]any{"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": 1}
			}, message: "Укажите описание траты"},
		{name: "expense amount not positive", typ: OpExpenseAdd,
			payload: func(trip Trip) any {
				return map[string]any{"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": 0, "description": "кофе"}
			}, message: "Сумма должна быть больше нуля"},
		{name: "expense description too long", typ: OpExpenseAdd,
			payload: func(trip Trip) any {
				return map[string]any{"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": 1, "description": longText}
			}, message: FieldTooLong("description", MaxDescriptionLen).Message},
		{name: "no rate snapshots is retryable", typ: OpExpenseAdd,
			setup: func(f *m4SyncFixture, _ Trip) { f.rates.snapshots = nil },
			payload: func(trip Trip) any {
				return map[string]any{"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": 1, "description": "кофе"}
			}, message: MsgNoSnapshotsClosest, retryable: true},
		{name: "integrity violation is final", typ: OpTripCreate,
			setup: func(f *m4SyncFixture, _ Trip) {
				f.store.m4FailInsertClientID = &IntegrityError{SQLState: "23503", Message: "insert violates foreign key constraint"}
			},
			payload: func(Trip) any { return map[string]any{"client_id": uuid.New(), "name": "x", "resolution": "new"} },
			message: "Связанная запись не найдена"},
		{name: "infrastructure failure is retryable", typ: OpTripCreate,
			setup:   func(f *m4SyncFixture, _ Trip) { f.store.m4FailInsertClientID = m4Boom },
			payload: func(Trip) any { return map[string]any{"client_id": uuid.New(), "name": "x", "resolution": "new"} },
			message: MsgSyncRetry, retryable: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := m4NewSync()
			start := NewDate(2026, 9, 10)
			trip := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "t", CurrencyCode: ptr("THB"), StartDate: &start})
			if c.setup != nil {
				c.setup(f, trip)
			}
			resp := f.run(t, m4Op(t, c.typ, "2026-09-16T10:00:00Z", c.payload(trip)))
			m4WantError(t, resp.Results[0], c.message, c.retryable)
			m4Expect(t, "errors are not stored", f.store.m4SyncCount(), 0)
			m4Expect(t, "trips unchanged", f.store.m4TripCount(), 1)
		})
	}
}

func TestSyncMalformedPayloadIsFinal(t *testing.T) {
	f := m4NewSync()
	resp := f.run(t, m4Op(t, OpExpenseAdd, "2026-09-16T10:00:00Z", `{"client_id": 5}`),
		m4Op(t, OpExpenseAdd, "2026-09-16T10:00:00Z", `{"amount": {"x": 1}}`))
	for _, raw := range resp.Results {
		v := m4WantStatus(t, raw, SyncStatusError)
		if !strings.HasPrefix(*v.Error, "Данные операции не разбираются") && *v.Error != "Данные операции должны быть объектом" {
			t.Fatalf("unexpected error text: %s", raw)
		}
		if *v.Retryable {
			t.Fatalf("malformed payload must be final: %s", raw)
		}
	}
	m4Expect(t, "type mismatch text", strings.HasPrefix(*m4Result(t, resp.Results[1]).Error, "Данные операции не разбираются"), true)
}

func TestSyncErrorRollsBackTheOperation(t *testing.T) {
	t.Run("validation after closing the active trip", func(t *testing.T) {
		f := m4NewSync()
		start := NewDate(2026, 9, 10)
		active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CountryCode: ptr("TH"), StartDate: &start})
		resp := f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "name": "x", "country_name": "Россия", "city": strings.Repeat("г", MaxCityLen+1),
		}))
		m4WantError(t, resp.Results[0], FieldTooLong("city", MaxCityLen).Message, false)
		m4Expect(t, "old trip still active", f.store.trip(active.ID).Status, StatusActive)
		m4Expect(t, "active in response", resp.ActiveTrip.ID, active.ID)
	})

	t.Run("store failure after the new trip was inserted", func(t *testing.T) {
		f := m4NewSync()
		start := NewDate(2026, 9, 10)
		active := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CountryCode: ptr("TH"), StartDate: &start})
		f.store.m4FailInsertClientID = m4Boom
		resp := f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "name": "x", "country_name": "Россия",
		}))
		m4WantError(t, resp.Results[0], MsgSyncRetry, true)
		m4Expect(t, "new trip rolled back", f.store.m4TripCount(), 1)
		m4Expect(t, "old trip still active", f.store.trip(active.ID).Status, StatusActive)
	})
}

func TestSyncParallelWinnerIsReplayed(t *testing.T) {
	f := m4NewSync()
	op := m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "x"})
	winner := json.RawMessage(fmt.Sprintf(`{"op_id":"%s","status":"ok","trip":{"id":"%s"}}`, op.OpID, uuid.New()))
	f.store.m4BeforeSaveSync = func(userID, opID uuid.UUID) error {
		f.store.m4CommittedElsewhere = map[[2]uuid.UUID]json.RawMessage{{userID, opID}: winner}
		return m4Unique("duplicate key value violates unique constraint \"pk_sync_operations\"")
	}

	resp := f.run(t, op)
	m4Expect(t, "winner", string(resp.Results[0]), string(winner))

	m4Expect(t, "loser rolled back", f.store.m4TripCount(), 0)
}

func TestSyncFailedOperationBlocksLaterOnes(t *testing.T) {
	f := m4NewSync()
	offline := uuid.New()
	resp := f.run(t,
		m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": offline, "name": "x", "country_name": "Атлантида"}),
		m4Op(t, OpTripSetCurrency, "2026-09-16T10:00:00Z", map[string]any{"trip": m4Ref(offline), "currency_code": "USD"}),
		m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "other"}),
	)
	m4WantError(t, resp.Results[0], MsgCountryUnsupported, false)
	m4WantStatus(t, resp.Results[1], SyncStatusBlocked)
	m4WantStatus(t, resp.Results[2], SyncStatusOK)
}

func TestClassifySyncError(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		message   string
		retryable bool
	}{
		{"validation", Validation("bad"), "bad", false},
		{"not found", NotFound(MsgTripNotFound), MsgTripNotFound, false},
		{"conflict", Conflict("busy"), "busy", false},
		{"field too long", FieldTooLong("city", MaxCityLen), FieldTooLong("city", MaxCityLen).Message, false},
		{"unknown currency", UnknownCurrency("xyz"), "Валюта xyz не найдена", false},
		{"no snapshots", NoSnapshots(MsgNoSnapshotsLatest), MsgNoSnapshotsLatest, true},
		{"wrapped business error", fmt.Errorf("apply: %w", Validation("wrapped")), "wrapped", false},
		{"integrity class 23", &IntegrityError{SQLState: "23502", Message: "null value"}, "Не заполнено обязательное поле", false},
		{"wrapped integrity", fmt.Errorf("insert: %w", &IntegrityError{SQLState: "23505", Message: "dup"}), "Такая запись уже есть", false},
		{"non-integrity sqlstate", &IntegrityError{SQLState: "40001", Message: "serialization"}, MsgSyncRetry, true},
		{"numeric overflow from the driver", fmt.Errorf("insert: %w", fakePgError{"22003"}), "Число вне допустимого диапазона", false},
		{"other data exception", fakePgError{"22007"}, "Некорректное значение в данных операции", false},
		{"serialization from the driver", fakePgError{"40001"}, MsgSyncRetry, true},
		{"anything else", m4Boom, MsgSyncRetry, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			message, retryable := classifySyncError(c.err)
			m4Expect(t, "message", message, c.message)
			m4Expect(t, "retryable", retryable, c.retryable)
		})
	}
}

type fakePgError struct{ code string }

func (e fakePgError) Error() string    { return "pg error " + e.code }
func (e fakePgError) SQLState() string { return e.code }

func TestSyncTooLargeAmountIsFinal(t *testing.T) {
	f := m4NewSync()
	start := NewDate(2026, 9, 10)
	trip := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "t", CurrencyCode: ptr("THB"), StartDate: &start})
	for _, amount := range []any{1e17, "1e20", "9999999999999999.995"} {
		resp := f.run(t, m4Op(t, OpExpenseAdd, "2026-09-16T10:00:00Z", map[string]any{
			"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": amount, "description": "кофе",
		}))
		m4WantError(t, resp.Results[0], msgAmountTooLarge, false)
	}
	m4Expect(t, "nothing stored", f.store.m4SyncCount(), 0)
}

func TestSyncTwinThatCommittedFirstWins(t *testing.T) {
	setup := func(t *testing.T, op *SyncOperation) (*m4SyncFixture, json.RawMessage) {
		f := m4NewSync()
		winner := json.RawMessage(fmt.Sprintf(`{"op_id":"%s","status":"ok","trip":{"id":"%s"}}`, op.OpID, uuid.New()))
		f.store.m4CommittedElsewhere = map[[2]uuid.UUID]json.RawMessage{{f.actor.UserID, op.OpID}: winner}
		f.store.m4CommitLater = 1
		return f, winner
	}

	t.Run("trip.create hits the one-active-trip index", func(t *testing.T) {
		op := m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "x"})
		f, winner := setup(t, op)
		f.store.failInsertTrip = ErrActiveTripExists
		resp := f.run(t, op)
		m4Expect(t, "winner", string(resp.Results[0]), string(winner))
	})

	t.Run("trip.create sees the winner's trip as a similar active trip", func(t *testing.T) {
		op := m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "x"})
		f, winner := setup(t, op)
		start := NewDate(2026, 9, 16)
		f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "x", StartDate: &start})
		resp := f.run(t, op)
		m4Expect(t, "winner", string(resp.Results[0]), string(winner))
	})

	t.Run("trip.arrive move hits the one-active-trip index", func(t *testing.T) {
		f := m4NewSync()
		start := NewDate(2026, 9, 10)
		old := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "t", CountryCode: ptr("TH"), StartDate: &start})
		op := m4Op(t, OpTripArrive, "2026-09-16T10:00:00Z", map[string]any{
			"trip": map[string]any{"id": old.ID}, "country_name": "Russia", "new_trip_client_id": uuid.New(),
		})
		winner := json.RawMessage(fmt.Sprintf(`{"op_id":"%s","status":"ok","trip":{"id":"%s"}}`, op.OpID, uuid.New()))
		f.store.m4CommittedElsewhere = map[[2]uuid.UUID]json.RawMessage{{f.actor.UserID, op.OpID}: winner}
		f.store.m4CommitLater = 1
		f.store.failInsertTrip = ErrActiveTripExists
		resp := f.run(t, op)
		m4Expect(t, "winner", string(resp.Results[0]), string(winner))
		m4Expect(t, "loser rolled back", f.store.trip(old.ID).Status, StatusActive)
	})

	t.Run("without a stored result the error stands", func(t *testing.T) {
		f := m4NewSync()
		f.store.failInsertTrip = ErrActiveTripExists
		resp := f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00Z", map[string]any{"client_id": uuid.New(), "name": "x"}))
		m4WantError(t, resp.Results[0], fmt.Sprintf(MsgActiveTripExists, "x"), false)
	})
}

func (f *m4SyncFixture) m4TripWithContents(t *testing.T, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	created := m4WantStatus(t, f.run(t, m4Op(t, OpTripCreate, "2026-09-16T10:00:00+07:00", map[string]any{
		"client_id": clientID, "name": "Asia", "country_name": "Thailand",
	})).Results[0], SyncStatusOK)
	m4WantStatus(t, f.run(t, m4Op(t, OpExpenseAdd, "2026-09-16T11:00:00+07:00", map[string]any{
		"client_id": uuid.New(), "trip": m4Ref(clientID), "amount": 350, "description": "кофе",
	})).Results[0], SyncStatusOK)

	f.files.files["a.png"] = []byte("bytes")
	photo := Photo{ID: uuid.New(), TripID: created.Trip.ID, StoredFileName: "a.png", CreatedAt: testNow}
	f.store.photos[photo.ID] = photo
	return created.Trip.ID
}

func TestSyncTripDelete(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()
	tripID := f.m4TripWithContents(t, clientID)

	resp := f.run(t, m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00", map[string]any{"trip": m4Ref(clientID)}))
	v := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	if v.DeletedTripID == nil || *v.DeletedTripID != tripID {
		t.Fatalf("deletedTripId: %s", resp.Results[0])
	}
	if v.Trip != nil {
		t.Errorf("a deleted trip must not come back as trip: %s", resp.Results[0])
	}
	m4Expect(t, "trips", f.store.m4TripCount(), 0)
	m4Expect(t, "expenses", len(f.store.expenses), 0)
	m4Expect(t, "photo rows", len(f.store.photos), 0)
	m4Expect(t, "photo files", len(f.files.files), 0)
	if resp.ActiveTrip != nil {
		t.Error("activeTrip must be null after the active trip was deleted")
	}

	if id, _ := f.store.TripIDByClientID(bg, f.actor.UserID, clientID); id != nil {
		t.Error("the client id outlived its trip")
	}
}

func TestSyncTripDeleteIsIdempotent(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()
	tripID := f.m4TripWithContents(t, clientID)

	op := m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00", map[string]any{"trip": m4Ref(clientID)})
	first := f.run(t, op)
	m4WantStatus(t, first.Results[0], SyncStatusOK)

	second := f.run(t, op)
	if !bytes.Equal(second.Results[0], first.Results[0]) {
		t.Fatalf("replay differs:\n%s\n%s", second.Results[0], first.Results[0])
	}

	byClient := m4WantStatus(t, f.run(t, m4Op(t, OpTripDelete, "2026-09-16T13:00:00+07:00",
		map[string]any{"trip": m4Ref(clientID)})).Results[0], SyncStatusOK)
	if byClient.DeletedTripID != nil {
		t.Errorf("an unknown client id cannot name a trip id: %+v", byClient.DeletedTripID)
	}
	byID := m4WantStatus(t, f.run(t, m4Op(t, OpTripDelete, "2026-09-16T13:00:00+07:00",
		map[string]any{"trip": map[string]any{"id": tripID}})).Results[0], SyncStatusOK)
	if byID.DeletedTripID == nil || *byID.DeletedTripID != tripID {
		t.Errorf("deletedTripId of a repeat: %+v", byID.DeletedTripID)
	}
}

func TestSyncTripDeleteOfSomeoneElsesTripTouchesNothing(t *testing.T) {
	f := m4NewSync()
	stranger := f.store.putTrip(Trip{UserID: uuid.New(), Name: "Not yours"})

	m4WantStatus(t, f.run(t, m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00",
		map[string]any{"trip": map[string]any{"id": stranger.ID}})).Results[0], SyncStatusOK)
	m4Expect(t, "trips", f.store.m4TripCount(), 1)
}

func TestSyncTripDeleteNeedsATripReference(t *testing.T) {
	f := m4NewSync()
	m4WantError(t, f.run(t, m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00",
		map[string]any{})).Results[0], "Не указана поездка", false)
}

func TestSyncTripDeleteKeepsFilesWhenTheOperationRollsBack(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()
	f.m4TripWithContents(t, clientID)
	f.store.m4BeforeSaveSync = func(uuid.UUID, uuid.UUID) error { return m4Boom }

	m4WantError(t, f.run(t, m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00",
		map[string]any{"trip": m4Ref(clientID)})).Results[0], MsgSyncRetry, true)
	m4Expect(t, "trip rolled back", f.store.m4TripCount(), 1)
	m4Expect(t, "photo rows rolled back", len(f.store.photos), 1)
	m4Expect(t, "photo files kept", len(f.files.files), 1)
}

func TestSyncTripDeleteBlocksLaterOperationsOfTheTrip(t *testing.T) {
	f := m4NewSync()
	clientID := uuid.New()
	f.m4TripWithContents(t, clientID)
	f.store.m4BeforeSaveSync = func(uuid.UUID, uuid.UUID) error { return m4Boom }

	resp := f.run(t,
		m4Op(t, OpTripDelete, "2026-09-16T12:00:00+07:00", map[string]any{"trip": m4Ref(clientID)}),
		m4Op(t, OpExpenseAdd, "2026-09-16T12:30:00+07:00", map[string]any{
			"client_id": uuid.New(), "trip": m4Ref(clientID), "amount": 10, "description": "чай",
		}),
	)
	m4WantError(t, resp.Results[0], MsgSyncRetry, true)
	m4WantStatus(t, resp.Results[1], SyncStatusBlocked)
}
