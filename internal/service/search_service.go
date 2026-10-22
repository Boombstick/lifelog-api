package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"

	"lifelog-api/internal/memcache"
	"lifelog-api/internal/search"
)

var ErrSearchOverloaded = search.ErrOverloaded

var ErrSearchTimeout = search.ErrTimeout

type SearchEngine interface {
	SearchFilms(ctx context.Context, q search.Query) (search.Page[search.FilmDoc], error)
	SearchPeople(ctx context.Context, q search.Query) (search.Page[search.PersonDoc], error)
	FilmBounds(ctx context.Context) (search.FilmBounds, error)
}

type WatchedFilms interface {
	WatchedFilmIDs(ctx context.Context, userID uint) ([]uint, error)
}

type SearchService struct {
	engine  SearchEngine
	watched WatchedFilms
	bounds  *memcache.Loader[FilmBoundsResponse]

	catalog *memcache.Loader[SearchPage[FilmCatalogItem]]
	films   *memcache.Loader[SearchPage[FilmSearchItem]]
	people  *memcache.Loader[SearchPage[PersonSearchItem]]
}

const (
	catalogCacheSize = 2000
	searchCacheSize  = 500
)

var searchCacheLookups = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "search_cache_lookups_total",
		Help: "Catalog and search pages looked up in the response cache, by result",
	},
	[]string{"cache", "result"},
)

func NewSearchService(engine SearchEngine, watched WatchedFilms, cacheTTL time.Duration) *SearchService {
	s := &SearchService{
		engine:  engine,
		watched: watched,
		bounds:  memcache.NewLoader[FilmBoundsResponse](referenceTTL, 1),
	}
	if cacheTTL > 0 {
		s.catalog = memcache.NewLoader[SearchPage[FilmCatalogItem]](cacheTTL, catalogCacheSize)
		s.films = memcache.NewLoader[SearchPage[FilmSearchItem]](cacheTTL, searchCacheSize)
		s.people = memcache.NewLoader[SearchPage[PersonSearchItem]](cacheTTL, searchCacheSize)
	}
	return s
}

type FilmBoundsResponse struct {
	YearMin    int `json:"year_min"`
	YearMax    int `json:"year_max"`
	RuntimeMin int `json:"runtime_min"`
	RuntimeMax int `json:"runtime_max"`
}

func (s *SearchService) Bounds(ctx context.Context) (FilmBoundsResponse, error) {
	return s.bounds.Get(ctx, "", s.loadBounds)
}

func (s *SearchService) loadBounds(ctx context.Context) (FilmBoundsResponse, error) {
	bounds, err := s.engine.FilmBounds(ctx)
	if err != nil {
		return FilmBoundsResponse{}, err
	}
	return FilmBoundsResponse{
		YearMin:    bounds.YearMin,
		YearMax:    bounds.YearMax,
		RuntimeMin: bounds.RuntimeMin,
		RuntimeMax: bounds.RuntimeMax,
	}, nil
}

type PersonSearchItem struct {
	ID           uint     `json:"id"`
	Name         string   `json:"name"`
	OriginalName string   `json:"original_name,omitempty"`
	PhotoURL     string   `json:"photo_url,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	FilmCount    int      `json:"film_count"`
	KnownFor     []string `json:"known_for,omitempty"`
}

type Facet struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type SearchPage[T any] struct {
	Items      []T     `json:"items"`
	Total      int     `json:"total"`
	NextCursor string  `json:"next_cursor,omitempty"`
	Facets     []Facet `json:"facets,omitempty"`
}

type FilmFilters struct {
	Kind        string
	Genres      []string
	YearFrom    int
	YearTo      int
	RuntimeFrom int
	RuntimeTo   int
	RatingFrom  float64
	LocalFrom   float64
}

type SearchRequest struct {
	Text    string
	Scope   string
	Filters FilmFilters
	Sort    string
	Order   string

	HideWatched bool
	UserID      uint
	Limit       int
	Cursor      string
	WithFacets  bool
}

var (
	ErrUnknownSort   = errors.New("неизвестная сортировка")
	ErrAnonymousHide = errors.New("скрыть просмотренные можно только со входом")
	ErrUnknownScope  = errors.New("неизвестная область поиска")
)

func (r SearchRequest) Validate() error {
	if r.Sort != "" && !search.SortMode(r.Sort).Valid() {
		return ErrUnknownSort
	}
	if r.Order != "" && !search.SortOrder(r.Order).Valid() {
		return ErrUnknownSort
	}
	if r.Scope != "" && !search.Scope(r.Scope).Valid() {
		return ErrUnknownScope
	}
	if r.HideWatched && r.UserID == 0 {
		return ErrAnonymousHide
	}
	return nil
}

func (r SearchRequest) toQuery() (search.Query, int) {
	after, total := decodeSearchCursor(r.Cursor)
	return search.Query{
		Text:  r.Text,
		Scope: search.Scope(r.Scope),
		Filters: search.Filters{
			Kind:        r.Filters.Kind,
			Genres:      r.Filters.Genres,
			YearFrom:    r.Filters.YearFrom,
			YearTo:      r.Filters.YearTo,
			RuntimeFrom: r.Filters.RuntimeFrom,
			RuntimeTo:   r.Filters.RuntimeTo,
			RatingFrom:  r.Filters.RatingFrom,
			LocalFrom:   r.Filters.LocalFrom,
		},
		Sort:       search.SortMode(r.Sort),
		Order:      search.SortOrder(r.Order),
		Limit:      r.Limit,
		After:      after,
		WithFacets: r.WithFacets,
		SkipTotal:  total > 0,
	}, total
}

func (r SearchRequest) cacheKey() string {
	genres := slices.Clone(r.Filters.Genres)
	slices.Sort(genres)
	f := r.Filters
	return fmt.Sprintf("%s|%s|%s|%s|%d|%d|%d|%d|%g|%g|%s|%s|%d|%s|%t",
		strings.ToLower(strings.TrimSpace(r.Text)), r.Scope, f.Kind, strings.Join(genres, ","),
		f.YearFrom, f.YearTo, f.RuntimeFrom, f.RuntimeTo, f.RatingFrom, f.LocalFrom,
		r.Sort, r.Order, r.Limit, r.Cursor, r.WithFacets)
}

func cached[T any](ctx context.Context, loader *memcache.Loader[T], name string, req SearchRequest, load func(context.Context) (T, error)) (T, error) {
	if loader == nil || req.HideWatched {
		return load(ctx)
	}
	key := req.cacheKey()
	if page, ok := loader.Cached(key); ok {
		searchCacheLookups.WithLabelValues(name, "hit").Inc()
		return page, nil
	}
	searchCacheLookups.WithLabelValues(name, "miss").Inc()
	return loader.Get(ctx, key, load)
}

func (s *SearchService) Films(ctx context.Context, req SearchRequest) (SearchPage[FilmSearchItem], error) {
	return cached(ctx, s.films, "films", req, func(ctx context.Context) (SearchPage[FilmSearchItem], error) {
		return searchFilms(ctx, s, req, false, filmSearchItem)
	})
}

func (s *SearchService) Catalog(ctx context.Context, req SearchRequest) (SearchPage[FilmCatalogItem], error) {
	return cached(ctx, s.catalog, "catalog", req, func(ctx context.Context) (SearchPage[FilmCatalogItem], error) {
		return searchFilms(ctx, s, req, true, filmCatalogItem)
	})
}

func searchFilms[T any](ctx context.Context, s *SearchService, req SearchRequest, lean bool, toItem func(search.FilmDoc) T) (SearchPage[T], error) {
	query, knownTotal := req.toQuery()
	query.Lean = lean
	if req.HideWatched {
		watched, err := s.watched.WatchedFilmIDs(ctx, req.UserID)
		if err != nil {
			return SearchPage[T]{}, err
		}
		query.Filters.ExcludeIDs = watched
	}

	page, err := s.engine.SearchFilms(ctx, query)
	if err != nil {
		return SearchPage[T]{}, err
	}

	items := make([]T, 0, len(page.Items))
	for _, doc := range page.Items {
		items = append(items, toItem(doc))
	}

	total := page.Total
	if query.SkipTotal {
		total = knownTotal
	}
	result := SearchPage[T]{
		Items:      items,
		Total:      total,
		NextCursor: encodeSearchCursor(page.NextAfter, total),
	}
	for _, bucket := range page.Facets {
		result.Facets = append(result.Facets, Facet{Key: bucket.Key, Count: bucket.Count})
	}
	return result, nil
}

func (s *SearchService) People(ctx context.Context, req SearchRequest) (SearchPage[PersonSearchItem], error) {
	return cached(ctx, s.people, "people", req, func(ctx context.Context) (SearchPage[PersonSearchItem], error) {
		return s.searchPeople(ctx, req)
	})
}

func (s *SearchService) searchPeople(ctx context.Context, req SearchRequest) (SearchPage[PersonSearchItem], error) {
	query, knownTotal := req.toQuery()
	page, err := s.engine.SearchPeople(ctx, query)
	if err != nil {
		return SearchPage[PersonSearchItem]{}, err
	}

	items := make([]PersonSearchItem, 0, len(page.Items))
	for _, doc := range page.Items {
		items = append(items, PersonSearchItem{
			ID:           doc.ID,
			Name:         doc.Name,
			OriginalName: doc.OriginalName,
			PhotoURL:     doc.PhotoURL,
			Roles:        doc.Roles,
			FilmCount:    doc.FilmCount,
			KnownFor:     doc.KnownFor,
		})
	}

	total := page.Total
	if query.SkipTotal {
		total = knownTotal
	}
	return SearchPage[PersonSearchItem]{
		Items:      items,
		Total:      total,
		NextCursor: encodeSearchCursor(page.NextAfter, total),
	}, nil
}

type CombinedSearch struct {
	Top    string                       `json:"top"`
	Films  SearchPage[FilmSearchItem]   `json:"films"`
	People SearchPage[PersonSearchItem] `json:"people"`
}

const (
	TopFilms  = "films"
	TopPeople = "people"
)

func (s *SearchService) Combined(ctx context.Context, req SearchRequest) (CombinedSearch, error) {
	var (
		films  SearchPage[FilmSearchItem]
		people SearchPage[PersonSearchItem]
	)
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		films, err = s.Films(gCtx, req)
		return err
	})
	g.Go(func() (err error) {
		people, err = s.People(gCtx, req)
		return err
	})
	if err := g.Wait(); err != nil {
		return CombinedSearch{}, err
	}
	return CombinedSearch{
		Top:    topBlock(req.Text, films.Items, people.Items),
		Films:  films,
		People: people,
	}, nil
}

func topBlock(text string, films []FilmSearchItem, people []PersonSearchItem) string {
	needle := normalizeForMatch(text)
	if needle == "" {
		return TopFilms
	}

	for _, item := range films {
		if normalizeForMatch(item.Title) == needle || normalizeForMatch(item.TitleOrig) == needle {
			return TopFilms
		}
	}
	for _, item := range people {
		if normalizeForMatch(item.Name) == needle || normalizeForMatch(item.OriginalName) == needle {
			return TopPeople
		}
	}
	if len(people) > 0 {
		return TopPeople
	}
	return TopFilms
}

func normalizeForMatch(value string) string {
	var builder strings.Builder
	for _, symbol := range strings.ToLower(value) {
		if unicode.IsLetter(symbol) || unicode.IsDigit(symbol) {
			builder.WriteRune(symbol)
		}
	}
	return builder.String()
}
