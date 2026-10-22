package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lifelog-api/internal/imdbimport"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

const importJobActiveIndex = "idx_import_jobs_one_active"

type ImportJobRepository struct {
	db *gorm.DB
}

func NewImportJobRepository(db *gorm.DB) *ImportJobRepository {
	return &ImportJobRepository{db: db}
}

type importJobRow struct {
	ID              uint
	UserID          uint
	Source          string
	Kind            string
	Status          string
	Total           int
	Processed       int
	Added           int
	SkippedExisting int
	NotFound        int
	Unsupported     int
	Failed          int
	CatalogAdded    int
	Error           *string
	NotFoundItems   string
	CreatedAt       time.Time
	FinishedAt      *time.Time
}

const importJobColumns = `id, user_id, source, kind, status, total, processed, added, skipped_existing,
	not_found, unsupported, failed, catalog_added, error, not_found_items::text AS not_found_items,
	created_at, finished_at`

func (row importJobRow) job() (imdbimport.Job, error) {
	job := imdbimport.Job{
		ID:              row.ID,
		UserID:          row.UserID,
		Source:          row.Source,
		Kind:            imdbimport.ListKind(row.Kind),
		Status:          imdbimport.Status(row.Status),
		Total:           row.Total,
		Processed:       row.Processed,
		Added:           row.Added,
		SkippedExisting: row.SkippedExisting,
		NotFound:        row.NotFound,
		Unsupported:     row.Unsupported,
		Failed:          row.Failed,
		CatalogAdded:    row.CatalogAdded,
		Error:           row.Error,
		CreatedAt:       row.CreatedAt,
		FinishedAt:      row.FinishedAt,
	}
	if row.NotFoundItems != "" {
		if err := json.Unmarshal([]byte(row.NotFoundItems), &job.NotFoundItems); err != nil {
			return job, fmt.Errorf("import job %d: not_found_items: %w", row.ID, err)
		}
	}
	return job, nil
}

func (r *ImportJobRepository) CreateJob(ctx context.Context, job *imdbimport.Job) error {
	var created struct {
		ID        uint
		CreatedAt time.Time
	}
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO import_jobs (user_id, source, kind, status, total)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at`,
		job.UserID, job.Source, string(job.Kind), string(job.Status), job.Total).
		Scan(&created).Error
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == importJobActiveIndex {
			return imdbimport.ErrImportRunning
		}
		return err
	}
	job.ID = created.ID
	job.CreatedAt = created.CreatedAt
	return nil
}

func (r *ImportJobRepository) SaveJob(ctx context.Context, job imdbimport.Job) error {
	items := job.NotFoundItems
	if items == nil {
		items = []imdbimport.NotFoundItem{}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`
		UPDATE import_jobs SET
			status = ?, processed = ?, added = ?, skipped_existing = ?, not_found = ?,
			unsupported = ?, failed = ?, catalog_added = ?, error = ?,
			not_found_items = ?::jsonb, finished_at = ?, updated_at = now()
		WHERE id = ?`,
		string(job.Status), job.Processed, job.Added, job.SkippedExisting, job.NotFound,
		job.Unsupported, job.Failed, job.CatalogAdded, job.Error,
		string(encoded), job.FinishedAt, job.ID).Error
}

func (r *ImportJobRepository) Job(ctx context.Context, userID, id uint) (*imdbimport.Job, error) {
	var rows []importJobRow
	err := r.db.WithContext(ctx).Raw(
		`SELECT `+importJobColumns+` FROM import_jobs WHERE id = ? AND user_id = ?`, id, userID).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, imdbimport.ErrJobNotFound
	}
	job, err := rows[0].job()
	return &job, err
}

func (r *ImportJobRepository) RecentJobs(ctx context.Context, userID uint, limit int) ([]imdbimport.Job, error) {
	var rows []importJobRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, source, kind, status, total, processed, added, skipped_existing,
			not_found, unsupported, failed, catalog_added, error, created_at, finished_at
		FROM import_jobs WHERE user_id = ? ORDER BY id DESC LIMIT ?`, userID, limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	jobs := make([]imdbimport.Job, 0, len(rows))
	for _, row := range rows {
		job, err := row.job()
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (r *ImportJobRepository) FailUnfinished(ctx context.Context, message string) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE import_jobs SET status = 'failed', error = ?, finished_at = now(), updated_at = now()
		WHERE status IN ('queued', 'running')`, message)
	return result.RowsAffected, result.Error
}

func (r *ImportJobRepository) HasActiveJob(ctx context.Context, userID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM import_jobs WHERE user_id = ? AND status IN ('queued', 'running')`, userID).
		Scan(&count).Error
	return count > 0, err
}
