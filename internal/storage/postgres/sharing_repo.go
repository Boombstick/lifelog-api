package postgres

import (
	"context"
	"errors"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SharingRepository struct {
	db *gorm.DB
}

func NewSharingRepository(db *gorm.DB) *SharingRepository {
	return &SharingRepository{db: db}
}

func (r *SharingRepository) Visibility(ctx context.Context, userID uint) (auth.ListVisibility, error) {
	var visibility auth.ListVisibility
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&visibility).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return auth.DefaultVisibility(userID), nil
	}
	return visibility, err
}

func (r *SharingRepository) Visibilities(ctx context.Context, userIDs []uint) (map[uint]auth.ListVisibility, error) {
	out := make(map[uint]auth.ListVisibility, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []auth.ListVisibility
	if err := r.db.WithContext(ctx).Where("user_id IN ?", userIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = row
	}
	for _, id := range userIDs {
		if _, ok := out[id]; !ok {
			out[id] = auth.DefaultVisibility(id)
		}
	}
	return out, nil
}

func (r *SharingRepository) SetVisibility(ctx context.Context, visibility auth.ListVisibility) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"watchlist", "watched", "rated"}),
	}).Create(&visibility).Error
}

func (r *SharingRepository) CreateShareLink(ctx context.Context, link *auth.ShareLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *SharingRepository) ShareLinkByToken(ctx context.Context, token string) (*auth.ShareLink, error) {
	var link auth.ShareLink
	err := r.db.WithContext(ctx).
		Where("token = ? AND revoked_at IS NULL AND expires_at > ?", token, time.Now()).
		First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.NewEntityNotFound("share link")
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *SharingRepository) ShareLinksOf(ctx context.Context, userID uint) ([]auth.ShareLink, error) {
	var links []auth.ShareLink
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Order("created_at DESC").
		Find(&links).Error
	return links, err
}

func (r *SharingRepository) RevokeShareLink(ctx context.Context, userID uint, token string) error {
	result := r.db.WithContext(ctx).Model(&auth.ShareLink{}).
		Where("token = ? AND user_id = ? AND revoked_at IS NULL", token, userID).
		Update("revoked_at", time.Now())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return model.NewEntityNotFound("share link")
	}
	return nil
}
