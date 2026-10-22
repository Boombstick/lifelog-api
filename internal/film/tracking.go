package film

import (
	"errors"
	"time"
)

const (
	SeriesStateWatching = "watching"
	SeriesStateDropped  = "dropped"

	SeasonStateWatching = "watching"
	SeasonStateWatched  = "watched"
)

var (
	ErrNotSeries               = errors.New("это не сериал")
	ErrSeasonNotFound          = errors.New("сезон не найден")
	ErrSeasonNotAired          = errors.New("сезон ещё не вышел")
	ErrBadSeasonState          = errors.New("состояние сезона: watching, watched или null")
	ErrSeasonRate              = errors.New("оценка сезона от 1 до 10 и только у просмотренного сезона")
	ErrSeriesDerived           = errors.New("у сериала просмотр и оценка считаются по сезонам")
	ErrSeriesReviewNeedsSeason = errors.New("отзыв на сериал можно оставить после первого просмотренного сезона")
)

type SeasonMark struct {
	UserID    uint      `gorm:"primaryKey"`
	SeasonID  uint      `gorm:"primaryKey;index"`
	Season    Season    `gorm:"foreignKey:SeasonID;constraint:OnDelete:CASCADE"`
	WatchedAt time.Time `gorm:"not null"`
}

func (SeasonMark) TableName() string { return "season_marks" }

type SeasonEntry struct {
	UserID    uint   `gorm:"primaryKey"`
	SeasonID  uint   `gorm:"primaryKey;index"`
	Season    Season `gorm:"foreignKey:SeasonID;constraint:OnDelete:CASCADE"`
	State     string `gorm:"size:16;not null"`
	Rate      *int16
	WatchedAt *time.Time
	UpdatedAt time.Time `gorm:"not null"`
}

func (SeasonEntry) TableName() string { return "season_entries" }

type SeasonEntryView struct {
	Number int
	State  string
	Rate   *int16
}

func (s Season) Aired(today time.Time) bool {
	return s.AirDate != nil && !s.AirDate.After(today)
}

func AiredRegularSeasons(seasons []Season, today time.Time, upTo *int) []Season {
	aired := make([]Season, 0, len(seasons))
	for _, season := range seasons {
		if season.Number <= 0 || !season.Aired(today) {
			continue
		}
		if upTo != nil && season.Number > *upTo {
			continue
		}
		aired = append(aired, season)
	}
	return aired
}
