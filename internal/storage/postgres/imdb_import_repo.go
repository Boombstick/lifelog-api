package postgres

import (
	"context"
	"time"

	"lifelog-api/internal/film"
	"lifelog-api/internal/imdbimport"
	"lifelog-api/internal/importer"

	"gorm.io/gorm/clause"
)

func (r *FilmRepository) FilmByIMDbID(ctx context.Context, imdbID string) (imdbimport.CardRef, bool, error) {
	var rows []film.Film
	err := r.db.WithContext(ctx).
		Select("id", "kind").
		Where("imdb_id = ?", imdbID).
		Limit(2).
		Find(&rows).Error
	if err != nil || len(rows) != 1 {
		return imdbimport.CardRef{}, false, err
	}
	return imdbimport.CardRef{ID: rows[0].ID, Kind: rows[0].Kind}, true, nil
}

func (r *FilmRepository) FilmByTMDBID(ctx context.Context, kind film.Kind, tmdbID int) (uint, bool, error) {
	var ids []uint
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Where("kind = ? AND tmdb_id = ?", kind, tmdbID).
		Limit(1).
		Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, false, err
	}
	return ids[0], true, nil
}

func (r *FilmRepository) MarkSnapshot(ctx context.Context, userID, filmID uint) (imdbimport.Snapshot, error) {
	var rows []struct {
		Watchlisted   bool
		Watched       bool
		Rated         bool
		Reviewed      bool
		Dropped       bool
		SeasonEntries int
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT COALESCE(fe.watchlisted, false)                    AS watchlisted,
		       COALESCE(fe.watched_at IS NOT NULL, false)          AS watched,
		       COALESCE(fe.rate IS NOT NULL, false)                AS rated,
		       COALESCE(fe.description IS NOT NULL, false)         AS reviewed,
		       COALESCE(fe.series_state = 'dropped', false)        AS dropped,
		       (SELECT COUNT(*) FROM season_entries e JOIN seasons s ON s.id = e.season_id
		        WHERE e.user_id = ? AND s.film_id = ?)             AS season_entries
		FROM (SELECT 1) one
		LEFT JOIN film_entries fe ON fe.user_id = ? AND fe.film_id = ?`,
		userID, filmID, userID, filmID).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return imdbimport.Snapshot{}, err
	}
	row := rows[0]
	return imdbimport.Snapshot{
		Watchlisted:   row.Watchlisted,
		Watched:       row.Watched,
		Rated:         row.Rated,
		Reviewed:      row.Reviewed,
		Dropped:       row.Dropped,
		SeasonEntries: row.SeasonEntries,
	}, nil
}

func (r *FilmRepository) AddToWatchListAt(ctx context.Context, filmID, userID uint, addedAt time.Time) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"watchlisted"}),
	}).Create(&film.FilmEntry{UserID: userID, FilmID: filmID, Watchlisted: true, CreatedAt: addedAt}).Error
}

func (r *FilmRepository) UntranslatedFilm(ctx context.Context, filmID uint) (*importer.UntranslatedFilm, error) {
	var rows []importer.UntranslatedFilm
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Select("id", "tmdb_id", "kind").
		Where("id = ?", filmID).
		Where("title_ru = '' OR title_ru IS NULL OR title_ru = title_en").
		Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (r *FilmRepository) IncompletePeopleOfFilm(ctx context.Context, filmID uint) ([]importer.IncompletePerson, error) {
	var rows []importer.IncompletePerson
	err := r.db.WithContext(ctx).
		Model(&film.Person{}).
		Select("people.id", "people.tmdb_id").
		Joins("JOIN film_credits ON film_credits.person_id = people.id").
		Where("film_credits.film_id = ?", filmID).
		Where("people.name !~ '[А-Яа-яЁё]' OR people.photo_url = '' OR people.photo_url IS NULL").
		Group("people.id, people.tmdb_id").
		Order("people.id").
		Find(&rows).Error
	return rows, err
}
