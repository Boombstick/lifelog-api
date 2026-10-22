package service

import (
	"context"
	"sort"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"

	"github.com/google/uuid"
)

type ReviewGate interface {
	Visibilities(ctx context.Context, userIDs []uint) (map[uint]auth.ListVisibility, error)
	FollowGraph
}

type FollowGraph interface {
	Following(ctx context.Context, viewer uuid.UUID) map[uuid.UUID]struct{}
}

type FilmCard struct {
	ID          uint             `json:"id"`
	Title       string           `json:"title"`
	TitleOrig   string           `json:"title_original,omitempty"`
	Kind        string           `json:"kind"`
	Year        int              `json:"year,omitempty"`
	ReleaseDate string           `json:"release_date,omitempty"`
	Runtime     int              `json:"runtime,omitempty"`
	Synopsis    string           `json:"synopsis,omitempty"`
	PosterURL   string           `json:"poster_url,omitempty"`
	TMDBID      int              `json:"tmdb_id"`
	IMDBID      string           `json:"imdb_id,omitempty"`
	TMDBRating  float32          `json:"tmdb_rating"`
	TMDBVotes   int              `json:"tmdb_votes"`
	LocalRating float32          `json:"local_rating"`
	LocalVotes  int              `json:"local_votes"`
	Genres      []string         `json:"genres"`
	GenreSlugs  []string         `json:"genre_slugs"`
	Credits     []FilmCardCredit `json:"credits"`

	MyEntry *FilmCardEntry `json:"my_entry"`

	Reviews []FilmCardReview `json:"reviews"`

	Series  *FilmCardSeries  `json:"series,omitempty"`
	Seasons []FilmCardSeason `json:"seasons,omitempty"`
}

type FilmCardCredit struct {
	PersonID     uint   `json:"person_id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
	PhotoURL     string `json:"photo_url"`
	Role         string `json:"role"`
	BillingOrder int    `json:"billing_order"`
}

type FilmCardEntry struct {
	Watchlisted bool       `json:"watchlisted"`
	WatchedAt   *time.Time `json:"watched_at"`
	Rate        *int16     `json:"rate"`
	Description *string    `json:"description"`

	SeriesState   *string               `json:"series_state,omitempty"`
	SeriesWatched *bool                 `json:"series_watched,omitempty"`
	SeriesRating  *float64              `json:"series_rating,omitempty"`
	Seasons       []FilmCardSeasonEntry `json:"seasons,omitempty"`
	CurrentSeason *int                  `json:"current_season,omitempty"`
	Progress      *FilmCardProgress     `json:"progress,omitempty"`
}

type FilmCardReview struct {
	UserName    string     `json:"user_name"`
	Rate        *int16     `json:"rate"`
	Description *string    `json:"description"`
	WatchedAt   *time.Time `json:"watched_at"`

	updatedAt time.Time
}

func (s *FilmService) Card(ctx context.Context, id uint, viewer *auth.Principal) (*FilmCard, error) {
	var viewerID uint
	if viewer != nil {
		viewerID = viewer.UserID
	}
	stored, err := s.repo.FindById(ctx, id, viewerID)
	if err != nil {
		return nil, err
	}
	card := filmCard(*stored)

	var seasons []film.Season
	if stored.Kind == film.KindSeries {
		details, err := s.repo.SeriesDetails(ctx, id)
		if err != nil {
			return nil, err
		}
		seasons, err = s.repo.Seasons(ctx, id)
		if err != nil {
			return nil, err
		}
		card.Series = seriesCard(details)
		card.Seasons = seasonCards(seasons, utcToday())
	}

	if viewer == nil {
		return &card, nil
	}

	for _, entry := range stored.Entries {
		if entry.UserID == viewer.UserID {
			mine := cardEntry(entry)
			if stored.Kind == film.KindSeries {
				tracked, err := s.repo.SeasonEntries(ctx, viewer.UserID, id)
				if err != nil {
					return nil, err
				}
				applySeriesProgress(&mine, seasons, tracked, entry.SeriesState, utcToday())
			}
			card.MyEntry = &mine
		}
	}
	allowed, err := s.visibleReviewers(ctx, *viewer, stored.Entries)
	if err != nil {
		return nil, err
	}
	for _, entry := range stored.Entries {
		if scope, ok := allowed[entry.UserID]; ok && entry.UserID != viewer.UserID && hasReview(entry) {
			review := cardReview(entry)
			if !scope.watched {
				review.WatchedAt = nil
			}
			card.Reviews = append(card.Reviews, review)
		}
	}

	sort.SliceStable(card.Reviews, func(i, j int) bool {
		return card.Reviews[i].updatedAt.After(card.Reviews[j].updatedAt)
	})
	return &card, nil
}

func (s *FilmService) visibleReviewers(ctx context.Context, viewer auth.Principal, entries []film.FilmEntry) (map[uint]fieldScope, error) {
	allowed := map[uint]fieldScope{}
	if s.reviews == nil {
		return allowed, nil
	}
	type author struct {
		id     uint
		authID uuid.UUID
	}
	var authors []author
	seen := map[uint]bool{}
	for _, entry := range entries {
		if entry.UserID == viewer.UserID || !hasReview(entry) || seen[entry.UserID] {
			continue
		}
		seen[entry.UserID] = true

		if entry.User.AuthUserID == nil {
			continue
		}
		authors = append(authors, author{entry.UserID, *entry.User.AuthUserID})
	}
	if len(authors) == 0 {
		return allowed, nil
	}

	following := s.reviews.Following(ctx, viewer.AuthUserID)
	var followed []uint
	for _, a := range authors {
		if _, ok := following[a.authID]; ok {
			followed = append(followed, a.id)
		}
	}
	if len(followed) == 0 {
		return allowed, nil
	}
	visibilities, err := s.reviews.Visibilities(ctx, followed)
	if err != nil {
		return nil, err
	}
	for _, id := range followed {
		visibility, ok := visibilities[id]
		if !ok {
			visibility = auth.DefaultVisibility(id)
		}
		if scope := fieldScopeOf(visibility); scope.rated {
			allowed[id] = scope
		}
	}
	return allowed, nil
}

func hasReview(entry film.FilmEntry) bool {
	return entry.Rate != nil || (entry.Description != nil && *entry.Description != "")
}

func filmCard(stored film.Film) FilmCard {
	title := stored.TitleRu
	if title == "" {
		title = stored.TitleEn
	}
	card := FilmCard{
		ID:          stored.ID,
		Title:       title,
		TitleOrig:   stored.TitleEn,
		Kind:        string(stored.Kind),
		Runtime:     stored.Runtime,
		Synopsis:    stored.Synopsis,
		PosterURL:   stored.PosterURL,
		TMDBID:      stored.TMDBID,
		IMDBID:      stored.IMDBID,
		TMDBRating:  stored.TMDBVoteAverage,
		TMDBVotes:   stored.TMDBVoteCount,
		LocalRating: stored.LocalRating,
		LocalVotes:  stored.LocalRatingCount,
		Genres:      make([]string, 0, len(stored.Genres)),
		GenreSlugs:  make([]string, 0, len(stored.Genres)),
		Credits:     make([]FilmCardCredit, 0, len(stored.Credits)),
		Reviews:     []FilmCardReview{},
	}

	if !stored.ReleaseDate.IsZero() {
		card.Year = stored.ReleaseDate.Year()
		card.ReleaseDate = stored.ReleaseDate.Format("2006-01-02")
	}
	for _, genre := range stored.Genres {
		name := genre.NameRu
		if name == "" {
			name = genre.NameEn
		}
		card.Genres = append(card.Genres, name)
		card.GenreSlugs = append(card.GenreSlugs, genre.ID)
	}
	for _, credit := range stored.Credits {
		card.Credits = append(card.Credits, FilmCardCredit{
			PersonID:     credit.PersonID,
			Name:         credit.Person.Name,
			OriginalName: credit.Person.OriginalName,
			PhotoURL:     credit.Person.PhotoURL,
			Role:         string(credit.Role),
			BillingOrder: credit.BillingOrder,
		})
	}

	sort.SliceStable(card.Credits, func(i, j int) bool {
		ri, rj := roleRank(card.Credits[i].Role), roleRank(card.Credits[j].Role)
		if ri != rj {
			return ri < rj
		}
		return card.Credits[i].BillingOrder < card.Credits[j].BillingOrder
	})
	return card
}

func roleRank(role string) int {
	switch film.CreditRole(role) {
	case film.CreditRoleCreator:
		return 0
	case film.CreditRoleDirector:
		return 1
	case film.CreditRoleActor:
		return 2
	}
	return 3
}

func cardEntry(entry film.FilmEntry) FilmCardEntry {
	return FilmCardEntry{
		Watchlisted: entry.Watchlisted,
		WatchedAt:   entry.WatchedAt,
		Rate:        entry.Rate,
		Description: entry.Description,
		SeriesState: entry.SeriesState,
	}
}

func cardReview(entry film.FilmEntry) FilmCardReview {
	return FilmCardReview{
		UserName:    entry.User.Username,
		Rate:        entry.Rate,
		Description: entry.Description,
		WatchedAt:   entry.WatchedAt,
		updatedAt:   entry.UpdatedAt,
	}
}
