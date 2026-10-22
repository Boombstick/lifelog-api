package service

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"lifelog-api/internal/search"
)

type searchEngineFake struct {
	films     search.Page[search.FilmDoc]
	people    search.Page[search.PersonDoc]
	lastQuery search.Query
}

func (e *searchEngineFake) SearchFilms(_ context.Context, q search.Query) (search.Page[search.FilmDoc], error) {
	e.lastQuery = q
	return e.films, nil
}

func (e *searchEngineFake) SearchPeople(_ context.Context, q search.Query) (search.Page[search.PersonDoc], error) {
	e.lastQuery = q
	return e.people, nil
}

func (e *searchEngineFake) FilmBounds(context.Context) (search.FilmBounds, error) {
	return search.FilmBounds{}, nil
}

type watchedFake []uint

func (w watchedFake) WatchedFilmIDs(context.Context, uint) ([]uint, error) { return w, nil }

func TestSearchRequestReachesQuery(t *testing.T) {
	t.Parallel()

	after := []any{7.5, float64(12)}
	engine := &searchEngineFake{}
	req := SearchRequest{
		Text: "дюна", Scope: "title",
		Filters: FilmFilters{
			Kind: "movie", Genres: []string{"drama"}, YearFrom: 2000, YearTo: 2020,
			RuntimeFrom: 60, RuntimeTo: 180, RatingFrom: 6.5, LocalFrom: 7,
		},
		Sort: "rating", Order: "asc", Limit: 30, Cursor: encodeCursor(after), WithFacets: true,
	}
	if _, err := NewSearchService(engine, watchedFake(nil), 0).Films(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	want := search.Query{
		Text: "дюна", Scope: "title",
		Filters: search.Filters{
			Kind: "movie", Genres: []string{"drama"}, YearFrom: 2000, YearTo: 2020,
			RuntimeFrom: 60, RuntimeTo: 180, RatingFrom: 6.5, LocalFrom: 7,
		},
		Sort: "rating", Order: "asc", Limit: 30, After: after, WithFacets: true,
	}
	if !reflect.DeepEqual(engine.lastQuery, want) {
		t.Fatalf("query\n%+v\nwant\n%+v", engine.lastQuery, want)
	}
}

func TestCatalogIsLeanAndSearchIsNot(t *testing.T) {
	t.Parallel()

	engine := &searchEngineFake{}
	s := NewSearchService(engine, watchedFake(nil), 0)
	if _, err := s.Catalog(context.Background(), SearchRequest{}); err != nil || !engine.lastQuery.Lean {
		t.Fatalf("catalog query lean=%v, err %v; want lean", engine.lastQuery.Lean, err)
	}
	if _, err := s.Films(context.Background(), SearchRequest{}); err != nil || engine.lastQuery.Lean {
		t.Fatalf("search query lean=%v, err %v; want full", engine.lastQuery.Lean, err)
	}
}

func TestHideWatchedExcludesWatchedIDs(t *testing.T) {
	t.Parallel()

	engine := &searchEngineFake{}
	s := NewSearchService(engine, watchedFake{3, 9}, 0)
	if _, err := s.Catalog(context.Background(), SearchRequest{HideWatched: true, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if got := engine.lastQuery.Filters.ExcludeIDs; !slices.Equal(got, []uint{3, 9}) {
		t.Fatalf("ExcludeIDs %v, want [3 9]", got)
	}
	if _, err := s.Catalog(context.Background(), SearchRequest{UserID: 1}); err != nil {
		t.Fatal(err)
	}
	if got := engine.lastQuery.Filters.ExcludeIDs; got != nil {
		t.Fatalf("ExcludeIDs %v without hide_watched, want none", got)
	}
}

func TestSearchPageCarriesCursorAndFacets(t *testing.T) {
	t.Parallel()

	engine := &searchEngineFake{films: search.Page[search.FilmDoc]{
		Items:     []search.FilmDoc{{ID: 1, TitleEn: "Dune"}},
		Total:     40,
		NextAfter: []any{float64(1)},
		Facets:    []search.FacetBucket{{Key: "drama", Count: 12}},
	}}
	page, err := NewSearchService(engine, watchedFake(nil), 0).Catalog(context.Background(), SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 40 || len(page.Items) != 1 || page.Items[0].Title != "Dune" {
		t.Fatalf("page %+v", page)
	}
	if after, total := decodeSearchCursor(page.NextCursor); len(after) != 1 || total != 40 {
		t.Fatalf("next_cursor %q", page.NextCursor)
	}
	if !slices.Equal(page.Facets, []Facet{{Key: "drama", Count: 12}}) {
		t.Fatalf("facets %+v", page.Facets)
	}
}

func TestPeopleSearchCopiesEveryField(t *testing.T) {
	t.Parallel()

	engine := &searchEngineFake{people: search.Page[search.PersonDoc]{
		Items: []search.PersonDoc{{
			ID: 4, Name: "Кристофер Нолан", OriginalName: "Christopher Nolan", PhotoURL: "https://p.jpg",
			Roles: []string{"director"}, FilmCount: 12, KnownFor: []string{"Довод"},
		}},
		Total:     1,
		NextAfter: []any{float64(4)},
	}}
	page, err := NewSearchService(engine, watchedFake(nil), 0).People(context.Background(), SearchRequest{Text: "нолан"})
	if err != nil {
		t.Fatal(err)
	}
	want := PersonSearchItem{
		ID: 4, Name: "Кристофер Нолан", OriginalName: "Christopher Nolan", PhotoURL: "https://p.jpg",
		Roles: []string{"director"}, FilmCount: 12, KnownFor: []string{"Довод"},
	}
	if len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], want) {
		t.Fatalf("items %+v, want %+v", page.Items, want)
	}
	if page.Total != 1 || page.NextCursor == "" {
		t.Fatalf("total %d, next_cursor %q", page.Total, page.NextCursor)
	}
}
