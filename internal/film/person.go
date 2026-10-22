package film

import "time"

type Person struct {
	ID           uint   `gorm:"primarykey"`
	Name         string `gorm:"size:255;not null"`
	OriginalName string `gorm:"size:255"`

	PhotoURL string       `gorm:"size:255"`
	TMDBID   int          `gorm:"uniqueIndex;not null"`
	Credits  []FilmCredit `gorm:"foreignKey:PersonID"`
	SyncedAt time.Time
}
