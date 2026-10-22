package postgres

import (
	"context"

	"lifelog-api/internal/auth"

	"gorm.io/gorm"
)

type AuthRepository struct {
	db *gorm.DB
}

func ProvideUserRepository(gdb *gorm.DB) *AuthRepository {
	return &AuthRepository{db: gdb}
}
func (r *AuthRepository) Create(ctx context.Context, user *auth.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}
