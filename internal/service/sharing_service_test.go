package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"

	"github.com/google/uuid"
)

type stubSharingRepo struct {
	visibility auth.ListVisibility
}

type stubFollows map[uuid.UUID]struct{}

func (f stubFollows) Following(context.Context, uuid.UUID) map[uuid.UUID]struct{} { return f }

func (r *stubSharingRepo) Visibility(context.Context, uint) (auth.ListVisibility, error) {
	return r.visibility, nil
}
func (r *stubSharingRepo) SetVisibility(context.Context, auth.ListVisibility) error { return nil }
func (r *stubSharingRepo) CreateShareLink(context.Context, *auth.ShareLink) error   { return nil }
func (r *stubSharingRepo) ShareLinkByToken(context.Context, string) (*auth.ShareLink, error) {
	return nil, errors.New("unused")
}
func (r *stubSharingRepo) ShareLinksOf(context.Context, uint) ([]auth.ShareLink, error) {
	return nil, nil
}
func (r *stubSharingRepo) RevokeShareLink(context.Context, uint, string) error { return nil }

func sharingFixture(t *testing.T, visibility auth.ListVisibility) (*SharingService, *stubFilmRepo, uuid.UUID, uuid.UUID) {
	t.Helper()
	ownerAuth, viewerAuth := uuid.New(), uuid.New()
	identity := newIdentityRepoFake()
	owner, err := identity.CreateForAuthID(context.Background(), ownerAuth, "owner", auth.RoleUser)
	if err != nil {
		t.Fatal(err)
	}
	visibility.UserID = owner.ID

	rate := int16(9)
	review := "secret review"
	watched := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	repo := &stubFilmRepo{
		entries: []film.FilmEntry{
			{FilmID: 1, Watchlisted: true, WatchedAt: &watched, Rate: &rate, Description: &review, Film: film.Film{Kind: film.KindMovie}},
			{FilmID: 2, WatchedAt: &watched, Film: film.Film{Kind: film.KindSeries}},
		},
		total:   2,
		ratings: map[uint]float64{2: 8.5},
	}
	follows := stubFollows{ownerAuth: {}}
	svc := NewSharingService(&stubSharingRepo{visibility: visibility}, identity, NewFilmService(repo, nil, nil, nil), follows, "")
	return svc, repo, viewerAuth, ownerAuth
}

func TestFilmsOfHidesRatingsWhenRatedIsPrivate(t *testing.T) {
	svc, repo, viewer, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilityPrivate,
		Watched:   auth.VisibilitySubscribers,
		Rated:     auth.VisibilityPrivate,
	})

	page, err := svc.FilmsOf(context.Background(), viewer, owner, 999, MyFilmsRequest{Set: "watched"})
	if err != nil {
		t.Fatalf("FilmsOf: %v", err)
	}
	for _, item := range page.Items {
		if item.Rate != nil || item.Description != nil || item.SeriesRating != nil {
			t.Fatalf("film %d leaks rating or review: %+v", item.ID, item)
		}
		if item.WatchedAt == nil {
			t.Fatalf("film %d lost its watched date, which is open", item.ID)
		}
		if item.Watchlisted {
			t.Fatalf("film %d leaks the private watchlist flag", item.ID)
		}
	}
	if repo.lastFilter.Sort != film.EntrySortWatched {
		t.Fatalf("sort %q, want watched", repo.lastFilter.Sort)
	}
}

func TestFilmsOfShowsRatingsWhenRatedIsOpen(t *testing.T) {
	svc, _, viewer, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilitySubscribers,
		Watched:   auth.VisibilitySubscribers,
		Rated:     auth.VisibilitySubscribers,
	})

	page, err := svc.FilmsOf(context.Background(), viewer, owner, 999, MyFilmsRequest{Set: "watched"})
	if err != nil {
		t.Fatalf("FilmsOf: %v", err)
	}
	if page.Items[0].Rate == nil || page.Items[0].Description == nil || !page.Items[0].Watchlisted {
		t.Fatalf("open fields were stripped: %+v", page.Items[0])
	}
	if page.Items[1].SeriesRating == nil {
		t.Fatal("series rating was stripped although rated is open")
	}
}

func TestFilmsOfHidesWatchedDateOnOpenWatchlist(t *testing.T) {
	svc, _, viewer, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilitySubscribers,
		Watched:   auth.VisibilityPrivate,
		Rated:     auth.VisibilityPrivate,
	})

	page, err := svc.FilmsOf(context.Background(), viewer, owner, 999, MyFilmsRequest{Set: "watchlist"})
	if err != nil {
		t.Fatalf("FilmsOf: %v", err)
	}
	for _, item := range page.Items {
		if item.WatchedAt != nil || item.Rate != nil || item.Description != nil {
			t.Fatalf("hidden fields leaked: %+v", item)
		}
	}
}

func TestFilmsOfRatedSetDropsHiddenWatchedOrder(t *testing.T) {
	svc, repo, viewer, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilityPrivate,
		Watched:   auth.VisibilityPrivate,
		Rated:     auth.VisibilitySubscribers,
	})

	if _, err := svc.FilmsOf(context.Background(), viewer, owner, 999, MyFilmsRequest{Set: "rated", Sort: "watched"}); err != nil {
		t.Fatalf("FilmsOf: %v", err)
	}
	if repo.lastFilter.Sort == film.EntrySortWatched {
		t.Fatal("a hidden watched date still orders the list")
	}
}

func TestFilmsOfOwnListIsUntouched(t *testing.T) {
	svc, _, _, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilityPrivate,
		Watched:   auth.VisibilityPrivate,
		Rated:     auth.VisibilityPrivate,
	})

	page, err := svc.FilmsOf(context.Background(), owner, owner, 1, MyFilmsRequest{Set: "watched"})
	if err != nil {
		t.Fatalf("FilmsOf: %v", err)
	}
	if page.Items[0].Rate == nil || page.Items[0].Description == nil {
		t.Fatal("own list must keep ratings")
	}
}

func TestFilmsOfHiddenSet(t *testing.T) {
	svc, _, viewer, owner := sharingFixture(t, auth.ListVisibility{
		Watchlist: auth.VisibilityPrivate,
		Watched:   auth.VisibilitySubscribers,
		Rated:     auth.VisibilityPrivate,
	})

	if _, err := svc.FilmsOf(context.Background(), viewer, owner, 999, MyFilmsRequest{Set: "rated"}); !errors.Is(err, ErrListHidden) {
		t.Fatalf("err = %v, want ErrListHidden", err)
	}
}
