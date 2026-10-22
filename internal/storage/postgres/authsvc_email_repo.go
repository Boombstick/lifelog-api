package postgres

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	uniqueViolation = "23505"
	loginIndex      = "idx_auth_users_login"
	emailIndex      = "idx_auth_users_email"
)

func (r *IdentityRepository) UserByEmail(ctx context.Context, email string) (*authsvc.User, error) {
	return r.firstUser(ctx, r.db.WithContext(ctx).Where("lower(email) = lower(?)", email))
}

func (r *IdentityRepository) EmailCode(ctx context.Context, key string) (*authsvc.EmailCode, error) {
	var code authsvc.EmailCode
	err := r.db.WithContext(ctx).Where("key = ?", key).First(&code).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, authsvc.ErrCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	return &code, nil
}

func (r *IdentityRepository) SaveEmailCode(ctx context.Context, code *authsvc.EmailCode) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(code).Error
}

func (r *IdentityRepository) DeleteEmailCode(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Where("key = ?", key).Delete(&authsvc.EmailCode{}).Error
}

func (r *IdentityRepository) TakeCodeAttempt(ctx context.Context, key string, maxAttempts int, now time.Time) (*authsvc.EmailCode, error) {
	var code authsvc.EmailCode
	result := r.db.WithContext(ctx).Raw(`
		UPDATE auth_email_codes SET attempts = attempts + 1
		WHERE key = ? AND attempts < ? AND code_hash <> '' AND expires_at > ?
		RETURNING *`, key, maxAttempts, now).Scan(&code)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, authsvc.ErrCodeInvalid
	}
	return &code, nil
}

func (r *IdentityRepository) HitRateCounter(ctx context.Context, key string, now time.Time, window time.Duration) (int, error) {
	var hits int
	cutoff := now.Add(-window)
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO auth_rate_counters (key, hits, window_start) VALUES (?, 1, ?)
		ON CONFLICT (key) DO UPDATE SET
			hits = CASE WHEN auth_rate_counters.window_start <= ? THEN 1 ELSE auth_rate_counters.hits + 1 END,
			window_start = CASE WHEN auth_rate_counters.window_start <= ? THEN EXCLUDED.window_start ELSE auth_rate_counters.window_start END
		RETURNING hits`, key, now, cutoff, cutoff).Scan(&hits).Error
	return hits, err
}

func (r *IdentityRepository) ResetRateCounter(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM auth_rate_counters WHERE key = ?`, key).Error
}

func consumeCode(tx *gorm.DB, key, codeHash string) error {
	result := tx.Where("key = ? AND code_hash = ?", key, codeHash).Delete(&authsvc.EmailCode{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return authsvc.ErrCodeInvalid
	}
	return nil
}

func uniqueConflict(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}
	switch pgErr.ConstraintName {
	case loginIndex:
		return authsvc.ErrLoginTaken
	case emailIndex:
		return authsvc.ErrEmailTaken
	}
	return err
}

func (r *IdentityRepository) CreateUserFromCode(ctx context.Context, key, codeHash string, user *authsvc.User, passwordHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := consumeCode(tx, key, codeHash); err != nil {
			return err
		}
		if err := tx.Create(user).Error; err != nil {
			return uniqueConflict(err)
		}
		password := authsvc.Password{UserID: user.ID, Hash: passwordHash, UpdatedAt: time.Now()}
		return tx.Create(&password).Error
	})
}

func (r *IdentityRepository) ResetPasswordWithCode(ctx context.Context, key, codeHash string, userID uuid.UUID, passwordHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := consumeCode(tx, key, codeHash); err != nil {
			return err
		}
		password := authsvc.Password{UserID: userID, Hash: passwordHash, UpdatedAt: time.Now()}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"hash", "updated_at"}),
		}).Create(&password).Error; err != nil {
			return err
		}
		return tx.Model(&authsvc.Session{}).
			Where("user_id = ? AND revoked_at IS NULL", userID).
			Update("revoked_at", time.Now()).Error
	})
}

func (r *IdentityRepository) AttachEmailWithCode(ctx context.Context, key, codeHash string, userID uuid.UUID, email string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := consumeCode(tx, key, codeHash); err != nil {
			return err
		}
		if err := tx.Model(&authsvc.User{}).Where("id = ?", userID).Update("email", email).Error; err != nil {
			return uniqueConflict(err)
		}
		return nil
	})
}
