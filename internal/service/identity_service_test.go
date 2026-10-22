package service

import (
	"context"
	"testing"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/model"

	"github.com/google/uuid"
)

type identityRepoFake struct {
	byAuth  map[uuid.UUID]*auth.User
	nextID  uint
	finds   int
	updates int
}

func newIdentityRepoFake() *identityRepoFake {
	return &identityRepoFake{byAuth: map[uuid.UUID]*auth.User{}, nextID: 1}
}

func (r *identityRepoFake) FindByAuthID(_ context.Context, id uuid.UUID) (*auth.User, error) {
	r.finds++
	user, ok := r.byAuth[id]
	if !ok {
		return nil, model.NewEntityNotFound("user")
	}
	copied := *user
	return &copied, nil
}

func (r *identityRepoFake) FindByID(context.Context, uint) (*auth.User, error) {
	return nil, model.NewEntityNotFound("user")
}

func (r *identityRepoFake) CreateForAuthID(_ context.Context, id uuid.UUID, name string, role auth.Role) (*auth.User, error) {
	authID := id
	user := &auth.User{Username: name, Role: role, AuthUserID: &authID}
	user.ID = r.nextID
	r.nextID++
	r.byAuth[id] = user
	copied := *user
	return &copied, nil
}

func (r *identityRepoFake) UpdateUsername(_ context.Context, id uint, name string) error {
	r.updates++
	for _, user := range r.byAuth {
		if user.ID == id {
			user.Username = name
		}
	}
	return nil
}

func (r *identityRepoFake) UpdateRole(_ context.Context, id uint, role auth.Role) error {
	r.updates++
	for _, user := range r.byAuth {
		if user.ID == id {
			user.Role = role
		}
	}
	return nil
}

func TestIdentityResolveIsCached(t *testing.T) {
	repo := newIdentityRepoFake()
	s := NewIdentityService(repo)
	id := uuid.New()

	first, err := s.Resolve(context.Background(), id, "user", "ann")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Resolve(context.Background(), id, "user", "ann")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("got %+v then %+v", first, second)
	}
	if repo.finds != 1 {
		t.Fatalf("repository was read %d times, want 1", repo.finds)
	}
}

func TestIdentityResolveFollowsRenamedClaim(t *testing.T) {
	ctx := context.Background()
	repo := newIdentityRepoFake()
	s := NewIdentityService(repo)
	id := uuid.New()

	first, _ := s.Resolve(ctx, id, "user", "ann")
	if _, err := s.Resolve(ctx, id, "user", "ann"); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 0 {
		t.Fatalf("unchanged name wrote %d times", repo.updates)
	}

	renamed, err := s.Resolve(ctx, id, "user", "Anna K")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.UserID != first.UserID {
		t.Fatalf("rename changed the local user: %d -> %d", first.UserID, renamed.UserID)
	}
	if got := repo.byAuth[id].Username; got != "Anna K" {
		t.Fatalf("stored name %q, want %q", got, "Anna K")
	}
	if repo.updates != 1 {
		t.Fatalf("rename wrote %d times, want 1", repo.updates)
	}

	finds := repo.finds
	if _, err := s.Resolve(ctx, id, "user", "Anna K"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, id, "user", ""); err != nil {
		t.Fatal(err)
	}
	if repo.finds != finds || repo.updates != 1 {
		t.Fatal("the renamed identity was not served from cache")
	}
}

func TestIdentityResolveFollowsDemotion(t *testing.T) {
	repo := newIdentityRepoFake()
	s := NewIdentityService(repo)
	id := uuid.New()

	if p, err := s.Resolve(context.Background(), id, "admin", "ann"); err != nil || p.Role != auth.RoleAdmin {
		t.Fatalf("first resolve: %+v, %v", p, err)
	}
	p, err := s.Resolve(context.Background(), id, "user", "ann")
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != auth.RoleUser {
		t.Fatalf("role after demotion = %q, want user", p.Role)
	}
	if stored := repo.byAuth[id].Role; stored != auth.RoleUser {
		t.Fatalf("stored role = %q, want user", stored)
	}
}

func TestIdentityResolveUnknownRoleIsUser(t *testing.T) {
	s := NewIdentityService(newIdentityRepoFake())
	p, err := s.Resolve(context.Background(), uuid.New(), "superuser", "ann")
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != auth.RoleUser {
		t.Fatalf("role = %q, want user", p.Role)
	}
}

func TestIdentityResolveEmptyClaimsKeepStoredCopy(t *testing.T) {
	ctx := context.Background()
	repo := newIdentityRepoFake()
	id := uuid.New()
	if _, err := NewIdentityService(repo).Resolve(ctx, id, "admin", "ann"); err != nil {
		t.Fatal(err)
	}

	p, err := NewIdentityService(repo).Resolve(ctx, id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != auth.RoleAdmin || repo.byAuth[id].Role != auth.RoleAdmin {
		t.Fatalf("empty role claim changed the role: principal %q, stored %q", p.Role, repo.byAuth[id].Role)
	}
	if got := repo.byAuth[id].Username; got != "ann" {
		t.Fatalf("empty name claim changed the name to %q", got)
	}
	if repo.updates != 0 {
		t.Fatalf("empty claims wrote %d times", repo.updates)
	}
}

func TestIdentityResolveNewUserWithoutNameGetsID(t *testing.T) {
	repo := newIdentityRepoFake()
	id := uuid.New()
	if _, err := NewIdentityService(repo).Resolve(context.Background(), id, "user", ""); err != nil {
		t.Fatal(err)
	}
	if got := repo.byAuth[id].Username; got != id.String() {
		t.Fatalf("new user name %q, want the auth id", got)
	}
}
