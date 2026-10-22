package postgres

import (
	"context"
	"fmt"
	"strings"

	"lifelog-api/internal/film"
	"lifelog-api/internal/indexer"

	"gorm.io/gorm"
)

type IndexRepository struct {
	db *gorm.DB
}

func NewIndexRepository(db *gorm.DB) *IndexRepository {
	return &IndexRepository{db: db}
}

func (r *IndexRepository) FilmsBatch(ctx context.Context, afterID uint, limit int) ([]film.Film, error) {
	var films []film.Film

	err := r.db.WithContext(ctx).
		Preload("Genres").
		Preload("Credits", func(db *gorm.DB) *gorm.DB {
			return db.Order("film_credits.billing_order ASC")
		}).
		Preload("Credits.Person").
		Where("id > ?", afterID).
		Order("id ASC").
		Limit(limit).
		Find(&films).Error
	if err != nil {
		return nil, err
	}
	return films, nil
}

type personRow struct {
	ID           uint
	Name         string
	OriginalName string
	PhotoURL     string
	TMDBID       int
	Roles        string
	FilmCount    int
	Popularity   float32
	KnownFor     string
}

const peopleSelect = `
		SELECT p.id,
		       p.name,
		       p.original_name,
		       p.photo_url,
		       p.tmdb_id,
		       string_agg(DISTINCT fc.role, ',')      AS roles,
		       COUNT(DISTINCT fc.film_id)             AS film_count,
		       COALESCE(MAX(f.tmdb_popularity), 0)    AS popularity,
		       COALESCE((
		           SELECT string_agg(t.title, '|')
		           FROM (
		               SELECT COALESCE(NULLIF(f2.title_ru, ''), f2.title_en) AS title
		               FROM film_credits fc2
		               JOIN films f2 ON f2.id = fc2.film_id
		               WHERE fc2.person_id = p.id
		               ORDER BY f2.tmdb_popularity DESC
		               LIMIT 3
		           ) t
		       ), '')                                 AS known_for
		FROM people p
		JOIN film_credits fc ON fc.person_id = p.id
		JOIN films f ON f.id = fc.film_id
		WHERE %s
		GROUP BY p.id, p.name, p.original_name, p.photo_url, p.tmdb_id
		ORDER BY p.id
		%s`

func (r *IndexRepository) PeopleBatch(ctx context.Context, afterID uint, limit int) ([]indexer.PersonRow, error) {
	query := fmt.Sprintf(peopleSelect, "p.id > ? AND fc.person_id > ?", "LIMIT ?")
	var rows []personRow
	if err := r.db.WithContext(ctx).Raw(query, afterID, afterID, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return toPersonRows(rows), nil
}

func (r *IndexRepository) PeopleOfFilms(ctx context.Context, filmIDs []uint) ([]indexer.PersonRow, error) {
	if len(filmIDs) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(peopleSelect, "p.id IN (SELECT person_id FROM film_credits WHERE film_id IN ?)", "")
	var rows []personRow
	if err := r.db.WithContext(ctx).Raw(query, filmIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return toPersonRows(rows), nil
}

func toPersonRows(rows []personRow) []indexer.PersonRow {
	people := make([]indexer.PersonRow, 0, len(rows))
	for _, row := range rows {
		people = append(people, indexer.PersonRow{
			ID:           row.ID,
			Name:         row.Name,
			OriginalName: row.OriginalName,
			PhotoURL:     row.PhotoURL,
			TMDBID:       row.TMDBID,
			Roles:        splitNonEmpty(row.Roles, ","),
			FilmCount:    row.FilmCount,
			Popularity:   row.Popularity,
			KnownFor:     splitNonEmpty(row.KnownFor, "|"),
		})
	}
	return people
}

func splitNonEmpty(value, sep string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, sep)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func (r *IndexRepository) FilmsByIDs(ctx context.Context, ids []uint) ([]film.Film, error) {
	var films []film.Film
	err := r.db.WithContext(ctx).
		Preload("Genres").
		Preload("Credits", func(db *gorm.DB) *gorm.DB {
			return db.Order("film_credits.billing_order ASC")
		}).
		Preload("Credits.Person").
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&films).Error
	return films, err
}
