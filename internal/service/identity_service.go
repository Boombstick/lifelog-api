package service

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/memcache"
	"lifelog-api/internal/model"

	"github.com/google/uuid"
)

const (
	identityCacheTTL  = 10 * time.Minute
	identityCacheSize = 10_000
)

type IdentityRepository interface {
	FindByAuthID(ctx context.Context, authUserID uuid.UUID) (*auth.User, error)
	FindByID(ctx context.Context, id uint) (*auth.User, error)
	CreateForAuthID(ctx context.Context, authUserID uuid.UUID, displayName string, role auth.Role) (*auth.User, error)
	UpdateUsername(ctx context.Context, id uint, displayName string) error
	UpdateRole(ctx context.Context, id uint, role auth.Role) error
}

type cachedIdentity struct {
	principal auth.Principal
	name      string
}

type IdentityService struct {
	repo  IdentityRepository
	cache *memcache.TTL[uuid.UUID, cachedIdentity]
}

func NewIdentityService(repo IdentityRepository) *IdentityService {
	return &IdentityService{
		repo:  repo,
		cache: memcache.NewTTL[uuid.UUID, cachedIdentity](identityCacheTTL, identityCacheSize),
	}
}

func (s *IdentityService) Resolve(ctx context.Context, authUserID uuid.UUID, role, name string) (auth.Principal, error) {
	localRole := roleFromClaim(role)
	if cached, ok := s.cache.Get(authUserID); ok &&
		(name == "" || cached.name == name) && (role == "" || cached.principal.Role == localRole) {
		return cached.principal, nil
	}
	gen := s.cache.Generation()
	principal, stored, err := s.resolve(ctx, authUserID, role, name)
	if err != nil {
		return auth.Principal{}, err
	}
	s.cache.Put(authUserID, cachedIdentity{principal: principal, name: stored}, gen)
	return principal, nil
}

func (s *IdentityService) resolve(ctx context.Context, authUserID uuid.UUID, role, name string) (auth.Principal, string, error) {
	localRole := roleFromClaim(role)
	user, err := s.repo.FindByAuthID(ctx, authUserID)
	switch {
	case err == nil:
		if name != "" && user.Username != name {
			if err := s.repo.UpdateUsername(ctx, user.ID, name); err != nil {
				return auth.Principal{}, "", err
			}
			user.Username = name
		}
		if role != "" && user.Role != localRole {
			if err := s.repo.UpdateRole(ctx, user.ID, localRole); err != nil {
				return auth.Principal{}, "", err
			}
			user.Role = localRole
		}
		return auth.Principal{UserID: user.ID, Role: user.Role, AuthUserID: authUserID}, user.Username, nil
	case !isNotFound(err):
		return auth.Principal{}, "", err
	}

	displayName := name
	if displayName == "" {
		displayName = authUserID.String()
	}
	user, err = s.repo.CreateForAuthID(ctx, authUserID, displayName, localRole)
	if err != nil {
		return auth.Principal{}, "", err
	}
	return auth.Principal{UserID: user.ID, Role: user.Role, AuthUserID: authUserID}, user.Username, nil
}

func roleFromClaim(role string) auth.Role {
	if auth.Role(role) == auth.RoleAdmin {
		return auth.RoleAdmin
	}
	return auth.RoleUser
}

func isNotFound(err error) bool {
	_, ok := errors.AsType[*model.EntityNotFoundError](err)
	return ok
}
