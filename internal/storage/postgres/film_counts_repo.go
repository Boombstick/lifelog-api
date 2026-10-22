package postgres

import (
	"context"

	"lifelog-api/internal/film"
)

func (r *FilmRepository) EntryCounts(ctx context.Context, userID uint) (film.EntryCounts, error) {
	var row struct {
		Watchlist int
		Watched   int
		Rated     int
		Watching  int
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) FILTER (WHERE `+watchlistSetSQL+`) AS watchlist,
			COUNT(*) FILTER (WHERE `+watchedSetSQL+`) AS watched,
			COUNT(*) FILTER (WHERE `+ratedSetSQL+`) AS rated,
			COUNT(*) FILTER (WHERE `+seriesWatchingSQL+`) AS watching
		FROM film_entries
		JOIN films ON films.id = film_entries.film_id
		WHERE film_entries.user_id = ?`, userID).
		Scan(&row).Error
	if err != nil {
		return film.EntryCounts{}, err
	}
	return film.EntryCounts(row), nil
}
