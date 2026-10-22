package postgres

import (
	"context"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *IdentityRepository) Block(ctx context.Context, block *authsvc.Block) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(block).Error; err != nil {
			return err
		}
		return tx.Where("(follower_id = ? AND target_id = ?) OR (follower_id = ? AND target_id = ?)",
			block.BlockerID, block.BlockedID, block.BlockedID, block.BlockerID).
			Delete(&authsvc.Subscription{}).Error
	})
}

func (r *IdentityRepository) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("blocker_id = ? AND blocked_id = ?", blockerID, blockedID).
		Delete(&authsvc.Block{}).Error
}

func (r *IdentityRepository) BlockRelations(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Raw(`
		SELECT blocked_id FROM auth_blocks WHERE blocker_id = ?
		UNION
		SELECT blocker_id FROM auth_blocks WHERE blocked_id = ?`, userID, userID).
		Scan(&ids).Error
	return ids, err
}

func (r *IdentityRepository) BlockState(ctx context.Context, viewerID, otherID uuid.UUID) (bool, bool, error) {
	var rows []authsvc.Block
	err := r.db.WithContext(ctx).
		Where("(blocker_id = ? AND blocked_id = ?) OR (blocker_id = ? AND blocked_id = ?)", otherID, viewerID, viewerID, otherID).
		Find(&rows).Error
	if err != nil {
		return false, false, err
	}
	var byOther, byViewer bool
	for _, row := range rows {
		if row.BlockerID == otherID {
			byOther = true
		} else {
			byViewer = true
		}
	}
	return byOther, byViewer, nil
}

func (r *IdentityRepository) BlocksOf(ctx context.Context, blockerID uuid.UUID) ([]authsvc.BlockView, error) {
	var views []authsvc.BlockView
	err := r.db.WithContext(ctx).Raw(`
		SELECT u.id AS user_id, COALESCE(u.login, '') AS login, u.display_name, b.created_at
		FROM auth_blocks b
		JOIN auth_users u ON u.id = b.blocked_id
		WHERE b.blocker_id = ?
		ORDER BY b.created_at DESC`, blockerID).Scan(&views).Error
	return views, err
}
