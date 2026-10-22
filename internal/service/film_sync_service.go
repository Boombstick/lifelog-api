package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"lifelog-api/internal/importer"

	"github.com/google/uuid"
)

type CatalogImporter interface {
	Run(ctx context.Context, opts importer.Options) (importer.Stats, error)
}

var (
	ErrSyncAlreadyRunning = errors.New("синхронизация уже идёт")
	ErrSyncUnavailable    = errors.New("синхронизация недоступна: не задан токен TMDB")
	ErrSyncYears          = errors.New("недопустимые годы")
)

const (
	minSyncYear = 1888

	maxSyncSpan  = 60
	syncMinVotes = 200
	syncWorkers  = 8
	syncBatch    = 100
	syncTopCast  = 10
)

type SyncState string

const (
	SyncStateIdle      SyncState = "idle"
	SyncStateRunning   SyncState = "running"
	SyncStateSucceeded SyncState = "succeeded"
	SyncStateFailed    SyncState = "failed"
)

type StartSyncRequest struct {
	YearFrom int `json:"year_from" binding:"required"`
	YearTo   int `json:"year_to" binding:"required"`
}

type SyncStats struct {
	Requested int64 `json:"requested"`
	Fetched   int64 `json:"fetched"`
	Saved     int64 `json:"saved"`
	Skipped   int64 `json:"skipped"`
	Failed    int64 `json:"failed"`
}

type SyncStatusResponse struct {
	State      SyncState  `json:"state"`
	RunID      string     `json:"run_id,omitempty"`
	YearFrom   int        `json:"year_from,omitempty"`
	YearTo     int        `json:"year_to,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Stats      *SyncStats `json:"stats,omitempty"`
	Error      string     `json:"error,omitempty"`

	IndexStale bool `json:"index_stale"`
}

type syncRun struct {
	state      SyncState
	id         string
	yearFrom   int
	yearTo     int
	startedAt  time.Time
	finishedAt time.Time
	stats      importer.Stats
	err        error
}

type FilmSyncService struct {
	importer CatalogImporter
	logger   *slog.Logger
	now      func() time.Time

	mu  sync.Mutex
	run syncRun
}

func NewFilmSyncService(catalogImporter CatalogImporter, logger *slog.Logger) *FilmSyncService {
	return &FilmSyncService{
		importer: catalogImporter,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		run:      syncRun{state: SyncStateIdle},
	}
}

func (s *FilmSyncService) Start(req StartSyncRequest) (SyncStatusResponse, error) {
	if s.importer == nil {
		return SyncStatusResponse{}, ErrSyncUnavailable
	}

	opts, err := syncOptions(req, s.now().Year())
	if err != nil {
		return SyncStatusResponse{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.run.state == SyncStateRunning {
		return s.statusLocked(), ErrSyncAlreadyRunning
	}

	s.run = syncRun{
		state:     SyncStateRunning,
		id:        uuid.NewString(),
		yearFrom:  req.YearFrom,
		yearTo:    req.YearTo,
		startedAt: s.now(),
	}
	status := s.statusLocked()

	go s.execute(status.RunID, opts)

	return status, nil
}

func (s *FilmSyncService) Status() SyncStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *FilmSyncService) execute(runID string, opts importer.Options) {
	stats, err := s.importer.Run(context.Background(), opts)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.run.stats = stats
	s.run.finishedAt = s.now()
	if err != nil {
		s.run.state = SyncStateFailed
		s.run.err = err
		s.logger.Error("синхронизация каталога не удалась", "run_id", runID, "error", err)
		return
	}
	s.run.state = SyncStateSucceeded
	s.logger.Info("синхронизация каталога завершена",
		"run_id", runID,
		"requested", stats.Requested,
		"saved", stats.Saved,
		"skipped", stats.Skipped,
		"failed", stats.Failed)
}

func (s *FilmSyncService) statusLocked() SyncStatusResponse {
	status := SyncStatusResponse{
		State:      s.run.state,
		RunID:      s.run.id,
		YearFrom:   s.run.yearFrom,
		YearTo:     s.run.yearTo,
		IndexStale: s.run.stats.Saved > 0,
	}
	if !s.run.startedAt.IsZero() {
		started := s.run.startedAt
		status.StartedAt = &started
	}
	if !s.run.finishedAt.IsZero() {
		finished := s.run.finishedAt
		status.FinishedAt = &finished
	}
	if s.run.state == SyncStateSucceeded || s.run.state == SyncStateFailed {
		status.Stats = &SyncStats{
			Requested: s.run.stats.Requested,
			Fetched:   s.run.stats.Fetched,
			Saved:     s.run.stats.Saved,
			Skipped:   s.run.stats.Skipped,
			Failed:    s.run.stats.Failed,
		}
	}
	if s.run.err != nil {
		status.Error = s.run.err.Error()
	}
	return status
}

func syncOptions(req StartSyncRequest, currentYear int) (importer.Options, error) {
	if err := validateSyncYears(req.YearFrom, req.YearTo, currentYear); err != nil {
		return importer.Options{}, err
	}
	return importer.Options{
		From:     time.Date(req.YearFrom, time.January, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(req.YearTo, time.December, 31, 0, 0, 0, 0, time.UTC),
		MinVotes: syncMinVotes,

		Limit:   0,
		Workers: syncWorkers,
		Batch:   syncBatch,
		TopCast: syncTopCast,
	}, nil
}

func validateSyncYears(from, to, currentYear int) error {
	maxYear := currentYear + 1
	switch {
	case from < minSyncYear:
		return fmt.Errorf("%w: год начала раньше %d", ErrSyncYears, minSyncYear)
	case to > maxYear:
		return fmt.Errorf("%w: год конца позже %d", ErrSyncYears, maxYear)
	case from > to:
		return fmt.Errorf("%w: год начала %d позже года конца %d", ErrSyncYears, from, to)
	case to-from+1 > maxSyncSpan:
		return fmt.Errorf("%w: диапазон шире %d лет", ErrSyncYears, maxSyncSpan)
	}
	return nil
}
