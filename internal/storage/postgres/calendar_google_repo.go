package postgres

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/calendar"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *CalendarRepository) SaveGoogleState(ctx context.Context, state *calendar.GoogleState) error {
	return r.db.WithContext(ctx).Create(state).Error
}

func (r *CalendarRepository) TakeGoogleState(ctx context.Context, hash string, now time.Time) (*calendar.GoogleState, error) {
	var states []calendar.GoogleState
	err := r.db.WithContext(ctx).
		Clauses(clause.Returning{}).
		Where("hash = ?", hash).
		Delete(&states).Error
	if err != nil {
		return nil, err
	}
	if len(states) == 0 || !states[0].ExpiresAt.After(now) {
		return nil, calendar.ErrGoogleBadState
	}
	return &states[0], nil
}

func (r *CalendarRepository) ForgetGoogleStates(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", before).
		Delete(&calendar.GoogleState{}).Error
}

func (r *CalendarRepository) GoogleLink(ctx context.Context, userID uuid.UUID) (*calendar.GoogleLink, error) {
	var link calendar.GoogleLink
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, calendar.ErrGoogleNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *CalendarRepository) SaveGoogleLink(ctx context.Context, link *calendar.GoogleLink) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"refresh_token", "email", "revoked", "last_sync_at", "last_error", "updated_at",
			}),
		}).
		Create(link).Error
}

func (r *CalendarRepository) ActiveGoogleLinks(ctx context.Context) ([]calendar.GoogleLink, error) {
	var links []calendar.GoogleLink
	err := r.db.WithContext(ctx).Where("NOT revoked").Order("user_id").Find(&links).Error
	return links, err
}

func (r *CalendarRepository) MarkGoogleSync(
	ctx context.Context,
	userID uuid.UUID,
	syncedAt *time.Time,
	lastError string,
	revoked bool,
) error {
	return r.db.WithContext(ctx).
		Model(&calendar.GoogleLink{}).
		Where("user_id = ?", userID).
		Updates(map[string]any{
			"last_sync_at": syncedAt,
			"last_error":   lastError,
			"revoked":      revoked,
			"updated_at":   time.Now().UTC(),
		}).Error
}

func (r *CalendarRepository) DeleteGoogleLink(ctx context.Context, userID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&calendar.ImportedEvent{}).Error; err != nil {
			return err
		}
		result := tx.Where("user_id = ?", userID).Delete(&calendar.GoogleLink{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return calendar.ErrGoogleNotLinked
		}
		return nil
	})
}

func (r *CalendarRepository) ReplaceImported(ctx context.Context, userID uuid.UUID, events []calendar.ImportedEvent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&calendar.ImportedEvent{}).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}

		position := make(map[string]int, len(events))
		unique := make([]calendar.ImportedEvent, 0, len(events))
		for _, event := range events {
			if i, seen := position[event.GoogleID]; seen {
				unique[i] = event
				continue
			}
			position[event.GoogleID] = len(unique)
			unique = append(unique, event)
		}
		return tx.CreateInBatches(unique, 500).Error
	})
}

func (r *CalendarRepository) ImportedBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]calendar.ImportedEvent, error) {
	var events []calendar.ImportedEvent
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Where("starts_at < ? AND COALESCE(ends_at, starts_at) >= ?", to, from).
		Order("starts_at").
		Find(&events).Error
	return events, err
}

func (r *CalendarRepository) AllImportedBetween(ctx context.Context, from, to time.Time) ([]calendar.ImportedEvent, error) {
	var events []calendar.ImportedEvent
	err := r.db.WithContext(ctx).
		Where("starts_at >= ? AND starts_at < ?", from, to).
		Order("starts_at").
		Find(&events).Error
	return events, err
}
