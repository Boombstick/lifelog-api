package importer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lifelog-api/internal/film"
)

type ChangeSource interface {
	ChangedIDs(ctx context.Context, kind film.Kind, start, end time.Time, page int) ([]int, int, error)
}

type SyncOptions struct {
	Kind film.Kind

	Since time.Duration

	MaxPages int
	Workers  int
	Batch    int
	TopCast  int
	DryRun   bool
}

type SyncStats struct {
	Changed int

	Ours   int
	Import Stats
}

const (
	defaultSyncSince    = 24 * time.Hour
	defaultSyncMaxPages = 200
	maxChangeWindow     = 14 * 24 * time.Hour
)

type Syncer struct {
	changes  ChangeSource
	importer *Importer
	storage  Storage
	logger   *slog.Logger
}

func NewSyncer(changes ChangeSource, importer *Importer, storage Storage, logger *slog.Logger) *Syncer {
	return &Syncer{changes: changes, importer: importer, storage: storage, logger: logger}
}

func (s *Syncer) Run(ctx context.Context, opts SyncOptions) (SyncStats, error) {
	var stats SyncStats

	switch opts.Kind {
	case "":
		opts.Kind = film.KindMovie
	case film.KindMovie, film.KindSeries:
	default:
		return stats, fmt.Errorf("неизвестный вид карточек %q", opts.Kind)
	}
	if opts.Since <= 0 {
		opts.Since = defaultSyncSince
	}
	if opts.Since > maxChangeWindow {
		return stats, fmt.Errorf("окно изменений шире 14 дней: %s", opts.Since)
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = defaultSyncMaxPages
	}

	end := time.Now().UTC()
	start := end.Add(-opts.Since)

	held, err := s.storage.ExistingFilmTMDBIDs(ctx, opts.Kind)
	if err != nil {
		return stats, fmt.Errorf("чтение загруженных id: %w", err)
	}
	if len(held) == 0 {
		s.logger.Info("каталог пуст, обновлять нечего", "kind", opts.Kind)
		return stats, nil
	}

	seen := make(map[int]struct{})
	var ids []int
	for page := 1; page <= opts.MaxPages; page++ {
		changed, totalPages, err := s.changes.ChangedIDs(ctx, opts.Kind, start, end, page)
		if err != nil {
			return stats, fmt.Errorf("список изменений, страница %d: %w", page, err)
		}
		stats.Changed += len(changed)
		for _, id := range changed {
			if _, ours := held[id]; !ours {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		if page >= totalPages {
			break
		}
		if page == opts.MaxPages {
			s.logger.Warn("список изменений обрезан потолком страниц",
				"kind", opts.Kind, "max_pages", opts.MaxPages, "total_pages", totalPages)
		}
	}

	stats.Ours = len(ids)
	s.logger.Info("изменения собраны",
		"kind", opts.Kind, "since", opts.Since, "changed", stats.Changed, "ours", stats.Ours)

	if len(ids) == 0 || opts.DryRun {
		return stats, nil
	}

	stats.Import, err = s.importer.Run(ctx, Options{
		Kind:    opts.Kind,
		IDs:     ids,
		Refresh: true,
		Workers: opts.Workers,
		Batch:   opts.Batch,
		TopCast: opts.TopCast,
	})
	return stats, err
}
