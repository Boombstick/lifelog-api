package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lifelog-api/internal/search"
)

type countingEngine struct {
	films, people atomic.Int32
	release       chan struct{}
	page          search.Page[search.FilmDoc]

	mu      sync.Mutex
	queries []search.Query
}

func (e *countingEngine) wait() {
	if e.release != nil {
		<-e.release
	}
}

func (e *countingEngine) SearchFilms(_ context.Context, q search.Query) (search.Page[search.FilmDoc], error) {
	e.films.Add(1)
	e.mu.Lock()
	e.queries = append(e.queries, q)
	e.mu.Unlock()
	e.wait()
	return e.page, nil
}

func (e *countingEngine) SearchPeople(context.Context, search.Query) (search.Page[search.PersonDoc], error) {
	e.people.Add(1)
	e.wait()
	return search.Page[search.PersonDoc]{Total: 1, Items: []search.PersonDoc{{ID: 1, Name: "Кристофер Нолан"}}}, nil
}

func (e *countingEngine) FilmBounds(context.Context) (search.FilmBounds, error) {
	return search.FilmBounds{}, nil
}

func TestCatalogCacheServesRepeatedQuestion(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{page: search.Page[search.FilmDoc]{Total: 3, Items: []search.FilmDoc{{ID: 1}}}}
	s := NewSearchService(engine, watchedFake(nil), time.Minute)
	req := SearchRequest{Filters: FilmFilters{Genres: []string{"drama", "comedy"}}, Sort: "popularity"}
	for range 5 {
		if _, err := s.Catalog(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}

	req.Filters.Genres = []string{"comedy", "drama"}
	if _, err := s.Catalog(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got := engine.films.Load(); got != 1 {
		t.Fatalf("Elasticsearch called %d times, want 1", got)
	}
}

func TestConcurrentSameQuestionReachesSearchOnce(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{release: make(chan struct{})}
	s := NewSearchService(engine, watchedFake(nil), time.Minute)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Catalog(context.Background(), SearchRequest{}); err != nil {
				t.Error(err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(engine.release)
	wg.Wait()
	if got := engine.films.Load(); got != 1 {
		t.Fatalf("Elasticsearch called %d times, want 1", got)
	}
}

func TestHideWatchedBypassesCache(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{}
	s := NewSearchService(engine, watchedFake{4}, time.Minute)
	req := SearchRequest{HideWatched: true, UserID: 7}
	for range 3 {
		if _, err := s.Catalog(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}
	if got := engine.films.Load(); got != 3 {
		t.Fatalf("Elasticsearch called %d times, want 3: personal pages must not be shared", got)
	}
}

func TestDisabledCacheAlwaysSearches(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{}
	s := NewSearchService(engine, watchedFake(nil), 0)
	for range 3 {
		if _, err := s.Films(context.Background(), SearchRequest{Text: "дюна"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := engine.films.Load(); got != 3 {
		t.Fatalf("Elasticsearch called %d times, want 3", got)
	}
}

func TestWaiterLeavesWithoutCancellingSharedLoad(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{release: make(chan struct{})}
	s := NewSearchService(engine, watchedFake(nil), time.Minute)

	gone, cancel := context.WithCancel(context.Background())
	leaver := make(chan error, 1)
	go func() {
		_, err := s.Catalog(gone, SearchRequest{})
		leaver <- err
	}()
	stayer := make(chan error, 1)
	go func() {
		_, err := s.Catalog(context.Background(), SearchRequest{})
		stayer <- err
	}()
	time.Sleep(30 * time.Millisecond)
	cancel()
	if err := <-leaver; !errors.Is(err, context.Canceled) {
		t.Fatalf("leaving caller got %v, want context.Canceled", err)
	}
	close(engine.release)
	if err := <-stayer; err != nil {
		t.Fatalf("the caller who stayed got %v", err)
	}
}

func TestLaterPagesTakeTotalFromCursor(t *testing.T) {
	t.Parallel()

	full := make([]search.FilmDoc, 20)
	engine := &countingEngine{page: search.Page[search.FilmDoc]{Total: 128, Items: full, NextAfter: []any{9.1, float64(40)}}}
	s := NewSearchService(engine, watchedFake(nil), 0)

	first, err := s.Catalog(context.Background(), SearchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 128 || first.NextCursor == "" {
		t.Fatalf("first page total %d cursor %q", first.Total, first.NextCursor)
	}

	engine.page.Total = 0
	second, err := s.Catalog(context.Background(), SearchRequest{Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if second.Total != 128 {
		t.Fatalf("second page total %d, want 128 from the cursor", second.Total)
	}
	engine.mu.Lock()
	q1, q2 := engine.queries[0], engine.queries[1]
	engine.mu.Unlock()
	if q1.SkipTotal || !q2.SkipTotal {
		t.Fatalf("SkipTotal first=%v second=%v, want false then true", q1.SkipTotal, q2.SkipTotal)
	}
	if len(q2.After) != 2 {
		t.Fatalf("search_after not passed on: %v", q2.After)
	}
}

func TestOldArrayCursorStillPagesAndCounts(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{page: search.Page[search.FilmDoc]{Total: 50}}
	s := NewSearchService(engine, watchedFake(nil), 0)
	page, err := s.Catalog(context.Background(), SearchRequest{Cursor: encodeCursor([]any{3.5, float64(8)})})
	if err != nil {
		t.Fatal(err)
	}
	q := engine.queries[0]
	if q.SkipTotal || len(q.After) != 2 || page.Total != 50 {
		t.Fatalf("old cursor: SkipTotal=%v after=%v total=%d", q.SkipTotal, q.After, page.Total)
	}
}

func TestCombinedRunsBothSearchesTogether(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{release: make(chan struct{})}
	s := NewSearchService(engine, watchedFake(nil), 0)
	done := make(chan error, 1)
	go func() {
		_, err := s.Combined(context.Background(), SearchRequest{Text: "нолан"})
		done <- err
	}()
	deadline := time.After(time.Second)
	for engine.films.Load() == 0 || engine.people.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("people search did not start while films search was still running")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(engine.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRatingWriteClearsCachedPages(t *testing.T) {
	t.Parallel()

	engine := &countingEngine{}
	s := NewSearchService(engine, watchedFake(nil), time.Minute)
	if _, err := s.Catalog(context.Background(), SearchRequest{}); err != nil {
		t.Fatal(err)
	}
	ratings := FlushingRatingIndex(noopRatings{}, s)
	if err := ratings.UpdateFilmRating(context.Background(), 1, 8, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog(context.Background(), SearchRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := engine.films.Load(); got != 2 {
		t.Fatalf("Elasticsearch called %d times, want 2: a rating change must not be served from cache", got)
	}
}

type noopRatings struct{}

func (noopRatings) UpdateFilmRating(context.Context, uint, float32, int) error { return nil }
func (noopRatings) UpdateFilmRatings(context.Context, []search.LocalRating) error {
	return nil
}
