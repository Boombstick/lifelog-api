package trips

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

type m4MergeFixture struct {
	store    *m4Store
	cache    *MemoryIdentityCache
	svc      *MergeService
	identity *IdentityService
	from     uuid.UUID
	into     uuid.UUID
}

func m4NewMerge() *m4MergeFixture {
	store := m4NewStore()
	cache := NewMemoryIdentityCache(5 * time.Minute)
	clock := fixedClock{testNow}
	return &m4MergeFixture{
		store:    store,
		cache:    cache,
		svc:      NewMergeService(store, cache, clock),
		identity: NewIdentityService(store, cache, clock),
		from:     uuid.New(),
		into:     uuid.New(),
	}
}

func TestMergeNoOps(t *testing.T) {
	f := m4NewMerge()
	m4NoErr(t, f.svc.Merge(bg, MergeRequest{}))
	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.from}))

	target := f.store.m4PutUser(User{AuthUserID: &f.into})
	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))
	if _, ok := f.store.m4User(target.ID); !ok {
		t.Fatal("a merge without a source must not touch the target")
	}
}

func TestMergeWithoutTargetMovesAuthID(t *testing.T) {
	f := m4NewMerge()
	source := f.store.m4PutUser(User{Username: "src", AuthUserID: &f.from})
	f.cache.PutByAuth(f.from, source)

	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))

	stored, ok := f.store.m4User(source.ID)
	if !ok {
		t.Fatal("source must survive when there is no target")
	}
	m4Expect(t, "auth id", *stored.AuthUserID, f.into)
	if _, ok := f.cache.ByAuth(f.from); ok {
		t.Fatal("source cache entry survived")
	}
}

func TestMergeBothSidesHaveTrips(t *testing.T) {
	f := m4NewMerge()
	source := f.store.m4PutUser(User{AuthUserID: &f.from})
	target := f.store.m4PutUser(User{AuthUserID: &f.into})
	start := NewDate(2026, 9, 1)

	sourceActive := f.store.putTrip(Trip{UserID: source.ID, Name: "src active", StartDate: &start})
	sourceDone := f.store.putTrip(Trip{UserID: source.ID, Name: "src done", Status: StatusCompleted, StartDate: &start, EndDate: &start})
	targetActive := f.store.putTrip(Trip{UserID: target.ID, Name: "dst active", StartDate: &start})
	expense := f.store.putExpense(Expense{TripID: sourceActive.ID, Amount: MustDecimal("1"), CurrencyCode: "USD", Description: "x"})

	clientID, opID := uuid.New(), uuid.New()
	m4NoErr(t, f.store.memStore.InsertTripClientID(bg, source.ID, clientID, sourceActive.ID))
	m4NoErr(t, f.store.memStore.SaveSyncResult(bg, source.ID, opID, json.RawMessage(`{"status":"ok"}`), testNow))

	f.cache.PutByAuth(f.from, source)
	f.cache.PutByAuth(f.into, target)

	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))

	if _, ok := f.store.m4User(source.ID); ok {
		t.Fatal("source user was not deleted")
	}
	moved := f.store.trip(sourceActive.ID)
	m4Expect(t, "moved active owner", moved.UserID, target.ID)
	m4Expect(t, "ended because target has an active trip", moved.Status, StatusCompleted)
	m4Expect(t, "ended today", *moved.EndDate, DateOf(testNow))
	m4Expect(t, "updated at", moved.UpdatedAt, testNow)
	m4Expect(t, "completed trip owner", f.store.trip(sourceDone.ID).UserID, target.ID)
	m4Expect(t, "target trip untouched", f.store.trip(targetActive.ID).Status, StatusActive)

	resolved, err := f.store.TripIDByClientID(bg, target.ID, clientID)
	m4NoErr(t, err)
	if resolved == nil || *resolved != sourceActive.ID {
		t.Fatal("trip client id was not reassigned")
	}
	stored, err := f.store.SyncResult(bg, target.ID, opID)
	m4NoErr(t, err)
	if stored == nil {
		t.Fatal("sync operation was not reassigned")
	}
	owned, err := f.store.OwnedExpense(bg, target.ID, expense.ID)
	m4NoErr(t, err)
	if owned == nil {
		t.Fatal("expense did not follow its trip")
	}

	if _, ok := f.cache.ByAuth(f.from); ok {
		t.Fatal("source cache entry survived")
	}
	if _, ok := f.cache.ByAuth(f.into); ok {
		t.Fatal("target cache entry survived")
	}
}

func TestMergeKeepsOneActiveTripWhenTargetHasNone(t *testing.T) {
	f := m4NewMerge()
	source := f.store.m4PutUser(User{AuthUserID: &f.from})
	target := f.store.m4PutUser(User{AuthUserID: &f.into})
	start := NewDate(2026, 9, 1)
	f.store.allowTwoActive = true
	first := f.store.putTrip(Trip{ID: uuid.MustParse("00000000-0000-7000-8000-000000000001"), UserID: source.ID, Name: "a", StartDate: &start})
	second := f.store.putTrip(Trip{ID: uuid.MustParse("00000000-0000-7000-8000-000000000002"), UserID: source.ID, Name: "b", StartDate: &start})
	f.store.allowTwoActive = false

	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))

	m4Expect(t, "first stays active", f.store.trip(first.ID).Status, StatusActive)
	m4Expect(t, "second is ended", f.store.trip(second.ID).Status, StatusCompleted)
	m4Expect(t, "first owner", f.store.trip(first.ID).UserID, target.ID)
	m4Expect(t, "second owner", f.store.trip(second.ID).UserID, target.ID)
}

func TestMergeMovesTelegramLink(t *testing.T) {
	const tg int64 = 555
	f := m4NewMerge()
	source := f.store.m4PutUser(User{Username: "bot", AuthUserID: &f.from, TelegramID: ptr(tg)})
	target := f.store.m4PutUser(User{Username: "app", AuthUserID: &f.into})

	before, err := f.identity.ByTelegram(bg, tg, f.from, "bot")
	m4NoErr(t, err)
	m4Expect(t, "resolved before", before.UserID, source.ID)

	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))

	stored, _ := f.store.m4User(target.ID)
	if stored.TelegramID == nil || *stored.TelegramID != tg {
		t.Fatalf("telegram id was not moved to the target: %v", stored.TelegramID)
	}
	if _, ok := f.cache.ByTelegram(tg, f.from); ok {
		t.Fatal("stale telegram cache entry survived")
	}

	after, err := f.identity.ByTelegram(bg, tg, f.into, "bot")
	m4NoErr(t, err)
	m4Expect(t, "resolved after", after.UserID, target.ID)
	m4Expect(t, "users", len(f.store.users), 1)
}

func TestMergeKeepsTargetTelegramLink(t *testing.T) {
	f := m4NewMerge()
	f.store.m4PutUser(User{AuthUserID: &f.from, TelegramID: ptr(int64(1))})
	target := f.store.m4PutUser(User{AuthUserID: &f.into, TelegramID: ptr(int64(2))})

	m4NoErr(t, f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}))

	stored, _ := f.store.m4User(target.ID)
	m4Expect(t, "target telegram", *stored.TelegramID, int64(2))
	m4Expect(t, "users", len(f.store.users), 1)
}

func TestMergeFailureRollsBackAndKeepsCache(t *testing.T) {
	f := m4NewMerge()
	source := f.store.m4PutUser(User{AuthUserID: &f.from, TelegramID: ptr(int64(9))})
	target := f.store.m4PutUser(User{AuthUserID: &f.into})
	trip := f.store.putTrip(Trip{UserID: source.ID, Name: "t"})
	f.cache.PutByAuth(f.from, source)
	f.store.m4FailDeleteUser = m4Boom

	if err := f.svc.Merge(bg, MergeRequest{From: f.from, Into: f.into}); err != m4Boom {
		t.Fatalf("got %v, want the store error", err)
	}
	m4Expect(t, "trip owner", f.store.trip(trip.ID).UserID, source.ID)
	stored, _ := f.store.m4User(target.ID)
	if stored.TelegramID != nil {
		t.Fatal("telegram move was not rolled back")
	}
	if _, ok := f.cache.ByAuth(f.from); !ok {
		t.Fatal("cache must not be invalidated when nothing was committed")
	}
}
