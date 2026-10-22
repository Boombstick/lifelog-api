package imdbimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/tmdb"
)

var (
	ErrImportRunning = errors.New("импорт уже идёт")
	ErrJobNotFound   = errors.New("импорт не найден")
	ErrUnavailable   = errors.New("импорт недоступен: не задан токен TMDB")
	ErrShuttingDown  = errors.New("сервис останавливается, запустите импорт позже")
)

type Finder interface {
	FindByIMDbID(ctx context.Context, imdbID string) (tmdb.FindResult, error)
}

type CardRef struct {
	ID   uint
	Kind film.Kind
}

type Catalog interface {
	FilmByIMDbID(ctx context.Context, imdbID string) (CardRef, bool, error)
	FilmByTMDBID(ctx context.Context, kind film.Kind, tmdbID int) (uint, bool, error)
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

type Marks interface {
	MarkSnapshot(ctx context.Context, userID, filmID uint) (Snapshot, error)
	Seasons(ctx context.Context, filmID uint) ([]film.Season, error)
	RateWatchedAt(ctx context.Context, filmID, userID uint, rate int16, watchedAt time.Time) error
	MarkSeasonsWatchedRated(ctx context.Context, userID, filmID uint, seasonIDs []uint, rate int16, watchedAt time.Time) error
	AddToWatchListAt(ctx context.Context, filmID, userID uint, addedAt time.Time) error
}

type RatingSync interface {
	SyncLocalRatings(ctx context.Context, filmIDs []uint) error
}

type Store interface {
	CreateJob(ctx context.Context, job *Job) error

	HasActiveJob(ctx context.Context, userID uint) (bool, error)
	SaveJob(ctx context.Context, job Job) error

	Job(ctx context.Context, userID, id uint) (*Job, error)
	RecentJobs(ctx context.Context, userID uint, limit int) ([]Job, error)
	FailUnfinished(ctx context.Context, message string) (int64, error)
}

type Deps struct {
	Finder    Finder
	Catalog   Catalog
	Fetcher   CardFetcher
	Localizer CardLocalizer
	Indexer   CardIndexer
	Marks     Marks
	Store     Store

	Ratings RatingSync
}

const (
	recentJobs        = 10
	saveEveryRows     = 25
	saveEveryElapsed  = 2 * time.Second
	persistTimeout    = 5 * time.Second
	ratingSyncTimeout = 30 * time.Second
)

type Service struct {
	deps   Deps
	logger *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func NewService(deps Deps, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{deps: deps, logger: logger, ctx: ctx, cancel: cancel}
}

func (s *Service) Recover(ctx context.Context) error {
	count, err := s.deps.Store.FailUnfinished(ctx, InterruptedMessage)
	if err != nil {
		return fmt.Errorf("imdb import: закрытие прерванных задач: %w", err)
	}
	if count > 0 {
		s.logger.Warn("imdb import jobs interrupted by restart", "count", count)
	}
	return nil
}

func (s *Service) Shutdown(timeout time.Duration) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		s.logger.Warn("imdb import jobs did not stop in time")
	}
}

func (s *Service) Start(ctx context.Context, userID uint, file io.Reader) (JobResponse, error) {
	if s.deps.Finder == nil {
		return JobResponse{}, ErrUnavailable
	}
	kind, rows, err := Parse(file)
	if err != nil {
		return JobResponse{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return JobResponse{}, ErrShuttingDown
	}
	active, err := s.deps.Store.HasActiveJob(ctx, userID)
	if err != nil {
		return JobResponse{}, err
	}
	if active {
		return JobResponse{}, ErrImportRunning
	}

	job := Job{
		UserID: userID,
		Source: SourceIMDb,
		Kind:   kind,
		Status: StatusQueued,
		Total:  len(rows),
	}
	if err := s.deps.Store.CreateJob(ctx, &job); err != nil {
		return JobResponse{}, err
	}

	s.wg.Add(1)
	go s.run(job, rows)
	return job.response(true), nil
}

func (s *Service) Get(ctx context.Context, userID, id uint) (JobResponse, error) {
	job, err := s.deps.Store.Job(ctx, userID, id)
	if err != nil {
		return JobResponse{}, err
	}
	return job.response(true), nil
}

func (s *Service) List(ctx context.Context, userID uint) (JobsResponse, error) {
	jobs, err := s.deps.Store.RecentJobs(ctx, userID, recentJobs)
	if err != nil {
		return JobsResponse{}, err
	}
	resp := JobsResponse{Items: make([]JobResponse, 0, len(jobs))}
	for _, job := range jobs {
		resp.Items = append(resp.Items, job.response(false))
	}
	return resp, nil
}

func (s *Service) run(job Job, rows []Row) {
	defer s.wg.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("imdb import panicked", "job_id", job.ID, "panic", fmt.Sprint(recovered))
			s.finish(&job, StatusFailed, "внутренняя ошибка импорта")
		}
	}()

	ctx := s.ctx
	job.Status = StatusRunning
	s.persist(job)
	lastSave := time.Now()
	rated := make(map[uint]struct{})

	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		result, catalogAdded := s.processRow(ctx, job.UserID, job.Kind, row, rated)
		if ctx.Err() != nil {
			break
		}
		job.record(row, result, catalogAdded)
		if job.Processed%saveEveryRows == 0 || time.Since(lastSave) >= saveEveryElapsed {
			s.persist(job)
			lastSave = time.Now()
		}
	}

	s.syncRatings(job.ID, rated)
	if ctx.Err() != nil {
		s.finish(&job, StatusFailed, InterruptedMessage)
		return
	}
	s.finish(&job, StatusDone, "")
	s.logger.Info("imdb import finished",
		"job_id", job.ID, "kind", job.Kind, "total", job.Total, "added", job.Added,
		"skipped_existing", job.SkippedExisting, "not_found", job.NotFound,
		"unsupported", job.Unsupported, "failed", job.Failed, "catalog_added", job.CatalogAdded)
}

func (s *Service) finish(job *Job, status Status, message string) {
	now := time.Now().UTC()
	job.Status = status
	job.FinishedAt = &now
	if message != "" {
		job.Error = &message
	}
	s.persist(*job)
}

func (s *Service) persist(job Job) {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := s.deps.Store.SaveJob(ctx, job); err != nil {
		s.logger.Error("imdb import progress not saved", "job_id", job.ID, "error", err)
	}
}

func (s *Service) syncRatings(jobID uint, rated map[uint]struct{}) {
	if s.deps.Ratings == nil || len(rated) == 0 {
		return
	}
	ids := make([]uint, 0, len(rated))
	for id := range rated {
		ids = append(ids, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ratingSyncTimeout)
	defer cancel()
	if err := s.deps.Ratings.SyncLocalRatings(ctx, ids); err != nil {
		s.logger.Warn("imdb import ratings not pushed to search", "job_id", jobID, "films", len(ids), "error", err)
	}
}

func (s *Service) processRow(ctx context.Context, userID uint, list ListKind, row Row, rated map[uint]struct{}) (outcome, bool) {
	if row.Err != nil {
		return outcomeFailed, false
	}
	class := ClassifyTitleType(row.TitleType)
	if class == ClassUnsupported {
		return outcomeUnsupported, false
	}

	ref, result, catalogAdded := s.resolve(ctx, row, class)
	if result != outcomeAdded {
		return result, catalogAdded
	}

	snapshot, err := s.deps.Marks.MarkSnapshot(ctx, userID, ref.ID)
	if err != nil {
		return s.rowFailed(row, "read marks", err), catalogAdded
	}
	var aired []film.Season
	if list == ListRatings && ref.Kind == film.KindSeries {
		seasons, err := s.deps.Marks.Seasons(ctx, ref.ID)
		if err != nil {
			return s.rowFailed(row, "read seasons", err), catalogAdded
		}
		aired = film.AiredRegularSeasons(seasons, utcToday(), nil)
	}

	action := Decide(list, ref.Kind, snapshot, len(aired))
	switch action {
	case ActionSkip:
		return outcomeSkipped, catalogAdded
	case ActionRateMovie:
		err = s.deps.Marks.RateWatchedAt(ctx, ref.ID, userID, row.Rating, dateOrNow(row.DateRated))
	case ActionRateSeasons:
		ids := make([]uint, 0, len(aired))
		for _, season := range aired {
			ids = append(ids, season.ID)
		}
		err = s.deps.Marks.MarkSeasonsWatchedRated(ctx, userID, ref.ID, ids, row.Rating, dateOrNow(row.DateRated))
	case ActionWatchlist:
		added := row.Created
		if added == nil {
			added = row.DateRated
		}
		err = s.deps.Marks.AddToWatchListAt(ctx, ref.ID, userID, dateOrNow(added))
	}
	if err != nil {
		return s.rowFailed(row, "apply mark", err), catalogAdded
	}
	if action == ActionRateMovie || action == ActionRateSeasons {
		rated[ref.ID] = struct{}{}
	}
	return outcomeAdded, catalogAdded
}

func (s *Service) resolve(ctx context.Context, row Row, class TitleClass) (CardRef, outcome, bool) {
	ref, found, err := s.deps.Catalog.FilmByIMDbID(ctx, row.IMDbID)
	if err != nil {
		return CardRef{}, s.rowFailed(row, "catalog lookup", err), false
	}
	if found {
		return ref, outcomeAdded, false
	}

	match, err := s.deps.Finder.FindByIMDbID(ctx, row.IMDbID)
	if err != nil {
		return CardRef{}, s.rowFailed(row, "tmdb find", err), false
	}
	kind, tmdbID, pick := PickCard(match, class)
	switch pick {
	case PickNotFound:
		return CardRef{}, outcomeNotFound, false
	case PickUnsupported:
		return CardRef{}, outcomeUnsupported, false
	}

	id, found, err := s.deps.Catalog.FilmByTMDBID(ctx, kind, tmdbID)
	if err != nil {
		return CardRef{}, s.rowFailed(row, "catalog lookup", err), false
	}
	if found {
		return CardRef{ID: id, Kind: kind}, outcomeAdded, false
	}

	if err := s.deps.Fetcher.ImportOne(ctx, kind, tmdbID); err != nil {
		if errors.Is(err, tmdb.ErrNotFound) {
			return CardRef{}, outcomeNotFound, false
		}
		if errors.Is(err, importer.ErrAdult) {
			return CardRef{}, outcomeUnsupported, false
		}
		return CardRef{}, s.rowFailed(row, "tmdb card", err), false
	}
	id, found, err = s.deps.Catalog.FilmByTMDBID(ctx, kind, tmdbID)
	if err != nil || !found {
		return CardRef{}, s.rowFailed(row, "catalog lookup after import", err), false
	}

	if s.deps.Localizer != nil {
		if err := s.deps.Localizer.Localize(ctx, id); err != nil && ctx.Err() == nil {
			s.logger.Warn("imported card not localized", "film_id", id, "error", err)
		}
	}
	if s.deps.Indexer != nil {
		if err := s.deps.Indexer.IndexCards(ctx, []uint{id}); err != nil && ctx.Err() == nil {
			s.logger.Warn("imported card not indexed", "film_id", id, "error", err)
		}
	}
	return CardRef{ID: id, Kind: kind}, outcomeAdded, true
}

func (s *Service) rowFailed(row Row, step string, err error) outcome {
	if s.ctx.Err() == nil {
		s.logger.Warn("imdb import row failed", "imdb_id", row.IMDbID, "line", row.Line, "step", step, "error", err)
	}
	return outcomeFailed
}

func utcToday() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func dateOrNow(value *time.Time) time.Time {
	if value == nil {
		return time.Now().UTC()
	}
	return *value
}
