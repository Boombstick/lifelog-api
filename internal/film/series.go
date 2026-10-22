package film

import "time"

type SeriesDetails struct {
	FilmID uint `gorm:"primaryKey;autoIncrement:false"`
	Film   Film `gorm:"foreignKey:FilmID;constraint:OnDelete:CASCADE"`

	Status           string     `gorm:"size:32"`
	Type             string     `gorm:"size:32"`
	FirstAirDate     *time.Time `gorm:"type:date"`
	LastAirDate      *time.Time `gorm:"type:date"`
	NextAirDate      *time.Time `gorm:"type:date"`
	NumberOfSeasons  int
	NumberOfEpisodes int
	InProduction     bool `gorm:"not null"`

	Networks string `gorm:"type:text"`
	SyncedAt time.Time
}

func (SeriesDetails) TableName() string { return "series_details" }

type Season struct {
	ID     uint `gorm:"primaryKey"`
	FilmID uint `gorm:"not null;uniqueIndex:idx_seasons_film_number,priority:1"`
	Film   Film `gorm:"foreignKey:FilmID;constraint:OnDelete:CASCADE"`
	TMDBID int  `gorm:"not null;uniqueIndex"`

	Number       int        `gorm:"not null;uniqueIndex:idx_seasons_film_number,priority:2"`
	Name         string     `gorm:"size:255"`
	Overview     string     `gorm:"size:2000"`
	AirDate      *time.Time `gorm:"type:date"`
	EpisodeCount int
	PosterURL    string `gorm:"size:512"`
	SyncedAt     time.Time
}
