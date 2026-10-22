package auth

import (
	"time"

	"github.com/google/uuid"
)

const (
	VisibilityPrivate     = "private"
	VisibilitySubscribers = "subscribers"
)

type Subscription struct {
	FollowerAuthID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TargetAuthID   uuid.UUID `gorm:"type:uuid;primaryKey;index"`
	UpdatedAt      time.Time `gorm:"not null"`
}

func (Subscription) TableName() string { return "subscriptions" }

type ListVisibility struct {
	UserID    uint   `gorm:"primaryKey"`
	Watchlist string `gorm:"size:16;not null"`
	Watched   string `gorm:"size:16;not null"`
	Rated     string `gorm:"size:16;not null"`
}

func (ListVisibility) TableName() string { return "list_visibility" }

func DefaultVisibility(userID uint) ListVisibility {
	return ListVisibility{
		UserID:    userID,
		Watchlist: VisibilityPrivate,
		Watched:   VisibilitySubscribers,
		Rated:     VisibilitySubscribers,
	}
}

func ValidVisibility(level string) bool {
	return level == VisibilityPrivate || level == VisibilitySubscribers
}

func (v ListVisibility) For(set string) string {
	switch set {
	case "watched", "watching":
		return v.Watched
	case "rated":
		return v.Rated
	default:
		return v.Watchlist
	}
}

type ShareLink struct {
	Token  string `gorm:"size:43;primaryKey"`
	UserID uint   `gorm:"index;not null"`
	Set    string `gorm:"size:16;not null"`
	FilmID *uint

	ExpiresAt time.Time  `gorm:"not null;index"`
	CreatedAt time.Time  `gorm:"not null"`
	RevokedAt *time.Time `gorm:"index"`
}

func (l ShareLink) Alive(now time.Time) bool {
	return l.RevokedAt == nil && now.Before(l.ExpiresAt)
}

func (ShareLink) TableName() string { return "share_links" }
