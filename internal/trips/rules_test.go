package trips

import (
	"testing"
	"time"
)

func wantErr(t *testing.T, err error, kind Kind, msg string) {
	t.Helper()
	e := mustErr(err)
	if e == nil {
		t.Fatalf("want %v %q, got %v", kind, msg, err)
	}
	if e.Kind != kind || e.Message != msg {
		t.Fatalf("want %v %q, got %v %q", kind, msg, e.Kind, e.Message)
	}
}

func TestTripStart(t *testing.T) {
	today := NewDate(2026, 9, 16)

	trip := Trip{Status: StatusPlanned}
	if err := trip.Start(nil, today); err != nil {
		t.Fatal(err)
	}
	if trip.Status != StatusActive || *trip.StartDate != today {
		t.Fatalf("got %v %v", trip.Status, trip.StartDate)
	}

	on := NewDate(2026, 9, 1)
	trip = Trip{Status: StatusPlanned}
	_ = trip.Start(&on, today)
	if *trip.StartDate != on {
		t.Fatalf("start %v", trip.StartDate)
	}

	for _, status := range []TripStatus{StatusActive, StatusCompleted} {
		trip := Trip{Status: status}
		wantErr(t, trip.Start(nil, today), KindConflict, "Попытка активировать уже активную или законченную поездку")
	}
}

func TestTripEnd(t *testing.T) {
	today := NewDate(2026, 9, 16)
	start := NewDate(2026, 9, 10)

	trip := Trip{Status: StatusActive, StartDate: &start}
	if err := trip.End(nil, today); err != nil {
		t.Fatal(err)
	}
	if trip.Status != StatusCompleted || *trip.EndDate != today {
		t.Fatalf("got %v %v", trip.Status, trip.EndDate)
	}

	early := NewDate(2026, 9, 5)
	trip = Trip{Status: StatusActive, StartDate: &start}
	_ = trip.End(&early, today)
	if *trip.EndDate != start {
		t.Fatalf("end not clamped: %v", trip.EndDate)
	}

	trip = Trip{Status: StatusPlanned}
	_ = trip.End(&early, today)
	if *trip.EndDate != early || trip.Status != StatusCompleted {
		t.Fatalf("planned end %v", trip.EndDate)
	}

	trip = Trip{Status: StatusCompleted}
	wantErr(t, trip.End(nil, today), KindConflict, "Попытка закончить уже законченную поездку")
}

func TestTripSetCountry(t *testing.T) {
	trip := Trip{}
	trip.SetCountry(thailand)
	if *trip.CountryCode != "TH" || *trip.CurrencyCode != "THB" || *trip.Timezone != "Asia/Bangkok" || *trip.CountryName != "Таиланд" {
		t.Fatalf("got %+v", trip)
	}

	trip = Trip{CurrencyCode: ptr("USD"), Timezone: ptr("Asia/Phuket")}
	trip.SetCountry(thailand)
	if *trip.CurrencyCode != "USD" || *trip.Timezone != "Asia/Phuket" {
		t.Fatalf("explicit values overwritten: %+v", trip)
	}

	trip = Trip{CurrencyCode: ptr(""), Timezone: ptr("  ")}
	trip.SetCountry(thailand)
	if *trip.CurrencyCode != "" || *trip.Timezone != "Asia/Bangkok" {
		t.Fatalf("got currency %q zone %q", *trip.CurrencyCode, *trip.Timezone)
	}
}

func TestTripSetTimeZone(t *testing.T) {
	trip := Trip{}
	wantErr(t, trip.SetTimeZone(" "), KindValidation, "Часовой пояс не может быть пустым")
	if err := trip.SetTimeZone("Asia/Bangkok"); err != nil || *trip.Timezone != "Asia/Bangkok" {
		t.Fatalf("got %v %v", trip.Timezone, err)
	}
}

func TestTripDurationDays(t *testing.T) {
	today := NewDate(2026, 9, 16)
	start := NewDate(2026, 9, 9)
	end := NewDate(2026, 9, 10)
	cases := []struct {
		trip Trip
		want int
	}{
		{Trip{}, 0},
		{Trip{EndDate: &end}, 0},
		{Trip{StartDate: &start}, 8},
		{Trip{StartDate: &start, EndDate: &end}, 2},
		{Trip{StartDate: &today}, 1},

		{Trip{StartDate: ptr(NewDate(2026, 9, 20))}, 0},
		{Trip{StartDate: ptr(NewDate(2026, 9, 17))}, 0},
	}
	for i, c := range cases {
		if got := c.trip.DurationDays(today); got != c.want {
			t.Errorf("%d: got %d, want %d", i, got, c.want)
		}
	}
}

func TestLocalDate(t *testing.T) {
	plus7 := time.FixedZone("", 7*3600)
	minus5 := time.FixedZone("", -5*3600)
	cases := []struct {
		name   string
		moment time.Time
		zone   *string
		want   Date
	}{
		{"no zone keeps own offset", time.Date(2026, 9, 16, 23, 30, 0, 0, plus7), nil, NewDate(2026, 9, 16)},
		{"blank zone keeps own offset", time.Date(2026, 9, 16, 1, 0, 0, 0, minus5), ptr(" "), NewDate(2026, 9, 16)},
		{"zone crosses midnight forward", time.Date(2026, 9, 16, 20, 0, 0, 0, time.UTC), ptr("Asia/Bangkok"), NewDate(2026, 9, 17)},
		{"zone crosses midnight back", time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC), ptr("America/New_York"), NewDate(2026, 9, 15)},
		{"offset input converted to zone", time.Date(2026, 9, 16, 23, 30, 0, 0, plus7), ptr("Europe/Moscow"), NewDate(2026, 9, 16)},
		{"half-hour zone", time.Date(2026, 9, 16, 18, 40, 0, 0, time.UTC), ptr("Asia/Kolkata"), NewDate(2026, 9, 17)},
	}
	for _, c := range cases {
		got, err := LocalDate(c.moment, c.zone)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := LocalDate(testNow, ptr("Mars/Olympus")); err == nil {
		t.Error("unknown zone accepted")
	}
}

func TestIsSimilarTrip(t *testing.T) {
	active := Trip{CountryCode: ptr("TH"), City: ptr(" Пхукет ")}
	cases := []struct {
		name    string
		active  Trip
		country *Country
		city    *string
		want    bool
	}{
		{"same country and city, case and spaces", active, &Country{Code: "th"}, ptr("пхукет"), true},
		{"other country", active, &russia, ptr("Пхукет"), false},
		{"other city", active, &thailand, ptr("Бангкок"), false},
		{"payload without country", active, nil, ptr("Пхукет"), true},
		{"payload without city", active, &thailand, ptr("  "), true},
		{"active without country", Trip{City: ptr("Пхукет")}, &russia, nil, true},
		{"active without city", Trip{CountryCode: ptr("TH"), City: ptr("")}, &thailand, ptr("Бангкок"), true},
		{"other country, no cities", Trip{CountryCode: ptr("TH")}, &russia, nil, false},
	}
	for _, c := range cases {
		if got := IsSimilarTrip(c.active, c.country, c.city); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestFillBlanks(t *testing.T) {
	payload := SyncTripCreatePayload{CurrencyCode: ptr("usd"), City: ptr("Пхукет"), Budget: ptr(MustDecimal("100"))}

	trip := Trip{}
	FillBlanks(&trip, payload, &thailand)

	if *trip.CurrencyCode != "USD" || *trip.CountryCode != "TH" || *trip.City != "Пхукет" || trip.Budget.String() != "100" || *trip.Timezone != "Asia/Bangkok" {
		t.Fatalf("got %+v", trip)
	}

	full := Trip{CurrencyCode: ptr("THB"), CountryCode: ptr("RU"), City: ptr("Москва"), Budget: ptr(MustDecimal("5"))}
	FillBlanks(&full, payload, &thailand)
	if *full.CurrencyCode != "THB" || *full.CountryCode != "RU" || *full.City != "Москва" || full.Budget.String() != "5" {
		t.Fatalf("filled values overwritten: %+v", full)
	}

	blank := Trip{CurrencyCode: ptr(" ")}
	FillBlanks(&blank, SyncTripCreatePayload{}, &thailand)
	if *blank.CurrencyCode != " " || *blank.CountryCode != "TH" {
		t.Fatalf("got %+v", blank)
	}
}

func TestFormatUTCOffset(t *testing.T) {
	cases := map[time.Duration]string{
		7 * time.Hour:                      "UTC+7",
		5*time.Hour + 30*time.Minute:       "UTC+5.5",
		5*time.Hour + 45*time.Minute:       "UTC+5.75",
		-3 * time.Hour:                     "UTC-3",
		-(9*time.Hour + 30*time.Minute):    "UTC-9.5",
		0:                                  "UTC+0",
		12*time.Hour + 45*time.Minute:      "UTC+12.75",
		-(30 * time.Minute):                "UTC-0.5",
		5*time.Hour + 20*time.Minute + 1e9: "UTC+5.33",
	}
	for offset, want := range cases {
		if got := FormatUTCOffset(offset); got != want {
			t.Errorf("%v: got %s, want %s", offset, got, want)
		}
	}
}
