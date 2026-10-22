package tmdbadd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/tmdb"
)

var (
	ErrQuery       = errors.New("Запрос должен быть от 2 до 100 символов")
	ErrPage        = errors.New("Номер страницы должен быть от 1 до 20")
	ErrKind        = errors.New("Укажите, фильм это или сериал")
	ErrTMDBID      = errors.New("Некорректный идентификатор TMDB")
	ErrAdult       = errors.New("Взрослый контент в каталог не добавляется")
	ErrNotFound    = errors.New("В TMDB такого не нашлось")
	ErrLimit       = errors.New("Лимит добавлений на сегодня исчерпан")
	ErrUnavailable = errors.New("TMDB сейчас недоступен")
)

const (
	MaxPage      = 20
	DailyLimit   = 20
	limitWindow  = 24 * time.Hour
	posterSize   = "w342"
	searchBudget = 20 * time.Second

	AddBudget = 90 * time.Second
)

type Searcher interface {
	SearchMulti(ctx context.Context, query string, page int) (tmdb.SearchPage, error)
	PosterURLSize(posterPath, size string) string
}

type CardKey struct {
	Kind   film.Kind
	TMDBID int
}

type Catalog interface {
	FilmIDsByTMDB(ctx context.Context, keys []CardKey) (map[CardKey]uint, error)
	FilmByTMDBID(ctx context.Context, kind film.Kind, tmdbID int) (uint, bool, error)
	ManualAddsSince(ctx context.Context, userID uint, since time.Time) (int64, error)

	ClaimManualAdd(ctx context.Context, filmID, userID uint, createdSince, at time.Time) (bool, error)
}

type CardFetcher interface {
	ImportOne(ctx context.Context, kind film.Kind, tmdbID int) error
}

type CardLocalizer interface {
	Localize(ctx context.Context, filmID uint) error
}

type CardIndexer interface {
	IndexCards(ctx context.Context, filmIDs []uint) error
}

type IndexRefresher interface {
	Refresh(ctx context.Context) error
}

type Deps struct {
	Searcher  Searcher
	Catalog   Catalog
	Fetcher   CardFetcher
	Localizer CardLocalizer
	Indexer   CardIndexer
	Refresher IndexRefresher
}

type Service struct {
	deps   Deps
	logger *slog.Logger
	now    func() time.Time

	adding sync.Map
}

func NewService(deps Deps, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{deps: deps, logger: logger, now: func() time.Time { return time.Now().UTC() }}
}

type SearchItem struct {
	TMDBID        int     `json:"tmdb_id"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Year          *int    `json:"year"`
	PosterURL     *string `json:"poster_url"`
	Overview      *string `json:"overview"`
	VoteCount     int     `json:"vote_count"`
	VoteAverage   float64 `json:"vote_average"`
	CatalogID     *uint   `json:"catalog_id"`
}

type SearchResponse struct {
	Items      []SearchItem `json:"items"`
	Page       int          `json:"page"`
	TotalPages int          `json:"total_pages"`
}

type AddResponse struct {
	ID      uint `json:"id"`
	Created bool `json:"created"`
}

func NormalizeQuery(raw string) (string, error) {
	query := strings.TrimSpace(raw)
	if !utf8.ValidString(query) {
		return "", ErrQuery
	}
	if n := utf8.RuneCountInString(query); n < 2 || n > 100 {
		return "", ErrQuery
	}
	return query, nil
}

func (s *Service) Search(ctx context.Context, rawQuery string, page int) (SearchResponse, error) {
	query, err := NormalizeQuery(rawQuery)
	if err != nil {
		return SearchResponse{}, err
	}
	if page < 1 || page > MaxPage {
		return SearchResponse{}, ErrPage
	}
	if s.deps.Searcher == nil {
		return SearchResponse{}, ErrUnavailable
	}

	searchCtx, cancel := context.WithTimeout(ctx, searchBudget)
	defer cancel()
	found, err := s.deps.Searcher.SearchMulti(searchCtx, query, page)
	if err != nil {
		if ctx.Err() == nil && tmdb.Unavailable(err) {
			s.logger.Warn("tmdb search failed", "error", err)
			return SearchResponse{}, ErrUnavailable
		}
		return SearchResponse{}, fmt.Errorf("tmdb search: %w", err)
	}

	keys := make([]CardKey, 0, len(found.Items))
	for _, item := range found.Items {
		keys = append(keys, CardKey{Kind: kindOf(item.MediaType), TMDBID: item.ID})
	}
	known, err := s.deps.Catalog.FilmIDsByTMDB(ctx, keys)
	if err != nil {
		return SearchResponse{}, err
	}

	resp := SearchResponse{
		Items: make([]SearchItem, 0, len(found.Items)),
		Page:  page,

		TotalPages: min(found.TotalPages, MaxPage),
	}
	for i, item := range found.Items {
		out := SearchItem{
			TMDBID:        item.ID,
			Kind:          string(keys[i].Kind),
			Title:         item.Title,
			OriginalTitle: item.OriginalTitle,
			VoteCount:     item.VoteCount,
			VoteAverage:   item.VoteAverage,
		}
		if out.Title == "" {
			out.Title = item.OriginalTitle
		}
		if !item.Date.IsZero() {
			year := item.Date.Year()
			out.Year = &year
		}
		if poster := s.deps.Searcher.PosterURLSize(item.PosterPath, posterSize); poster != "" {
			out.PosterURL = &poster
		}
		if overview := strings.TrimSpace(item.Overview); overview != "" {
			out.Overview = &overview
		}
		if id, ok := known[keys[i]]; ok {
			out.CatalogID = &id
		}
		resp.Items = append(resp.Items, out)
	}
	return resp, nil
}

func ParseKind(raw string) (film.Kind, error) {
	switch film.Kind(raw) {
	case film.KindMovie, film.KindSeries:
		return film.Kind(raw), nil
	}
	return "", ErrKind
}

func (s *Service) Add(ctx context.Context, userID uint, kind film.Kind, tmdbID int) (AddResponse, error) {
	if kind != film.KindMovie && kind != film.KindSeries {
		return AddResponse{}, ErrKind
	}
	if tmdbID <= 0 {
		return AddResponse{}, ErrTMDBID
	}

	lock, _ := s.adding.LoadOrStore(userID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), AddBudget)
	defer cancel()

	startedAt := s.now()
	id, found, err := s.deps.Catalog.FilmByTMDBID(ctx, kind, tmdbID)
	if err != nil {
		return AddResponse{}, err
	}
	if found {
		return AddResponse{ID: id, Created: false}, nil
	}
	if s.deps.Fetcher == nil {
		return AddResponse{}, ErrUnavailable
	}

	used, err := s.deps.Catalog.ManualAddsSince(ctx, userID, startedAt.Add(-limitWindow))
	if err != nil {
		return AddResponse{}, err
	}
	if used >= DailyLimit {
		return AddResponse{}, ErrLimit
	}

	if err := s.deps.Fetcher.ImportOne(ctx, kind, tmdbID); err != nil {
		switch {
		case errors.Is(err, tmdb.ErrNotFound):
			return AddResponse{}, ErrNotFound
		case errors.Is(err, importer.ErrAdult):
			return AddResponse{}, ErrAdult
		case tmdb.Unavailable(err):
			s.logger.Warn("tmdb card not fetched", "kind", kind, "tmdb_id", tmdbID, "error", err)
			return AddResponse{}, ErrUnavailable
		}

		if id, found, lookupErr := s.deps.Catalog.FilmByTMDBID(ctx, kind, tmdbID); lookupErr == nil && found {
			return AddResponse{ID: id, Created: false}, nil
		}
		return AddResponse{}, fmt.Errorf("import card: %w", err)
	}

	id, found, err = s.deps.Catalog.FilmByTMDBID(ctx, kind, tmdbID)
	if err != nil {
		return AddResponse{}, err
	}
	if !found {
		return AddResponse{}, fmt.Errorf("card %s/%d missing after import", kind, tmdbID)
	}

	claimed, err := s.deps.Catalog.ClaimManualAdd(ctx, id, userID, startedAt, s.now())
	if err != nil {
		return AddResponse{}, err
	}
	if !claimed {
		return AddResponse{ID: id, Created: false}, nil
	}

	if s.deps.Localizer != nil {
		if err := s.deps.Localizer.Localize(ctx, id); err != nil {
			s.logger.Warn("added card not localized", "film_id", id, "error", err)
		}
	}
	if s.deps.Indexer != nil {
		if err := s.deps.Indexer.IndexCards(ctx, []uint{id}); err != nil {
			s.logger.Warn("added card not indexed", "film_id", id, "error", err)
		} else if s.deps.Refresher != nil {
			if err := s.deps.Refresher.Refresh(ctx); err != nil {
				s.logger.Warn("index not refreshed after add", "film_id", id, "error", err)
			}
		}
	}
	s.logger.Info("card added from tmdb", "film_id", id, "kind", kind, "tmdb_id", tmdbID, "user_id", userID)
	return AddResponse{ID: id, Created: true}, nil
}

func kindOf(mediaType string) film.Kind {
	if mediaType == "tv" {
		return film.KindSeries
	}
	return film.KindMovie
}
