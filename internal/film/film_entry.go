package film

import (
	"time"

	"lifelog-api/internal/auth"
)

type FilmEntry struct {
	UserID      uint       `gorm:"primaryKey"`
	FilmID      uint       `gorm:"primaryKey"`
	User        auth.User  `gorm:"foreignKey:UserID"`
	Film        Film       `gorm:"foreignKey:FilmID"`
	Watchlisted bool       `gorm:"not null;default:false"`
	WatchedAt   *time.Time `gorm:"check:chk_film_entry_needs_watched,(rate IS NULL AND description IS NULL) OR watched_at IS NOT NULL"`
	Rate        *int16     `gorm:"check:chk_film_entry_rate_range,rate IS NULL OR rate BETWEEN 1 AND 10"`
	Description *string    `gorm:"size:255"`

	SeriesState *string `gorm:"size:16"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type EntrySet string

const (
	EntrySetWatchlist EntrySet = "watchlist"
	EntrySetWatched   EntrySet = "watched"
	EntrySetRated     EntrySet = "rated"
	EntrySetWatching  EntrySet = "watching"
)

func (s EntrySet) Valid() bool {
	switch s {
	case EntrySetWatchlist, EntrySetWatched, EntrySetRated, EntrySetWatching:
		return true
	}
	return false
}

type EntrySort string

const (
	EntrySortAdded   EntrySort = "added"
	EntrySortWatched EntrySort = "watched"
	EntrySortRate    EntrySort = "rate"
	EntrySortTitle   EntrySort = "title"
)

func (s EntrySort) AllowedIn(set EntrySet) bool {
	switch s {
	case EntrySortAdded, EntrySortTitle:
		return true
	case EntrySortWatched:
		return set == EntrySetWatched || set == EntrySetRated
	case EntrySortRate:
		return set == EntrySetRated
	}
	return false
}

func DefaultEntrySort(set EntrySet) EntrySort {
	switch set {
	case EntrySetWatched:
		return EntrySortWatched
	case EntrySetRated:
		return EntrySortRate
	default:
		return EntrySortAdded
	}
}

type EntryFilter struct {
	UserID uint
	Set    EntrySet
	Sort   EntrySort

	AfterValue any
	AfterID    uint
	Limit      int
}

type EntryCounts struct {
	Watchlist int
	Watched   int
	Rated     int
	Watching  int
}
