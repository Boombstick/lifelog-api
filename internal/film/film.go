package film

import (
	"time"
)

type Kind string

const (
	KindMovie  Kind = "movie"
	KindSeries Kind = "series"
)

type Film struct {
	ID        uint `gorm:"primaryKey;autoIncrement:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
	TitleEn   string       `gorm:"size:255;not null"`
	TitleRu   string       `gorm:"size:255;not null"`
	Kind      Kind         `gorm:"size:16;not null;default:movie;uniqueIndex:idx_films_kind_tmdb_id,priority:1"`
	Synopsis  string       `gorm:"size:2000"`
	PosterURL string       `gorm:"size:512"`
	Genres    []Genre      `gorm:"many2many:film_genres;"`
	Credits   []FilmCredit `gorm:"foreignKey:FilmID"`
	Entries   []FilmEntry  `gorm:"foreignKey:FilmID"`

	SyncedAt time.Time
	IMDBID   string `gorm:"size:16;index"`

	TMDBID int `gorm:"uniqueIndex:idx_films_kind_tmdb_id,priority:2"`

	TMDBPopularity float32 `gorm:"index"`
	TMDBPosterPath string  `gorm:"size:255"`
	ReleaseDate    time.Time

	Runtime int `gorm:"index"`

	CollectionID *int        `gorm:"index"`
	Collection   *Collection `gorm:"foreignKey:CollectionID;references:TMDBID"`

	TMDBVoteAverage  float32 `gorm:"index"`
	TMDBVoteCount    int
	LocalRating      float32 `gorm:"index"`
	LocalRatingCount int

	AddedBy *uint      `json:"-"`
	AddedAt *time.Time `json:"-"`
}

type LocalRating struct {
	FilmID uint
	Rating float32
	Votes  int
}
