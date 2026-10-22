package importer

import (
	"context"
	"log/slog"
	"sync/atomic"

	"lifelog-api/internal/film"

	"golang.org/x/sync/errgroup"
)

type RecommendationSource interface {
	Recommendations(ctx context.Context, movieID int) ([]int, error)
	TVRecommendations(ctx context.Context, tvID int) ([]int, error)
}

type CatalogFilm struct {
	ID     uint
	TMDBID int
	Kind   film.Kind
}

type RecommendationStorage interface {
	CatalogFilms(ctx context.Context, afterID uint, limit int) ([]CatalogFilm, error)

	ReplaceRecommendations(ctx context.Context, filmID uint, kind film.Kind, tmdbIDs []int) (int, error)
}

type Recommender struct {
	source  RecommendationSource
	storage RecommendationStorage
	logger  *slog.Logger
}

func NewRecommender(source RecommendationSource, storage RecommendationStorage, logger *slog.Logger) *Recommender {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recommender{source: source, storage: storage, logger: logger}
}

type RecommendStats struct {
	Checked int
	Linked  int
	Errors  int
}

func (r *Recommender) Run(ctx context.Context, workers int, dryRun bool) (RecommendStats, error) {
	if workers <= 0 {
		workers = 4
	}

	var linked, failed atomic.Int64
	checked := 0

	var afterID uint
	for {
		films, err := r.storage.CatalogFilms(ctx, afterID, batchSize)
		if err != nil {
			return stats(checked, &linked, &failed), err
		}
		if len(films) == 0 {
			return stats(checked, &linked, &failed), nil
		}
		afterID = films[len(films)-1].ID

		group, groupCtx := errgroup.WithContext(ctx)
		group.SetLimit(workers)
		for _, item := range films {
			group.Go(func() error {
				ids, err := r.recommendations(groupCtx, item)
				if err != nil {
					r.logger.Warn("рекомендации не получены", "tmdb_id", item.TMDBID, "error", err)
					failed.Add(1)
					return nil
				}
				if len(ids) == 0 || dryRun {
					linked.Add(int64(len(ids)))
					return nil
				}

				saved, err := r.storage.ReplaceRecommendations(groupCtx, item.ID, item.Kind, ids)
				if err != nil {
					return err
				}
				linked.Add(int64(saved))
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return stats(checked, &linked, &failed), err
		}
		checked += len(films)
		r.logger.Info("рекомендации собраны", "фильмов", checked, "связей", linked.Load())

		if err := ctx.Err(); err != nil {
			return stats(checked, &linked, &failed), err
		}
	}
}

func (r *Recommender) recommendations(ctx context.Context, item CatalogFilm) ([]int, error) {
	if item.Kind == film.KindSeries {
		return r.source.TVRecommendations(ctx, item.TMDBID)
	}
	return r.source.Recommendations(ctx, item.TMDBID)
}

func stats(checked int, linked, failed *atomic.Int64) RecommendStats {
	return RecommendStats{Checked: checked, Linked: int(linked.Load()), Errors: int(failed.Load())}
}
