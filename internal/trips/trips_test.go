package trips

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTripFixture() (*memStore, *TripService, *fakeTZ, uuid.UUID) {
	store, svc, tz, _, user := newTripFixtureWithFiles()
	return store, svc, tz, user
}

func newTripFixtureWithFiles() (*memStore, *TripService, *fakeTZ, *fakeFiles, uuid.UUID) {
	store := seededStore()
	tz := &fakeTZ{zone: "Asia/Bangkok", offset: 7 * time.Hour}
	files := newFakeFiles()
	return store, NewTripService(store, tz, files, fixedClock{testNow}, quietLogger), tz, files, uuid.New()
}

func TestTripHistory(t *testing.T) {
	store, svc, _, user := newTripFixture()
	start := NewDate(2026, 9, 9)
	end := NewDate(2026, 9, 10)
	active := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
	done := store.putTrip(Trip{UserID: user, Name: "Old", Status: StatusCompleted, StartDate: &start, EndDate: &end})
	store.putTrip(Trip{UserID: uuid.New(), Name: "Someone else's"})
	store.putExpense(Expense{TripID: active.ID, AmountUSD: MustDecimal("10.586556")})
	store.putExpense(Expense{TripID: active.ID, AmountUSD: MustDecimal("5.000000"), DuplicateOfID: ptr(int64(1))})

	got, err := svc.History(bg, appActor(user))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items %d", len(got.Items))
	}
	for _, item := range got.Items {
		switch item.ID {
		case active.ID:
			if !item.IsActive || item.DurationDays != 8 || *item.CountryName != "Таиланд" || item.TotalUSD.String() != "10.586556" || item.ExpenseCount != 1 {
				t.Errorf("active item %+v", item)
			}
		case done.ID:
			if item.IsActive || item.DurationDays != 2 || item.Status != StatusCompleted {
				t.Errorf("done item %+v", item)
			}
		}
	}

	empty, _ := svc.History(bg, appActor(uuid.New()))
	if empty.Items == nil {
		t.Error("items must marshal as []")
	}
}

func TestTripActive(t *testing.T) {
	store, svc, _, user := newTripFixture()

	got, err := svc.Active(bg, appActor(user))
	if err != nil || got.HasActiveTrip || got.Trip != nil {
		t.Fatalf("no trip: %+v %v", got, err)
	}

	start := NewDate(2026, 9, 16)
	trip := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH"), Timezone: ptr("Asia/Bangkok")})
	got, err = svc.Active(bg, appActor(user))
	if err != nil || !got.HasActiveTrip || got.Trip.ID != trip.ID || got.Trip.DurationDays != 1 || *got.Trip.CountryName != "Таиланд" || *got.Trip.Timezone != "Asia/Bangkok" {
		t.Fatalf("one trip: %+v %v", got.Trip, err)
	}

	store.putTrip(Trip{UserID: user, Name: "Second"})
	_, err = svc.Active(bg, appActor(user))
	wantErr(t, err, KindConflict, MsgSequenceMoreThanOne)
}

func TestTripCreate(t *testing.T) {
	t.Run("minimal trip starts today with nulls kept", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		got, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: " Asia 2026 "})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != " Asia 2026 " || got.CountryName != nil || got.City != nil || got.CurrencyCode != nil || *got.StartDate != NewDate(2026, 9, 16) {
			t.Fatalf("response %+v", got)
		}
		stored := store.trip(got.ID)
		if stored.Status != StatusActive || stored.Source != SourceApp || stored.City != nil || stored.Timezone != nil ||
			!stored.CreatedAt.Equal(testNow) || !stored.UpdatedAt.Equal(testNow) || stored.UserID != user {
			t.Fatalf("stored %+v", stored)
		}
		if got.ID.Version() != 7 {
			t.Errorf("id version %d", got.ID.Version())
		}
	})

	t.Run("country fills currency and zone; bot source", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		got, err := svc.Create(bg, botActor(user), CreateTripCommand{
			Name: "Asia", CountryName: ptr("  ТАИЛАНД "), City: ptr("Пхукет"), Budget: ptr(MustDecimal("5000")),
		})
		if err != nil {
			t.Fatal(err)
		}
		if *got.CountryName != "Таиланд" || *got.City != "Пхукет" || *got.CurrencyCode != "THB" {
			t.Fatalf("response %+v", got)
		}
		stored := store.trip(got.ID)
		if stored.Source != SourceBot || *stored.Timezone != "Asia/Bangkok" || *stored.CountryCode != "TH" || stored.Budget.String() != "5000" {
			t.Fatalf("stored %+v", stored)
		}
		if store.calls["CountryByName:таиланд"] != 1 {
			t.Errorf("country lookup not trimmed and lower-cased: %v", store.log)
		}
	})

	t.Run("explicit currency is upper-cased and kept over the country default", func(t *testing.T) {
		_, svc, _, user := newTripFixture()
		got, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Asia", CountryName: ptr("Thailand"), CurrencyCode: ptr("usd")})
		if err != nil || *got.CurrencyCode != "USD" {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("sync start date and source", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		on := NewDate(2026, 9, 1)
		got, err := svc.Create(bg, botActor(user), CreateTripCommand{Name: "Asia", StartDate: &on, Source: ptr(SourceApp)})
		if err != nil || *got.StartDate != on || store.trip(got.ID).Source != SourceApp {
			t.Fatalf("got %+v %v", got, err)
		}
	})

	t.Run("errors in order", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		long := strings.Repeat("я", 121)

		_, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Asia", City: &long})
		wantErr(t, err, KindValidation, "Поле city не может быть длиннее 120 символов")
		if _, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Asia", City: ptr(strings.Repeat("я", 120))}); err != nil {
			t.Fatalf("120 runes rejected: %v", err)
		}

		store2, svc2, _, _ := newTripFixture()
		_, err = svc2.Create(bg, appActor(user), CreateTripCommand{Name: "Asia", CurrencyCode: ptr("xyz"), CountryName: ptr("Narnia")})
		wantErr(t, err, KindValidation, "Валюта xyz не найдена")
		_, err = svc2.Create(bg, appActor(user), CreateTripCommand{Name: "Asia", CountryName: ptr("TH")})
		wantErr(t, err, KindValidation, "Выбранная страна не поддерживается или не существует")
		if store2.calls["InsertTrip"] != 0 {
			t.Error("failed create inserted a trip")
		}

		_, err = svc.Create(bg, appActor(user), CreateTripCommand{Name: "Second", City: &long})
		wantErr(t, err, KindConflict, "У вас уже есть активная поездка 'Asia'. Завершите её командой /trip_end перед созданием новой.")
		_ = store
	})

	t.Run("index violation is the same conflict", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		store.failInsertTrip = ErrActiveTripExists
		_, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Mine"})
		wantErr(t, err, KindConflict, "У вас уже есть активная поездка 'Mine'. Завершите её командой /trip_end перед созданием новой.")

		store.putTrip(Trip{UserID: user, Name: "Winner"})
		_, err = svc.Create(bg, appActor(user), CreateTripCommand{Name: "Mine"})
		wantErr(t, err, KindConflict, "У вас уже есть активная поездка 'Winner'. Завершите её командой /trip_end перед созданием новой.")
	})

	t.Run("infrastructure errors pass through", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		boom := errors.New("boom")
		store.failInsertTrip = boom
		if _, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Mine"}); !errors.Is(err, boom) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTripEndService(t *testing.T) {
	store, svc, _, user := newTripFixture()

	_, err := svc.End(bg, appActor(user), EndTripCommand{})
	wantErr(t, err, KindValidation, "Нет активных поездок")
	_, err = svc.End(bg, appActor(user), EndTripCommand{TripID: ptr(uuid.New())})
	wantErr(t, err, KindNotFound, "Поездка не найдена")

	start := NewDate(2026, 9, 9)
	trip := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start})
	got, err := svc.End(bg, appActor(user), EndTripCommand{})
	if err != nil || got.Name != "Asia" || *got.StartDate != start || *got.EndDate != NewDate(2026, 9, 16) {
		t.Fatalf("got %+v %v", got, err)
	}
	if stored := store.trip(trip.ID); stored.Status != StatusCompleted || !stored.UpdatedAt.Equal(testNow) {
		t.Fatalf("stored %+v", stored)
	}

	_, err = svc.End(bg, appActor(user), EndTripCommand{TripID: &trip.ID})
	wantErr(t, err, KindConflict, "Попытка закончить уже законченную поездку")

	_, err = svc.End(bg, appActor(uuid.New()), EndTripCommand{TripID: &trip.ID})
	wantErr(t, err, KindNotFound, "Поездка не найдена")

	noStart := store.putTrip(Trip{UserID: user, Name: "Odd", Status: StatusPlanned})
	on := NewDate(2026, 9, 20)
	got, err = svc.End(bg, appActor(user), EndTripCommand{TripID: &noStart.ID, On: &on})
	if err != nil || *got.StartDate != on || *got.EndDate != on {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestTripArrive(t *testing.T) {
	t.Run("no active trip, unknown country", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		_, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand"})
		wantErr(t, err, KindValidation, "У вас нет активной поездки")
		_, err = svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand", TripID: ptr(uuid.New())})
		wantErr(t, err, KindNotFound, "Поездка не найдена")

		store.putTrip(Trip{UserID: user, Name: "Asia"})
		_, err = svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Нарния"})
		wantErr(t, err, KindValidation, "Выбранная страна не поддерживается или не существует")
	})

	t.Run("first arrival binds the country to the trip", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		start := NewDate(2026, 9, 9)
		trip := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr(" "), CurrencyCode: ptr("USD")})
		got, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "thailand", City: ptr("Пхукет")})
		if err != nil {
			t.Fatal(err)
		}
		want := ArriveResponse{TripID: trip.ID, CountryName: "Таиланд", City: got.City, CurrencyName: "Тайский бат", CurrencyCode: "THB"}
		if got != want || *got.City != "Пхукет" {
			t.Fatalf("got %+v", got)
		}
		stored := store.trip(trip.ID)

		if *stored.CurrencyCode != "USD" || *stored.CountryCode != "TH" || *stored.Timezone != "Asia/Bangkok" || *stored.City != "Пхукет" || stored.Status != StatusActive {
			t.Fatalf("stored %+v", stored)
		}
		if store.calls["InsertTrip"] != 0 {
			t.Error("first arrival created a trip")
		}
	})

	t.Run("first arrival with a blank city keeps the old city", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		trip := store.putTrip(Trip{UserID: user, Name: "Asia", City: ptr("Старый")})
		got, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Russia", City: ptr(" ")})
		if err != nil || *got.City != "Старый" || !got.IsMultiTimezone || got.StartedNewTrip {
			t.Fatalf("got %+v %v", got, err)
		}
		if *store.trip(trip.ID).City != "Старый" {
			t.Fatal("city overwritten")
		}
	})

	t.Run("same place is a conflict, case-insensitive; null and empty city differ", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		store.putTrip(Trip{UserID: user, Name: "Asia", CountryCode: ptr("th"), City: ptr("ПХУКЕТ")})
		_, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand", City: ptr("пхукет")})
		wantErr(t, err, KindConflict, "Вы уже находитесь здесь: Таиланд")

		store2, svc2, _, _ := newTripFixture()
		store2.putTrip(Trip{UserID: user, Name: "Asia", CountryCode: ptr("TH")})
		_, err = svc2.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand"})
		wantErr(t, err, KindConflict, "Вы уже находитесь здесь: Таиланд")

		got, err := svc2.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand", City: ptr("")})
		if err != nil || !got.StartedNewTrip {
			t.Fatalf("null vs empty city must move: %+v %v", got, err)
		}
	})

	t.Run("move ends the trip and starts a new one", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		start := NewDate(2026, 9, 9)
		old := store.putTrip(Trip{UserID: user, Name: "Asia", Budget: ptr(MustDecimal("5000")), StartDate: &start,
			CountryCode: ptr("TH"), City: ptr("Пхукет"), CurrencyCode: ptr("USD"), Timezone: ptr("Asia/Bangkok"), Source: SourceApp})
		got, err := svc.Arrive(bg, botActor(user), ArriveCommand{CountryName: "Грузия"})
		if err != nil {
			t.Fatal(err)
		}
		if !got.StartedNewTrip || got.TripID == old.ID || got.City != nil || got.CurrencyCode != "GEL" || got.CurrencyName != "Грузинский лари" || !got.IsMultiCurrency {
			t.Fatalf("got %+v", got)
		}
		ended := store.trip(old.ID)
		if ended.Status != StatusCompleted || *ended.EndDate != NewDate(2026, 9, 16) {
			t.Fatalf("old %+v", ended)
		}
		next := store.trip(got.TripID)
		if next.Name != "Asia" || next.Budget.String() != "5000" || next.City != nil || *next.CurrencyCode != "GEL" ||
			*next.Timezone != "Asia/Tbilisi" || next.Source != SourceBot || next.Status != StatusActive || *next.StartDate != NewDate(2026, 9, 16) {
			t.Fatalf("new %+v", next)
		}
	})

	t.Run("same country, other city is a move", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		store.putTrip(Trip{UserID: user, Name: "Asia", CountryCode: ptr("TH"), City: ptr("Пхукет")})
		got, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand", City: ptr("Бангкок")})
		if err != nil || !got.StartedNewTrip || *got.City != "Бангкок" {
			t.Fatalf("got %+v %v", got, err)
		}
		if n := len(store.trips); n != 2 {
			t.Fatalf("trips %d", n)
		}
	})

	t.Run("sync move uses the date; a completed trip can't move", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		start := NewDate(2026, 9, 9)
		old := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
		on := NewDate(2026, 9, 12)
		got, err := svc.Arrive(bg, botActor(user), ArriveCommand{CountryName: "Russia", TripID: &old.ID, On: &on, Source: ptr(SourceApp)})
		if err != nil {
			t.Fatal(err)
		}
		if *store.trip(old.ID).EndDate != on || *store.trip(got.TripID).StartDate != on || store.trip(got.TripID).Source != SourceApp {
			t.Fatal("dates or source not applied")
		}
		_, err = svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Georgia", TripID: &old.ID})
		wantErr(t, err, KindConflict, "Попытка закончить уже законченную поездку")
	})

	t.Run("city too long after the same-place check", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		store.putTrip(Trip{UserID: user, Name: "Asia"})
		_, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Thailand", City: ptr(strings.Repeat("a", 121))})
		wantErr(t, err, KindValidation, "Поле city не может быть длиннее 120 символов")
	})

	t.Run("index violation on the new trip", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		store.putTrip(Trip{UserID: user, Name: "Asia", CountryCode: ptr("TH")})
		store.failInsertTrip = ErrActiveTripExists
		_, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Russia"})
		wantErr(t, err, KindConflict, "У вас уже есть активная поездка 'Asia'. Завершите её командой /trip_end перед созданием новой.")
	})

	t.Run("a failed insert keeps the old trip active", func(t *testing.T) {
		store := m4NewStore()
		svc := NewTripService(store, &fakeTZ{zone: "Asia/Bangkok", offset: 7 * time.Hour}, newFakeFiles(), fixedClock{testNow}, quietLogger)
		user := uuid.New()
		start := NewDate(2026, 9, 9)
		old := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
		store.failInsertTrip = errors.New("connection reset")
		if _, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Russia"}); err == nil {
			t.Fatal("want an error")
		}
		if got := store.trip(old.ID); got.Status != StatusActive || got.EndDate != nil {
			t.Fatalf("old trip changed: %+v", got)
		}
	})

	t.Run("a backdated move starts the new trip no earlier than the old one", func(t *testing.T) {
		store, svc, _, user := newTripFixture()
		start := NewDate(2026, 9, 10)
		old := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
		on := NewDate(2026, 9, 9)
		got, err := svc.Arrive(bg, appActor(user), ArriveCommand{CountryName: "Russia", TripID: &old.ID, On: &on})
		if err != nil {
			t.Fatal(err)
		}
		if *store.trip(old.ID).EndDate != start || *store.trip(got.TripID).StartDate != start {
			t.Fatalf("old ends %v, new starts %v; both want %v", *store.trip(old.ID).EndDate, *store.trip(got.TripID).StartDate, start)
		}
	})
}

func TestTripSetCurrency(t *testing.T) {
	store, svc, _, user := newTripFixture()
	_, err := svc.SetCurrency(bg, appActor(user), SetCurrencyCommand{Currency: "USD"})
	wantErr(t, err, KindValidation, "Нет активных поездок")
	_, err = svc.SetCurrency(bg, appActor(user), SetCurrencyCommand{Currency: "USD", TripID: ptr(uuid.New())})
	wantErr(t, err, KindNotFound, "Поездка не найдена")

	trip := store.putTrip(Trip{UserID: user, Name: "Asia", CurrencyCode: ptr("THB")})
	_, err = svc.SetCurrency(bg, appActor(user), SetCurrencyCommand{Currency: "usd"})
	wantErr(t, err, KindValidation, "Валюта usd не найдена")

	got, err := svc.SetCurrency(bg, appActor(user), SetCurrencyCommand{Currency: "USD"})
	if err != nil || got != (SetCurrencyResponse{CurrencyCode: "USD", CurrencyName: "Доллар США"}) {
		t.Fatalf("got %+v %v", got, err)
	}
	if *store.trip(trip.ID).CurrencyCode != "USD" {
		t.Fatal("not saved")
	}

	done := store.putTrip(Trip{UserID: user, Name: "Old", Status: StatusCompleted})
	if _, err := svc.SetCurrency(bg, appActor(user), SetCurrencyCommand{Currency: "RUB", TripID: &done.ID}); err != nil {
		t.Fatalf("completed trip by id: %v", err)
	}
}

func TestTripSetTimezone(t *testing.T) {
	store, svc, tz, user := newTripFixture()
	_, err := svc.SetTimezone(bg, appActor(user), SetTimezoneCommand{Latitude: 7.9, Longitude: 98.3})
	wantErr(t, err, KindValidation, "Нет активных поездок")

	trip := store.putTrip(Trip{UserID: user, Name: "India", Timezone: ptr("Asia/Bangkok")})
	tz.zone, tz.offset = "Asia/Kolkata", 5*time.Hour+30*time.Minute
	got, err := svc.SetTimezone(bg, appActor(user), SetTimezoneCommand{Latitude: 19, Longitude: 72.8})
	if err != nil || got.Timezone != "UTC+5.5" {
		t.Fatalf("got %+v %v", got, err)
	}
	if *store.trip(trip.ID).Timezone != "Asia/Kolkata" || tz.gotYear != 2026 {
		t.Fatalf("stored %v year %d", *store.trip(trip.ID).Timezone, tz.gotYear)
	}

	tz.lookupErr = Validation("Широта вне допустимого диапазона")
	_, err = svc.SetTimezone(bg, appActor(user), SetTimezoneCommand{Latitude: 91})
	wantErr(t, err, KindValidation, "Широта вне допустимого диапазона")

	tz.lookupErr = nil
	tz.zone = "Etc/Unknown"
	tz.offsetErr = errors.New("unknown zone")
	if _, err := svc.SetTimezone(bg, appActor(user), SetTimezoneCommand{}); mustErr(err) != nil || err == nil {
		t.Fatalf("offset failure must be a 500, got %v", err)
	}
	if *store.trip(trip.ID).Timezone != "Asia/Kolkata" {
		t.Fatal("zone saved despite the offset failure")
	}
}

func TestTripDTO(t *testing.T) {
	store, svc, _, user := newTripFixture()
	_, err := svc.TripDTO(bg, appActor(user), uuid.New())
	wantErr(t, err, KindNotFound, "Поездка не найдена")

	start := NewDate(2026, 9, 15)
	trip := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
	got, err := svc.TripDTO(bg, appActor(user), trip.ID)
	if err != nil || got.ClientIDs == nil || len(got.ClientIDs) != 0 || got.Status != StatusActive || got.DurationDays != 2 || *got.CountryName != "Таиланд" {
		t.Fatalf("got %+v %v", got, err)
	}

	client := uuid.New()
	_ = store.InsertTripClientID(bg, user, client, trip.ID)
	got, _ = svc.TripDTO(bg, appActor(user), trip.ID)
	if len(got.ClientIDs) != 1 || got.ClientIDs[0] != client {
		t.Fatalf("client ids %v", got.ClientIDs)
	}
}

func TestTripServiceWithStore(t *testing.T) {
	store, svc, _, user := newTripFixture()
	other := seededStore()
	bound := svc.WithStore(other)
	if _, err := bound.Create(bg, appActor(user), CreateTripCommand{Name: "Tx"}); err != nil {
		t.Fatal(err)
	}
	if len(other.trips) != 1 || len(store.trips) != 0 {
		t.Fatal("WithStore did not rebind the store")
	}
}

func tripWithContents(store *memStore, files *fakeFiles, user uuid.UUID) (Trip, uuid.UUID) {
	start := NewDate(2026, 9, 15)
	trip := store.putTrip(Trip{UserID: user, Name: "Asia", StartDate: &start, CountryCode: ptr("TH")})
	first := store.putExpense(Expense{TripID: trip.ID, AmountUSD: MustDecimal("10.00")})
	store.putExpense(Expense{TripID: trip.ID, AmountUSD: MustDecimal("5.00"), DuplicateOfID: &first.ID})
	for _, name := range []string{"a.png", "b.png"} {
		files.files[name] = []byte("bytes")
		photo := Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: name, CreatedAt: testNow}
		store.photos[photo.ID] = photo
	}
	client := uuid.New()
	_ = store.InsertTripClientID(bg, user, client, trip.ID)
	return trip, client
}

func TestTripDeleteTakesEverything(t *testing.T) {
	store, svc, _, files, user := newTripFixtureWithFiles()
	trip, client := tripWithContents(store, files, user)

	other := store.putTrip(Trip{UserID: user, Name: "Other", Status: StatusCompleted})
	store.putExpense(Expense{TripID: other.ID, AmountUSD: MustDecimal("1.00")})

	got, err := svc.Delete(bg, appActor(user), trip.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TripID != trip.ID || got.Expenses != 2 || got.Photos != 2 {
		t.Fatalf("deleted %+v", got)
	}
	if _, ok := store.trips[trip.ID]; ok {
		t.Error("the trip row is still there")
	}
	if len(store.expenses) != 1 || len(store.photos) != 0 {
		t.Errorf("expenses %d, photos %d", len(store.expenses), len(store.photos))
	}
	if id, _ := store.TripIDByClientID(bg, user, client); id != nil {
		t.Error("the sync client id survived the trip it pointed at")
	}
	if len(files.files) != 0 {
		t.Errorf("photo files left on disk: %v", files.files)
	}
	if len(files.deleteLog) != 2 {
		t.Errorf("photo store called %d times, want 2", len(files.deleteLog))
	}

	if indexOf(store.log, "DeleteExpensesOfTrip") > indexOf(store.log, "DeleteTrip") {
		t.Errorf("wrong order: %v", store.log)
	}
}

func TestTripDeleteActiveTrip(t *testing.T) {
	store, svc, _, files, user := newTripFixtureWithFiles()
	trip, _ := tripWithContents(store, files, user)

	if _, err := svc.Delete(bg, appActor(user), trip.ID); err != nil {
		t.Fatal(err)
	}
	active, err := svc.Active(bg, appActor(user))
	if err != nil || active.HasActiveTrip {
		t.Fatalf("active trip after deleting it: %+v %v", active, err)
	}

	if _, err := svc.Create(bg, appActor(user), CreateTripCommand{Name: "Next"}); err != nil {
		t.Fatalf("cannot start a trip after deleting the active one: %v", err)
	}
}

func TestTripDeleteUnknownAndStrangerAre404(t *testing.T) {
	store, svc, _, files, user := newTripFixtureWithFiles()
	trip, _ := tripWithContents(store, files, user)

	_, err := svc.Delete(bg, appActor(user), uuid.New())
	wantErr(t, err, KindNotFound, MsgTripNotFound)

	_, err = svc.Delete(bg, appActor(uuid.New()), trip.ID)
	wantErr(t, err, KindNotFound, MsgTripNotFound)
	if _, ok := store.trips[trip.ID]; !ok {
		t.Error("a stranger deleted the trip")
	}
	if len(store.expenses) != 2 || len(files.files) != 2 {
		t.Error("a stranger's request touched the contents")
	}

	if _, err := svc.Delete(bg, appActor(user), trip.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Delete(bg, appActor(user), trip.ID)
	wantErr(t, err, KindNotFound, MsgTripNotFound)
}

func TestTripDeleteSurvivesAMissingFile(t *testing.T) {
	store, svc, _, files, user := newTripFixtureWithFiles()
	trip, _ := tripWithContents(store, files, user)
	files.deleteErr = errors.New("no such file")

	if _, err := svc.Delete(bg, appActor(user), trip.ID); err != nil {
		t.Fatalf("a failing photo store broke the delete: %v", err)
	}
	if _, ok := store.trips[trip.ID]; ok {
		t.Error("the trip row survived a file that could not be removed")
	}
}

func indexOf(log []string, call string) int {
	for i, name := range log {
		if name == call {
			return i
		}
	}
	return -1
}
