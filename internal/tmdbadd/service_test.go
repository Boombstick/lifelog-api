package tmdbadd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/safedial"
	"lifelog-api/internal/tmdb"
)

type fake struct {
	page       tmdb.SearchPage
	searchErr  error
	cards      map[CardKey]uint
	nextID     uint
	used       int64
	importErr  error
	imported   []CardKey
	claimOK    bool
	localized  []uint
	indexed    []uint
	refreshed  int
	keysLookup [][]CardKey

	usedSince  time.Time
	claimSince time.Time

	raceCard bool

	importing atomic.Int32
	overlap   atomic.Bool
}

func (f *fake) SearchMulti(context.Context, string, int) (tmdb.SearchPage, error) {
	return f.page, f.searchErr
}

func (f *fake) PosterURLSize(path, size string) string {
	if path == "" {
		return ""
	}
	return "https://img/" + size + path
}

func (f *fake) FilmIDsByTMDB(_ context.Context, keys []CardKey) (map[CardKey]uint, error) {
	f.keysLookup = append(f.keysLookup, keys)
	out := map[CardKey]uint{}
	for _, key := range keys {
		if id, ok := f.cards[key]; ok {
			out[key] = id
		}
	}
	return out, nil
}

func (f *fake) FilmByTMDBID(_ context.Context, kind film.Kind, tmdbID int) (uint, bool, error) {
	id, ok := f.cards[CardKey{kind, tmdbID}]
	return id, ok, nil
}

func (f *fake) ManualAddsSince(_ context.Context, _ uint, since time.Time) (int64, error) {
	f.usedSince = since
	return f.used, nil
}

func (f *fake) ClaimManualAdd(_ context.Context, _, _ uint, createdSince, _ time.Time) (bool, error) {
	f.claimSince = createdSince
	if f.claimOK {
		f.used++
	}
	return f.claimOK, nil
}

func (f *fake) ImportOne(ctx context.Context, kind film.Kind, tmdbID int) error {
	if f.importing.Add(1) > 1 {
		f.overlap.Store(true)
	}

	time.Sleep(5 * time.Millisecond)
	f.importing.Add(-1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.importErr != nil {
		if f.raceCard {
			f.cards[CardKey{kind, tmdbID}] = 77
		}
		return f.importErr
	}
	f.imported = append(f.imported, CardKey{kind, tmdbID})
	f.nextID++
	f.cards[CardKey{kind, tmdbID}] = f.nextID
	return nil
}

func (f *fake) Localize(_ context.Context, id uint) error {
	f.localized = append(f.localized, id)
	return nil
}

func (f *fake) IndexCards(_ context.Context, ids []uint) error {
	f.indexed = append(f.indexed, ids...)
	return nil
}

func (f *fake) Refresh(context.Context) error {
	f.refreshed++
	return nil
}

func newFake() (*fake, *Service) {
	f := &fake{cards: map[CardKey]uint{}, nextID: 500, claimOK: true}
	return f, NewService(Deps{Searcher: f, Catalog: f, Fetcher: f, Localizer: f, Indexer: f, Refresher: f}, nil)
}

func TestSearchMapsItems(t *testing.T) {
	f, svc := newFake()
	f.cards[CardKey{film.KindSeries, 90228}] = 7
	f.page = tmdb.SearchPage{Page: 1, TotalPages: 57, Items: []tmdb.SearchItem{
		{ID: 693134, MediaType: "movie", Title: "", OriginalTitle: "Dune: Part Two",
			Date: tmdb.Date{Time: time.Date(2024, 2, 27, 0, 0, 0, 0, time.UTC)}, PosterPath: "/p.jpg", Overview: " ", VoteCount: 6200, VoteAverage: 8.2},
		{ID: 90228, MediaType: "tv", Title: "Дюна: Пророчество", OriginalTitle: "Dune: Prophecy"},
	}}

	resp, err := svc.Search(context.Background(), "  Дюна ", 1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.TotalPages != MaxPage || len(resp.Items) != 2 || len(f.keysLookup) != 1 {
		t.Fatalf("resp %+v lookups %d", resp, len(f.keysLookup))
	}
	movie, series := resp.Items[0], resp.Items[1]
	if movie.Kind != "movie" || movie.Title != "Dune: Part Two" || movie.Year == nil || *movie.Year != 2024 ||
		movie.PosterURL == nil || *movie.PosterURL != "https://img/w342/p.jpg" || movie.Overview != nil || movie.CatalogID != nil {
		t.Errorf("movie %+v", movie)
	}
	if series.Kind != "series" || series.Year != nil || series.PosterURL != nil || series.CatalogID == nil || *series.CatalogID != 7 {
		t.Errorf("series %+v", series)
	}
}

func TestSearchValidation(t *testing.T) {
	_, svc := newFake()
	cases := []struct {
		query string
		page  int
		want  error
	}{
		{" я ", 1, ErrQuery},
		{"\xff\xfeДюна", 1, ErrQuery},
		{strings.Repeat("я", 101), 1, ErrQuery},
		{"Дюна", 0, ErrPage},
		{"Дюна", 21, ErrPage},
	}
	for _, c := range cases {
		if _, err := svc.Search(context.Background(), c.query, c.page); !errors.Is(err, c.want) {
			t.Errorf("%q page %d: %v, want %v", c.query, c.page, err, c.want)
		}
	}
}

func TestSearchUnavailable(t *testing.T) {
	f, svc := newFake()
	for _, cause := range []error{
		&tmdb.APIError{StatusCode: 503},
		fmt.Errorf("wrapped: %w", safedial.ErrUnresolved),
		&url.Error{Op: "Get", URL: "x", Err: errors.New("connection refused")},
	} {
		f.searchErr = cause
		if _, err := svc.Search(context.Background(), "Дюна", 1); !errors.Is(err, ErrUnavailable) {
			t.Errorf("%v: got %v", cause, err)
		}
	}
	noToken := NewService(Deps{Catalog: f}, nil)
	if _, err := noToken.Search(context.Background(), "Дюна", 1); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no token: %v", err)
	}
}

func TestAddExistingSpendsNothing(t *testing.T) {
	f, svc := newFake()
	f.cards[CardKey{film.KindMovie, 1}] = 42
	f.used = DailyLimit
	resp, err := svc.Add(context.Background(), 9, film.KindMovie, 1)
	if err != nil || resp != (AddResponse{ID: 42, Created: false}) || len(f.imported) != 0 {
		t.Fatalf("resp %+v err %v imported %v", resp, err, f.imported)
	}
}

func TestAddCreates(t *testing.T) {
	f, svc := newFake()
	f.used = DailyLimit - 1
	resp, err := svc.Add(context.Background(), 9, film.KindSeries, 90228)
	if err != nil || !resp.Created || resp.ID != 501 {
		t.Fatalf("resp %+v err %v", resp, err)
	}
	if len(f.localized) != 1 || len(f.indexed) != 1 || f.refreshed != 1 {
		t.Errorf("localized %v indexed %v refreshed %d", f.localized, f.indexed, f.refreshed)
	}
}

func TestAddLostRaceIsNotCreated(t *testing.T) {
	f, svc := newFake()
	f.claimOK = false
	resp, err := svc.Add(context.Background(), 9, film.KindMovie, 3)
	if err != nil || resp.Created || resp.ID == 0 || len(f.indexed) != 0 {
		t.Fatalf("resp %+v err %v indexed %v", resp, err, f.indexed)
	}
}

func TestAddErrors(t *testing.T) {
	cases := []struct {
		name      string
		used      int64
		importErr error
		want      error
	}{
		{"limit", DailyLimit, nil, ErrLimit},
		{"not found", 0, tmdb.ErrNotFound, ErrNotFound},
		{"adult", 0, importer.ErrAdult, ErrAdult},
		{"tmdb down", 0, fmt.Errorf("синхронизация жанров: %w", &tmdb.APIError{StatusCode: 502}), ErrUnavailable},
	}
	for _, c := range cases {
		f, svc := newFake()
		f.used, f.importErr = c.used, c.importErr
		if _, err := svc.Add(context.Background(), 9, film.KindMovie, 3); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}

	f, svc := newFake()
	f.importErr = errors.New("duplicate key")
	if _, err := svc.Add(context.Background(), 9, film.KindMovie, 3); err == nil || errors.Is(err, ErrUnavailable) {
		t.Errorf("storage error mapped to %v", err)
	}
}

func TestParseKind(t *testing.T) {
	for raw, ok := range map[string]bool{"movie": true, "series": true, "tv": false, "": false} {
		if _, err := ParseKind(raw); (err == nil) != ok {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestAddLimitHoldsUnderParallelAdds(t *testing.T) {
	f, svc := newFake()
	f.used = DailyLimit - 1

	const parallel = 5
	errs := make(chan error, parallel)
	for i := range parallel {
		go func() {
			_, err := svc.Add(context.Background(), 9, film.KindMovie, 1000+i)
			errs <- err
		}()
	}
	created, limited := 0, 0
	for range parallel {
		switch err := <-errs; {
		case err == nil:
			created++
		case errors.Is(err, ErrLimit):
			limited++
		default:
			t.Fatalf("unexpected error %v", err)
		}
	}
	if created != 1 || limited != parallel-1 {
		t.Fatalf("created %d, limited %d; want 1 and %d", created, limited, parallel-1)
	}
	if f.overlap.Load() {
		t.Fatal("two adds of one user imported at the same time")
	}
}

func TestAddAsksForTheRightWindows(t *testing.T) {
	f, svc := newFake()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.Add(context.Background(), 9, film.KindMovie, 3); err != nil {
		t.Fatal(err)
	}
	if !f.usedSince.Equal(now.Add(-24 * time.Hour)) {
		t.Errorf("limit counted since %v, want a day back", f.usedSince)
	}

	if !f.claimSince.Equal(now) {
		t.Errorf("claim since %v, want the request start", f.claimSince)
	}
}

func TestAddSurvivesCancelledRequest(t *testing.T) {
	f, svc := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp, err := svc.Add(ctx, 9, film.KindMovie, 3)
	if err != nil || !resp.Created || len(f.indexed) != 1 {
		t.Fatalf("resp %+v err %v indexed %v", resp, err, f.indexed)
	}
}

func TestAddFailedImportOfRacedCardIsNotAnError(t *testing.T) {
	f, svc := newFake()
	f.importErr = errors.New("deadlock detected")
	f.raceCard = true
	resp, err := svc.Add(context.Background(), 9, film.KindMovie, 3)
	if err != nil || resp != (AddResponse{ID: 77, Created: false}) {
		t.Fatalf("resp %+v err %v", resp, err)
	}
}

func TestAddRefusesZeroID(t *testing.T) {
	_, svc := newFake()
	if _, err := svc.Add(context.Background(), 9, film.KindMovie, 0); !errors.Is(err, ErrTMDBID) {
		t.Fatalf("got %v, want ErrTMDBID", err)
	}
}
