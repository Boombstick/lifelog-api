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

type CalendarRepository struct {
	db *gorm.DB
}

func NewCalendarRepository(db *gorm.DB) *CalendarRepository {
	return &CalendarRepository{db: db}
}

func (r *CalendarRepository) CreateEvent(ctx context.Context, event *calendar.Event) error {
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *CalendarRepository) UpdateEvent(ctx context.Context, event *calendar.Event) error {
	result := r.db.WithContext(ctx).
		Model(&calendar.Event{}).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", event.ID, event.UserID).
		Updates(map[string]any{
			"title":      event.Title,
			"details":    event.Details,
			"location":   event.Location,
			"starts_at":  event.StartsAt,
			"ends_at":    event.EndsAt,
			"all_day":    event.AllDay,
			"timezone":   event.Timezone,
			"updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return calendar.ErrNotFound
	}
	return nil
}

func (r *CalendarRepository) DeleteEvent(ctx context.Context, userID, id uuid.UUID) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).
		Model(&calendar.Event{}).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		Update("deleted_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return calendar.ErrNotFound
	}
	return nil
}

func (r *CalendarRepository) Event(ctx context.Context, userID, id uuid.UUID) (*calendar.Event, error) {
	var event calendar.Event
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, calendar.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *CalendarRepository) EventsBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]calendar.Event, error) {
	var events []calendar.Event
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Where("starts_at < ? AND COALESCE(ends_at, starts_at) >= ?", to, from).
		Order("starts_at").
		Find(&events).Error
	return events, err
}

func (r *CalendarRepository) AllEventsBetween(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
	var events []calendar.Event
	err := r.db.WithContext(ctx).
		Where("deleted_at IS NULL").
		Where("starts_at >= ? AND starts_at < ?", from, to).
		Order("starts_at").
		Find(&events).Error
	return events, err
}

func (r *CalendarRepository) Settings(ctx context.Context, userID uuid.UUID) (*calendar.Settings, error) {
	var settings calendar.Settings
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, calendar.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *CalendarRepository) SaveSettings(ctx context.Context, settings *calendar.Settings) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"digest_at", "timezone", "enabled", "lead_minutes", "updated_at",
			}),
		}).
		Select("user_id", "digest_at", "timezone", "enabled", "lead_minutes", "created_at", "updated_at").
		Create(settings).Error
}

func (r *CalendarRepository) AllSettings(ctx context.Context) ([]calendar.Settings, error) {
	defaults := calendar.DefaultSettings(uuid.Nil)
	var settings []calendar.Settings
	err := r.db.WithContext(ctx).Raw(`
		SELECT user_id, digest_at, timezone, enabled, lead_minutes
		FROM calendar_settings
		WHERE enabled
		UNION ALL
		SELECT p.user_id, ?::text, ?::text, ?::boolean, ?::bigint
		FROM (
			SELECT user_id FROM calendar_events
			WHERE deleted_at IS NULL AND COALESCE(ends_at, starts_at) >= now() - interval '2 days'
			UNION
			SELECT user_id FROM calendar_imported_events
			WHERE COALESCE(ends_at, starts_at) >= now() - interval '2 days'
		) p
		WHERE ?::boolean
			AND NOT EXISTS (SELECT 1 FROM calendar_settings s WHERE s.user_id = p.user_id)`,
		defaults.DigestAt, defaults.Timezone, defaults.Enabled, defaults.LeadMinutes, defaults.Enabled,
	).Scan(&settings).Error
	return settings, err
}

func (r *CalendarRepository) MarkSent(ctx context.Context, userID uuid.UUID, ref, slot string) (bool, error) {
	delivery := calendar.Delivery{UserID: userID, Ref: ref, Slot: slot, SentAt: time.Now().UTC()}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&delivery)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *CalendarRepository) ForgetDelivery(ctx context.Context, userID uuid.UUID, ref, slot string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND ref = ? AND slot = ?", userID, ref, slot).
		Delete(&calendar.Delivery{}).Error
}

func (r *CalendarRepository) ForgetDeliveries(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("sent_at < ?", before).
		Delete(&calendar.Delivery{})
	return result.RowsAffected, result.Error
}
