package postgres

import (
	"context"

	"github.com/google/uuid"
)

const followsFilter = `s.status = 'active'
	AND NOT EXISTS (
		SELECT 1 FROM auth_blocks b
		WHERE (b.blocker_id = s.follower_id AND b.blocked_id = s.target_id)
		   OR (b.blocker_id = s.target_id AND b.blocked_id = s.follower_id))`

func (r *IdentityRepository) FollowingOf(ctx context.Context, followerID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.target_id
		FROM auth_subscriptions s
		JOIN auth_users u ON u.id = s.target_id
		WHERE s.follower_id = ? AND `+followsFilter, followerID).
		Scan(&ids).Error
	return ids, err
}

func (r *IdentityRepository) FollowersAmong(ctx context.Context, targetID uuid.UUID, candidates []uuid.UUID) ([]uuid.UUID, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Raw(`
		SELECT s.follower_id
		FROM auth_subscriptions s
		JOIN auth_users u ON u.id = s.target_id
		WHERE s.target_id = ? AND s.follower_id = ANY(?::uuid[]) AND `+followsFilter,
		targetID, uuidArray(candidates)).
		Scan(&ids).Error
	return ids, err
}
