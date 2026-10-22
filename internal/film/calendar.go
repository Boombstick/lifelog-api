package film

import (
	"time"

	"github.com/google/uuid"
)

type CalendarKind string

const (
	CalendarFilmRelease CalendarKind = "film_release"
	CalendarSeasonStart CalendarKind = "season_start"
)

type UpcomingEvent struct {
	AuthUserID uuid.UUID    `json:"user_id"`
	Kind       CalendarKind `json:"kind"`
	FilmID     uint         `json:"film_id"`
	Title      string       `json:"title"`
	Date       time.Time    `json:"date"`

	SeasonNumber *int   `json:"season_number,omitempty"`
	PosterURL    string `json:"poster_url,omitempty"`
}
