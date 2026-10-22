package postgres

import (
	"context"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/tmdbadd"
)

func (r *FilmRepository) FilmIDsByTMDB(ctx context.Context, keys []tmdbadd.CardKey) (map[tmdbadd.CardKey]uint, error) {
	out := make(map[tmdbadd.CardKey]uint, len(keys))
	byKind := map[film.Kind][]int{}
	for _, key := range keys {
		byKind[key.Kind] = append(byKind[key.Kind], key.TMDBID)
	}
	if len(byKind) == 0 {
		return out, nil
	}

	movies, series := byKind[film.KindMovie], byKind[film.KindSeries]
	if len(movies) == 0 {
		movies = []int{0}
	}
	if len(series) == 0 {
		series = []int{0}
	}

	var rows []film.Film
	err := r.db.WithContext(ctx).
		Select("id", "kind", "tmdb_id").
		Where("(kind = ? AND tmdb_id IN ?) OR (kind = ? AND tmdb_id IN ?)",
			film.KindMovie, movies, film.KindSeries, series).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[tmdbadd.CardKey{Kind: row.Kind, TMDBID: row.TMDBID}] = row.ID
	}
	return out, nil
}

func (r *FilmRepository) ManualAddsSince(ctx context.Context, userID uint, since time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Where("added_by = ? AND added_at > ?", userID, since).
		Count(&count).Error
	return count, err
}

func (r *FilmRepository) ClaimManualAdd(ctx context.Context, filmID, userID uint, createdSince, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Where("id = ? AND added_at IS NULL AND created_at >= ?", filmID, createdSince).
		UpdateColumns(map[string]any{"added_by": userID, "added_at": at})
	return result.RowsAffected == 1, result.Error
}
