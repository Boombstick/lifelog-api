package postgres

import (
	"context"
	"errors"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (r *AuthRepository) FindByAuthID(ctx context.Context, authUserID uuid.UUID) (*auth.User, error) {
	user := auth.User{}
	err := r.db.WithContext(ctx).Where("auth_user_id = ?", authUserID).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("user")
		}
		return nil, err
	}
	return &user, nil
}

func (r *AuthRepository) CreateForAuthID(ctx context.Context, authUserID uuid.UUID, displayName string, role auth.Role) (*auth.User, error) {
	user := auth.User{
		Username:   displayName,
		Role:       role,
		AuthUserID: &authUserID,
	}
	if err := r.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *AuthRepository) FindByID(ctx context.Context, id uint) (*auth.User, error) {
	user := auth.User{}
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("user")
		}
		return nil, err
	}
	return &user, nil
}

func (r *AuthRepository) UpdateUsername(ctx context.Context, id uint, displayName string) error {
	return r.db.WithContext(ctx).Model(&auth.User{}).Where("id = ?", id).Update("username", displayName).Error
}

func (r *AuthRepository) UpdateRole(ctx context.Context, id uint, role auth.Role) error {
	return r.db.WithContext(ctx).Model(&auth.User{}).Where("id = ?", id).Update("role", role).Error
}
