package imdbimport

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdb"
)

type fakeWorld struct {
	mu sync.Mutex

	cards  map[uint]fakeCard
	nextID uint

	find      map[string]tmdb.FindResult
	findCalls []string
	fetched   []int
	indexed   []uint
	localized []uint
	synced    []uint

	snapshots    map[uint]Snapshot
	seasons      map[uint][]film.Season
	rated        map[uint]int16
	seasonsRated map[uint][]uint
	watchlist    map[uint]time.Time

	jobs map[uint]Job
}

type fakeCard struct {
	kind   film.Kind
	tmdbID int
	imdbID string
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{
		cards:        map[uint]fakeCard{},
		nextID:       100,
		find:         map[string]tmdb.FindResult{},
		snapshots:    map[uint]Snapshot{},
		seasons:      map[uint][]film.Season{},
		rated:        map[uint]int16{},
		seasonsRated: map[uint][]uint{},
		watchlist:    map[uint]time.Time{},
		jobs:         map[uint]Job{},
	}
}

func (w *fakeWorld) FindByIMDbID(_ context.Context, id string) (tmdb.FindResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.findCalls = append(w.findCalls, id)
	return w.find[id], nil
}

func (w *fakeWorld) FilmByIMDbID(_ context.Context, imdbID string) (CardRef, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, card := range w.cards {
		if card.imdbID == imdbID {
			return CardRef{ID: id, Kind: card.kind}, true, nil
		}
	}
	return CardRef{}, false, nil
}

func (w *fakeWorld) FilmByTMDBID(_ context.Context, kind film.Kind, tmdbID int) (uint, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id, card := range w.cards {
		if card.kind == kind && card.tmdbID == tmdbID {
			return id, true, nil
		}
	}
	return 0, false, nil
}

func (w *fakeWorld) ImportOne(_ context.Context, kind film.Kind, tmdbID int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if tmdbID == 404 {
		return tmdb.ErrNotFound
	}
	w.fetched = append(w.fetched, tmdbID)
	w.nextID++
	w.cards[w.nextID] = fakeCard{kind: kind, tmdbID: tmdbID}
	if kind == film.KindSeries {
		aired := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
		w.seasons[w.nextID] = []film.Season{
			{ID: 1, Number: 0, AirDate: &aired},
			{ID: 2, Number: 1, AirDate: &aired},
			{ID: 3, Number: 2, AirDate: &aired},
			{ID: 4, Number: 3},
		}
	}
	return nil
}

func (w *fakeWorld) Localize(_ context.Context, filmID uint) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.localized = append(w.localized, filmID)
	return nil
}

func (w *fakeWorld) IndexCards(_ context.Context, ids []uint) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.indexed = append(w.indexed, ids...)
	return nil
}

func (w *fakeWorld) MarkSnapshot(_ context.Context, _, filmID uint) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshots[filmID], nil
}

func (w *fakeWorld) Seasons(_ context.Context, filmID uint) ([]film.Season, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seasons[filmID], nil
}

func (w *fakeWorld) RateWatchedAt(_ context.Context, filmID, _ uint, rate int16, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.rated[filmID] = rate
	s := w.snapshots[filmID]
	s.Watched, s.Rated, s.Watchlisted = true, true, false
	w.snapshots[filmID] = s
	return nil
}

func (w *fakeWorld) MarkSeasonsWatchedRated(_ context.Context, _, filmID uint, ids []uint, _ int16, _ time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seasonsRated[filmID] = ids
	s := w.snapshots[filmID]
	s.SeasonEntries += len(ids)
	w.snapshots[filmID] = s
	return nil
}

func (w *fakeWorld) AddToWatchListAt(_ context.Context, filmID, _ uint, at time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watchlist[filmID] = at
	s := w.snapshots[filmID]
	s.Watchlisted = true
	w.snapshots[filmID] = s
	return nil
}

func (w *fakeWorld) CreateJob(_ context.Context, job *Job) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, existing := range w.jobs {
		if existing.UserID == job.UserID && (existing.Status == StatusQueued || existing.Status == StatusRunning) {
			return ErrImportRunning
		}
	}
	job.ID = uint(len(w.jobs) + 1)
	job.CreatedAt = time.Now()
	w.jobs[job.ID] = *job
	return nil
}

func (w *fakeWorld) HasActiveJob(_ context.Context, userID uint) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, existing := range w.jobs {
		if existing.UserID == userID && (existing.Status == StatusQueued || existing.Status == StatusRunning) {
			return true, nil
		}
	}
	return false, nil
}

func (w *fakeWorld) SaveJob(_ context.Context, job Job) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.jobs[job.ID] = job
	return nil
}

func (w *fakeWorld) Job(_ context.Context, userID, id uint) (*Job, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	job, ok := w.jobs[id]
	if !ok || job.UserID != userID {
		return nil, ErrJobNotFound
	}
	return &job, nil
}

func (w *fakeWorld) RecentJobs(context.Context, uint, int) ([]Job, error) { return nil, nil }

func (w *fakeWorld) FailUnfinished(context.Context, string) (int64, error) { return 0, nil }

func newTestService(w *fakeWorld) *Service {
	return NewService(Deps{
		Finder: w, Catalog: w, Fetcher: w, Localizer: w, Indexer: w, Marks: w, Store: w, Ratings: w,
	}, nil)
}

func waitDone(t *testing.T, svc *Service, userID, id uint) JobResponse {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := svc.Get(context.Background(), userID, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == StatusDone || job.Status == StatusFailed {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return JobResponse{}
}

const serviceRatings = `Const,Your Rating,Date Rated,Title,Title Type,Year
tt0816692,9,2024-03-01,Interstellar,Movie,2014
tt1375666,8,2024-03-02,Inception,Movie,2010
tt0903747,10,2024-03-03,Breaking Bad,TV Series,2008
tt0959621,8,2024-03-04,Pilot,TV Episode,2008
tt0000000,5,2024-03-05,Nothing,Movie,1999
tt0000404,5,2024-03-06,Deleted,Movie,1998
bogus,5,2024-03-07,Broken,Movie,1997
`

func TestImportRatingsEndToEnd(t *testing.T) {
	t.Parallel()

	w := newFakeWorld()

	w.cards[1] = fakeCard{kind: film.KindMovie, tmdbID: 157336, imdbID: "tt0816692"}
	w.snapshots[1] = Snapshot{Watched: true, Rated: true}
	w.find["tt1375666"] = tmdb.FindResult{MovieIDs: []int{27205}}
	w.find["tt0903747"] = tmdb.FindResult{TVIDs: []int{1396}}
	w.find["tt0000404"] = tmdb.FindResult{MovieIDs: []int{404}}

	svc := newTestService(w)
	started, err := svc.Start(context.Background(), 7, strings.NewReader(serviceRatings))
	if err != nil {
		t.Fatal(err)
	}
	if started.Kind != ListRatings || started.Total != 7 || started.Status != StatusQueued {
		t.Fatalf("started %+v", started)
	}
	job := waitDone(t, svc, 7, started.ID)

	want := JobResponse{Status: StatusDone, Total: 7, Processed: 7, Added: 2, SkippedExisting: 1, NotFound: 2, Unsupported: 1, Failed: 1, CatalogAdded: 2}
	if job.Status != want.Status || job.Processed != want.Processed || job.Added != want.Added ||
		job.SkippedExisting != want.SkippedExisting || job.NotFound != want.NotFound ||
		job.Unsupported != want.Unsupported || job.Failed != want.Failed || job.CatalogAdded != want.CatalogAdded {
		t.Errorf("job %+v", job)
	}
	if job.FinishedAt == nil || job.Error != nil {
		t.Errorf("finished %v error %v", job.FinishedAt, job.Error)
	}
	if job.NotFoundItems == nil || len(*job.NotFoundItems) != 2 || (*job.NotFoundItems)[0].IMDbID != "tt0000000" || *(*job.NotFoundItems)[0].Year != 1999 {
		t.Errorf("not found items %+v", job.NotFoundItems)
	}

	for _, id := range w.findCalls {
		if id == "tt0816692" || id == "tt0959621" || id == "bogus" {
			t.Errorf("unexpected find for %s", id)
		}
	}
	if len(w.indexed) != 2 || len(w.localized) != 2 {
		t.Errorf("indexed %v localized %v", w.indexed, w.localized)
	}
	if w.rated[1] != 0 {
		t.Error("a hand-made rating must be kept")
	}

	var seriesID uint
	for id, card := range w.cards {
		if card.kind == film.KindSeries {
			seriesID = id
		}
	}

	if got := w.seasonsRated[seriesID]; len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("series seasons rated %v", got)
	}

	w.mu.Lock()
	synced := slices.Clone(w.synced)
	w.mu.Unlock()
	slices.Sort(synced)
	if len(synced) != 2 || !slices.Contains(synced, seriesID) || slices.Contains(synced, 1) {
		t.Errorf("ratings synced for %v", synced)
	}

	again, err := svc.Start(context.Background(), 7, strings.NewReader(serviceRatings))
	if err != nil {
		t.Fatal(err)
	}
	second := waitDone(t, svc, 7, again.ID)
	if second.Added != 0 || second.SkippedExisting != 3 || second.CatalogAdded != 0 {
		t.Errorf("second run %+v", second)
	}
}

func TestImportWatchlistDatesAndSkips(t *testing.T) {
	t.Parallel()

	w := newFakeWorld()
	w.cards[1] = fakeCard{kind: film.KindMovie, tmdbID: 27205, imdbID: "tt1375666"}
	w.cards[2] = fakeCard{kind: film.KindSeries, tmdbID: 1399, imdbID: "tt0944947"}
	w.snapshots[2] = Snapshot{SeasonEntries: 1}

	svc := newTestService(w)
	started, err := svc.Start(context.Background(), 3, strings.NewReader(watchlistExport))
	if err != nil {
		t.Fatal(err)
	}
	job := waitDone(t, svc, 3, started.ID)
	if job.Kind != ListWatchlist || job.Added != 1 || job.SkippedExisting != 1 {
		t.Errorf("job %+v", job)
	}
	if at := w.watchlist[1]; at.Format(dateLayout) != "2021-01-15" {
		t.Errorf("watchlist date %v", at)
	}
	if w.rated[1] != 0 {
		t.Error("a watchlist file must not rate")
	}
	if len(w.synced) != 0 {
		t.Errorf("a watchlist file must not push ratings, got %v", w.synced)
	}
}

func TestImportRejections(t *testing.T) {
	t.Parallel()

	w := newFakeWorld()
	svc := newTestService(w)
	if _, err := svc.Start(context.Background(), 1, strings.NewReader("Name,Year\nHeat,1995\n")); !errors.Is(err, ErrNotIMDb) {
		t.Errorf("err %v", err)
	}

	w.jobs[9] = Job{ID: 9, UserID: 1, Status: StatusRunning}
	if _, err := svc.Start(context.Background(), 1, strings.NewReader(serviceRatings)); !errors.Is(err, ErrImportRunning) {
		t.Errorf("err %v, want ErrImportRunning", err)
	}
	if _, err := svc.Get(context.Background(), 2, 9); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("someone else's job: %v", err)
	}

	noTMDB := NewService(Deps{Catalog: w, Marks: w, Store: w}, nil)
	if _, err := noTMDB.Start(context.Background(), 5, strings.NewReader(serviceRatings)); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err %v, want ErrUnavailable", err)
	}
}

func (w *fakeWorld) SyncLocalRatings(_ context.Context, ids []uint) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.synced = append(w.synced, ids...)
	return nil
}
