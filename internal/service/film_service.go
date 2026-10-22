package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/memcache"
)

type CreateReviewRequest struct {
	Rate        *int16  `json:"rate" binding:"omitempty,gte=1,lte=10"`
	Description *string `json:"description" binding:"omitempty,max=255"`
}

type RatingPublisher interface {
	RateMovie(ctx context.Context, tmdbID int, value float64) error
	RateTV(ctx context.Context, tmdbID int, value float64) error
}

type FilmService struct {
	repo FilmRepository

	ratings RatingPublisher

	reviews ReviewGate
	logger  *slog.Logger

	publishing chan struct{}
	genres     *memcache.Loader[[]GenreResponse]

	ratingIndex LocalRatingIndex
	indexing    chan struct{}
}

type FilmRepository interface {
	FindAll(ctx context.Context) ([]film.Film, error)
	Genres(ctx context.Context) ([]film.Genre, error)

	FindById(ctx context.Context, id uint, viewer uint) (*film.Film, error)
	FilmWithGenres(ctx context.Context, id uint) (*film.Film, error)
	AddRateAndDescription(ctx context.Context, filmID uint, userID uint, rate *int16, description *string) error
	AddToWatchList(ctx context.Context, filmID uint, userID uint) error
	AddToWatched(ctx context.Context, filmID uint, userID uint, time time.Time) error
	RemoveFromWatchList(ctx context.Context, filmID uint, userID uint) error
	RemoveFromWatched(ctx context.Context, filmID uint, userID uint) error
	RemoveReview(ctx context.Context, filmID uint, userID uint) error
	FindEntry(ctx context.Context, filmID, userID uint) (*film.FilmEntry, error)
	FindEntries(ctx context.Context, filter film.EntryFilter) ([]film.FilmEntry, int, error)
	EntryCounts(ctx context.Context, userID uint) (film.EntryCounts, error)

	FilmIdentity(ctx context.Context, filmID uint) (film.Kind, int, error)
	SeriesDetails(ctx context.Context, filmID uint) (*film.SeriesDetails, error)
	Seasons(ctx context.Context, filmID uint) ([]film.Season, error)
	SeasonByNumber(ctx context.Context, filmID uint, number int) (*film.Season, error)
	SeasonEntries(ctx context.Context, userID, filmID uint) ([]film.SeasonEntryView, error)
	SeriesRatings(ctx context.Context, userID uint, filmIDs []uint) (map[uint]float64, error)
	SetSeasonEntry(ctx context.Context, userID, filmID uint, season film.Season, state *string, rate *int16, setRate bool, now time.Time) error
	MarkSeasonsWatched(ctx context.Context, userID, filmID uint, seasonIDs []uint, now time.Time) error
	SetSeriesState(ctx context.Context, userID, filmID uint, state string) error
	ClearSeriesState(ctx context.Context, userID, filmID uint, state string) error

	LocalRatings(ctx context.Context, filmIDs []uint) ([]film.LocalRating, error)
}

func NewFilmService(repo FilmRepository, ratings RatingPublisher, reviews ReviewGate, logger *slog.Logger) *FilmService {
	if logger == nil {
		logger = slog.Default()
	}
	return &FilmService{
		repo:       repo,
		ratings:    ratings,
		reviews:    reviews,
		logger:     logger,
		publishing: make(chan struct{}, maxRatingPublishers),
		indexing:   make(chan struct{}, maxRatingIndexers),
		genres:     memcache.NewLoader[[]GenreResponse](referenceTTL, 1),
	}
}
func (s *FilmService) AddToWatchList(ctx context.Context, filmID uint, userID uint) error {
	err := s.repo.AddToWatchList(ctx, filmID, userID)
	return err
}
func (s *FilmService) AddToWatched(ctx context.Context, filmID uint, userID uint) error {
	if err := s.rejectSeries(ctx, filmID); err != nil {
		return err
	}
	err := s.repo.AddToWatched(ctx, filmID, userID, time.Now())
	return err
}

func (s *FilmService) RemoveFromWatchList(ctx context.Context, filmID uint, userID uint) error {
	return s.repo.RemoveFromWatchList(ctx, filmID, userID)
}

func (s *FilmService) RemoveFromWatched(ctx context.Context, filmID uint, userID uint) error {
	if err := s.rejectSeries(ctx, filmID); err != nil {
		return err
	}
	return s.repo.RemoveFromWatched(ctx, filmID, userID)
}

func (s *FilmService) RemoveReview(ctx context.Context, filmID uint, userID uint) error {
	if err := s.repo.RemoveReview(ctx, filmID, userID); err != nil {
		return err
	}
	s.pushLocalRating(filmID)
	return nil
}

func (s *FilmService) FindAll(ctx context.Context) ([]film.Film, error) {
	films, err := s.repo.FindAll(ctx)
	if err != nil {
		return nil, err
	}
	return films, nil
}
func (s *FilmService) AddRateAndDescription(ctx context.Context, filmID, userID uint, req *CreateReviewRequest) error {
	kind, tmdbID, err := s.repo.FilmIdentity(ctx, filmID)
	if err != nil {
		return err
	}

	if kind == film.KindSeries {
		if req.Rate != nil {
			return film.ErrSeriesDerived
		}
		tracked, err := s.repo.SeasonEntries(ctx, userID, filmID)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(tracked, func(e film.SeasonEntryView) bool { return e.State == film.SeasonStateWatched }) {
			return film.ErrSeriesReviewNeedsSeason
		}
	}
	if err := s.repo.AddRateAndDescription(ctx, filmID, userID, req.Rate, req.Description); err != nil {
		return err
	}
	if req.Rate != nil {
		s.pushLocalRating(filmID)
	}

	if req.Rate != nil && s.ratings != nil && tmdbID > 0 {
		s.publishRating(kind, tmdbID, float64(*req.Rate))
	}
	return nil
}

const maxRatingPublishers = 4

func (s *FilmService) publishRating(kind film.Kind, tmdbID int, value float64) {
	select {
	case s.publishing <- struct{}{}:
	default:

		s.logger.Warn("оценка не отправлена в TMDB: очередь занята", "kind", kind, "tmdb_id", tmdbID)
		return
	}

	go func() {
		defer func() { <-s.publishing }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		rate := s.ratings.RateMovie
		if kind == film.KindSeries {
			rate = s.ratings.RateTV
		}
		if err := rate(ctx, tmdbID, value); err != nil {
			s.logger.Warn("оценка не доехала до TMDB", "kind", kind, "tmdb_id", tmdbID, "rate", value, "error", err)
			return
		}
		s.logger.Info("оценка отправлена в TMDB", "kind", kind, "tmdb_id", tmdbID, "rate", value)
	}()
}

type GenreResponse struct {
	Slug   string `json:"slug"`
	NameRu string `json:"name_ru"`
	NameEn string `json:"name_en"`
}

func (s *FilmService) Genres(ctx context.Context) ([]GenreResponse, error) {
	return s.genres.Get(ctx, "", s.loadGenres)
}

func (s *FilmService) loadGenres(ctx context.Context) ([]GenreResponse, error) {
	genres, err := s.repo.Genres(ctx)
	if err != nil {
		return nil, err
	}

	response := make([]GenreResponse, 0, len(genres))
	for _, item := range genres {
		response = append(response, GenreResponse{
			Slug:   item.ID,
			NameRu: item.NameRu,
			NameEn: item.NameEn,
		})
	}
	return response, nil
}

type FilmEntryResponse struct {
	FilmID      uint       `json:"film_id"`
	Watchlisted bool       `json:"watchlisted"`
	WatchedAt   *time.Time `json:"watched_at"`
	Rate        *int16     `json:"rate"`
	Description *string    `json:"description"`
}

func (s *FilmService) MyEntry(ctx context.Context, filmID, userID uint) (*FilmEntryResponse, error) {
	entry, err := s.repo.FindEntry(ctx, filmID, userID)
	if err != nil {
		return nil, err
	}
	return &FilmEntryResponse{
		FilmID:      entry.FilmID,
		Watchlisted: entry.Watchlisted,
		WatchedAt:   entry.WatchedAt,
		Rate:        entry.Rate,
		Description: entry.Description,
	}, nil
}

type MyFilmItem struct {
	FilmRow

	TitleOrig    string     `json:"-"`
	Watchlisted  bool       `json:"watchlisted"`
	WatchedAt    *time.Time `json:"watched_at"`
	Rate         *int16     `json:"rate"`
	SeriesRating *float64   `json:"series_rating,omitempty"`
	Description  *string    `json:"description"`
	AddedAt      time.Time  `json:"added_at"`
}

type MyFilmsPage struct {
	Items      []MyFilmItem `json:"items"`
	Total      int          `json:"total"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type MyFilmsRequest struct {
	Set    string
	Sort   string
	Limit  int
	Cursor string
}

type MyFilmCounts struct {
	Watchlist int `json:"watchlist"`
	Watched   int `json:"watched"`
	Rated     int `json:"rated"`
	Watching  int `json:"watching"`
}

func (s *FilmService) MyFilmCounts(ctx context.Context, userID uint) (MyFilmCounts, error) {
	counts, err := s.repo.EntryCounts(ctx, userID)
	if err != nil {
		return MyFilmCounts{}, fmt.Errorf("count personal sets: %w", err)
	}
	return MyFilmCounts{
		Watchlist: counts.Watchlist,
		Watched:   counts.Watched,
		Rated:     counts.Rated,
		Watching:  counts.Watching,
	}, nil
}

func (s *FilmService) MyFilms(ctx context.Context, userID uint, req MyFilmsRequest) (MyFilmsPage, error) {
	set := film.EntrySet(req.Set)
	if req.Set == "" {
		set = film.EntrySetWatchlist
	}
	if !set.Valid() {
		return MyFilmsPage{}, film.ErrUnknownEntrySet
	}

	sortMode := film.EntrySort(req.Sort)
	if req.Sort == "" {
		sortMode = film.DefaultEntrySort(set)
	}
	if !sortMode.AllowedIn(set) {
		return MyFilmsPage{}, film.ErrSortNotApplicable
	}

	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	afterValue, afterID := decodeEntryCursor(req.Cursor, sortMode)
	entries, total, err := s.repo.FindEntries(ctx, film.EntryFilter{
		UserID:     userID,
		Set:        set,
		Sort:       sortMode,
		AfterValue: afterValue,
		AfterID:    afterID,
		Limit:      limit,
	})
	if err != nil {
		return MyFilmsPage{}, err
	}

	ratings, err := s.seriesRatings(ctx, userID, entries)
	if err != nil {
		return MyFilmsPage{}, err
	}
	page := MyFilmsPage{Items: make([]MyFilmItem, 0, len(entries)), Total: total}
	for _, entry := range entries {
		item := myFilmItem(entry)
		if rating, ok := ratings[entry.FilmID]; ok {
			item.SeriesRating = &rating
		}
		page.Items = append(page.Items, item)
	}

	if len(entries) == limit {
		last := entries[len(entries)-1]
		value := entrySortValue(last, sortMode)

		if rating, ok := ratings[last.FilmID]; ok && sortMode == film.EntrySortRate {
			value = rating
		}
		page.NextCursor = encodeEntryCursor(value, last.FilmID)
	}
	return page, nil
}

func entrySortValue(entry film.FilmEntry, sortMode film.EntrySort) any {
	switch sortMode {
	case film.EntrySortWatched:
		return entry.WatchedAt
	case film.EntrySortRate:
		return entry.Rate
	case film.EntrySortTitle:
		return entry.Film.TitleRu
	default:
		return entry.CreatedAt
	}
}

func myFilmItem(entry film.FilmEntry) MyFilmItem {
	_, original := filmTitles(entry.Film.TitleRu, entry.Film.TitleEn)
	row := filmRow(entry.Film)

	row.ID = entry.FilmID
	return MyFilmItem{
		FilmRow:     row,
		TitleOrig:   original,
		Watchlisted: entry.Watchlisted,
		WatchedAt:   entry.WatchedAt,
		Rate:        entry.Rate,
		Description: entry.Description,
		AddedAt:     entry.CreatedAt,
	}
}

func (s *FilmService) SharedEntry(ctx context.Context, userID, filmID uint) (*MyFilmItem, error) {
	entry, err := s.repo.FindEntry(ctx, filmID, userID)
	if err != nil {
		return nil, err
	}
	stored, err := s.repo.FilmWithGenres(ctx, filmID)
	if err != nil {
		return nil, err
	}
	entry.Film = *stored

	item := myFilmItem(*entry)
	return &item, nil
}
