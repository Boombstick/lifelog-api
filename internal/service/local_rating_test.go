package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

type update struct {
	filmID uint
	rating float32
	votes  int
}

type fakeRatingIndex struct {
	err   error
	calls chan update

	mu   sync.Mutex
	bulk [][]search.LocalRating
}

func newFakeRatingIndex(err error) *fakeRatingIndex {
	return &fakeRatingIndex{err: err, calls: make(chan update, 8)}
}

func (f *fakeRatingIndex) UpdateFilmRating(_ context.Context, filmID uint, rating float32, votes int) error {
	f.calls <- update{filmID, rating, votes}
	return f.err
}

func (f *fakeRatingIndex) UpdateFilmRatings(_ context.Context, ratings []search.LocalRating) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulk = append(f.bulk, ratings)
	return f.err
}

func (f *fakeRatingIndex) next(t *testing.T) update {
	t.Helper()
	select {
	case u := <-f.calls:
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("no index update")
		return update{}
	}
}

func (f *fakeRatingIndex) none(t *testing.T) {
	t.Helper()
	select {
	case u := <-f.calls:
		t.Fatalf("unexpected index update %+v", u)
	case <-time.After(50 * time.Millisecond):
	}
}

func ratedRepo() *stubFilmRepo {
	return &stubFilmRepo{localRatings: map[uint]film.LocalRating{
		7: {FilmID: 7, Rating: 8.5, Votes: 2},
	}}
}

func TestRatingChangePushesStoredRating(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rate := int16(9)

	for name, act := range map[string]func(*FilmService) error{
		"rate": func(s *FilmService) error {
			return s.AddRateAndDescription(ctx, 7, 1, &CreateReviewRequest{Rate: &rate})
		},
		"remove review": func(s *FilmService) error { return s.RemoveReview(ctx, 7, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			index := newFakeRatingIndex(nil)
			svc := NewFilmService(ratedRepo(), nil, nil, nil)
			svc.SetRatingIndex(index)

			if err := act(svc); err != nil {
				t.Fatalf("call: %v", err)
			}

			if got, want := index.next(t), (update{7, 8.5, 2}); got != want {
				t.Errorf("update = %+v, want %+v", got, want)
			}
			index.none(t)
		})
	}
}

func TestDescriptionOnlyDoesNotPush(t *testing.T) {
	t.Parallel()
	index := newFakeRatingIndex(nil)
	svc := NewFilmService(ratedRepo(), nil, nil, nil)
	svc.SetRatingIndex(index)

	text := "ok"
	if err := svc.AddRateAndDescription(context.Background(), 7, 1, &CreateReviewRequest{Description: &text}); err != nil {
		t.Fatalf("call: %v", err)
	}
	index.none(t)
}

func TestIndexFailureDoesNotFailRating(t *testing.T) {
	t.Parallel()
	index := newFakeRatingIndex(errors.New("cluster down"))
	svc := NewFilmService(ratedRepo(), nil, nil, nil)
	svc.SetRatingIndex(index)

	rate := int16(9)
	if err := svc.AddRateAndDescription(context.Background(), 7, 1, &CreateReviewRequest{Rate: &rate}); err != nil {
		t.Fatalf("index failure leaked into the call: %v", err)
	}
	index.next(t)
}

func TestSyncLocalRatingsBatches(t *testing.T) {
	t.Parallel()
	repo := &stubFilmRepo{localRatings: map[uint]film.LocalRating{}}
	ids := make([]uint, 0, ratingSyncBatchSize+1)
	for id := uint(1); id <= ratingSyncBatchSize+1; id++ {
		repo.localRatings[id] = film.LocalRating{FilmID: id, Rating: 7, Votes: 1}
		ids = append(ids, id)
	}
	index := newFakeRatingIndex(nil)
	svc := NewFilmService(repo, nil, nil, nil)
	svc.SetRatingIndex(index)

	if err := svc.SyncLocalRatings(context.Background(), ids); err != nil {
		t.Fatalf("sync: %v", err)
	}
	sizes := make([]int, 0, len(index.bulk))
	for _, batch := range index.bulk {
		sizes = append(sizes, len(batch))
	}
	if !slices.Equal(sizes, []int{ratingSyncBatchSize, 1}) {
		t.Errorf("batch sizes = %v", sizes)
	}
}
