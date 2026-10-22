package trips

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func m4NewIdentity() (*m4Store, *IdentityService, *MemoryIdentityCache) {
	store := m4NewStore()
	cache := NewMemoryIdentityCache(5 * time.Minute)
	return store, NewIdentityService(store, cache, fixedClock{testNow}), cache
}

func TestIdentityByAuthUserCreatesAndCaches(t *testing.T) {
	store, svc, _ := m4NewIdentity()
	auth := uuid.New()

	actor, err := svc.ByAuthUser(bg, auth, "Женя")
	m4NoErr(t, err)
	m4Expect(t, "mode", actor.Mode, ModeApp)
	m4Expect(t, "auth", actor.AuthUserID, auth)
	m4Expect(t, "username", actor.Username, "Женя")
	if actor.TelegramID != nil {
		t.Fatalf("telegram id: got %v, want nil", *actor.TelegramID)
	}

	stored, ok := store.m4User(actor.UserID)
	if !ok {
		t.Fatal("user was not inserted")
	}
	m4Expect(t, "uuid version", stored.ID.Version(), uuid.Version(7))
	m4Expect(t, "created at", stored.CreatedAt, testNow)
	m4Expect(t, "stored auth", *stored.AuthUserID, auth)

	reads := store.m4AuthReads
	again, err := svc.ByAuthUser(bg, auth, "другое имя")
	m4NoErr(t, err)
	m4Expect(t, "cached user", again.UserID, actor.UserID)
	m4Expect(t, "reads after cache hit", store.m4AuthReads, reads)
}

func TestIdentityByAuthUserFindsExisting(t *testing.T) {
	store, svc, _ := m4NewIdentity()
	auth := uuid.New()
	tg := int64(42)
	existing := store.m4PutUser(User{Username: "old", AuthUserID: &auth, TelegramID: &tg})

	actor, err := svc.ByAuthUser(bg, auth, "new")
	m4NoErr(t, err)
	m4Expect(t, "user", actor.UserID, existing.ID)
	m4Expect(t, "username is never updated", actor.Username, "old")
	m4Expect(t, "telegram id", *actor.TelegramID, tg)
}

func TestIdentityByAuthUserRace(t *testing.T) {
	store, svc, _ := m4NewIdentity()
	auth := uuid.New()
	var winner User
	store.m4BeforeInsertUser = func() {
		winner = store.m4PutUser(User{Username: "winner", AuthUserID: &auth})
	}

	actor, err := svc.ByAuthUser(bg, auth, "loser")
	m4NoErr(t, err)
	m4Expect(t, "user", actor.UserID, winner.ID)
	m4Expect(t, "users", len(store.users), 1)
}

func TestIdentityByAuthUserInsertFailure(t *testing.T) {
	store, svc, _ := m4NewIdentity()
	store.m4FailInsertUser = m4Boom
	if _, err := svc.ByAuthUser(bg, uuid.New(), "x"); err != m4Boom {
		t.Fatalf("got %v, want the store error", err)
	}
}

func TestIdentityByAuthUserTruncatesLongName(t *testing.T) {
	store, svc, _ := m4NewIdentity()
	name := ""
	for range 120 {
		name += "я"
	}
	actor, err := svc.ByAuthUser(bg, uuid.New(), name)
	m4NoErr(t, err)
	stored, _ := store.m4User(actor.UserID)
	m4Expect(t, "rune count", len([]rune(stored.Username)), MaxUsernameLen)
}

func TestIdentityByTelegram(t *testing.T) {
	const tg int64 = 777

	t.Run("links a NULL auth id", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		existing := store.m4PutUser(User{Username: "bot", TelegramID: ptr(tg)})

		actor, err := svc.ByTelegram(bg, tg, auth, "ignored")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, existing.ID)
		m4Expect(t, "mode", actor.Mode, ModeBot)
		stored, _ := store.m4User(existing.ID)
		m4Expect(t, "linked auth", *stored.AuthUserID, auth)
	})

	t.Run("keeps a different auth id", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		other := uuid.New()
		existing := store.m4PutUser(User{Username: "bot", TelegramID: ptr(tg), AuthUserID: &other})

		actor, err := svc.ByTelegram(bg, tg, uuid.New(), "ignored")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, existing.ID)
		stored, _ := store.m4User(existing.ID)
		m4Expect(t, "auth untouched", *stored.AuthUserID, other)
	})

	t.Run("creates a user with both ids", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()

		actor, err := svc.ByTelegram(bg, tg, auth, "tg-777")
		m4NoErr(t, err)
		stored, ok := store.m4User(actor.UserID)
		if !ok {
			t.Fatal("user was not inserted")
		}
		m4Expect(t, "telegram", *stored.TelegramID, tg)
		m4Expect(t, "auth", *stored.AuthUserID, auth)
		m4Expect(t, "username", stored.Username, "tg-777")
		m4Expect(t, "actor telegram", *actor.TelegramID, tg)
	})

	t.Run("race on telegram id re-reads and links", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		var winner User
		store.m4BeforeInsertUser = func() {
			winner = store.m4PutUser(User{Username: "winner", TelegramID: ptr(tg)})
		}

		actor, err := svc.ByTelegram(bg, tg, auth, "loser")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, winner.ID)
		stored, _ := store.m4User(winner.ID)
		m4Expect(t, "linked auth", *stored.AuthUserID, auth)
		m4Expect(t, "users", len(store.users), 1)
	})

	t.Run("clash on auth id attaches telegram to the app user", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		app := store.m4PutUser(User{Username: "app", AuthUserID: &auth})

		actor, err := svc.ByTelegram(bg, tg, auth, "bot")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, app.ID)
		m4Expect(t, "actor telegram", *actor.TelegramID, tg)
		stored, _ := store.m4User(app.ID)
		m4Expect(t, "stored telegram", *stored.TelegramID, tg)
		m4Expect(t, "users", len(store.users), 1)
	})

	t.Run("clash on auth id keeps another telegram link", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		app := store.m4PutUser(User{Username: "app", AuthUserID: &auth, TelegramID: ptr(int64(1))})

		actor, err := svc.ByTelegram(bg, tg, auth, "bot")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, app.ID)
		stored, _ := store.m4User(app.ID)
		m4Expect(t, "stored telegram", *stored.TelegramID, int64(1))
	})

	t.Run("linking a NULL auth id held by another user leaves it unlinked", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		store.m4PutUser(User{Username: "app", AuthUserID: &auth})
		bot := store.m4PutUser(User{Username: "bot", TelegramID: ptr(tg)})

		actor, err := svc.ByTelegram(bg, tg, auth, "bot")
		m4NoErr(t, err)
		m4Expect(t, "user", actor.UserID, bot.ID)
		stored, _ := store.m4User(bot.ID)
		if stored.AuthUserID != nil {
			t.Fatalf("auth id: got %v, want nil", *stored.AuthUserID)
		}
	})

	t.Run("cached by telegram and auth id", func(t *testing.T) {
		store, svc, _ := m4NewIdentity()
		auth := uuid.New()
		first, err := svc.ByTelegram(bg, tg, auth, "bot")
		m4NoErr(t, err)
		reads := store.m4TelegramReads
		second, err := svc.ByTelegram(bg, tg, auth, "bot")
		m4NoErr(t, err)
		m4Expect(t, "user", second.UserID, first.UserID)
		m4Expect(t, "reads", store.m4TelegramReads, reads)
	})
}

func TestIdentityIdentify(t *testing.T) {
	_, svc, _ := m4NewIdentity()
	id := uuid.New()
	got := svc.Identify(Actor{UserID: id, TelegramID: ptr(int64(5)), Username: "tg", Mode: ModeBot})
	m4Expect(t, "user", got.UserID, id)
	m4Expect(t, "telegram", *got.TelegramID, int64(5))
	m4Expect(t, "username", got.Username, "tg")
}

func TestDisplayNameForAuth(t *testing.T) {
	id := uuid.MustParse("01a0ab58-200b-7655-8000-000000000000")
	cases := []struct {
		name, displayName, want string
	}{
		{"Женя", "zhenya", "Женя"},
		{"", "zhenya", "zhenya"},
		{"", "", "user-01a0ab58"},
	}
	for _, c := range cases {
		m4Expect(t, c.want, DisplayNameForAuth(c.name, c.displayName, id), c.want)
	}
}

func TestMemoryIdentityCache(t *testing.T) {
	clock := &m4Clock{now: testNow}
	cache := NewMemoryIdentityCache(5 * time.Minute)
	cache.now = clock.Now

	alice := User{ID: uuid.New(), Username: "alice"}
	bob := User{ID: uuid.New(), Username: "bob"}
	aliceAuth, bobAuth := uuid.New(), uuid.New()
	cache.PutByAuth(aliceAuth, alice)
	cache.PutByTelegram(1, aliceAuth, alice)
	cache.PutByAuth(bobAuth, bob)

	if got, ok := cache.ByAuth(aliceAuth); !ok || got.ID != alice.ID {
		t.Fatal("alice by auth should be cached")
	}
	if _, ok := cache.ByTelegram(1, uuid.New()); ok {
		t.Fatal("the telegram key includes the auth id")
	}

	t.Run("invalidate drops every key of one user", func(t *testing.T) {
		cache.InvalidateUser(alice.ID)
		if _, ok := cache.ByAuth(aliceAuth); ok {
			t.Fatal("alice by auth survived invalidation")
		}
		if _, ok := cache.ByTelegram(1, aliceAuth); ok {
			t.Fatal("alice by telegram survived invalidation")
		}
		if _, ok := cache.ByAuth(bobAuth); !ok {
			t.Fatal("bob must stay cached")
		}
	})

	t.Run("an invalidated user is not re-cached within the TTL", func(t *testing.T) {
		cache.PutByAuth(aliceAuth, alice)
		if _, ok := cache.ByAuth(aliceAuth); ok {
			t.Fatal("stale put after invalidation was accepted")
		}
		clock.now = clock.now.Add(5 * time.Minute)
		cache.PutByAuth(aliceAuth, alice)
		if _, ok := cache.ByAuth(aliceAuth); !ok {
			t.Fatal("put after the quarantine was refused")
		}
	})

	t.Run("entries expire after the TTL", func(t *testing.T) {
		clock.now = clock.now.Add(4 * time.Minute)
		if _, ok := cache.ByAuth(aliceAuth); !ok {
			t.Fatal("expired too early")
		}
		clock.now = clock.now.Add(time.Minute)
		if _, ok := cache.ByAuth(aliceAuth); ok {
			t.Fatal("entry outlived its TTL")
		}
		if _, ok := cache.ByAuth(bobAuth); ok {
			t.Fatal("bob outlived his TTL")
		}
	})
}

func TestMemoryIdentityCacheIsBounded(t *testing.T) {
	cache := NewMemoryIdentityCache(5 * time.Minute)
	cache.max = 3
	var auths []uuid.UUID
	for i := range 5 {
		auth := uuid.New()
		auths = append(auths, auth)
		user := User{ID: uuid.New()}
		cache.PutByAuth(auth, user)
		cache.PutByTelegram(int64(i), auth, user)
	}
	if len(cache.byAuth) != 3 || len(cache.byTelegram) != 3 {
		t.Fatalf("sizes %d/%d, want 3/3", len(cache.byAuth), len(cache.byTelegram))
	}
	if _, ok := cache.ByAuth(auths[4]); !ok {
		t.Fatal("the newest entry was evicted")
	}
}
