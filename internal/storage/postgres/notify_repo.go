package postgres

import (
	"context"
	"time"

	"lifelog-api/internal/notify"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotifyRepository struct {
	db *gorm.DB
}

func NewNotifyRepository(db *gorm.DB) *NotifyRepository {
	return &NotifyRepository{db: db}
}

func (r *NotifyRepository) UpsertDevice(ctx context.Context, d *notify.Device) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if d.Token != nil {
			err := tx.Exec(`DELETE FROM push_devices
				WHERE token = ? AND NOT (user_id = ? AND device_id = ?)`,
				*d.Token, d.UserID, d.DeviceID).Error
			if err != nil {
				return err
			}
		}
		if d.Endpoint != nil {
			err := tx.Exec(`DELETE FROM push_devices
				WHERE endpoint = ? AND NOT (user_id = ? AND device_id = ?)`,
				*d.Endpoint, d.UserID, d.DeviceID).Error
			if err != nil {
				return err
			}
		}
		err := tx.Exec(`INSERT INTO push_devices
				(id, user_id, device_id, channel, token, endpoint, p256dh, auth,
				 platform, label, enabled, created_at, updated_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, device_id) DO UPDATE SET
				channel      = EXCLUDED.channel,
				token        = EXCLUDED.token,
				endpoint     = EXCLUDED.endpoint,
				p256dh       = EXCLUDED.p256dh,
				auth         = EXCLUDED.auth,
				platform     = EXCLUDED.platform,
				label        = EXCLUDED.label,
				updated_at   = EXCLUDED.updated_at,
				last_seen_at = EXCLUDED.last_seen_at,
				last_error   = NULL`,
			d.ID, d.UserID, d.DeviceID, d.Channel, d.Token, d.Endpoint, d.P256dh, d.Auth,
			d.Platform, d.Label, d.Enabled, d.CreatedAt, d.UpdatedAt, d.LastSeenAt).Error
		if err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM push_devices
			WHERE user_id = ? AND id NOT IN (
				SELECT id FROM push_devices WHERE user_id = ?
				ORDER BY last_seen_at DESC LIMIT ?)`,
			d.UserID, d.UserID, notify.MaxDevicesPerUser).Error
	})
}

func (r *NotifyRepository) DeleteDevice(ctx context.Context, userID uuid.UUID, deviceID string) error {
	return r.db.WithContext(ctx).
		Exec(`DELETE FROM push_devices WHERE user_id = ? AND device_id = ?`, userID, deviceID).Error
}

func (r *NotifyRepository) SetDeviceEnabled(ctx context.Context, userID uuid.UUID, deviceID string, enabled bool) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE push_devices SET enabled = ?, updated_at = ?
		WHERE user_id = ? AND device_id = ?`, enabled, dbNow(), userID, deviceID)
	return result.RowsAffected == 1, result.Error
}

func (r *NotifyRepository) Devices(ctx context.Context, userID uuid.UUID) ([]notify.Device, error) {
	var devices []notify.Device
	err := r.db.WithContext(ctx).
		Select("device_id", "channel", "platform", "label", "enabled", "created_at", "last_seen_at").
		Where("user_id = ?", userID).
		Order("last_seen_at DESC, id").
		Find(&devices).Error
	return devices, err
}

func (r *NotifyRepository) PushEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var values []bool
	err := r.db.WithContext(ctx).
		Raw(`SELECT push_enabled FROM push_settings WHERE user_id = ?`, userID).
		Scan(&values).Error
	if err != nil || len(values) == 0 {
		return true, err
	}
	return values[0], nil
}

func (r *NotifyRepository) SetPushEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO push_settings (user_id, push_enabled, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET
			push_enabled = EXCLUDED.push_enabled,
			updated_at   = EXCLUDED.updated_at`, userID, enabled, dbNow()).Error
}

func (r *NotifyRepository) Enqueue(ctx context.Context, userIDs []uuid.UUID, channels []string, payload []byte) (int, error) {
	if len(userIDs) == 0 || len(channels) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Exec(`INSERT INTO push_outbox (device_id, payload, attempts, next_attempt_at, created_at)
		SELECT d.id, ?::jsonb, 0, now(), now()
		FROM push_devices d
		LEFT JOIN push_settings s ON s.user_id = d.user_id
		WHERE d.user_id = ANY(?::uuid[])
		  AND d.enabled
		  AND d.channel IN ?
		  AND COALESCE(s.push_enabled, true)`,
		string(payload), uuidArray(userIDs), channels)
	return int(result.RowsAffected), result.Error
}

type claimedRow struct {
	OutboxID int64
	Attempts int
	Payload  string
	DeviceID uuid.UUID
	UserID   uuid.UUID
	Channel  string
	Token    string
	Endpoint string
	P256dh   string
	Auth     string
}

func (r *NotifyRepository) Claim(ctx context.Context, limit int, lease time.Duration) ([]notify.Job, error) {
	var rows []claimedRow
	err := r.db.WithContext(ctx).Raw(`UPDATE push_outbox o
		SET next_attempt_at = now() + make_interval(secs => ?)
		FROM push_devices d
		WHERE o.id IN (
			SELECT id FROM push_outbox
			WHERE next_attempt_at <= now()
			ORDER BY next_attempt_at, id
			LIMIT ?
			FOR UPDATE SKIP LOCKED)
		  AND d.id = o.device_id
		RETURNING o.id AS outbox_id, o.attempts, o.payload::text AS payload,
			d.id AS device_id, d.user_id, d.channel, COALESCE(d.token, '') AS token,
			COALESCE(d.endpoint, '') AS endpoint, COALESCE(d.p256dh, '') AS p256dh,
			COALESCE(d.auth, '') AS auth`,
		lease.Seconds(), limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	jobs := make([]notify.Job, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, notify.Job{
			ID:       row.OutboxID,
			Attempts: row.Attempts,
			Payload:  []byte(row.Payload),
			Target: notify.Target{
				ID:       row.DeviceID,
				UserID:   row.UserID,
				Channel:  row.Channel,
				Token:    row.Token,
				Endpoint: row.Endpoint,
				P256dh:   row.P256dh,
				Auth:     row.Auth,
			},
		})
	}
	return jobs, nil
}

func (r *NotifyRepository) Complete(ctx context.Context, id int64, device uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM push_outbox WHERE id = ?`, id).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE push_devices SET last_error = NULL
			WHERE id = ? AND last_error IS NOT NULL`, device).Error
	})
}

func (r *NotifyRepository) Remove(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM push_outbox WHERE id = ?`, id).Error
}

func (r *NotifyRepository) Reschedule(ctx context.Context, id int64, attempts int, at time.Time) error {
	return r.db.WithContext(ctx).
		Exec(`UPDATE push_outbox SET attempts = ?, next_attempt_at = ? WHERE id = ?`, attempts, at, id).Error
}

func (r *NotifyRepository) RemoveDevice(ctx context.Context, target notify.Target) error {
	return r.db.WithContext(ctx).Exec(`
		DELETE FROM push_devices
		WHERE id = ? AND COALESCE(token, '') = ? AND COALESCE(endpoint, '') = ?`,
		target.ID, target.Token, target.Endpoint).Error
}

func (r *NotifyRepository) DeviceError(ctx context.Context, device uuid.UUID, text string) error {
	return r.db.WithContext(ctx).
		Exec(`UPDATE push_devices SET last_error = ? WHERE id = ?`, text, device).Error
}
