package postgres

import (
	"context"
	"fmt"
	"time"

	"lifelog-api/internal/film"

	"github.com/google/uuid"
)

const upcomingSQL = `
SELECT u.auth_user_id                               AS auth_user_id,
       'film_release'                               AS kind,
       f.id                                         AS film_id,
       COALESCE(NULLIF(f.title_ru, ''), f.title_en) AS title,
       f.release_date                               AS date,
       NULL::int                                    AS season_number,
       f.poster_url                                 AS poster_url
FROM film_entries fe
JOIN users u ON u.id = fe.user_id AND u.auth_user_id IS NOT NULL
JOIN films f ON f.id = fe.film_id
WHERE fe.watchlisted%[1]s
  AND f.kind = 'movie'
  AND f.release_date >= ? AND f.release_date < ?

UNION ALL

SELECT u.auth_user_id,
       'season_start',
       f.id,
       COALESCE(NULLIF(f.title_ru, ''), f.title_en),
       s.air_date,
       s.number,
       COALESCE(NULLIF(s.poster_url, ''), f.poster_url)
FROM film_entries fe
JOIN users u ON u.id = fe.user_id AND u.auth_user_id IS NOT NULL
JOIN films f ON f.id = fe.film_id
JOIN seasons s ON s.film_id = f.id
WHERE f.kind = 'series'%[1]s
  AND COALESCE(fe.series_state, '') <> 'dropped'
  AND s.number > 0
  AND s.air_date >= ? AND s.air_date < ?

ORDER BY date, title`

type upcomingRow struct {
	AuthUserID   uuid.UUID
	Kind         string
	FilmID       uint
	Title        string
	Date         time.Time
	SeasonNumber *int
	PosterURL    string
}

func (r *FilmRepository) Upcoming(ctx context.Context, from, to time.Time, authUser uuid.UUID) ([]film.UpcomingEvent, error) {
	query := fmt.Sprintf(upcomingSQL, "")
	args := []any{from, to, from, to}
	if authUser != uuid.Nil {
		query = fmt.Sprintf(upcomingSQL, " AND u.auth_user_id = ?")
		args = []any{authUser, from, to, authUser, from, to}
	}

	var rows []upcomingRow
	if err := r.db.WithContext(ctx).
		Raw(query, args...).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]film.UpcomingEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, film.UpcomingEvent{
			AuthUserID:   row.AuthUserID,
			Kind:         film.CalendarKind(row.Kind),
			FilmID:       row.FilmID,
			Title:        row.Title,
			Date:         row.Date,
			SeasonNumber: row.SeasonNumber,
			PosterURL:    row.PosterURL,
		})
	}
	return events, nil
}
