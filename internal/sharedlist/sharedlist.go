package sharedlist

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	MaxTitleLength = 80
	MaxMembers     = 20
	MaxItems       = 200

	KeptDraws = 50

	MinTopCount = 3
	MaxTopCount = 10
)

type Mode string

const (
	ModeOne Mode = "one"
	ModeTop Mode = "top"
)

var (
	ErrListFull  = errors.New("в списке уже 200 фильмов")
	ErrListEmpty = errors.New("список пуст")
)

type List struct {
	ID          uint      `gorm:"primaryKey"`
	OwnerAuthID uuid.UUID `gorm:"type:uuid;not null"`
	Title       string    `gorm:"size:80;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (List) TableName() string { return "shared_lists" }

type Member struct {
	ListID       uint      `gorm:"primaryKey"`
	MemberAuthID uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt    time.Time
}

func (Member) TableName() string { return "shared_list_members" }

type Item struct {
	ListID        uint      `gorm:"primaryKey"`
	FilmID        uint      `gorm:"primaryKey"`
	AddedByAuthID uuid.UUID `gorm:"type:uuid;not null"`
	AddedAt       time.Time
}

func (Item) TableName() string { return "shared_list_items" }

type Draw struct {
	ID            uint `gorm:"primaryKey"`
	ListID        uint
	Mode          Mode
	Count         int
	DrawnByAuthID uuid.UUID `gorm:"type:uuid;not null"`
	CreatedAt     time.Time

	FilmIDs []uint `gorm:"-"`
}

func (Draw) TableName() string { return "shared_list_draws" }

type DrawFilm struct {
	DrawID   uint `gorm:"primaryKey"`
	Position int  `gorm:"primaryKey"`
	FilmID   uint
}

func (DrawFilm) TableName() string { return "shared_list_draw_films" }
