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

const telegramLinkIndex = "idx_auth_telegram_links_telegram_id"

type IdentityRepository struct {
	db *gorm.DB
}

func NewIdentityRepository(db *gorm.DB) *IdentityRepository {
	return &IdentityRepository{db: db}
}

func (r *IdentityRepository) CreateUser(ctx context.Context, user *authsvc.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *IdentityRepository) UserByID(ctx context.Context, id uuid.UUID) (*authsvc.User, error) {
	return r.firstUser(ctx, r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *IdentityRepository) UserByLogin(ctx context.Context, login string) (*authsvc.User, error) {
	return r.firstUser(ctx, r.db.WithContext(ctx).Where("lower(login) = lower(?)", login))
}

func (r *IdentityRepository) UserByTelegram(ctx context.Context, telegramID int64) (*authsvc.User, error) {
	query := r.db.WithContext(ctx).
		Joins("JOIN auth_telegram_links ON auth_telegram_links.user_id = auth_users.id").
		Where("auth_telegram_links.telegram_id = ?", telegramID)
	return r.firstUser(ctx, query)
}

func (r *IdentityRepository) firstUser(ctx context.Context, query *gorm.DB) (*authsvc.User, error) {
	var user authsvc.User
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, authsvc.ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *IdentityRepository) SetPassword(ctx context.Context, userID uuid.UUID, hash string) error {
	password := authsvc.Password{UserID: userID, Hash: hash, UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hash", "updated_at"}),
	}).Create(&password).Error
}

func (r *IdentityRepository) PasswordOf(ctx context.Context, userID uuid.UUID) (*authsvc.Password, error) {
	var password authsvc.Password
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&password).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, authsvc.ErrNoPassword
		}
		return nil, err
	}
	return &password, nil
}

func (r *IdentityRepository) LinkTelegram(ctx context.Context, userID uuid.UUID, telegramID int64) error {
	link := authsvc.TelegramLink{UserID: userID, TelegramID: telegramID, LinkedAt: time.Now()}
	err := r.db.WithContext(ctx).Create(&link).Error
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}
	if pgErr.ConstraintName == telegramLinkIndex {
		return authsvc.ErrTelegramTaken
	}
	return authsvc.ErrTelegramAlreadyLinked
}

func (r *IdentityRepository) UnlinkTelegram(ctx context.Context, userID uuid.UUID) error {
	result := r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&authsvc.TelegramLink{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return authsvc.ErrTelegramNotLinked
	}
	return nil
}

func (r *IdentityRepository) CreateSession(ctx context.Context, session *authsvc.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *IdentityRepository) SessionByRefreshHash(ctx context.Context, hash string) (*authsvc.Session, error) {
	var session authsvc.Session
	if err := r.db.WithContext(ctx).Where("refresh_hash = ?", hash).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, authsvc.ErrRefreshInvalid
		}
		return nil, err
	}
	return &session, nil
}

func (r *IdentityRepository) RotateSession(ctx context.Context, oldID uuid.UUID, next *authsvc.Session) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&authsvc.Session{}).
			Where("id = ? AND used_at IS NULL", oldID).
			Update("used_at", time.Now())
		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			return authsvc.ErrRefreshReused
		}
		return tx.Create(next).Error
	})
}

func (r *IdentityRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&authsvc.Session{}).
		Where("family_id = ? AND revoked_at IS NULL", familyID).
		Update("revoked_at", time.Now()).Error
}

func (r *IdentityRepository) ReplaceNonce(ctx context.Context, nonce *authsvc.LinkNonce) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND used_at IS NULL", nonce.UserID).Delete(&authsvc.LinkNonce{}).Error; err != nil {
			return err
		}
		return tx.Create(nonce).Error
	})
}

func (r *IdentityRepository) ConsumeNonce(ctx context.Context, code string) (*authsvc.LinkNonce, error) {
	var nonces []authsvc.LinkNonce
	err := r.db.WithContext(ctx).Raw(
		`UPDATE auth_link_nonces SET used_at = ? WHERE code = ? AND used_at IS NULL AND expires_at > ?
		 RETURNING code, user_id, created_at, expires_at, used_at`,
		time.Now(), code, time.Now()).Scan(&nonces).Error
	if err != nil {
		return nil, err
	}
	if len(nonces) == 0 {
		return nil, authsvc.ErrNonceInvalid
	}
	return &nonces[0], nil
}

func (r *IdentityRepository) AbsorbSilent(ctx context.Context, silent, into uuid.UUID) (*authsvc.Merge, error) {
	merge := authsvc.Merge{
		ID:        uuid.New(),
		FromUser:  silent,
		IntoUser:  into,
		Status:    authsvc.MergeStatusPending,
		CreatedAt: time.Now(),
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var source authsvc.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", silent).First(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authsvc.ErrTelegramTaken
		}
		if err != nil {
			return err
		}
		if !source.IsSilent() {
			return authsvc.ErrTelegramTaken
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", into).First(&authsvc.User{}).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authsvc.ErrUserNotFound
			}
			return err
		}

		var targetLinks int64
		if err := tx.Model(&authsvc.TelegramLink{}).Where("user_id = ?", into).Count(&targetLinks).Error; err != nil {
			return err
		}
		if targetLinks > 0 {
			return authsvc.ErrTelegramAlreadyLinked
		}

		var link authsvc.TelegramLink
		err = tx.Where("user_id = ?", silent).First(&link).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
		case err != nil:
			return err
		default:

			if err := tx.Where("user_id = ?", silent).Delete(&authsvc.TelegramLink{}).Error; err != nil {
				return err
			}
			moved := authsvc.TelegramLink{UserID: into, TelegramID: link.TelegramID, LinkedAt: time.Now()}
			if err := tx.Create(&moved).Error; err != nil {
				return err
			}
		}

		if err := deleteUserRows(tx, silent); err != nil {
			return err
		}
		return tx.Create(&merge).Error
	})
	if err != nil {
		return nil, err
	}
	return &merge, nil
}

func deleteUserRows(tx *gorm.DB, userID uuid.UUID) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM auth_passwords WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM auth_telegram_links WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM auth_sessions WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM auth_link_nonces WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM auth_email_codes WHERE user_id = ?`, []any{userID}},
		{`DELETE FROM auth_subscriptions WHERE follower_id = ? OR target_id = ?`, []any{userID, userID}},
		{`DELETE FROM auth_blocks WHERE blocker_id = ? OR blocked_id = ?`, []any{userID, userID}},
		{`DELETE FROM auth_users WHERE id = ?`, []any{userID}},
	}
	for _, statement := range statements {
		if err := tx.Exec(statement.query, statement.args...).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *IdentityRepository) PendingMerges(ctx context.Context) ([]authsvc.Merge, error) {
	var merges []authsvc.Merge
	err := r.db.WithContext(ctx).Where("status = ?", authsvc.MergeStatusPending).Order("created_at").Find(&merges).Error
	return merges, err
}

func (r *IdentityRepository) CompleteMerge(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&authsvc.Merge{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": authsvc.MergeStatusDone, "done_at": now}).Error
}

func (r *IdentityRepository) CreateUserWithPassword(ctx context.Context, user *authsvc.User, hash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		password := authsvc.Password{UserID: user.ID, Hash: hash, UpdatedAt: time.Now()}
		return tx.Create(&password).Error
	})
}

func (r *IdentityRepository) DeleteExpired(ctx context.Context) (int64, error) {
	var removed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cutoff := time.Now().Add(-24 * time.Hour)
		result := tx.Where("expires_at < ? OR (revoked_at IS NOT NULL AND revoked_at < ?)", time.Now(), cutoff).
			Delete(&authsvc.Session{})
		if result.Error != nil {
			return result.Error
		}
		removed = result.RowsAffected

		codes := tx.Where("expires_at < ?", time.Now()).Delete(&authsvc.EmailCode{})
		if codes.Error != nil {
			return codes.Error
		}
		removed += codes.RowsAffected

		counters := tx.Exec("DELETE FROM auth_rate_counters WHERE window_start < ?", cutoff)
		if counters.Error != nil {
			return counters.Error
		}
		removed += counters.RowsAffected

		nonces := tx.Where("expires_at < ?", time.Now()).Delete(&authsvc.LinkNonce{})
		if nonces.Error != nil {
			return nonces.Error
		}
		removed += nonces.RowsAffected
		return nil
	})
	return removed, err
}

func (r *IdentityRepository) ActiveSessions(ctx context.Context, userID uuid.UUID) ([]authsvc.DeviceSession, error) {
	var sessions []authsvc.DeviceSession
	err := r.db.WithContext(ctx).Raw(`
		SELECT family_id,
		       (array_agg(user_agent ORDER BY issued_at DESC))[1] AS user_agent,
		       min(issued_at) AS first_seen,
		       max(issued_at) AS last_seen,
		       max(expires_at) AS expires_at
		FROM auth_sessions
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > now()
		GROUP BY family_id
		ORDER BY max(issued_at) DESC`, userID).Scan(&sessions).Error
	return sessions, err
}

func (r *IdentityRepository) RevokeUserFamily(ctx context.Context, userID, familyID uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&authsvc.Session{}).
		Where("family_id = ? AND user_id = ? AND revoked_at IS NULL", familyID, userID).
		Update("revoked_at", time.Now())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return authsvc.ErrSessionNotFound
	}
	return nil
}

func (r *IdentityRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&authsvc.Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", time.Now()).Error
}

func (r *IdentityRepository) RevokeDevice(ctx context.Context, userID uuid.UUID, deviceID string) error {
	return r.db.WithContext(ctx).Model(&authsvc.Session{}).
		Where("user_id = ? AND device_id = ? AND revoked_at IS NULL", userID, deviceID).
		Update("revoked_at", time.Now()).Error
}

func (r *IdentityRepository) RevokeOtherFamilies(ctx context.Context, userID, keep uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&authsvc.Session{}).
		Where("user_id = ? AND family_id <> ? AND revoked_at IS NULL", userID, keep).
		Update("revoked_at", time.Now()).Error
}

func (r *IdentityRepository) Subscribe(ctx context.Context, subscription *authsvc.Subscription) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "follower_id"}, {Name: "target_id"}},
		DoNothing: true,
	}).Create(subscription).Error
}

func (r *IdentityRepository) Unsubscribe(ctx context.Context, followerID, targetID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("follower_id = ? AND target_id = ?", followerID, targetID).
		Delete(&authsvc.Subscription{}).Error
}

func (r *IdentityRepository) SetSubscriptionStatus(ctx context.Context, followerID, targetID uuid.UUID, status string) error {
	result := r.db.WithContext(ctx).Model(&authsvc.Subscription{}).
		Where("follower_id = ? AND target_id = ?", followerID, targetID).
		Updates(map[string]interface{}{"status": status, "decided_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return authsvc.ErrSubscriptionAbsent
	}
	return nil
}

func (r *IdentityRepository) SubscriptionsOf(ctx context.Context, followerID uuid.UUID) ([]authsvc.SubscriptionView, error) {
	return r.subscriptionViews(ctx, `
		SELECT u.id AS user_id, COALESCE(u.login, '') AS login, u.display_name, s.status, s.created_at
		FROM auth_subscriptions s
		JOIN auth_users u ON u.id = s.target_id
		WHERE s.follower_id = ?
		ORDER BY s.created_at DESC`, followerID)
}

func (r *IdentityRepository) SubscribersOf(ctx context.Context, targetID uuid.UUID) ([]authsvc.SubscriptionView, error) {
	return r.subscriptionViews(ctx, `
		SELECT u.id AS user_id, COALESCE(u.login, '') AS login, u.display_name, s.status, s.created_at
		FROM auth_subscriptions s
		JOIN auth_users u ON u.id = s.follower_id
		WHERE s.target_id = ?
		ORDER BY s.created_at DESC`, targetID)
}

func (r *IdentityRepository) subscriptionViews(ctx context.Context, query string, id uuid.UUID) ([]authsvc.SubscriptionView, error) {
	var views []authsvc.SubscriptionView
	err := r.db.WithContext(ctx).Raw(query, id).Scan(&views).Error
	return views, err
}

func (r *IdentityRepository) UpdateProfile(ctx context.Context, userID uuid.UUID, role authsvc.Role, email *string, displayName string) error {
	err := r.db.WithContext(ctx).Model(&authsvc.User{}).Where("id = ?", userID).Updates(map[string]any{
		"role":         role,
		"email":        email,
		"display_name": displayName,
	}).Error
	return uniqueConflict(err)
}

func (r *IdentityRepository) DemoteAdmins(ctx context.Context, keep uuid.UUID) (int64, error) {
	result := r.db.WithContext(ctx).Model(&authsvc.User{}).
		Where("role = ? AND id <> ?", authsvc.RoleAdmin, keep).
		Update("role", authsvc.RoleUser)
	return result.RowsAffected, result.Error
}

func (r *IdentityRepository) SetPrivate(ctx context.Context, userID uuid.UUID, isPrivate bool) error {
	return r.db.WithContext(ctx).Model(&authsvc.User{}).
		Where("id = ?", userID).
		Update("is_private", isPrivate).Error
}

func (r *IdentityRepository) SetDisplayName(ctx context.Context, userID uuid.UUID, name string) error {
	result := r.db.WithContext(ctx).Model(&authsvc.User{}).
		Where("id = ?", userID).
		Update("display_name", name)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return authsvc.ErrUserNotFound
	}
	return nil
}

func (r *IdentityRepository) TelegramIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := make(map[uuid.UUID]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}

	var rows []struct {
		UserID     uuid.UUID
		TelegramID int64
	}
	if err := r.db.WithContext(ctx).
		Table("auth_telegram_links").
		Select("auth_telegram_links.user_id, auth_telegram_links.telegram_id").
		Where("auth_telegram_links.user_id IN ?", userIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = row.TelegramID
	}
	return out, nil
}
