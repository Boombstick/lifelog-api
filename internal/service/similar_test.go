package service

import (
	"context"
	"errors"
	"testing"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

type fakeRecommendRepo struct {
	similar []film.Film
	limits  []int
}

func (r *fakeRecommendRepo) RecommendedFilms(_ context.Context, _ uint, limit int) ([]film.Film, error) {
	r.limits = append(r.limits, limit)
	return r.similar, nil
}

func (r *fakeRecommendRepo) RatedFilms(context.Context, uint, int16, int, film.Kind) ([]film.Film, error) {
	return nil, nil
}

func (r *fakeRecommendRepo) SeenFilmIDs(context.Context, uint) ([]uint, error) { return nil, nil }

type fakeSimilarEngine struct {
	docs   []search.FilmDoc
	limits []int
	err    error
}

func (e *fakeSimilarEngine) SimilarTo(_ context.Context, _ uint, limit int) (search.Page[search.FilmDoc], error) {
	e.limits = append(e.limits, limit)
	return search.Page[search.FilmDoc]{Items: e.docs}, e.err
}

func (e *fakeSimilarEngine) RecommendFor(context.Context, search.TasteProfile, int) (search.Page[search.FilmDoc], error) {
	return search.Page[search.FilmDoc]{}, nil
}

func TestSimilarFallsBackToOurOwnPicksAndSaysSo(t *testing.T) {
	t.Parallel()

	repo := &fakeRecommendRepo{}
	engine := &fakeSimilarEngine{docs: []search.FilmDoc{{ID: 5, TitleSort: "Довод"}}}
	got, err := NewRecommendService(repo, engine).Similar(context.Background(), 1, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceOwn {
		t.Errorf("source = %q, want %q: TMDB returned nothing", got.Source, SourceOwn)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}
}

func TestSimilarPrefersTheTMDBList(t *testing.T) {
	t.Parallel()

	repo := &fakeRecommendRepo{similar: []film.Film{{ID: 2}, {ID: 3}}}
	engine := &fakeSimilarEngine{}
	got, err := NewRecommendService(repo, engine).Similar(context.Background(), 1, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceTMDB || len(got.Items) != 2 {
		t.Fatalf("got %+v", got)
	}
	if len(engine.limits) != 0 {
		t.Error("the index was queried although TMDB had an answer")
	}
}

func TestSimilarOwnSourceSkipsTMDB(t *testing.T) {
	t.Parallel()

	repo := &fakeRecommendRepo{similar: []film.Film{{ID: 2}}}
	engine := &fakeSimilarEngine{docs: []search.FilmDoc{{ID: 9}}}
	got, err := NewRecommendService(repo, engine).Similar(context.Background(), 1, SourceOwn, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceOwn || len(repo.limits) != 0 {
		t.Fatalf("got %+v, TMDB calls %v", got, repo.limits)
	}
	if _, err := NewRecommendService(repo, engine).Similar(context.Background(), 1, "netflix", 10); !errors.Is(err, ErrUnknownSource) {
		t.Errorf("unknown source: want ErrUnknownSource, got %v", err)
	}
}

func TestSimilarClampsTheLimit(t *testing.T) {
	t.Parallel()

	for name, given := range map[string]int{"missing": 0, "negative": -1, "over the ceiling": 500} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRecommendRepo{similar: []film.Film{{ID: 2}}}
			if _, err := NewRecommendService(repo, &fakeSimilarEngine{}).Similar(context.Background(), 1, "", given); err != nil {
				t.Fatal(err)
			}
			if len(repo.limits) != 1 || repo.limits[0] != 12 {
				t.Errorf("storage was asked for %v, want the default 12", repo.limits)
			}
		})
	}

	repo := &fakeRecommendRepo{similar: []film.Film{{ID: 2}}}
	if _, err := NewRecommendService(repo, &fakeSimilarEngine{}).Similar(context.Background(), 1, "", 30); err != nil {
		t.Fatal(err)
	}
	if repo.limits[0] != 30 {
		t.Errorf("limit = %d, want 30", repo.limits[0])
	}
}
