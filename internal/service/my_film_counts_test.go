package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"lifelog-api/internal/film"
)

func TestMyFilmCountsMapsEverySet(t *testing.T) {
	t.Parallel()

	repo := &stubFilmRepo{counts: film.EntryCounts{Watchlist: 4, Watched: 7, Rated: 5, Watching: 2}}

	got, err := NewFilmService(repo, nil, nil, nil).MyFilmCounts(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastFilter.UserID != 42 {
		t.Errorf("counted for user %d, want 42", repo.lastFilter.UserID)
	}
	want := MyFilmCounts{Watchlist: 4, Watched: 7, Rated: 5, Watching: 2}
	if got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}

	body, err := json.Marshal(MyFilmCounts{})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"watchlist":0,"watched":0,"rated":0,"watching":0}` {
		t.Errorf("body = %s", body)
	}
}

func TestMyFilmCountsWrapsStorageError(t *testing.T) {
	t.Parallel()

	failure := errors.New("connection reset")
	repo := &stubFilmRepo{countsErr: failure}

	_, err := NewFilmService(repo, nil, nil, nil).MyFilmCounts(context.Background(), 1)
	if !errors.Is(err, failure) {
		t.Errorf("err = %v, want it to wrap the storage error", err)
	}
}
