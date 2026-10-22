package service

import (
	"context"
	"errors"
	"strings"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"

	"github.com/google/uuid"
)

var ErrListHidden = errors.New("список закрыт")

var ErrBadVisibility = errors.New("допустимо private или subscribers")

type SharingRepository interface {
	Visibility(ctx context.Context, userID uint) (auth.ListVisibility, error)
	SetVisibility(ctx context.Context, visibility auth.ListVisibility) error
	CreateShareLink(ctx context.Context, link *auth.ShareLink) error
	ShareLinkByToken(ctx context.Context, token string) (*auth.ShareLink, error)
	ShareLinksOf(ctx context.Context, userID uint) ([]auth.ShareLink, error)
	RevokeShareLink(ctx context.Context, userID uint, token string) error
}

type SharingService struct {
	repo      SharingRepository
	identity  IdentityRepository
	films     *FilmService
	follows   FollowGraph
	publicURL string
}

func NewSharingService(repo SharingRepository, identity IdentityRepository, films *FilmService, follows FollowGraph, publicURL string) *SharingService {
	return &SharingService{repo: repo, identity: identity, films: films, follows: follows, publicURL: strings.TrimRight(publicURL, "/")}
}

func (s *SharingService) Visibility(ctx context.Context, userID uint) (auth.ListVisibility, error) {
	return s.repo.Visibility(ctx, userID)
}

func (s *SharingService) SetVisibility(ctx context.Context, userID uint, watchlist, watched, rated *string) error {
	current, err := s.repo.Visibility(ctx, userID)
	if err != nil {
		return err
	}

	for field, value := range map[*string]*string{
		&current.Watchlist: watchlist,
		&current.Watched:   watched,
		&current.Rated:     rated,
	} {
		if value == nil {
			continue
		}
		if !auth.ValidVisibility(*value) {
			return ErrBadVisibility
		}
		*field = *value
	}

	current.UserID = userID
	return s.repo.SetVisibility(ctx, current)
}

func (s *SharingService) FilmsOf(
	ctx context.Context,
	viewerAuthID, ownerAuthID uuid.UUID,
	viewerUserID uint,
	req MyFilmsRequest,
) (MyFilmsPage, error) {
	owner, err := s.identity.FindByAuthID(ctx, ownerAuthID)
	if err != nil {
		return MyFilmsPage{}, err
	}

	if owner.ID == viewerUserID {
		return s.films.MyFilms(ctx, owner.ID, req)
	}

	visibility, err := s.repo.Visibility(ctx, owner.ID)
	if err != nil {
		return MyFilmsPage{}, err
	}
	allowed, err := s.canView(ctx, viewerAuthID, ownerAuthID, visibility, req.Set)
	if err != nil {
		return MyFilmsPage{}, err
	}
	if !allowed {
		return MyFilmsPage{}, ErrListHidden
	}

	scope := fieldScopeOf(visibility)
	req.Sort = scope.sortFor(req)
	page, err := s.films.MyFilms(ctx, owner.ID, req)
	if err != nil {
		return MyFilmsPage{}, err
	}
	for i := range page.Items {
		scope.strip(&page.Items[i])
	}
	return page, nil
}

func (s *SharingService) canView(ctx context.Context, viewerAuthID, ownerAuthID uuid.UUID, visibility auth.ListVisibility, set string) (bool, error) {
	if visibility.For(set) != auth.VisibilitySubscribers || s.follows == nil {
		return false, nil
	}
	_, ok := s.follows.Following(ctx, viewerAuthID)[ownerAuthID]
	return ok, nil
}

type fieldScope struct {
	rated, watched, watchlist bool
}

func fieldScopeOf(visibility auth.ListVisibility) fieldScope {
	return fieldScope{
		rated:     visibility.Rated == auth.VisibilitySubscribers,
		watched:   visibility.Watched == auth.VisibilitySubscribers,
		watchlist: visibility.Watchlist == auth.VisibilitySubscribers,
	}
}

func (f fieldScope) sortFor(req MyFilmsRequest) string {
	switch film.EntrySort(req.Sort) {
	case film.EntrySortRate:
		if !f.rated {
			return ""
		}
	case film.EntrySortWatched:
		if !f.watched {
			return string(film.EntrySortAdded)
		}
	}
	return req.Sort
}

func (f fieldScope) strip(item *MyFilmItem) {
	if !f.rated {
		item.Rate = nil
		item.Description = nil
		item.SeriesRating = nil
	}
	if !f.watched {
		item.WatchedAt = nil
	}
	if !f.watchlist {
		item.Watchlisted = false
	}
}
