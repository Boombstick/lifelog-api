package service

import (
	"context"
	"time"

	"lifelog-api/internal/search"
)

type LocalRatingIndex interface {
	UpdateFilmRating(ctx context.Context, filmID uint, rating float32, votes int) error
	UpdateFilmRatings(ctx context.Context, ratings []search.LocalRating) error
}

const (
	maxRatingIndexers   = 4
	ratingIndexTimeout  = 5 * time.Second
	ratingSyncBatchSize = 500
)

func (s *FilmService) SetRatingIndex(index LocalRatingIndex) {
	s.ratingIndex = index
}

func (s *FilmService) pushLocalRating(filmID uint) {
	if s.ratingIndex == nil {
		return
	}
	select {
	case s.indexing <- struct{}{}:
	default:
		s.logger.Warn("local rating not pushed to search: queue full", "film_id", filmID)
		return
	}

	go func() {
		defer func() { <-s.indexing }()

		ctx, cancel := context.WithTimeout(context.Background(), ratingIndexTimeout)
		defer cancel()

		ratings, err := s.repo.LocalRatings(ctx, []uint{filmID})
		if err != nil {
			s.logger.Warn("local rating not pushed to search: read failed", "film_id", filmID, "error", err)
			return
		}
		if len(ratings) == 0 {
			return
		}
		r := ratings[0]
		if err := s.ratingIndex.UpdateFilmRating(ctx, r.FilmID, r.Rating, r.Votes); err != nil {
			s.logger.Warn("local rating not pushed to search", "film_id", filmID, "error", err)
		}
	}()
}

func (s *FilmService) SyncLocalRatings(ctx context.Context, filmIDs []uint) error {
	if s.ratingIndex == nil {
		return nil
	}
	for start := 0; start < len(filmIDs); start += ratingSyncBatchSize {
		batch := filmIDs[start:min(start+ratingSyncBatchSize, len(filmIDs))]
		ratings, err := s.repo.LocalRatings(ctx, batch)
		if err != nil {
			return err
		}
		updates := make([]search.LocalRating, 0, len(ratings))
		for _, r := range ratings {
			updates = append(updates, search.LocalRating{FilmID: r.FilmID, Rating: r.Rating, Votes: r.Votes})
		}
		if err := s.ratingIndex.UpdateFilmRatings(ctx, updates); err != nil {
			return err
		}
	}
	return nil
}
