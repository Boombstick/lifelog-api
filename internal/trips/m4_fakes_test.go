package trips

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type m4Store struct {
	*memStore

	txDepth int

	m4BeforeInsertUser func()
	m4FailInsertUser   error

	m4BeforeSaveSync     func(userID, opID uuid.UUID) error
	m4FailInsertClientID error
	m4FailDeleteUser     error

	m4CommittedElsewhere map[[2]uuid.UUID]json.RawMessage

	m4CommitLater int

	m4AuthReads     int
	m4TelegramReads int
}

var _ Store = (*m4Store)(nil)

func m4NewStore() *m4Store { return &m4Store{memStore: seededStore()} }

type m4Snapshot struct {
	users          map[uuid.UUID]User
	categories     map[int64]Category
	trips          map[uuid.UUID]Trip
	expenses       map[int64]Expense
	photos         map[uuid.UUID]Photo
	clientIDs      map[[2]uuid.UUID]uuid.UUID
	syncs          map[[2]uuid.UUID]json.RawMessage
	nextExpenseID  int64
	nextCategoryID int64
}

func m4CopyMap[K comparable, V any](m map[K]V) map[K]V {
	c := make(map[K]V, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func (s *m4Store) snapshot() m4Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return m4Snapshot{
		users:          m4CopyMap(s.users),
		categories:     m4CopyMap(s.categories),
		trips:          m4CopyMap(s.trips),
		expenses:       m4CopyMap(s.expenses),
		photos:         m4CopyMap(s.photos),
		clientIDs:      m4CopyMap(s.clientIDs),
		syncs:          m4CopyMap(s.syncs),
		nextExpenseID:  s.nextExpenseID,
		nextCategoryID: s.nextCategoryID,
	}
}

func (s *m4Store) restore(snap m4Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users = snap.users
	s.categories = snap.categories
	s.trips = snap.trips
	s.expenses = snap.expenses
	s.photos = snap.photos
	s.clientIDs = snap.clientIDs
	s.syncs = snap.syncs
	s.nextExpenseID = snap.nextExpenseID
	s.nextCategoryID = snap.nextCategoryID
}

func (s *m4Store) InTx(ctx context.Context, fn func(tx Store) error) error {
	if s.txDepth > 0 {
		return fn(s)
	}
	snap := s.snapshot()
	s.txDepth++
	err := fn(s)
	s.txDepth--
	if err != nil {
		s.restore(snap)
	}
	return err
}

func m4Unique(msg string) error {
	return &IntegrityError{SQLState: "23505", Message: msg}
}

func (s *m4Store) UserByAuthID(ctx context.Context, authUserID uuid.UUID) (*User, error) {
	s.m4AuthReads++
	return s.memStore.UserByAuthID(ctx, authUserID)
}

func (s *m4Store) UserByTelegramID(ctx context.Context, telegramID int64) (*User, error) {
	s.m4TelegramReads++
	return s.memStore.UserByTelegramID(ctx, telegramID)
}

func (s *m4Store) uniqueClash(self uuid.UUID, authUserID *uuid.UUID, telegramID *int64) error {
	for _, u := range s.users {
		if u.ID == self {
			continue
		}
		if authUserID != nil && u.AuthUserID != nil && *u.AuthUserID == *authUserID {
			return m4Unique("duplicate key value violates unique constraint \"ix_users_auth_user_id\"")
		}
		if telegramID != nil && u.TelegramID != nil && *u.TelegramID == *telegramID {
			return m4Unique("duplicate key value violates unique constraint \"ix_users_telegram_id\"")
		}
	}
	return nil
}

func (s *m4Store) InsertUser(ctx context.Context, user *User) error {
	if s.m4BeforeInsertUser != nil {
		hook := s.m4BeforeInsertUser
		s.m4BeforeInsertUser = nil
		hook()
	}
	if s.m4FailInsertUser != nil {
		return s.m4FailInsertUser
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[user.ID]; ok {
		return m4Unique("duplicate key value violates unique constraint \"pk_users\"")
	}
	if err := s.uniqueClash(user.ID, user.AuthUserID, user.TelegramID); err != nil {
		return err
	}
	s.users[user.ID] = *user
	return nil
}

func (s *m4Store) SetUserAuthID(ctx context.Context, userID, authUserID uuid.UUID) error {
	s.mu.Lock()
	err := s.uniqueClash(userID, &authUserID, nil)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.memStore.SetUserAuthID(ctx, userID, authUserID)
}

func (s *m4Store) SetUserTelegramID(ctx context.Context, userID uuid.UUID, telegramID *int64) error {
	s.mu.Lock()
	err := s.uniqueClash(userID, nil, telegramID)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.memStore.SetUserTelegramID(ctx, userID, telegramID)
}

func (s *m4Store) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	if s.m4FailDeleteUser != nil {
		return s.m4FailDeleteUser
	}
	return s.memStore.DeleteUser(ctx, userID)
}

func (s *m4Store) InsertTripClientID(ctx context.Context, userID, clientID, tripID uuid.UUID) error {
	if s.m4FailInsertClientID != nil {
		return s.m4FailInsertClientID
	}
	return s.memStore.InsertTripClientID(ctx, userID, clientID, tripID)
}

func (s *m4Store) SaveSyncResult(ctx context.Context, userID, opID uuid.UUID, result json.RawMessage, processedAt time.Time) error {
	if s.m4BeforeSaveSync != nil {
		return s.m4BeforeSaveSync(userID, opID)
	}
	s.mu.Lock()
	_, exists := s.syncs[[2]uuid.UUID{userID, opID}]
	s.mu.Unlock()
	if exists {
		return m4Unique("duplicate key value violates unique constraint \"pk_sync_operations\"")
	}
	return s.memStore.SaveSyncResult(ctx, userID, opID, result, processedAt)
}

func (s *m4Store) ReassignSyncData(ctx context.Context, fromUserID, toUserID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, tripID := range s.clientIDs {
		if key[0] == fromUserID {
			delete(s.clientIDs, key)
			s.clientIDs[[2]uuid.UUID{toUserID, key[1]}] = tripID
		}
	}
	for key, raw := range s.syncs {
		if key[0] == fromUserID {
			delete(s.syncs, key)
			s.syncs[[2]uuid.UUID{toUserID, key[1]}] = raw
		}
	}
	return nil
}

func (s *m4Store) m4PutUser(u User) User {
	if u.ID == uuid.Nil {
		u.ID = uuid.Must(uuid.NewV7())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[u.ID] = u
	return u
}

func (s *m4Store) m4User(id uuid.UUID) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	return u, ok
}

func (s *m4Store) m4TripCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.trips)
}

func (s *m4Store) m4SyncCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.syncs)
}

type m4Clock struct{ now time.Time }

func (c *m4Clock) Now() time.Time { return c.now }

func m4Expect[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
}

func m4NoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

var m4Boom = errors.New("m4: connection reset")

func (s *m4Store) SyncResult(ctx context.Context, userID, opID uuid.UUID) (json.RawMessage, error) {
	if raw, ok := s.m4CommittedElsewhere[[2]uuid.UUID{userID, opID}]; ok {
		if s.m4CommitLater == 0 {
			return raw, nil
		}
		s.m4CommitLater--
	}
	return s.memStore.SyncResult(ctx, userID, opID)
}
