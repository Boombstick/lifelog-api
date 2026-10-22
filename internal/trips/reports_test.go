package trips

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func newReportFixture() (*memStore, *ReportService, uuid.UUID) {
	store := seededStore()
	store.categories[2] = Category{ID: 2, Name: "Транспорт"}
	store.categories[9] = Category{ID: 9, Name: "Визы и документы"}
	store.categories[11] = Category{ID: 11, Name: "ёлки"}
	store.categories[12] = Category{ID: 12, Name: "едa"}
	return store, NewReportService(store, fixedClock{testNow}), uuid.New()
}

func TestReportOld(t *testing.T) {
	store, svc, user := newReportFixture()
	trip := store.putTrip(Trip{UserID: user, Name: "Asia"})
	old := store.putTrip(Trip{UserID: user, Name: "Old", Status: StatusCompleted})
	today := NewDate(2026, 9, 16)
	add := func(tripID uuid.UUID, category int64, amount, currency string, day Date, flagged bool) {
		e := Expense{TripID: tripID, CategoryID: category, Amount: MustDecimal(amount), CurrencyCode: currency, OccurredAt: day}
		if flagged {
			e.DuplicateOfID = ptr(int64(1))
		}
		store.putExpense(e)
	}
	add(trip.ID, 3, "350.00", "THB", today, false)
	add(trip.ID, 3, "120.00", "THB", today, false)
	add(trip.ID, 3, "5.00", "USD", today, false)
	add(old.ID, 2, "10.00", "GEL", today, false)
	add(trip.ID, 9, "1.00", "USD", today, false)
	add(trip.ID, 3, "999.00", "THB", today, true)
	add(trip.ID, 1, "2000.00", "THB", today.AddDays(-1), false)

	got, err := svc.Report(bg, appActor(user), DateRange{})
	if err != nil {
		t.Fatal(err)
	}
	if got.From != today || got.To != today {
		t.Fatalf("defaults %v %v", got.From, got.To)
	}
	body, _ := json.Marshal(got)
	want := `{"result":[` +
		`{"category_name":"Визы и документы","amounts":[{"currency":"USD","amount":1.00}]},` +
		`{"category_name":"Кафе и рестораны","amounts":[{"currency":"THB","amount":470.00},{"currency":"USD","amount":5.00}]},` +
		`{"category_name":"Транспорт","amounts":[{"currency":"GEL","amount":10.00}]}],` +
		`"total_amounts":[{"currency":"GEL","amount":10.00},{"currency":"THB","amount":470.00},{"currency":"USD","amount":6.00}],` +
		`"from":"2026-09-16","to":"2026-09-16"}`
	if string(body) != want {
		t.Fatalf("got  %s\nwant %s", body, want)
	}

	from := today.AddDays(-1)
	got, _ = svc.Report(bg, appActor(user), DateRange{From: &from})
	if got.From != from || got.To != today || len(got.Result) != 4 {
		t.Fatalf("explicit from %+v", got)
	}

	empty, _ := svc.Report(bg, appActor(uuid.New()), DateRange{})
	body, _ = json.Marshal(empty)
	if string(body) != `{"result":[],"total_amounts":[],"from":"2026-09-16","to":"2026-09-16"}` {
		t.Fatalf("empty %s", body)
	}
}

func TestCultureLess(t *testing.T) {
	names := []string{"Прочее", "ёлки", "Жильё", "авиа", "Ели", "Елка", "ели"}
	sorted := append([]string(nil), names...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if cultureLess(sorted[j], sorted[i]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	want := []string{"авиа", "Ели", "ели", "Елка", "ёлки", "Жильё", "Прочее"}
	if fmt.Sprint(sorted) != fmt.Sprint(want) {
		t.Fatalf("got %v", sorted)
	}
}

func TestParseDimensions(t *testing.T) {
	cases := map[string][]string{
		"":                                {},
		"  ":                              {},
		"trip":                            {"trip"},
		" Category , TRIP,foo,trip,,city": {"category", "trip", "city"},
		"city,city":                       {"city"},
		"trips,categories":                {},
	}
	for in, want := range cases {
		got := ParseDimensions(in)
		if got == nil || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

type builderFixture struct {
	store      *memStore
	svc        *ReportService
	user       uuid.UUID
	asia, city Trip
}

func newBuilderFixture() *builderFixture {
	store, svc, user := newReportFixture()
	f := &builderFixture{store: store, svc: svc, user: user}
	f.asia = store.putTrip(Trip{UserID: user, Name: "Asia", City: ptr("Пхукет")})
	f.city = store.putTrip(Trip{UserID: user, Name: "Nowhere", Status: StatusCompleted, City: ptr("  ")})
	day := func(d int) Date { return NewDate(2026, 9, d) }
	put := func(id int64, trip Trip, category int64, amount, usd, currency string, d int, flagged bool) {
		e := Expense{ID: id, TripID: trip.ID, CategoryID: category, Amount: MustDecimal(amount), AmountUSD: MustDecimal(usd), CurrencyCode: currency, OccurredAt: day(d), Description: "e"}
		if flagged {
			e.DuplicateOfID = ptr(int64(1))
		}
		store.putExpense(e)
	}
	put(1, f.asia, 1, "2000.00", "60.494604", "THB", 17, false)
	put(2, f.asia, 3, "350.00", "10.586556", "THB", 16, false)
	put(3, f.asia, 3, "700.00", "21.173112", "THB", 17, false)
	put(4, f.city, 3, "30.00", "30", "USD", 10, false)
	put(5, f.city, 7, "10.00", "3.5", "GEL", 10, false)
	put(6, f.asia, 3, "350.00", "10.586556", "THB", 16, true)
	store.putExpense(Expense{ID: 7, TripID: store.putTrip(Trip{UserID: uuid.New()}).ID, AmountUSD: MustDecimal("1")})
	return f
}

func TestReportBuilderNoDimensions(t *testing.T) {
	f := newBuilderFixture()
	got, err := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Dimensions: "nope"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(got)
	want := `{"groups":[{"trip_id":null,"trip_name":null,"city":null,"category_id":null,"category_name":null,"count":5,"total_usd":125.754272,` +
		`"amounts":[{"currency":"THB","amount":3050.00},{"currency":"USD","amount":30.00},{"currency":"GEL","amount":10.00}],"expenses":[]}],` +
		`"dimensions":[],"total_count":5,"total_usd":125.754272,"from":null,"to":null,"truncated":false}`
	if string(body) != want {
		t.Fatalf("got  %s\nwant %s", body, want)
	}

	none, _ := f.svc.Builder(bg, appActor(uuid.New()), ReportBuilderQuery{})
	body, _ = json.Marshal(none)
	if string(body) != `{"groups":[],"dimensions":[],"total_count":0,"total_usd":0,"from":null,"to":null,"truncated":false}` {
		t.Fatalf("empty %s", body)
	}
}

func TestReportBuilderDimensions(t *testing.T) {
	f := newBuilderFixture()

	byTrip, _ := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Dimensions: "trip"})
	if len(byTrip.Groups) != 2 || *byTrip.Groups[0].TripName != "Asia" || byTrip.Groups[0].Count != 3 ||
		byTrip.Groups[0].TotalUSD.String() != "92.254272" || byTrip.Groups[0].City != nil || byTrip.Groups[0].CategoryID != nil ||
		*byTrip.Groups[1].TripID != f.city.ID {
		t.Fatalf("by trip %+v", byTrip.Groups)
	}

	byCity, _ := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Dimensions: "city"})
	if len(byCity.Groups) != 2 || *byCity.Groups[0].City != "Пхукет" || byCity.Groups[1].City != nil || byCity.Groups[0].TripID != nil {
		t.Fatalf("by city: blank city must be a null group, got %+v", byCity.Groups)
	}

	byCategoryTrip, _ := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Dimensions: "category,trip", IncludeExpenses: true})
	if fmt.Sprint(byCategoryTrip.Dimensions) != "[category trip]" {
		t.Fatalf("dimensions %v", byCategoryTrip.Dimensions)
	}
	type key struct {
		trip     string
		category int64
		count    int
		usd      string
	}
	var keys []key
	for _, g := range byCategoryTrip.Groups {
		keys = append(keys, key{*g.TripName, *g.CategoryID, g.Count, g.TotalUSD.String()})
	}
	want := []key{{"Asia", 1, 1, "60.494604"}, {"Asia", 3, 2, "31.759668"}, {"Nowhere", 3, 1, "30"}, {"Nowhere", 7, 1, "3.5"}}
	if fmt.Sprint(keys) != fmt.Sprint(want) {
		t.Fatalf("groups %v", keys)
	}
	cafe := byCategoryTrip.Groups[1]
	if cafe.Expenses[0].ID != 3 || cafe.Expenses[1].ID != 2 || *cafe.CategoryName != "Кафе и рестораны" {
		t.Fatalf("expense order %+v", cafe.Expenses)
	}
	nowhere := byCategoryTrip.Groups[2].Expenses[0]
	if nowhere.City != "  " || nowhere.TripName != "Nowhere" || nowhere.CategoryName != "Кафе и рестораны" || nowhere.Amount.String() != "30.00" {
		t.Fatalf("expense item %+v", nowhere)
	}

	all, _ := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Dimensions: "trip,city,category"})
	if len(all.Groups) != 4 || all.Groups[0].City == nil || all.Groups[0].TripID == nil || all.Groups[0].CategoryID == nil {
		t.Fatalf("all dimensions %+v", all.Groups)
	}

	from, to := NewDate(2026, 9, 16), NewDate(2026, 9, 16)
	ranged, _ := f.svc.Builder(bg, appActor(f.user), ReportBuilderQuery{Range: DateRange{From: &from, To: &to}})
	if ranged.TotalCount != 1 || *ranged.From != from || *ranged.To != to {
		t.Fatalf("range %+v", ranged)
	}
}

func TestReportBuilderNullTripCityInExpenses(t *testing.T) {
	store, svc, user := newReportFixture()
	trip := store.putTrip(Trip{UserID: user, Name: "NoCity"})
	store.putExpense(Expense{TripID: trip.ID, CategoryID: 3, AmountUSD: MustDecimal("1"), Amount: MustDecimal("1.00"), CurrencyCode: "USD"})
	got, _ := svc.Builder(bg, appActor(user), ReportBuilderQuery{Dimensions: "city", IncludeExpenses: true})
	if got.Groups[0].City != nil || got.Groups[0].Expenses[0].City != "" {
		t.Fatalf("got %+v", got.Groups[0])
	}
}

func TestReportBuilderTruncation(t *testing.T) {
	store, svc, user := newReportFixture()
	trip := store.putTrip(Trip{UserID: user, Name: "Big"})

	id := int64(1)
	fill := func(category int64, n int, usd string) {
		for i := 0; i < n; i++ {
			store.putExpense(Expense{ID: id, TripID: trip.ID, CategoryID: category, AmountUSD: MustDecimal(usd), Amount: MustDecimal("1.00"), CurrencyCode: "USD", OccurredAt: NewDate(2026, 9, 1+i%28)})
			id++
		}
	}
	fill(1, 1500, "2")
	fill(3, 700, "1")
	fill(7, 10, "0.5")

	got, err := svc.Builder(bg, appActor(user), ReportBuilderQuery{Dimensions: "category", IncludeExpenses: true})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.TotalCount != 2210 {
		t.Fatalf("truncated %v total %d", got.Truncated, got.TotalCount)
	}
	counts := []int{len(got.Groups[0].Expenses), len(got.Groups[1].Expenses), len(got.Groups[2].Expenses)}
	if fmt.Sprint(counts) != "[1500 500 0]" {
		t.Fatalf("kept %v", counts)
	}
	if got.Groups[1].Count != 700 || got.Groups[2].Count != 10 || got.Groups[2].Expenses == nil {
		t.Fatalf("sums must stay complete and slices non-nil: %+v", got.Groups[2])
	}

	exact, _ := svc.Builder(bg, appActor(user), ReportBuilderQuery{Dimensions: "category"})
	if exact.Truncated {
		t.Fatal("without expenses nothing is truncated")
	}

	store2, svc2, user2 := newReportFixture()
	trip2 := store2.putTrip(Trip{UserID: user2})
	for i := int64(1); i <= BuilderExpenseLimit; i++ {
		store2.putExpense(Expense{ID: i, TripID: trip2.ID, CategoryID: 3, AmountUSD: MustDecimal("1")})
	}
	atLimit, _ := svc2.Builder(bg, appActor(user2), ReportBuilderQuery{IncludeExpenses: true})
	if atLimit.Truncated || len(atLimit.Groups[0].Expenses) != BuilderExpenseLimit {
		t.Fatalf("at limit truncated=%v", atLimit.Truncated)
	}
}
