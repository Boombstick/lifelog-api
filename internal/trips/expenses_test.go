package trips

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type expenseFixture struct {
	store  *memStore
	svc    *ExpenseService
	search *fakeSearch
	rates  *fakeRates
	user   uuid.UUID
}

func newExpenseFixture() *expenseFixture {
	store := seededStore()
	search := &fakeSearch{hits: map[string]CategoryRef{"кофе": {ID: 3, Name: "Кафе и рестораны"}, "отель": {ID: 1, Name: "Жильё"}}}
	rates := &fakeRates{snapshots: []Snapshot{{
		ID:        110,
		CreatedAt: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
		Rates:     map[string]Decimal{"USD": MustDecimal("1.0"), "THB": MustDecimal("33.0608"), "RUB": MustDecimal("80"), "VND": MustDecimal("26000")},
	}}}
	dictionary := NewDictionaryService(store, &fakeIndex{}, quietLogger)
	svc := NewExpenseService(store, dictionary, search, rates, fixedClock{testNow}, quietLogger)
	return &expenseFixture{store: store, svc: svc, search: search, rates: rates, user: uuid.New()}
}

func (f *expenseFixture) activeTrip(mutate func(*Trip)) Trip {
	trip := Trip{UserID: f.user, Name: "Asia", CurrencyCode: ptr("THB"), CountryCode: ptr("TH")}
	if mutate != nil {
		mutate(&trip)
	}
	return f.store.putTrip(trip)
}

func (f *expenseFixture) stored(id int64) Expense {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	return f.store.expenses[id]
}

func TestAddExpenseHappyPath(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)

	got, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("350"), Description: "  кофе  "})
	if err != nil {
		t.Fatal(err)
	}
	if got != (AddExpenseResponse{CategoryName: "Кафе и рестораны", Amount: got.Amount, Currency: "THB", ID: got.ID}) || got.Amount.String() != "350" || got.ID == 0 {
		t.Fatalf("got %+v", got)
	}
	e := f.stored(got.ID)
	if e.TripID != trip.ID || e.CategoryID != 3 || e.CurrencyCode != "THB" || e.AmountUSD.String() != "10.586556" ||
		e.SnapshotID != 110 || e.Description != "кофе" || e.Source != SourceApp || e.DuplicateOfID != nil ||
		e.OccurredAt != NewDate(2026, 9, 16) || !e.CreatedAt.Equal(testNow) || e.ClientID != nil {
		t.Fatalf("stored %+v", e)
	}
}

func TestAddExpenseCurrencyAndRounding(t *testing.T) {
	cases := []struct {
		name      string
		trip      func(*Trip)
		currency  *string
		amount    string
		wantCode  string
		wantUSD   string
		wantReply string
	}{
		{"request currency is upper-cased", nil, ptr("rub"), "100", "RUB", "1.250000", "RUB"},
		{"trip currency", nil, nil, "350", "THB", "10.586556", "THB"},
		{"blank request currency falls back to trip", nil, ptr("  "), "350", "THB", "10.586556", "THB"},
		{"no trip currency is USD", func(t *Trip) { t.CurrencyCode = ptr(" ") }, nil, "12.5", "USD", "12.500000", "USD"},
		{"banker's rounding keeps scale", nil, ptr("VND"), "13", "VND", "0.000500", "VND"},

		{"half to even", nil, ptr("RUB"), "0.001", "RUB", "0.000012", "RUB"},

		{"tiny amount stores zero", nil, ptr("VND"), "0.01", "VND", "0.000000", "VND"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newExpenseFixture()
			f.activeTrip(c.trip)
			got, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal(c.amount), Currency: c.currency, Description: "кофе"})
			if err != nil {
				t.Fatal(err)
			}
			e := f.stored(got.ID)
			if got.Currency != c.wantReply || e.CurrencyCode != c.wantCode || e.AmountUSD.String() != c.wantUSD {
				t.Fatalf("reply %s, stored %s %s", got.Currency, e.CurrencyCode, e.AmountUSD)
			}
		})
	}
}

func TestAddExpenseErrors(t *testing.T) {
	t.Run("unsupported currency is checked first, as sent", func(t *testing.T) {
		f := newExpenseFixture()
		_, err := f.svc.Add(bg, Actor{}, AddExpenseCommand{Amount: MustDecimal("1"), Currency: ptr("xyz"), Description: "a"})
		wantErr(t, err, KindValidation, "Валюта xyz не поддерживается")
	})

	t.Run("anonymous user", func(t *testing.T) {
		f := newExpenseFixture()
		_, err := f.svc.Add(bg, Actor{Mode: ModeApp}, AddExpenseCommand{Amount: MustDecimal("1"), Currency: ptr("usd"), Description: "a"})
		wantErr(t, err, KindValidation, "Пользователь не найден")
	})

	t.Run("trip lookup", func(t *testing.T) {
		f := newExpenseFixture()
		_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a"})
		wantErr(t, err, KindValidation, "У вас нет активной поездки")
		_, err = f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a", TripID: ptr(uuid.New())})
		wantErr(t, err, KindNotFound, "Поездка не найдена")

		f.activeTrip(nil)
		f.activeTrip(nil)
		_, err = f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a"})
		wantErr(t, err, KindConflict, MsgSequenceMoreThanOne)
	})

	t.Run("no snapshots", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(nil)
		f.rates.snapshots = nil
		_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("0"), Description: "a"})
		wantErr(t, err, KindConflict, MsgNoSnapshotsLatest)
		if !mustErr(err).Retryable {
			t.Error("no snapshots must be retryable (R8)")
		}
		_, err = f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a", OccurredAt: ptr(testNow)})
		wantErr(t, err, KindConflict, MsgNoSnapshotsClosest)
		if !mustErr(err).Retryable {
			t.Error("no snapshots must be retryable (R8)")
		}
	})

	t.Run("rates failure is a 500", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(nil)
		boom := errors.New("db down")
		f.rates.err = boom
		if _, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a"}); !errors.Is(err, boom) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("no rate for a known currency", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(nil)
		_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Currency: ptr("gel"), Description: "a"})
		wantErr(t, err, KindValidation, "Нет курса для валюты GEL")
	})

	t.Run("zero rate is a 500, not a panic", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(nil)
		f.rates.snapshots[0].Rates["THB"] = MustDecimal("0")
		_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a"})
		if err == nil || mustErr(err) != nil {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("domain validation", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(nil)
		add := func(amount, description string) error {
			_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal(amount), Description: description})
			return err
		}
		wantErr(t, add("0", "кофе"), KindValidation, "Сумма должна быть больше нуля")
		wantErr(t, add("-5", "кофе"), KindValidation, "Сумма должна быть больше нуля")
		wantErr(t, add("5", " \t "), KindValidation, "Укажите описание траты")
		wantErr(t, add("5", strings.Repeat("я", 251)), KindValidation, "Поле description не может быть длиннее 250 символов")
		if err := add("5", "  "+strings.Repeat("я", 250)+"  "); err != nil {
			t.Fatalf("250 runes after trim rejected: %v", err)
		}

		f.rates.snapshots[0].Rates["THB"] = MustDecimal("-1")
		wantErr(t, add("5", "кофе"), KindValidation, "Сумма в долларах должна быть больше нуля")
		f.rates.snapshots[0].Rates["THB"] = MustDecimal("33")
		f.rates.snapshots[0].ID = 0
		wantErr(t, add("5", "кофе"), KindValidation, "Не найден снимок курсов валют")
	})

	t.Run("snapshot errors come before amount validation", func(t *testing.T) {
		f := newExpenseFixture()
		f.activeTrip(func(t *Trip) { t.CurrencyCode = ptr("GEL") })
		_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("0"), Description: "a"})
		wantErr(t, err, KindValidation, "Нет курса для валюты GEL")
	})
}

func TestAddExpenseCategoryFallback(t *testing.T) {
	f := newExpenseFixture()
	f.activeTrip(nil)

	got, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Description: "что-то"})
	if err != nil || got.CategoryName != "Прочее" || f.stored(got.ID).CategoryID != 10 {
		t.Fatalf("no hit: %+v %v", got, err)
	}

	f.search.err = errors.New("es down")
	got, err = f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("2"), Description: "кофе"})
	if err != nil || got.CategoryName != "Прочее" || f.stored(got.ID).CategoryID != 10 {
		t.Fatalf("search error: %+v %v", got, err)
	}
}

func TestAddExpenseDates(t *testing.T) {
	plus7 := time.FixedZone("", 7*3600)
	cases := []struct {
		name string
		zone *string
		cmd  AddExpenseCommand
		want Date
	}{
		{"no date, no zone is today UTC", nil, AddExpenseCommand{}, NewDate(2026, 9, 16)},
		{"no date, zone ahead of UTC", ptr("Pacific/Kiritimati"), AddExpenseCommand{}, NewDate(2026, 9, 17)},
		{"offset date without zone is the UTC day", nil,
			AddExpenseCommand{Date: ptr(time.Date(2026, 9, 17, 5, 0, 0, 0, plus7))}, NewDate(2026, 9, 16)},
		{"UTC date converted to zone", ptr("Asia/Bangkok"),
			AddExpenseCommand{Date: ptr(time.Date(2026, 9, 16, 20, 0, 0, 0, time.UTC))}, NewDate(2026, 9, 17)},

		{"bot midnight date west of UTC", ptr("America/New_York"),
			AddExpenseCommand{Date: ptr(time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))}, NewDate(2026, 9, 9)},
		{"occurredAt without zone keeps the device offset", nil,
			AddExpenseCommand{OccurredAt: ptr(time.Date(2026, 9, 16, 23, 30, 0, 0, plus7)), Date: ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))}, NewDate(2026, 9, 16)},
		{"occurredAt converted to the trip zone", ptr("Europe/Moscow"),
			AddExpenseCommand{OccurredAt: ptr(time.Date(2026, 9, 17, 2, 0, 0, 0, plus7))}, NewDate(2026, 9, 16)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newExpenseFixture()
			f.activeTrip(func(t *Trip) { t.Timezone = c.zone })
			cmd := c.cmd
			cmd.Amount, cmd.Description = MustDecimal("10"), "кофе"
			got, err := f.svc.Add(bg, appActor(f.user), cmd)
			if err != nil {
				t.Fatal(err)
			}
			if d := f.stored(got.ID).OccurredAt; d != c.want {
				t.Fatalf("got %v, want %v", d, c.want)
			}
		})
	}
}

func TestAddExpenseClosestSnapshot(t *testing.T) {
	f := newExpenseFixture()
	f.activeTrip(nil)
	f.rates.snapshots = append(f.rates.snapshots, Snapshot{
		ID: 50, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Rates: map[string]Decimal{"THB": MustDecimal("35")},
	})
	got, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("350"), Description: "кофе", OccurredAt: ptr(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	if e := f.stored(got.ID); e.SnapshotID != 50 || e.AmountUSD.String() != "10.000000" {
		t.Fatalf("stored %+v", e)
	}
	got, _ = f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("350"), Description: "кофе"})
	if f.stored(got.ID).SnapshotID != 110 {
		t.Fatal("without occurredAt the latest snapshot is used")
	}
}

func TestAddExpenseClientID(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	client := uuid.New()

	first, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("350.004"), Description: "кофе", ClientID: &client, TripID: &trip.ID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Amount.String() != "350.004" {
		t.Fatalf("reply echoes the request amount, got %s", first.Amount)
	}

	again, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Currency: ptr("usd"), Description: "отель", ClientID: &client})
	if err != nil {
		t.Fatal(err)
	}
	if again != (AddExpenseResponse{CategoryName: "Кафе и рестораны", Amount: again.Amount, Currency: "THB", ID: first.ID}) || again.Amount.String() != "350.00" {
		t.Fatalf("replay %+v", again)
	}
	if f.store.calls["InsertExpense"] != 1 {
		t.Fatalf("inserts %d", f.store.calls["InsertExpense"])
	}

	_, err = f.svc.Add(bg, appActor(uuid.New()), AddExpenseCommand{Amount: MustDecimal("1"), Description: "a", ClientID: &client})
	wantErr(t, err, KindValidation, "Трата с таким client_id уже есть у другого пользователя")

	done := f.store.putTrip(Trip{UserID: f.user, Name: "Old", Status: StatusCompleted})
	if _, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("1"), Currency: ptr("USD"), Description: "a", TripID: &done.ID}); err != nil {
		t.Fatalf("completed trip by id: %v", err)
	}
}

func TestAddExpenseLookAlike(t *testing.T) {
	f := newExpenseFixture()
	f.activeTrip(nil)
	add := func(actor Actor, amount string, cmd AddExpenseCommand) Expense {
		t.Helper()
		cmd.Amount, cmd.Description = MustDecimal(amount), "кофе"
		got, err := f.svc.Add(bg, actor, cmd)
		if err != nil {
			t.Fatal(err)
		}
		return f.stored(got.ID)
	}
	bot, app := botActor(f.user), appActor(f.user)

	original := add(bot, "350", AddExpenseCommand{})
	if original.Source != SourceBot || original.DuplicateOfID != nil {
		t.Fatalf("original %+v", original)
	}
	sameSource := add(bot, "350", AddExpenseCommand{})
	if sameSource.DuplicateOfID != nil {
		t.Fatal("same source must not be flagged")
	}
	fromApp := add(app, "350.00", AddExpenseCommand{})
	if fromApp.DuplicateOfID == nil || *fromApp.DuplicateOfID != original.ID {
		t.Fatalf("app look-alike %+v", fromApp)
	}

	secondBot := add(bot, "350", AddExpenseCommand{})
	if secondBot.DuplicateOfID != nil {
		t.Fatalf("flagged rows are not originals: %+v", secondBot)
	}

	viaSync := add(bot, "350", AddExpenseCommand{Source: ptr(SourceApp)})
	if viaSync.Source != SourceApp || viaSync.DuplicateOfID == nil || *viaSync.DuplicateOfID != original.ID {
		t.Fatalf("sync look-alike %+v", viaSync)
	}

	if e := add(app, "350", AddExpenseCommand{Currency: ptr("RUB")}); e.DuplicateOfID != nil {
		t.Fatal("other currency flagged")
	}
	if e := add(app, "350", AddExpenseCommand{Date: ptr(testNow.AddDate(0, 0, -1))}); e.DuplicateOfID != nil {
		t.Fatal("other day flagged")
	}
	if e := add(app, "350.004", AddExpenseCommand{}); e.DuplicateOfID != nil {
		t.Fatal("unrounded amount must not match the stored 350.00")
	}
}

func TestExpenseList(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	other := f.store.putTrip(Trip{UserID: f.user, Name: "Old", Status: StatusCompleted})
	day := func(d int) Date { return NewDate(2026, 9, d) }
	f.store.putExpense(Expense{ID: 1, TripID: trip.ID, CategoryID: 3, Amount: MustDecimal("350.00"), AmountUSD: MustDecimal("10.586556"), CurrencyCode: "THB", OccurredAt: day(16), Description: "кофе"})
	f.store.putExpense(Expense{ID: 2, TripID: trip.ID, CategoryID: 1, Amount: MustDecimal("2000.00"), AmountUSD: MustDecimal("60.494604"), CurrencyCode: "THB", OccurredAt: day(15)})
	f.store.putExpense(Expense{ID: 3, TripID: trip.ID, CategoryID: 3, Amount: MustDecimal("120.00"), AmountUSD: MustDecimal("3.629676"), CurrencyCode: "THB", OccurredAt: day(16)})
	f.store.putExpense(Expense{ID: 4, TripID: trip.ID, CategoryID: 3, Amount: MustDecimal("350.00"), AmountUSD: MustDecimal("10.586556"), CurrencyCode: "THB", OccurredAt: day(17), DuplicateOfID: ptr(int64(1))})
	f.store.putExpense(Expense{ID: 5, TripID: other.ID, CategoryID: 7, Amount: MustDecimal("1.00"), AmountUSD: MustDecimal("1"), CurrencyCode: "USD", OccurredAt: day(1)})

	got, err := f.svc.List(bg, appActor(f.user), ListExpensesQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 4 || got.TotalUSD.String() != "74.710836" || len(got.Categories) != 2 {
		t.Fatalf("totals %+v", got)
	}
	housing, cafe := got.Categories[0], got.Categories[1]
	if housing.CategoryID != 1 || housing.TotalUSD.String() != "60.494604" || housing.Count != 1 {
		t.Fatalf("first group %+v", housing)
	}
	if cafe.CategoryName != "Кафе и рестораны" || cafe.Count != 3 || cafe.TotalUSD.String() != "14.216232" {
		t.Fatalf("cafe group %+v", cafe)
	}
	if ids := [3]int64{cafe.Items[0].ID, cafe.Items[1].ID, cafe.Items[2].ID}; ids != [3]int64{4, 3, 1} {
		t.Fatalf("item order %v", ids)
	}
	if cafe.Items[0].DuplicateOfID == nil || cafe.Items[0].Amount.String() != "350.00" {
		t.Fatalf("flagged item %+v", cafe.Items[0])
	}

	byTrip, _ := f.svc.List(bg, appActor(f.user), ListExpensesQuery{TripID: &other.ID})
	if byTrip.Count != 1 || byTrip.Categories[0].CategoryID != 7 {
		t.Fatalf("by trip %+v", byTrip)
	}
	byCategory, _ := f.svc.List(bg, appActor(f.user), ListExpensesQuery{CategoryID: ptr(int64(1))})
	if byCategory.Count != 1 {
		t.Fatalf("by category %+v", byCategory)
	}

	stranger, err := f.svc.List(bg, appActor(uuid.New()), ListExpensesQuery{TripID: &trip.ID})
	if err != nil || stranger.Count != 0 || stranger.Categories == nil || stranger.TotalUSD.String() != "0" {
		t.Fatalf("someone else's trip %+v %v", stranger, err)
	}
}

func TestExpenseListStableGroupOrder(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	f.store.putExpense(Expense{ID: 1, TripID: trip.ID, CategoryID: 7, AmountUSD: MustDecimal("5"), OccurredAt: NewDate(2026, 9, 16)})
	f.store.putExpense(Expense{ID: 2, TripID: trip.ID, CategoryID: 1, AmountUSD: MustDecimal("5.00"), OccurredAt: NewDate(2026, 9, 15)})
	got, _ := f.svc.List(bg, appActor(f.user), ListExpensesQuery{})
	if got.Categories[0].CategoryID != 7 || got.Categories[1].CategoryID != 1 {
		t.Fatalf("ties must keep first-appearance order: %+v", got.Categories)
	}
}

func TestExpenseTotal(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	done := f.store.putTrip(Trip{UserID: f.user, Status: StatusCompleted})
	f.store.putExpense(Expense{TripID: trip.ID, Amount: MustDecimal("350.00"), CurrencyCode: "THB", OccurredAt: NewDate(2026, 9, 10)})
	f.store.putExpense(Expense{TripID: trip.ID, Amount: MustDecimal("120.00"), CurrencyCode: "USD", OccurredAt: NewDate(2026, 9, 12)})
	f.store.putExpense(Expense{TripID: trip.ID, Amount: MustDecimal("350.00"), CurrencyCode: "THB", OccurredAt: NewDate(2026, 9, 12), DuplicateOfID: ptr(int64(1))})
	f.store.putExpense(Expense{TripID: done.ID, Amount: MustDecimal("9.00"), CurrencyCode: "THB", OccurredAt: NewDate(2026, 9, 12)})

	got, err := f.svc.Total(bg, appActor(f.user), DateRange{})
	if err != nil || got.TotalExpense.String() != "470.00" {
		t.Fatalf("mixed currencies are summed raw (R5): %+v %v", got, err)
	}
	from := NewDate(2026, 9, 11)
	got, _ = f.svc.Total(bg, appActor(f.user), DateRange{From: &from, To: &from})
	if got.TotalExpense.String() != "0" {
		t.Fatalf("empty range %s", got.TotalExpense)
	}
}

func TestExpenseDuplicates(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	strangerTrip := f.store.putTrip(Trip{UserID: uuid.New()})
	client := uuid.New()
	f.store.putExpense(Expense{ID: 1, TripID: trip.ID, CategoryID: 3, Amount: MustDecimal("350.00"), CreatedAt: testNow})
	f.store.putExpense(Expense{ID: 2, TripID: trip.ID, CategoryID: 3, Amount: MustDecimal("350.00"), CreatedAt: testNow, DuplicateOfID: ptr(int64(1)), ClientID: &client})
	f.store.putExpense(Expense{ID: 3, TripID: trip.ID, CategoryID: 3, CreatedAt: testNow.Add(time.Hour), DuplicateOfID: ptr(int64(1))})
	f.store.putExpense(Expense{ID: 4, TripID: strangerTrip.ID})
	f.store.putExpense(Expense{ID: 5, TripID: trip.ID, CreatedAt: testNow.Add(2 * time.Hour), DuplicateOfID: ptr(int64(4))})

	got, err := f.svc.Duplicates(bg, appActor(f.user))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Expense.ID != 3 || got[1].Expense.ID != 2 || got[1].DuplicateOf.ID != 1 {
		t.Fatalf("pairs %+v", got)
	}
	if got[1].Expense.ClientID == nil || *got[1].Expense.ClientID != client || got[1].Expense.CategoryName != "Кафе и рестораны" || got[1].Expense.TripID != trip.ID {
		t.Fatalf("dto %+v", got[1].Expense)
	}

	none, err := f.svc.Duplicates(bg, appActor(uuid.New()))
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("empty must be [], got %v %v", none, err)
	}
}

func TestExpenseNotDuplicateAndDelete(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	f.store.putExpense(Expense{ID: 1, TripID: trip.ID})
	f.store.putExpense(Expense{ID: 2, TripID: trip.ID, DuplicateOfID: ptr(int64(1))})

	wantErr(t, f.svc.NotDuplicate(bg, appActor(uuid.New()), 2), KindNotFound, "Трата не найдена")
	wantErr(t, f.svc.NotDuplicate(bg, appActor(f.user), 99), KindNotFound, "Трата не найдена")
	if err := f.svc.NotDuplicate(bg, appActor(f.user), 2); err != nil || f.stored(2).DuplicateOfID != nil {
		t.Fatalf("clear %v", err)
	}
	if err := f.svc.NotDuplicate(bg, appActor(f.user), 2); err != nil {
		t.Fatalf("unflagged is fine too: %v", err)
	}

	wantErr(t, f.svc.Delete(bg, appActor(uuid.New()), 1), KindNotFound, "Трата не найдена")
	if err := f.svc.Delete(bg, appActor(f.user), 1); err != nil {
		t.Fatal(err)
	}
	wantErr(t, f.svc.Delete(bg, appActor(f.user), 1), KindNotFound, "Трата не найдена")
}

func TestExpenseDTO(t *testing.T) {
	f := newExpenseFixture()
	trip := f.activeTrip(nil)
	f.store.putExpense(Expense{ID: 7, TripID: trip.ID, CategoryID: 1, Amount: MustDecimal("2000.00"), AmountUSD: MustDecimal("60.494604"), CurrencyCode: "THB", Description: "отель", OccurredAt: NewDate(2026, 9, 17)})

	_, err := f.svc.ExpenseDTO(bg, appActor(uuid.New()), 7)
	wantErr(t, err, KindNotFound, "Трата не найдена")
	got, err := f.svc.ExpenseDTO(bg, appActor(f.user), 7)
	if err != nil || got.CategoryName != "Жильё" || got.TripID != trip.ID || got.Amount.String() != "2000.00" || got.ClientID != nil {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestAddExpenseRejectsMalformedCurrencyCode(t *testing.T) {
	for _, code := range []string{"USDD", "US"} {
		t.Run(code, func(t *testing.T) {
			f := newExpenseFixture()

			f.rates.snapshots[0].Rates[code] = MustDecimal("2")
			f.activeTrip(func(trip *Trip) { trip.CurrencyCode = ptr(code) })

			_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal("10"), Description: "кофе"})
			var business *Error
			if !errors.As(err, &business) || business.Kind != KindValidation {
				t.Fatalf("err = %v, want a validation error", err)
			}
			if business.Message != "Нужен трёхбуквенный код валюты" {
				t.Errorf("message %q, want the currency code one", business.Message)
			}
			if len(f.store.expenses) != 0 {
				t.Error("the expense was stored despite the bad currency code")
			}
		})
	}
}

func TestAddExpenseAmountUpperBound(t *testing.T) {
	cases := []struct {
		amount string
		ok     bool
	}{
		{"9999999999999999.99", true},
		{"9999999999999999.994", true},
		{"9999999999999999.995", false},
		{"10000000000000000", false},
		{"1e20", false},
	}
	for _, c := range cases {
		t.Run(c.amount, func(t *testing.T) {
			f := newExpenseFixture()
			f.activeTrip(func(trip *Trip) { trip.CurrencyCode = ptr("USD") })
			_, err := f.svc.Add(bg, appActor(f.user), AddExpenseCommand{Amount: MustDecimal(c.amount), Description: "кофе"})
			if c.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			wantErr(t, err, KindValidation, msgAmountTooLarge)
			if len(f.store.expenses) != 0 {
				t.Error("the expense was stored")
			}
		})
	}
}
