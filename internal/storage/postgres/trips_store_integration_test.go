package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/trips"
	"lifelog-api/internal/trips/seed"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const tripsTestDSNEnv = "LIFELOG_TRIPS_TEST_PG"

const tripsTestDB = "lifelog_trips_m1store"

func newTripsTestStore(t *testing.T) (*TripsStore, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv(tripsTestDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping Postgres integration test", tripsTestDSNEnv)
	}

	admin, err := gorm.Open(postgres.Open(dsn+" dbname=postgres"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	drop := func() { _ = admin.Exec(`DROP DATABASE IF EXISTS ` + tripsTestDB + ` WITH (FORCE)`).Error }
	drop()
	if err := admin.Exec(`CREATE DATABASE ` + tripsTestDB).Error; err != nil {
		t.Fatal(err)
	}

	cfg := &PgConfig{Database: tripsTestDB}
	for _, kv := range strings.Fields(dsn) {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "host":
			cfg.Host = v
		case "port":
			cfg.Port = v
		case "user":
			cfg.User = v
		case "password":
			cfg.Password = v
		}
	}
	db, err := NewTripsPostgres(cfg)
	if err != nil {
		drop()
		t.Fatalf("NewTripsPostgres: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		drop()
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	store := NewTripsStore(db)
	data, err := seed.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SeedDictionaries(context.Background(), data); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return store, db
}

func tripsV7(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tripsPtr[T any](v T) *T { return &v }

func tripsNow() time.Time { return trips.SystemClock{}.Now() }

func tripsNewUser(t *testing.T, s *TripsStore, name string) trips.User {
	t.Helper()
	u := trips.User{ID: tripsV7(t), Username: name, CreatedAt: tripsNow(), AuthUserID: tripsPtr(uuid.New())}
	if err := s.InsertUser(context.Background(), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func tripsNewTrip(t *testing.T, s *TripsStore, userID uuid.UUID, name string, status trips.TripStatus) trips.Trip {
	t.Helper()
	ts := tripsNow()
	trip := trips.Trip{
		ID: tripsV7(t), UserID: userID, Name: name, Status: status, Source: trips.SourceApp,
		CountryCode: tripsPtr("TH"), CurrencyCode: tripsPtr("THB"), Timezone: tripsPtr("Asia/Bangkok"),
		StartDate: tripsPtr(trips.NewDate(2026, 9, 9)), CreatedAt: ts, UpdatedAt: ts,
	}
	if err := s.InsertTrip(context.Background(), &trip); err != nil {
		t.Fatal(err)
	}
	return trip
}

func tripsWantMessage(t *testing.T, err error, msg string) {
	t.Helper()
	e, ok := trips.AsError(err)
	if !ok || e.Message != msg {
		t.Fatalf("error = %v, want business error %q", err, msg)
	}
}

func tripsWantUnique(t *testing.T, err error) {
	t.Helper()
	ie, ok := trips.AsIntegrityError(err)
	if !ok || !ie.IsUniqueViolation() {
		t.Fatalf("error = %v, want unique violation", err)
	}
}

func TestTripsStoreIntegration(t *testing.T) {
	s, db := newTripsTestStore(t)
	ctx := context.Background()

	t.Run("seed", func(t *testing.T) {
		counts := map[string]int64{}
		for _, table := range []string{"currencies", "countries", "expense_categories"} {
			var n int64
			db.Raw(`SELECT COUNT(*) FROM ` + table).Scan(&n)
			counts[table] = n
		}
		if counts["currencies"] != 166 || counts["countries"] != 250 || counts["expense_categories"] != 10 {
			t.Fatalf("counts after seed: %v", counts)
		}

		if err := s.SetCategoryKeywords(ctx, 1, []string{"custom"}); err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`UPDATE expense_categories SET name = 'x' WHERE id = 1`).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`UPDATE currencies SET name_ru = 'x' WHERE code = 'USD'`).Error; err != nil {
			t.Fatal(err)
		}
		data, _ := seed.Load()
		for i := 0; i < 2; i++ {
			if err := s.SeedDictionaries(ctx, data); err != nil {
				t.Fatalf("reseed %d: %v", i, err)
			}
		}
		c, err := s.CategoryByID(ctx, 1)
		if err != nil || c == nil {
			t.Fatal(c, err)
		}
		if c.Name != "Жильё" || len(c.Keywords) != 1 || c.Keywords[0] != "custom" {
			t.Errorf("category 1 after reseed: %+v", c)
		}
		usd, _ := s.CurrencyByCode(ctx, "USD")
		if usd == nil || usd.NameRu != "Доллар США" {
			t.Errorf("USD after reseed: %+v", usd)
		}

		cat := trips.Category{Name: "Тест", Keywords: []string{"а", "б"}}
		if err := s.InsertCategory(ctx, &cat); err != nil {
			t.Fatal(err)
		}
		if cat.ID != 11 {
			t.Errorf("new category id = %d, want 11", cat.ID)
		}
		if err := s.SeedDictionaries(ctx, data); err != nil {
			t.Fatal(err)
		}
		all, _ := s.Categories(ctx)
		if len(all) != 11 || all[10].Name != "Тест" || len(all[10].Keywords) != 2 {
			t.Errorf("categories after reseed: %d, last %+v", len(all), all[len(all)-1])
		}
	})

	t.Run("dictionary", func(t *testing.T) {
		countries, err := s.Countries(ctx)
		if err != nil || len(countries) != 250 {
			t.Fatal(len(countries), err)
		}
		if countries[0].NameRu[:2] != "А" {
			t.Errorf("countries not ordered by name_ru: first is %q", countries[0].NameRu)
		}
		currencies, err := s.Currencies(ctx)
		if err != nil || len(currencies) != 166 {
			t.Fatal(len(currencies), err)
		}
		for i := 1; i < len(currencies); i++ {
			if currencies[i-1].Code >= currencies[i].Code {
				t.Fatalf("currencies not ordinal: %s, %s", currencies[i-1].Code, currencies[i].Code)
			}
		}

		for _, name := range []string{"таиланд", "thailand"} {
			c, err := s.CountryByName(ctx, name)
			if err != nil || c == nil || c.Code != "TH" || c.DefaultTimezone != "Asia/Bangkok" {
				t.Errorf("CountryByName(%q) = %+v, %v", name, c, err)
			}
		}
		if c, err := s.CountryByName(ctx, "th"); c != nil || err != nil {
			t.Errorf("CountryByName(th) = %+v, %v", c, err)
		}
		if err := db.Exec(`INSERT INTO countries (code, name, name_ru, default_currency_code, is_multi_currency)
		                   VALUES ('ZZ', 'Thailand', 'Двойник', 'USD', false)`).Error; err != nil {
			t.Fatal(err)
		}
		_, err = s.CountryByName(ctx, "thailand")
		tripsWantMessage(t, err, trips.MsgSequenceMoreThanOne)
		db.Exec(`DELETE FROM countries WHERE code = 'ZZ'`)

		if c, err := s.CurrencyByCode(ctx, "usd"); c != nil || err != nil {
			t.Errorf("CurrencyByCode is case-insensitive: %+v, %v", c, err)
		}
		if c, _ := s.CategoryByID(ctx, 999); c != nil {
			t.Errorf("CategoryByID(999) = %+v", c)
		}
		long := trips.Category{Name: strings.Repeat("я", 51), Keywords: nil}
		tripsWantMessage(t, s.InsertCategory(ctx, &long), "Поле name не может быть длиннее 50 символов")
	})

	t.Run("users", func(t *testing.T) {
		tg := int64(424242)
		u := trips.User{ID: tripsV7(t), TelegramID: &tg, Username: "Женя", CreatedAt: tripsNow(), AuthUserID: tripsPtr(uuid.New())}
		if err := s.InsertUser(ctx, &u); err != nil {
			t.Fatal(err)
		}
		byAuth, err := s.UserByAuthID(ctx, *u.AuthUserID)
		if err != nil || byAuth == nil || byAuth.ID != u.ID || !byAuth.CreatedAt.Equal(u.CreatedAt) || byAuth.CreatedAt.Location() != time.UTC {
			t.Fatalf("UserByAuthID = %+v, %v", byAuth, err)
		}
		byTG, _ := s.UserByTelegramID(ctx, tg)
		if byTG == nil || byTG.ID != u.ID || *byTG.TelegramID != tg {
			t.Fatalf("UserByTelegramID = %+v", byTG)
		}
		if none, err := s.UserByAuthID(ctx, uuid.New()); none != nil || err != nil {
			t.Fatalf("missing user = %+v, %v", none, err)
		}

		dup := trips.User{ID: tripsV7(t), TelegramID: &tg, Username: "dup", CreatedAt: tripsNow()}
		tripsWantUnique(t, s.InsertUser(ctx, &dup))
		tooLong := trips.User{ID: tripsV7(t), Username: strings.Repeat("x", 101), CreatedAt: tripsNow()}
		tripsWantMessage(t, s.InsertUser(ctx, &tooLong), "Поле username не может быть длиннее 100 символов")

		other := tripsNewUser(t, s, "other")
		tripsWantUnique(t, s.SetUserTelegramID(ctx, other.ID, &tg))
		if err := s.SetUserTelegramID(ctx, u.ID, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.SetUserTelegramID(ctx, other.ID, &tg); err != nil {
			t.Fatal(err)
		}
		newAuth := uuid.New()
		if err := s.SetUserAuthID(ctx, u.ID, newAuth); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.UserByAuthID(ctx, newAuth); got == nil || got.ID != u.ID || got.TelegramID != nil {
			t.Fatalf("after SetUserAuthID: %+v", got)
		}

		tripsNewTrip(t, s, u.ID, "cascade", trips.StatusCompleted)
		if err := s.DeleteUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if list, _ := s.TripsOfUser(ctx, u.ID); len(list) != 0 {
			t.Fatalf("trips survived the user: %d", len(list))
		}
	})

	t.Run("trips", func(t *testing.T) {
		u := tripsNewUser(t, s, "traveller")
		stranger := tripsNewUser(t, s, "stranger")
		ts := tripsNow()

		bare := trips.Trip{ID: tripsV7(t), UserID: u.ID, Name: "Bare", Status: trips.StatusCompleted,
			Source: trips.SourceBot, Budget: tripsPtr(trips.MustDecimal("5000.50")), CreatedAt: ts, UpdatedAt: ts}
		if err := s.InsertTrip(ctx, &bare); err != nil {
			t.Fatal(err)
		}
		var nullCity, nullTZ bool
		db.Raw(`SELECT city IS NULL, timezone IS NULL FROM trips WHERE id = ?`, bare.ID).Row().Scan(&nullCity, &nullTZ)
		if !nullCity || !nullTZ {
			t.Errorf("nil city/timezone not stored as NULL: %v %v", nullCity, nullTZ)
		}
		got, err := s.OwnedTrip(ctx, u.ID, bare.ID)
		if err != nil || got == nil {
			t.Fatal(got, err)
		}
		if got.Budget.String() != "5000.50" || got.City != nil || got.CountryName != nil || got.Source != trips.SourceBot || got.StartDate != nil {
			t.Errorf("bare trip read back: %+v", got)
		}
		if other, _ := s.OwnedTrip(ctx, stranger.ID, bare.ID); other != nil {
			t.Errorf("stranger sees the trip")
		}

		active := tripsNewTrip(t, s, u.ID, "Asia", trips.StatusActive)
		list, _ := s.ActiveTrips(ctx, u.ID)
		if len(list) != 1 || list[0].ID != active.ID || list[0].CountryName == nil || *list[0].CountryName != "Таиланд" ||
			*list[0].StartDate != trips.NewDate(2026, 9, 9) {
			t.Fatalf("ActiveTrips = %+v", list)
		}

		second := trips.Trip{ID: tripsV7(t), UserID: u.ID, Name: "Second", Status: trips.StatusActive, Source: trips.SourceApp, CreatedAt: ts, UpdatedAt: ts}
		if err := s.InsertTrip(ctx, &second); !errors.Is(err, trips.ErrActiveTripExists) {
			t.Fatalf("second active insert: %v", err)
		}
		bare.Status = trips.StatusActive
		if err := s.UpdateTrip(ctx, &bare); !errors.Is(err, trips.ErrActiveTripExists) {
			t.Fatalf("second active update: %v", err)
		}
		bare.Status = trips.StatusCompleted

		err = s.InTx(ctx, func(tx trips.Store) error {
			if err := tx.InsertTrip(ctx, &second); !errors.Is(err, trips.ErrActiveTripExists) {
				t.Errorf("in tx: %v", err)
			}
			active.Status = trips.StatusCompleted
			active.EndDate = tripsPtr(trips.NewDate(2026, 9, 16))
			if err := tx.UpdateTrip(ctx, &active); err != nil {
				return err
			}
			return tx.InsertTrip(ctx, &second)
		})
		if err != nil {
			t.Fatalf("tx after savepoint: %v", err)
		}
		list, _ = s.ActiveTrips(ctx, u.ID)
		if len(list) != 1 || list[0].ID != second.ID {
			t.Fatalf("after tx the active trip is %+v", list)
		}

		rolled := trips.Trip{ID: tripsV7(t), UserID: u.ID, Name: "Rolled", Status: trips.StatusCompleted, Source: trips.SourceApp, CreatedAt: ts, UpdatedAt: ts}
		boom := errors.New("boom")
		if err := s.InTx(ctx, func(tx trips.Store) error {
			if err := tx.InsertTrip(ctx, &rolled); err != nil {
				return err
			}
			return tx.InTx(ctx, func(inner trips.Store) error { return boom })
		}); !errors.Is(err, boom) {
			t.Fatalf("InTx error = %v", err)
		}
		if got, _ := s.OwnedTrip(ctx, u.ID, rolled.ID); got != nil {
			t.Fatalf("rolled back trip exists")
		}

		unknown := trips.Trip{ID: tripsV7(t), UserID: u.ID, Name: "X", Status: trips.StatusCompleted, Source: trips.SourceApp,
			CurrencyCode: tripsPtr("XXX"), CreatedAt: ts, UpdatedAt: ts}
		tripsWantMessage(t, s.InsertTrip(ctx, &unknown), "Валюта XXX не найдена")
		unknown.CurrencyCode = tripsPtr("ABCD")
		tripsWantMessage(t, s.InsertTrip(ctx, &unknown), "Валюта ABCD не найдена")
		unknown.CurrencyCode = nil
		unknown.City = tripsPtr(strings.Repeat("г", 121))
		tripsWantMessage(t, s.InsertTrip(ctx, &unknown), "Поле city не может быть длиннее 120 символов")
		unknown.City = tripsPtr(strings.Repeat("г", 120))
		if err := s.InsertTrip(ctx, &unknown); err != nil {
			t.Fatalf("120-char city: %v", err)
		}

		moved := bare
		moved.UserID = stranger.ID
		moved.UpdatedAt = tripsNow()
		if err := s.UpdateTrip(ctx, &moved); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.OwnedTrip(ctx, stranger.ID, bare.ID); got == nil || !got.UpdatedAt.Equal(moved.UpdatedAt) {
			t.Fatalf("moved trip = %+v", got)
		}

		all, _ := s.TripsOfUser(ctx, u.ID)
		for i := 1; i < len(all); i++ {
			if all[i-1].ID.String() >= all[i].ID.String() {
				t.Fatalf("TripsOfUser not ordered by id")
			}
		}

		ids, err := s.TripClientIDs(ctx, second.ID)
		if err != nil || ids == nil || len(ids) != 0 {
			t.Fatalf("TripClientIDs empty = %#v, %v", ids, err)
		}
		c1, c2 := uuid.New(), uuid.New()
		for _, c := range []uuid.UUID{c1, c2} {
			if err := s.InsertTripClientID(ctx, u.ID, c, second.ID); err != nil {
				t.Fatal(err)
			}
		}
		tripsWantUnique(t, s.InsertTripClientID(ctx, u.ID, c1, second.ID))
		if id, _ := s.TripIDByClientID(ctx, u.ID, c1); id == nil || *id != second.ID {
			t.Fatalf("TripIDByClientID = %v", id)
		}
		if id, _ := s.TripIDByClientID(ctx, stranger.ID, c1); id != nil {
			t.Fatalf("stranger resolves the client id")
		}
		ids, _ = s.TripClientIDs(ctx, second.ID)
		if len(ids) != 2 || ids[0].String() > ids[1].String() {
			t.Fatalf("TripClientIDs = %v", ids)
		}
	})

	var snap trips.Snapshot
	t.Run("snapshots", func(t *testing.T) {
		if got, err := s.LatestSnapshot(ctx); got != nil || err != nil {
			t.Fatalf("empty LatestSnapshot = %+v, %v", got, err)
		}
		base := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
		for i, thb := range []string{"33.0608", "33.5000"} {
			sn := trips.Snapshot{BaseCurrency: trips.USD, CreatedAt: base.Add(time.Duration(i) * 12 * time.Hour),
				Rates: map[string]trips.Decimal{"USD": trips.MustDecimal("1.0"), "THB": trips.MustDecimal(thb)}}
			if err := s.InsertSnapshot(ctx, &sn); err != nil {
				t.Fatal(err)
			}
			if sn.ID == 0 {
				t.Fatal("snapshot id not written back")
			}
			snap = sn
		}
		latest, err := s.LatestSnapshot(ctx)
		if err != nil || latest == nil || latest.ID != snap.ID || latest.Rates["THB"].String() != "33.5000" ||
			latest.Rates["USD"].String() != "1.0" || !latest.CreatedAt.Equal(snap.CreatedAt) {
			t.Fatalf("LatestSnapshot = %+v, %v", latest, err)
		}
		before, after, err := s.SnapshotsAround(ctx, base.Add(time.Hour))
		if err != nil || before == nil || after == nil || before.Rates["THB"].String() != "33.0608" || after.ID != snap.ID {
			t.Fatalf("SnapshotsAround = %+v %+v %v", before, after, err)
		}
		before, after, _ = s.SnapshotsAround(ctx, base.Add(-time.Hour))
		if before != nil || after == nil {
			t.Fatalf("before the first: %+v %+v", before, after)
		}
		before, after, _ = s.SnapshotsAround(ctx, base.Add(12*time.Hour))
		if before == nil || before.ID != snap.ID || after != nil {
			t.Fatalf("at the last: %+v %+v", before, after)
		}
	})

	t.Run("expenses", func(t *testing.T) {
		u := tripsNewUser(t, s, "spender")
		stranger := tripsNewUser(t, s, "stranger2")
		trip := tripsNewTrip(t, s, u.ID, "Asia", trips.StatusActive)
		old := tripsNewTrip(t, s, u.ID, "Old", trips.StatusCompleted)
		day := trips.NewDate(2026, 9, 17)

		add := func(tripID uuid.UUID, amount, usd, desc string, date trips.Date, category int64, source trips.Source, clientID *uuid.UUID, dup *int64) trips.Expense {
			t.Helper()
			e := trips.Expense{TripID: tripID, Amount: trips.MustDecimal(amount), AmountUSD: trips.MustDecimal(usd),
				OccurredAt: date, Description: desc, CurrencyCode: "THB", SnapshotID: snap.ID, CategoryID: category,
				CreatedAt: tripsNow(), ClientID: clientID, DuplicateOfID: dup, Source: source}
			if err := s.InsertExpense(ctx, &e); err != nil {
				t.Fatal(err)
			}
			return e
		}

		clientID := uuid.New()
		coffee := add(trip.ID, "350", "10.586550", "кофе", day, 3, trips.SourceBot, &clientID, nil)
		if coffee.ID == 0 {
			t.Fatal("expense id not written back")
		}
		got, err := s.OwnedExpense(ctx, u.ID, coffee.ID)
		if err != nil || got == nil {
			t.Fatal(got, err)
		}
		if got.Amount.String() != "350.00" || got.AmountUSD.String() != "10.586550" || got.OccurredAt != day ||
			got.CategoryName != "Кафе и рестораны" || got.TripUserID != u.ID || got.TripName != "Asia" ||
			got.ClientID == nil || *got.ClientID != clientID || got.Source != trips.SourceBot || got.DuplicateOfID != nil {
			t.Fatalf("OwnedExpense = %+v", got)
		}
		if got, _ := s.OwnedExpense(ctx, stranger.ID, coffee.ID); got != nil {
			t.Fatal("stranger owns the expense")
		}
		if byClient, _ := s.ExpenseByClientID(ctx, clientID); byClient == nil || byClient.ID != coffee.ID || byClient.TripUserID != u.ID {
			t.Fatalf("ExpenseByClientID = %+v", byClient)
		}
		if none, err := s.ExpenseByClientID(ctx, uuid.New()); none != nil || err != nil {
			t.Fatal(none, err)
		}

		rounded := add(trip.ID, "1.005", "0.030000", "round", day.AddDays(-5), 10, trips.SourceApp, nil, nil)
		if got, _ := s.OwnedExpense(ctx, u.ID, rounded.ID); got.Amount.String() != "1.01" {
			t.Errorf("1.005 stored as %s", got.Amount)
		}

		look := trips.LookAlikeQuery{TripID: trip.ID, Amount: trips.MustDecimal("350"), CurrencyCode: "THB", OccurredAt: day, Source: trips.SourceApp}
		if id, err := s.LookAlike(ctx, look); err != nil || id == nil || *id != coffee.ID {
			t.Fatalf("LookAlike = %v, %v", id, err)
		}
		look.Source = trips.SourceBot
		if id, _ := s.LookAlike(ctx, look); id != nil {
			t.Fatalf("LookAlike matched the same source")
		}
		look.Source = trips.SourceApp
		look.Amount = trips.MustDecimal("1.005")
		look.OccurredAt = day.AddDays(-5)
		if id, _ := s.LookAlike(ctx, look); id != nil {
			t.Fatalf("unrounded 1.005 matched stored 1.01 (R11 says it must not)")
		}

		flagged := add(trip.ID, "350.00", "10.586550", "кофе ещё", day, 3, trips.SourceApp, nil, &coffee.ID)
		add(trip.ID, "120", "3.629673", "обед", day, 3, trips.SourceApp, nil, nil)
		add(old.ID, "2000", "60.494604", "отель", trips.NewDate(2026, 8, 1), 1, trips.SourceApp, nil, nil)

		dupClient := trips.Expense{TripID: trip.ID, Amount: trips.MustDecimal("1"), AmountUSD: trips.MustDecimal("1"), OccurredAt: day,
			Description: "d", CurrencyCode: "THB", SnapshotID: snap.ID, CategoryID: 10, CreatedAt: tripsNow(), ClientID: &clientID, Source: trips.SourceApp}
		tripsWantUnique(t, s.InsertExpense(ctx, &dupClient))
		dupClient.ClientID = nil
		dupClient.Description = strings.Repeat("д", 251)
		tripsWantMessage(t, s.InsertExpense(ctx, &dupClient), "Поле description не может быть длиннее 250 символов")
		dupClient.Description = "d"
		dupClient.CurrencyCode = "QQQ"
		tripsWantMessage(t, s.InsertExpense(ctx, &dupClient), "Валюта QQQ не найдена")

		list, err := s.ListExpenses(ctx, trips.ExpenseFilter{UserID: u.ID})
		if err != nil || len(list) != 4 {
			t.Fatalf("ListExpenses(active) = %d, %v", len(list), err)
		}
		for i := 1; i < len(list); i++ {
			a, b := list[i-1], list[i]
			if a.OccurredAt.Before(b.OccurredAt) || (a.OccurredAt == b.OccurredAt && a.ID < b.ID) {
				t.Fatalf("ListExpenses order broken at %d", i)
			}
		}
		if list, _ := s.ListExpenses(ctx, trips.ExpenseFilter{UserID: u.ID, TripID: &old.ID}); len(list) != 1 || list[0].TripName != "Old" {
			t.Fatalf("ListExpenses(old trip) = %+v", list)
		}
		if list, _ := s.ListExpenses(ctx, trips.ExpenseFilter{UserID: stranger.ID, TripID: &trip.ID}); len(list) != 0 {
			t.Fatalf("stranger lists %d expenses", len(list))
		}
		if list, _ := s.ListExpenses(ctx, trips.ExpenseFilter{UserID: u.ID, CategoryID: tripsPtr(int64(10))}); len(list) != 1 {
			t.Fatalf("ListExpenses(category 10) = %d", len(list))
		}
		if list, _ := s.ListExpenses(ctx, trips.ExpenseFilter{UserID: stranger.ID}); list == nil || len(list) != 0 {
			t.Fatalf("no active trip: %#v", list)
		}

		total, err := s.TotalAmount(ctx, u.ID, trips.DateRange{})
		if err != nil || total.String() != "471.01" {
			t.Fatalf("TotalAmount = %s, %v", total, err)
		}
		total, _ = s.TotalAmount(ctx, u.ID, trips.DateRange{From: tripsPtr(day), To: tripsPtr(day)})
		if total.String() != "470.00" {
			t.Fatalf("TotalAmount(day) = %s", total)
		}
		total, _ = s.TotalAmount(ctx, stranger.ID, trips.DateRange{})
		if total.String() != "0" {
			t.Fatalf("TotalAmount(none) = %s", total)
		}

		rows, err := s.ReportRows(ctx, u.ID, trips.DateRange{From: tripsPtr(trips.NewDate(2026, 8, 1))})
		if err != nil || len(rows) != 4 {
			t.Fatalf("ReportRows = %+v, %v", rows, err)
		}
		builder, err := s.BuilderRows(ctx, u.ID, trips.DateRange{To: tripsPtr(trips.NewDate(2026, 8, 31))})
		if err != nil || len(builder) != 1 || builder[0].Amount.String() != "2000.00" {
			t.Fatalf("BuilderRows = %+v, %v", builder, err)
		}

		history, err := s.TripHistory(ctx, u.ID)
		if err != nil || len(history) != 2 || history[0].ID != trip.ID {
			t.Fatalf("TripHistory = %+v, %v", history, err)
		}
		if history[0].ExpenseCount != 3 || history[0].TotalUSD.String() != "14.246223" || history[1].TotalUSD.String() != "60.494604" {
			t.Fatalf("history totals = %d %s / %s", history[0].ExpenseCount, history[0].TotalUSD, history[1].TotalUSD)
		}
		if h, _ := s.TripHistory(ctx, stranger.ID); h == nil || len(h) != 0 {
			t.Fatalf("empty history = %#v", h)
		}

		flaggedList, _ := s.FlaggedExpenses(ctx, u.ID)
		if len(flaggedList) != 1 || flaggedList[0].ID != flagged.ID || *flaggedList[0].DuplicateOfID != coffee.ID {
			t.Fatalf("FlaggedExpenses = %+v", flaggedList)
		}
		if found, _ := s.ClearDuplicateFlag(ctx, stranger.ID, flagged.ID); found {
			t.Fatal("stranger cleared the flag")
		}
		if found, err := s.ClearDuplicateFlag(ctx, u.ID, flagged.ID); !found || err != nil {
			t.Fatal(found, err)
		}
		if found, _ := s.ClearDuplicateFlag(ctx, u.ID, flagged.ID); !found {
			t.Fatal("clearing an unflagged expense must still be found")
		}

		reflagged := add(trip.ID, "5", "0.151000", "x", day, 10, trips.SourceApp, nil, &coffee.ID)
		if found, _ := s.DeleteExpense(ctx, stranger.ID, coffee.ID); found {
			t.Fatal("stranger deleted the expense")
		}
		if found, err := s.DeleteExpense(ctx, u.ID, coffee.ID); !found || err != nil {
			t.Fatal(found, err)
		}
		if got, _ := s.OwnedExpense(ctx, u.ID, reflagged.ID); got.DuplicateOfID != nil {
			t.Fatal("flag survived the deleted original")
		}
		if found, _ := s.DeleteExpense(ctx, u.ID, coffee.ID); found {
			t.Fatal("second delete found a row")
		}
	})

	t.Run("photos", func(t *testing.T) {
		u := tripsNewUser(t, s, "photographer")
		stranger := tripsNewUser(t, s, "stranger3")
		trip := tripsNewTrip(t, s, u.ID, "Photos", trips.StatusActive)

		base := tripsNow()
		var ids []uuid.UUID
		for i, caption := range []*string{nil, tripsPtr("Пляж")} {
			p := trips.Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: strings.ReplaceAll(uuid.NewString(), "-", "") + ".png",
				OriginalFileName: tripsPtr("photo.png"), ContentType: "image/PNG", SizeBytes: 82, Caption: caption,
				CreatedAt: base.Add(time.Duration(i) * time.Second)}
			if err := s.InsertPhoto(ctx, &p); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, p.ID)
		}
		list, err := s.TripPhotos(ctx, u.ID, trip.ID)
		if err != nil || len(list) != 2 || list[0].ID != ids[1] || *list[0].Caption != "Пляж" || list[1].Caption != nil ||
			list[0].ContentType != "image/PNG" || !list[1].CreatedAt.Equal(base) {
			t.Fatalf("TripPhotos = %+v, %v", list, err)
		}
		if list, _ := s.TripPhotos(ctx, stranger.ID, trip.ID); list == nil || len(list) != 0 {
			t.Fatalf("stranger photos = %#v", list)
		}
		if p, _ := s.OwnedPhoto(ctx, u.ID, ids[0]); p == nil || p.SizeBytes != 82 {
			t.Fatalf("OwnedPhoto = %+v", p)
		}
		if p, _ := s.OwnedPhoto(ctx, stranger.ID, ids[0]); p != nil {
			t.Fatal("stranger owns the photo")
		}
		long := trips.Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: "a.png", ContentType: "image/png", Caption: tripsPtr(strings.Repeat("п", 501)), CreatedAt: base}
		tripsWantMessage(t, s.InsertPhoto(ctx, &long), "Поле caption не может быть длиннее 500 символов")
		if err := s.DeletePhoto(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}
		if p, _ := s.OwnedPhoto(ctx, u.ID, ids[0]); p != nil {
			t.Fatal("photo not deleted")
		}
	})

	t.Run("delete trip", func(t *testing.T) {
		u := tripsNewUser(t, s, "deleter")
		stranger := tripsNewUser(t, s, "stranger4")
		trip := tripsNewTrip(t, s, u.ID, "Doomed", trips.StatusActive)
		keep := tripsNewTrip(t, s, u.ID, "Keeper", trips.StatusCompleted)

		day := trips.NewDate(2026, 9, 17)
		expense := trips.Expense{TripID: trip.ID, Amount: trips.MustDecimal("350"), AmountUSD: trips.MustDecimal("10.5"),
			OccurredAt: day, Description: "кофе", CurrencyCode: "THB", SnapshotID: snap.ID, CategoryID: 3,
			CreatedAt: tripsNow(), Source: trips.SourceApp}
		if err := s.InsertExpense(ctx, &expense); err != nil {
			t.Fatal(err)
		}
		survivor := trips.Expense{TripID: keep.ID, Amount: trips.MustDecimal("10"), AmountUSD: trips.MustDecimal("0.3"),
			OccurredAt: day, Description: "чай", CurrencyCode: "THB", SnapshotID: snap.ID, CategoryID: 3,
			CreatedAt: tripsNow(), Source: trips.SourceApp}
		if err := s.InsertExpense(ctx, &survivor); err != nil {
			t.Fatal(err)
		}
		photo := trips.Photo{ID: uuid.New(), TripID: trip.ID, StoredFileName: "gone.png", ContentType: "image/png",
			SizeBytes: 10, CreatedAt: tripsNow()}
		if err := s.InsertPhoto(ctx, &photo); err != nil {
			t.Fatal(err)
		}
		clientID := uuid.New()
		if err := s.InsertTripClientID(ctx, u.ID, clientID, trip.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := s.DeleteTrip(ctx, u.ID, trip.ID); err == nil {
			t.Fatal("a trip with expenses was deleted despite the RESTRICT")
		} else if ie, ok := trips.AsIntegrityError(err); !ok || ie.SQLState != "23503" {
			t.Fatalf("error = %v, want a foreign key violation", err)
		}

		if found, err := s.DeleteTrip(ctx, stranger.ID, trip.ID); err != nil || found {
			t.Fatalf("stranger delete: found %v, err %v", found, err)
		}

		if n, err := s.DeleteExpensesOfTrip(ctx, trip.ID); err != nil || n != 1 {
			t.Fatalf("DeleteExpensesOfTrip = %d, %v", n, err)
		}
		if n, err := s.DeleteTripPhotos(ctx, trip.ID); err != nil || n != 1 {
			t.Fatalf("DeleteTripPhotos = %d, %v", n, err)
		}
		if found, err := s.DeleteTrip(ctx, u.ID, trip.ID); err != nil || !found {
			t.Fatalf("DeleteTrip = %v, %v", found, err)
		}

		if found, err := s.DeleteTrip(ctx, u.ID, trip.ID); err != nil || found {
			t.Fatalf("second DeleteTrip = %v, %v", found, err)
		}

		if got, _ := s.OwnedTrip(ctx, u.ID, trip.ID); got != nil {
			t.Error("the trip row survived")
		}
		if got, _ := s.OwnedPhoto(ctx, u.ID, photo.ID); got != nil {
			t.Error("the photo row survived")
		}
		if got, _ := s.TripIDByClientID(ctx, u.ID, clientID); got != nil {
			t.Error("the client id survived the trip")
		}
		if got, _ := s.OwnedExpense(ctx, u.ID, survivor.ID); got == nil {
			t.Error("an expense of another trip was deleted")
		}
	})

	t.Run("sync", func(t *testing.T) {
		u := tripsNewUser(t, s, "syncer")
		target := tripsNewUser(t, s, "survivor")
		trip := tripsNewTrip(t, s, u.ID, "Sync", trips.StatusActive)
		op := uuid.New()

		if r, err := s.SyncResult(ctx, u.ID, op); r != nil || err != nil {
			t.Fatal(r, err)
		}
		result := json.RawMessage(`{"op_id":"` + op.String() + `","status":"ok","expense":{"amount":350.00}}`)
		if err := s.SaveSyncResult(ctx, u.ID, op, result, tripsNow()); err != nil {
			t.Fatal(err)
		}
		stored, err := s.SyncResult(ctx, u.ID, op)
		if err != nil || !json.Valid(stored) || !strings.Contains(string(stored), `"amount": 350.00`) {
			t.Fatalf("SyncResult = %s, %v", stored, err)
		}

		err = s.InTx(ctx, func(tx trips.Store) error {
			tripsWantUnique(t, tx.SaveSyncResult(ctx, u.ID, op, result, tripsNow()))
			return tx.InsertTripClientID(ctx, u.ID, uuid.New(), trip.ID)
		})
		if err != nil {
			t.Fatalf("tx after sync unique violation: %v", err)
		}

		if err := s.ReassignSyncData(ctx, u.ID, target.ID); err != nil {
			t.Fatal(err)
		}
		if r, _ := s.SyncResult(ctx, target.ID, op); r == nil {
			t.Fatal("sync result not moved")
		}
		if r, _ := s.SyncResult(ctx, u.ID, op); r != nil {
			t.Fatal("sync result still on the source")
		}
		ids, _ := s.TripClientIDs(ctx, trip.ID)
		if len(ids) != 1 {
			t.Fatalf("client ids = %v", ids)
		}
		if id, _ := s.TripIDByClientID(ctx, target.ID, ids[0]); id == nil {
			t.Fatal("client id not moved")
		}
	})
}
