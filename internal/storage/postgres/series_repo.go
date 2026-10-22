package postgres

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *FilmRepository) FilmIdentity(ctx context.Context, filmID uint) (film.Kind, int, error) {
	var stored film.Film
	err := r.db.WithContext(ctx).Select("id", "kind", "tmdb_id").Where("id = ?", filmID).First(&stored).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, model.NewEntityNotFound("film")
	}
	return stored.Kind, stored.TMDBID, err
}

func (r *FilmRepository) SeriesDetails(ctx context.Context, filmID uint) (*film.SeriesDetails, error) {
	var details film.SeriesDetails
	err := r.db.WithContext(ctx).Where("film_id = ?", filmID).First(&details).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &details, nil
}

func (r *FilmRepository) Seasons(ctx context.Context, filmID uint) ([]film.Season, error) {
	var seasons []film.Season
	err := r.db.WithContext(ctx).
		Where("film_id = ? AND number >= 0", filmID).
		Order("number").
		Find(&seasons).Error
	return seasons, err
}

func (r *FilmRepository) SeasonByNumber(ctx context.Context, filmID uint, number int) (*film.Season, error) {
	if number < 0 {
		return nil, film.ErrSeasonNotFound
	}
	var season film.Season
	err := r.db.WithContext(ctx).Where("film_id = ? AND number = ?", filmID, number).First(&season).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, film.ErrSeasonNotFound
	}
	if err != nil {
		return nil, err
	}
	return &season, nil
}

func (r *FilmRepository) SeasonEntries(ctx context.Context, userID, filmID uint) ([]film.SeasonEntryView, error) {
	var views []film.SeasonEntryView
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.number, e.state, e.rate FROM season_entries e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.user_id = ? AND s.film_id = ? AND s.number >= 0
		ORDER BY s.number`, userID, filmID).Scan(&views).Error
	return views, err
}

func (r *FilmRepository) SeriesRatings(ctx context.Context, userID uint, filmIDs []uint) (map[uint]float64, error) {
	ratings := map[uint]float64{}
	if len(filmIDs) == 0 {
		return ratings, nil
	}
	var rows []struct {
		FilmID uint
		Rating float64
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.film_id, ROUND(AVG(e.rate)::numeric, 1)::float8 AS rating
		FROM season_entries e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.user_id = ? AND s.film_id IN ? AND s.number > 0 AND e.rate IS NOT NULL
		GROUP BY s.film_id`, userID, filmIDs).Scan(&rows).Error
	for _, row := range rows {
		ratings[row.FilmID] = row.Rating
	}
	return ratings, err
}

func (r *FilmRepository) SetSeasonEntry(ctx context.Context, userID, filmID uint, season film.Season, state *string, rate *int16, setRate bool, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFilm(tx, filmID); err != nil {
			return err
		}
		if state == nil {
			if err := tx.Where("user_id = ? AND season_id = ?", userID, season.ID).Delete(&film.SeasonEntry{}).Error; err != nil {
				return err
			}
			return recalcLocalRating(tx, filmID)
		}

		var existing film.SeasonEntry
		err := tx.Where("user_id = ? AND season_id = ?", userID, season.ID).First(&existing).Error
		found := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		entry := film.SeasonEntry{UserID: userID, SeasonID: season.ID, State: *state, UpdatedAt: now}
		if *state == film.SeasonStateWatched {
			entry.WatchedAt = &now
			if found && existing.State == film.SeasonStateWatched {
				entry.WatchedAt = existing.WatchedAt
				entry.Rate = existing.Rate
			}
			if setRate {
				entry.Rate = rate
			}
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "season_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"state", "rate", "watched_at", "updated_at"}),
		}).Omit("Season").Create(&entry).Error; err != nil {
			return err
		}

		if err := upsertSeriesEntry(tx, userID, filmID, *state == film.SeasonStateWatched, now); err != nil {
			return err
		}
		return recalcLocalRating(tx, filmID)
	})
}

func (r *FilmRepository) MarkSeasonsWatched(ctx context.Context, userID, filmID uint, seasonIDs []uint, now time.Time) error {
	return r.markSeasonsWatched(ctx, userID, filmID, seasonIDs, nil, now, now)
}

func (r *FilmRepository) MarkSeasonsWatchedRated(ctx context.Context, userID, filmID uint, seasonIDs []uint, rate int16, watchedAt time.Time) error {
	return r.markSeasonsWatched(ctx, userID, filmID, seasonIDs, &rate, watchedAt, time.Now())
}

func (r *FilmRepository) markSeasonsWatched(ctx context.Context, userID, filmID uint, seasonIDs []uint, rate *int16, watchedAt, now time.Time) error {
	if len(seasonIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFilm(tx, filmID); err != nil {
			return err
		}
		entries := make([]film.SeasonEntry, 0, len(seasonIDs))
		for _, id := range seasonIDs {
			entries = append(entries, film.SeasonEntry{UserID: userID, SeasonID: id, State: film.SeasonStateWatched, Rate: rate, WatchedAt: &watchedAt, UpdatedAt: now})
		}
		assignments := map[string]any{
			"state":      film.SeasonStateWatched,
			"updated_at": now,
			"watched_at": gorm.Expr("CASE WHEN season_entries.state = 'watched' THEN season_entries.watched_at ELSE ? END", watchedAt),
		}
		if rate != nil {
			assignments["rate"] = *rate
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "season_id"}},
			DoUpdates: clause.Assignments(assignments),
		}).Omit("Season").Create(&entries).Error; err != nil {
			return err
		}
		if err := upsertSeriesEntry(tx, userID, filmID, true, watchedAt); err != nil {
			return err
		}
		return recalcLocalRating(tx, filmID)
	})
}

func upsertSeriesEntry(tx *gorm.DB, userID, filmID uint, watched bool, now time.Time) error {
	assignments := map[string]any{
		"series_state": gorm.Expr("CASE WHEN film_entries.series_state = 'dropped' THEN NULL ELSE film_entries.series_state END"),
	}
	entry := film.FilmEntry{UserID: userID, FilmID: filmID}
	if watched {
		entry.WatchedAt = &now
		assignments["watched_at"] = gorm.Expr("COALESCE(film_entries.watched_at, ?)", now)
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
		DoUpdates: clause.Assignments(assignments),
	}).Create(&entry).Error
}

func (r *FilmRepository) SetSeriesState(ctx context.Context, userID, filmID uint, state string) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"series_state"}),
	}).Create(&film.FilmEntry{UserID: userID, FilmID: filmID, SeriesState: &state}).Error
}

func (r *FilmRepository) ClearSeriesState(ctx context.Context, userID, filmID uint, state string) error {
	return r.db.WithContext(ctx).Model(&film.FilmEntry{}).
		Where("user_id = ? AND film_id = ? AND series_state = ?", userID, filmID, state).
		Update("series_state", nil).Error
}
