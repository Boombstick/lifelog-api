package postgres

import (
	"context"
	"errors"
	"fmt"
	"lifelog-api/internal/film"
	"lifelog-api/internal/importer"
	"lifelog-api/internal/model"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FilmRepository struct {
	db *gorm.DB
}

func ProvideFilmRepository(gdb *gorm.DB) *FilmRepository {
	return &FilmRepository{db: gdb}
}

const listHardLimit = 50

func (r *FilmRepository) FindAll(ctx context.Context) ([]film.Film, error) {
	var result []film.Film
	err := r.db.WithContext(ctx).Model(&film.Film{}).
		Preload("Genres").
		Order("tmdb_popularity DESC, id ASC").
		Limit(listHardLimit).
		Find(&result).Error
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *FilmRepository) FindById(ctx context.Context, id uint, viewer uint) (*film.Film, error) {
	result := &film.Film{}
	query := r.db.WithContext(ctx).Model(&film.Film{}).Where("id = ?", id).
		Preload("Genres").
		Preload("Credits", func(db *gorm.DB) *gorm.DB {
			return db.Order("film_credits.role ASC, film_credits.billing_order ASC")
		}).
		Preload("Credits.Person")
	if viewer != 0 {
		query = query.Preload("Entries", func(db *gorm.DB) *gorm.DB {
			return db.Where("(rate IS NOT NULL OR description <> '' OR user_id = ?)", viewer).Preload("User")
		})
	}
	err := query.First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("film")
		}
		return nil, err
	}
	return result, nil
}

func (r *FilmRepository) FilmWithGenres(ctx context.Context, id uint) (*film.Film, error) {
	result := &film.Film{}
	err := r.db.WithContext(ctx).Preload("Genres").Where("id = ?", id).First(result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.NewEntityNotFound("film")
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (r *FilmRepository) AddToWatched(ctx context.Context, filmID uint, userID uint, watchedAt time.Time) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"watched_at": gorm.Expr("COALESCE(film_entries.watched_at, ?)", watchedAt),
		}),
	}).Create(&film.FilmEntry{UserID: userID, FilmID: filmID, WatchedAt: &watchedAt}).Error
}
func (r *FilmRepository) AddToWatchList(ctx context.Context, filmID uint, userID uint) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"watchlisted"}),
	}).Create(&film.FilmEntry{UserID: userID, FilmID: filmID, Watchlisted: true}).Error
}

func (r *FilmRepository) AddRateAndDescription(ctx context.Context, filmID uint, userID uint, rate *int16, description *string) error {
	return r.saveReview(ctx, filmID, userID, rate, description, time.Now())
}

func (r *FilmRepository) RateWatchedAt(ctx context.Context, filmID, userID uint, rate int16, watchedAt time.Time) error {
	return r.saveReview(ctx, filmID, userID, &rate, nil, watchedAt)
}

func (r *FilmRepository) saveReview(ctx context.Context, filmID, userID uint, rate *int16, description *string, now time.Time) error {
	if rate == nil && description == nil {
		return nil
	}

	clearText := description != nil && strings.TrimSpace(*description) == ""
	if clearText {
		description = nil
		if rate == nil {
			return r.db.WithContext(ctx).Model(&film.FilmEntry{}).
				Where("user_id = ? AND film_id = ?", userID, filmID).
				Update("description", nil).Error
		}
	}

	entry := &film.FilmEntry{UserID: userID, FilmID: filmID, WatchedAt: &now, Rate: rate, Description: description}
	assignments := map[string]interface{}{
		"watched_at":  gorm.Expr("COALESCE(film_entries.watched_at, ?)", now),
		"watchlisted": false,
	}
	if rate != nil {
		assignments["rate"] = *rate
	}
	if description != nil {
		assignments["description"] = *description
	} else if clearText {
		assignments["description"] = nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if rate != nil {
			if err := lockFilm(tx, filmID); err != nil {
				return err
			}
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "film_id"}},
			DoUpdates: clause.Assignments(assignments),
		}).Create(entry).Error
		if err != nil {
			return err
		}

		if rate == nil {
			return nil
		}
		return recalcLocalRating(tx, filmID)
	})
}

func (r *FilmRepository) RemoveFromWatchList(ctx context.Context, filmID uint, userID uint) error {
	return r.db.WithContext(ctx).Model(&film.FilmEntry{}).
		Where("user_id = ? AND film_id = ?", userID, filmID).
		Update("watchlisted", false).Error
}

func (r *FilmRepository) RemoveFromWatched(ctx context.Context, filmID uint, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		entry := film.FilmEntry{}
		err := tx.Where("user_id = ? AND film_id = ?", userID, filmID).First(&entry).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if entry.Rate != nil || (entry.Description != nil && strings.TrimSpace(*entry.Description) != "") {
			return film.ErrRatedCannotUnwatch
		}

		return tx.Model(&film.FilmEntry{}).
			Where("user_id = ? AND film_id = ?", userID, filmID).
			Updates(map[string]any{"watched_at": nil, "description": nil}).Error
	})
}

func (r *FilmRepository) RemoveReview(ctx context.Context, filmID uint, userID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockFilm(tx, filmID); err != nil {
			return err
		}
		result := tx.Model(&film.FilmEntry{}).
			Where("user_id = ? AND film_id = ?", userID, filmID).
			Updates(map[string]interface{}{"rate": nil, "description": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		return recalcLocalRating(tx, filmID)
	})
}

func lockFilm(tx *gorm.DB, filmID uint) error {
	return tx.Exec("SELECT 1 FROM films WHERE id = ? FOR UPDATE", filmID).Error
}

func recalcLocalRating(tx *gorm.DB, filmID uint) error {
	const query = `
		UPDATE films f
		SET local_rating = COALESCE(agg.avg_rate, 0),
		    local_rating_count = COALESCE(agg.cnt, 0)
		FROM (
			SELECT AVG(user_rate)::numeric(4,2) AS avg_rate, COUNT(*) AS cnt
			FROM (
				SELECT fe.rate::numeric AS user_rate
				FROM film_entries fe
				JOIN films x ON x.id = fe.film_id
				WHERE fe.film_id = ? AND x.kind <> 'series' AND fe.rate IS NOT NULL
				UNION ALL
				SELECT AVG(e.rate)::numeric AS user_rate
				FROM season_entries e
				JOIN seasons s ON s.id = e.season_id
				JOIN films x ON x.id = s.film_id
				WHERE s.film_id = ? AND x.kind = 'series' AND s.number > 0 AND e.rate IS NOT NULL
				GROUP BY e.user_id
			) per_user
		) agg
		WHERE f.id = ?`

	return tx.Exec(query, filmID, filmID, filmID).Error
}

func (r *FilmRepository) LocalRatings(ctx context.Context, filmIDs []uint) ([]film.LocalRating, error) {
	var rows []film.LocalRating
	if len(filmIDs) == 0 {
		return rows, nil
	}
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Select("id AS film_id", "local_rating AS rating", "local_rating_count AS votes").
		Where("id IN ?", filmIDs).
		Scan(&rows).Error
	return rows, err
}

func (r *FilmRepository) Genres(ctx context.Context) ([]film.Genre, error) {
	var genres []film.Genre
	if err := r.db.WithContext(ctx).Order("name_ru ASC").Find(&genres).Error; err != nil {
		return nil, err
	}
	return genres, nil
}

func (r *FilmRepository) FindEntries(ctx context.Context, filter film.EntryFilter) ([]film.FilmEntry, int, error) {
	base := func() *gorm.DB {
		db := r.db.WithContext(ctx).Model(&film.FilmEntry{}).
			Joins("JOIN films ON films.id = film_entries.film_id").
			Where("film_entries.user_id = ?", filter.UserID)

		switch filter.Set {
		case film.EntrySetWatched:
			db = db.Where(watchedSetSQL)
		case film.EntrySetRated:
			db = db.Where(ratedSetSQL)
		case film.EntrySetWatching:
			db = db.Where(seriesWatchingSQL)
		default:
			db = db.Where(watchlistSetSQL)
		}
		return db
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	column, ascending := entryOrder(filter.Sort)
	direction := "DESC"
	comparison := "<"
	if ascending {
		direction = "ASC"
		comparison = ">"
	}

	db := base().
		Select("film_entries.*").
		Preload("Film").
		Preload("Film.Genres").
		Order(fmt.Sprintf("%s %s, film_entries.film_id %s", column, direction, direction)).
		Limit(filter.Limit)

	if filter.AfterID > 0 && filter.AfterValue != nil {
		condition := fmt.Sprintf("(%s, film_entries.film_id) %s (?, ?)", column, comparison)
		db = db.Where(condition, filter.AfterValue, filter.AfterID)
	}

	var entries []film.FilmEntry
	if err := db.Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	if err := r.showSeriesLastWatched(ctx, filter.UserID, entries); err != nil {
		return nil, 0, err
	}
	return entries, int(total), nil
}

func (r *FilmRepository) showSeriesLastWatched(ctx context.Context, userID uint, entries []film.FilmEntry) error {
	ids := make([]uint, 0)
	for _, entry := range entries {
		if entry.Film.Kind == film.KindSeries {
			ids = append(ids, entry.FilmID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	var rows []struct {
		FilmID    uint
		WatchedAt time.Time
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.film_id, MAX(e.watched_at) AS watched_at
		FROM season_entries e
		JOIN seasons s ON s.id = e.season_id
		WHERE e.user_id = ? AND s.film_id IN ? AND s.number > 0 AND e.state = 'watched' AND e.watched_at IS NOT NULL
		GROUP BY s.film_id`, userID, ids).Scan(&rows).Error
	if err != nil {
		return err
	}
	latest := make(map[uint]time.Time, len(rows))
	for _, row := range rows {
		latest[row.FilmID] = row.WatchedAt
	}
	for i := range entries {
		if at, ok := latest[entries[i].FilmID]; ok {
			entries[i].WatchedAt = &at
		}
	}
	return nil
}

func entryOrder(sort film.EntrySort) (column string, ascending bool) {
	switch sort {
	case film.EntrySortWatched:
		return entryWatchedSQL, false
	case film.EntrySortRate:
		return entryRateSQL, false
	case film.EntrySortTitle:
		return "films.title_ru", true
	default:
		return "film_entries.created_at", false
	}
}

func (r *FilmRepository) FindEntry(ctx context.Context, filmID, userID uint) (*film.FilmEntry, error) {
	entry := film.FilmEntry{}
	err := r.db.WithContext(ctx).
		Where("film_id = ? AND user_id = ?", filmID, userID).
		First(&entry).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("film entry")
		}
		return nil, err
	}
	return &entry, nil
}

func (r *FilmRepository) WatchedFilmIDs(ctx context.Context, userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).
		Model(&film.FilmEntry{}).
		Joins("JOIN films ON films.id = film_entries.film_id").
		Where("film_entries.user_id = ?", userID).
		Where(watchedSetSQL).
		Order("film_entries.film_id").
		Limit(excludeIDsLimit).
		Pluck("film_entries.film_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *FilmRepository) SeenFilmIDs(ctx context.Context, userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).
		Model(&film.FilmEntry{}).
		Where("user_id = ? AND watched_at IS NOT NULL", userID).
		Order("film_id").
		Limit(excludeIDsLimit).
		Pluck("film_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

const excludeIDsLimit = 20000

func (r *FilmRepository) UntranslatedFilms(ctx context.Context, afterID uint, limit int) ([]importer.UntranslatedFilm, error) {
	var rows []importer.UntranslatedFilm
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Select("id", "tmdb_id", "kind").
		Where("id > ?", afterID).
		Where("title_ru = '' OR title_ru IS NULL OR title_ru = title_en").
		Order("id").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *FilmRepository) IncompletePeople(ctx context.Context, afterID uint, limit int) ([]importer.IncompletePerson, error) {
	var rows []importer.IncompletePerson
	err := r.db.WithContext(ctx).
		Model(&film.Person{}).
		Select("id", "tmdb_id").
		Where("id > ?", afterID).
		Where("name !~ '[А-Яа-яЁё]' OR photo_url = '' OR photo_url IS NULL").
		Order("id").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *FilmRepository) SetFilmTitleRu(ctx context.Context, id uint, title string) error {
	return r.db.WithContext(ctx).
		Model(&film.Film{}).
		Where("id = ?", id).
		Update("title_ru", title).Error
}

func (r *FilmRepository) UpdatePerson(ctx context.Context, id uint, name, photoURL string) error {
	changes := map[string]any{}
	if name != "" {
		changes["name"] = name
	}
	if photoURL != "" {
		changes["photo_url"] = photoURL
	}
	if len(changes) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&film.Person{}).
		Where("id = ?", id).
		Updates(changes).Error
}

func (r *FilmRepository) CatalogFilms(ctx context.Context, afterID uint, limit int) ([]importer.CatalogFilm, error) {
	var rows []importer.CatalogFilm
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Select("id", "tmdb_id", "kind").
		Where("id > ?", afterID).
		Order("id").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

func (r *FilmRepository) ReplaceRecommendations(ctx context.Context, filmID uint, kind film.Kind, tmdbIDs []int) (int, error) {
	if len(tmdbIDs) == 0 {
		return 0, nil
	}

	type match struct {
		ID     uint
		TMDBID int
	}
	var found []match
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Select("id", "tmdb_id").
		Where("kind = ? AND tmdb_id IN ?", kind, tmdbIDs).
		Find(&found).Error
	if err != nil {
		return 0, err
	}

	local := make(map[int]uint, len(found))
	for _, item := range found {
		local[item.TMDBID] = item.ID
	}

	rows := make([]film.FilmRecommendation, 0, len(found))
	for position, tmdbID := range tmdbIDs {
		id, ok := local[tmdbID]
		if !ok || id == filmID {
			continue
		}
		rows = append(rows, film.FilmRecommendation{FilmID: filmID, SimilarID: id, Position: position})
	}

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("film_id = ?", filmID).Delete(&film.FilmRecommendation{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (r *FilmRepository) RecommendedFilms(ctx context.Context, filmID uint, limit int) ([]film.Film, error) {
	var films []film.Film
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Preload("Genres").
		Joins("JOIN film_recommendations ON film_recommendations.similar_id = films.id").
		Where("film_recommendations.film_id = ?", filmID).
		Order("film_recommendations.position").
		Limit(limit).
		Find(&films).Error
	return films, err
}

func (r *FilmRepository) RatedFilms(ctx context.Context, userID uint, minRate int16, limit int, kind film.Kind) ([]film.Film, error) {
	var films []film.Film
	err := r.db.WithContext(ctx).
		Model(&film.Film{}).
		Preload("Genres").
		Preload("Credits.Person").
		Joins("JOIN film_entries ON film_entries.film_id = films.id").
		Where("films.kind = ?", kind).
		Where("film_entries.user_id = ?", userID).
		Where(entryRateSQL+" >= ?", minRate).
		Order(entryRateSQL + " DESC, film_entries.updated_at DESC").
		Limit(limit).
		Find(&films).Error
	return films, err
}

const (
	seriesWatchedSQL = `films.kind = 'series'
		AND EXISTS (SELECT 1 FROM seasons s WHERE s.film_id = films.id AND s.number > 0 AND s.air_date <= CURRENT_DATE)
		AND NOT EXISTS (
			SELECT 1 FROM seasons s
			WHERE s.film_id = films.id AND s.number > 0 AND s.air_date <= CURRENT_DATE
			  AND NOT EXISTS (
				SELECT 1 FROM season_entries e
				WHERE e.season_id = s.id AND e.user_id = film_entries.user_id AND e.state = 'watched'))`

	seriesRatedSQL = `films.kind = 'series' AND EXISTS (
		SELECT 1 FROM season_entries e JOIN seasons s ON s.id = e.season_id
		WHERE s.film_id = films.id AND s.number > 0 AND e.user_id = film_entries.user_id AND e.rate IS NOT NULL)`

	seriesWatchingSQL = `films.kind = 'series'
		AND COALESCE(film_entries.series_state, '') <> 'dropped'
		AND EXISTS (
			SELECT 1 FROM season_entries e JOIN seasons s ON s.id = e.season_id
			WHERE s.film_id = films.id AND e.user_id = film_entries.user_id AND e.state = 'watching')`

	watchlistSetSQL = `film_entries.watchlisted`
	watchedSetSQL   = `(films.kind <> 'series' AND film_entries.watched_at IS NOT NULL) OR (` + seriesWatchedSQL + `)`
	ratedSetSQL     = `(films.kind <> 'series' AND film_entries.rate IS NOT NULL) OR (` + seriesRatedSQL + `)`

	entryRateSQL = `COALESCE(film_entries.rate::numeric, (
		SELECT ROUND(AVG(e.rate)::numeric, 1) FROM season_entries e JOIN seasons s ON s.id = e.season_id
		WHERE s.film_id = film_entries.film_id AND s.number > 0 AND e.user_id = film_entries.user_id AND e.rate IS NOT NULL))`

	seriesLastWatchedSQL = `(SELECT MAX(e.watched_at) FROM season_entries e JOIN seasons s ON s.id = e.season_id
		WHERE s.film_id = film_entries.film_id AND s.number > 0 AND e.user_id = film_entries.user_id AND e.state = 'watched')`

	entryWatchedSQL = `CASE WHEN films.kind = 'series'
		THEN COALESCE(` + seriesLastWatchedSQL + `, film_entries.watched_at)
		ELSE film_entries.watched_at END`
)
