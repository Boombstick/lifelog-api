package importer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"lifelog-api/internal/film"
)

type fakeChanges struct {
	pages [][]int
	asked []int
	kind  film.Kind
	win   time.Duration
}

func (f *fakeChanges) ChangedIDs(_ context.Context, kind film.Kind, start, end time.Time, page int) ([]int, int, error) {
	f.asked = append(f.asked, page)
	f.kind = kind
	f.win = end.Sub(start)
	if page < 1 || page > len(f.pages) {
		return nil, len(f.pages), nil
	}
	return f.pages[page-1], len(f.pages), nil
}

type syncStore struct {
	held      map[int]struct{}
	heldErr   error
	requested []int
}

func (s *syncStore) UpsertGenres(context.Context, []film.Genre) error { return nil }
func (s *syncStore) GenreSlugsByTMDBID(context.Context) (map[int]string, error) {
	return map[int]string{}, nil
}
func (s *syncStore) ExistingFilmTMDBIDs(context.Context, film.Kind) (map[int]struct{}, error) {
	return s.held, s.heldErr
}
func (s *syncStore) SaveFilms(context.Context, []PreparedFilm) error { return nil }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func held(ids ...int) map[int]struct{} {
	out := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func TestSyncFetchesOnlyCardsWeHold(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{pages: [][]int{{1, 2, 3}, {4, 5, 2}}}
	store := &syncStore{held: held(2, 5, 999)}
	syncer := NewSyncer(changes, New(nil, store, quietLogger()), store, quietLogger())

	stats, err := syncer.Run(context.Background(), SyncOptions{Kind: film.KindSeries, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Changed != 6 {
		t.Errorf("changed=%d, want every id the feed reported", stats.Changed)
	}

	if stats.Ours != 2 {
		t.Errorf("ours=%d, want 2 (deduplicated, held only)", stats.Ours)
	}
	if len(changes.asked) != 2 {
		t.Errorf("asked pages %v, want both pages walked", changes.asked)
	}
	if changes.kind != film.KindSeries {
		t.Errorf("kind=%q did not reach the change feed", changes.kind)
	}
}

func TestSyncDefaultsToOneDay(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{pages: [][]int{{7}}}
	store := &syncStore{held: held(7)}
	syncer := NewSyncer(changes, New(nil, store, quietLogger()), store, quietLogger())

	if _, err := syncer.Run(context.Background(), SyncOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if changes.win < 23*time.Hour || changes.win > 25*time.Hour {
		t.Errorf("window %v, want about 24h by default", changes.win)
	}
}

func TestSyncRejectsTooWideWindow(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{pages: [][]int{{1}}}
	store := &syncStore{held: held(1)}
	syncer := NewSyncer(changes, New(nil, store, quietLogger()), store, quietLogger())

	if _, err := syncer.Run(context.Background(), SyncOptions{Since: 15 * 24 * time.Hour}); err == nil {
		t.Fatal("a window wider than 14 days must be refused")
	}
	if len(changes.asked) != 0 {
		t.Errorf("asked %v, want no request at all", changes.asked)
	}
}

func TestSyncStopsOnStorageError(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{pages: [][]int{{1}}}
	store := &syncStore{heldErr: errors.New("database is down")}
	syncer := NewSyncer(changes, New(nil, store, quietLogger()), store, quietLogger())

	if _, err := syncer.Run(context.Background(), SyncOptions{}); err == nil {
		t.Fatal("an unreadable catalogue must stop the sync")
	}
	if len(changes.asked) != 0 {
		t.Errorf("asked %v, want no request before the catalogue is known", changes.asked)
	}
}

func TestSyncSkipsEmptyCatalogue(t *testing.T) {
	t.Parallel()

	changes := &fakeChanges{pages: [][]int{{1, 2}}}
	store := &syncStore{held: map[int]struct{}{}}
	syncer := NewSyncer(changes, New(nil, store, quietLogger()), store, quietLogger())

	stats, err := syncer.Run(context.Background(), SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Changed != 0 || stats.Ours != 0 || len(changes.asked) != 0 {
		t.Errorf("stats %+v, asked %v: an empty catalogue must ask nothing", stats, changes.asked)
	}
}
