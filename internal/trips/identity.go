package trips

import (
	"context"
	"strings"
	"sync"
	"time"

	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

type IdentityService struct {
	store Store
	cache IdentityCache
	clock Clock
}

func NewIdentityService(store Store, cache IdentityCache, clock Clock) *IdentityService {
	return &IdentityService{store: store, cache: cache, clock: clock}
}

func (s *IdentityService) ByAuthUser(ctx context.Context, authUserID uuid.UUID, displayName string) (Actor, error) {
	if user, ok := s.cache.ByAuth(authUserID); ok {
		return actorOf(user, authUserID, ModeApp), nil
	}
	user, err := s.userByAuth(ctx, authUserID, displayName)
	if err != nil {
		return Actor{}, err
	}
	s.cache.PutByAuth(authUserID, *user)
	return actorOf(*user, authUserID, ModeApp), nil
}

func (s *IdentityService) userByAuth(ctx context.Context, authUserID uuid.UUID, displayName string) (*User, error) {
	user, err := s.store.UserByAuthID(ctx, authUserID)
	if err != nil || user != nil {
		return user, err
	}
	authID := authUserID
	user, err = s.newUser(nil, &authID, displayName)
	if err != nil {
		return nil, err
	}
	insertErr := s.store.InsertUser(ctx, user)
	if insertErr == nil {
		return user, nil
	}

	if !isUniqueViolation(insertErr) {
		return nil, insertErr
	}
	existing, err := s.store.UserByAuthID(ctx, authUserID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, insertErr
	}
	return existing, nil
}

func (s *IdentityService) ByTelegram(ctx context.Context, telegramID int64, authUserID uuid.UUID, displayName string) (Actor, error) {
	if user, ok := s.cache.ByTelegram(telegramID, authUserID); ok {
		return actorOf(user, authUserID, ModeBot), nil
	}
	user, err := s.userByTelegram(ctx, telegramID, authUserID, displayName)
	if err != nil {
		return Actor{}, err
	}
	s.cache.PutByTelegram(telegramID, authUserID, *user)
	return actorOf(*user, authUserID, ModeBot), nil
}

func (s *IdentityService) userByTelegram(ctx context.Context, telegramID int64, authUserID uuid.UUID, displayName string) (*User, error) {
	user, err := s.store.UserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return s.linkAuth(ctx, user, authUserID)
	}

	tg := telegramID
	authID := authUserID
	user, err = s.newUser(&tg, &authID, displayName)
	if err != nil {
		return nil, err
	}
	insertErr := s.store.InsertUser(ctx, user)
	if insertErr == nil {
		return user, nil
	}
	if !isUniqueViolation(insertErr) {
		return nil, insertErr
	}

	existing, err := s.store.UserByTelegramID(ctx, telegramID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return s.linkAuth(ctx, existing, authUserID)
	}

	byAuth, err := s.store.UserByAuthID(ctx, authUserID)
	if err != nil {
		return nil, err
	}
	if byAuth == nil {
		return nil, insertErr
	}
	if byAuth.TelegramID != nil {
		return byAuth, nil
	}
	if err := s.store.SetUserTelegramID(ctx, byAuth.ID, &tg); err != nil {
		if !isUniqueViolation(err) {
			return nil, err
		}

		existing, readErr := s.store.UserByTelegramID(ctx, telegramID)
		if readErr != nil {
			return nil, readErr
		}
		if existing == nil {
			return nil, err
		}
		return existing, nil
	}
	byAuth.TelegramID = &tg
	return byAuth, nil
}

func (s *IdentityService) linkAuth(ctx context.Context, user *User, authUserID uuid.UUID) (*User, error) {
	if user.AuthUserID != nil {
		return user, nil
	}
	if err := s.store.SetUserAuthID(ctx, user.ID, authUserID); err != nil {
		if isUniqueViolation(err) {
			return user, nil
		}
		return nil, err
	}
	authID := authUserID
	user.AuthUserID = &authID
	return user, nil
}

func (s *IdentityService) newUser(telegramID *int64, authUserID *uuid.UUID, displayName string) (*User, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return &User{
		ID:         id,
		TelegramID: telegramID,

		Username:   textutil.Truncate(displayName, MaxUsernameLen),
		CreatedAt:  s.clock.Now(),
		AuthUserID: authUserID,
	}, nil
}

func (s *IdentityService) Identify(actor Actor) BotIdentifyResponse {
	return BotIdentifyResponse{
		UserID:     actor.UserID,
		TelegramID: actor.TelegramID,
		Username:   actor.Username,
	}
}

func DisplayNameForAuth(name, displayName string, authUserID uuid.UUID) string {
	if name != "" {
		return name
	}
	if displayName != "" {
		return displayName
	}
	return "user-" + strings.ReplaceAll(authUserID.String(), "-", "")[:8]
}

func actorOf(user User, authUserID uuid.UUID, mode ActorMode) Actor {
	return Actor{
		UserID:     user.ID,
		AuthUserID: authUserID,
		TelegramID: user.TelegramID,
		Username:   user.Username,
		Mode:       mode,
	}
}

func isUniqueViolation(err error) bool {
	integrity, ok := AsIntegrityError(err)
	return ok && integrity.IsUniqueViolation()
}

type MemoryIdentityCache struct {
	ttl time.Duration
	max int
	now func() time.Time

	mu          sync.Mutex
	byAuth      map[uuid.UUID]identityEntry
	byTelegram  map[telegramKey]identityEntry
	invalidated map[uuid.UUID]time.Time
	lastSweep   time.Time
}

type telegramKey struct {
	telegramID int64
	authUserID uuid.UUID
}

type identityEntry struct {
	user    User
	expires time.Time
}

func NewMemoryIdentityCache(ttl time.Duration) *MemoryIdentityCache {
	return &MemoryIdentityCache{
		ttl:         ttl,
		max:         identityCacheMaxEntries,
		now:         time.Now,
		byAuth:      map[uuid.UUID]identityEntry{},
		byTelegram:  map[telegramKey]identityEntry{},
		invalidated: map[uuid.UUID]time.Time{},
	}
}

func (c *MemoryIdentityCache) ByAuth(authUserID uuid.UUID) (User, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(c.byAuth[authUserID])
}

func (c *MemoryIdentityCache) PutByAuth(authUserID uuid.UUID, user User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entry(user); ok {
		if _, exists := c.byAuth[authUserID]; !exists {
			makeRoom(c.byAuth, c.max)
		}
		c.byAuth[authUserID] = entry
	}
}

func (c *MemoryIdentityCache) ByTelegram(telegramID int64, authUserID uuid.UUID) (User, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(c.byTelegram[telegramKey{telegramID, authUserID}])
}

func (c *MemoryIdentityCache) PutByTelegram(telegramID int64, authUserID uuid.UUID, user User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.entry(user); ok {
		key := telegramKey{telegramID, authUserID}
		if _, exists := c.byTelegram[key]; !exists {
			makeRoom(c.byTelegram, c.max)
		}
		c.byTelegram[key] = entry
	}
}

func (c *MemoryIdentityCache) InvalidateUser(userID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.byAuth {
		if entry.user.ID == userID {
			delete(c.byAuth, key)
		}
	}
	for key, entry := range c.byTelegram {
		if entry.user.ID == userID {
			delete(c.byTelegram, key)
		}
	}
	c.invalidated[userID] = c.now().Add(c.ttl)
}

func (c *MemoryIdentityCache) get(entry identityEntry) (User, bool) {
	if entry.expires.IsZero() || !c.now().Before(entry.expires) {
		return User{}, false
	}
	return entry.user, true
}

func (c *MemoryIdentityCache) entry(user User) (identityEntry, bool) {
	now := c.now()
	if now.Sub(c.lastSweep) >= c.ttl {
		c.sweep(now)
	}
	if until, ok := c.invalidated[user.ID]; ok && now.Before(until) {
		return identityEntry{}, false
	}
	return identityEntry{user: user, expires: now.Add(c.ttl)}, true
}

func (c *MemoryIdentityCache) sweep(now time.Time) {
	for key, entry := range c.byAuth {
		if !now.Before(entry.expires) {
			delete(c.byAuth, key)
		}
	}
	for key, entry := range c.byTelegram {
		if !now.Before(entry.expires) {
			delete(c.byTelegram, key)
		}
	}
	for id, until := range c.invalidated {
		if !now.Before(until) {
			delete(c.invalidated, id)
		}
	}
	c.lastSweep = now
}

var _ IdentityCache = (*MemoryIdentityCache)(nil)

const identityCacheMaxEntries = 10_000

func makeRoom[K comparable](m map[K]identityEntry, max int) {
	if len(m) < max {
		return
	}
	var (
		victim   K
		earliest time.Time
		seen     int
	)
	for key, entry := range m {
		if seen == 0 || entry.expires.Before(earliest) {
			victim, earliest = key, entry.expires
		}
		seen++
		if seen == 32 {
			break
		}
	}
	delete(m, victim)
}
