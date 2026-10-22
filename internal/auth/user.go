package auth

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	Username string `gorm:"size:255;not null"`
	Role     Role   `gorm:"size:16;not null;default:user" json:"role"`

	AuthUserID *uuid.UUID `gorm:"type:uuid" json:"-"`
}
type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)
