package authsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RootAdmin struct {
	Login       string
	Password    string
	Email       string
	DisplayName string
}

var ErrManagedPassword = errors.New("пароль этой учётки задаётся конфигом сервиса и меняется только выкладкой")

func (s *Service) SetRootLogin(login string) { s.rootLogin = normalizeLogin(login) }

func (s *Service) EnsureRootAdmin(ctx context.Context, admin RootAdmin) (int64, error) {
	login := normalizeLogin(admin.Login)
	if login == "" || admin.Password == "" {
		return 0, errors.New("главный админ: нужны логин и пароль")
	}
	if len([]rune(admin.Password)) < minPasswordLength {
		return 0, ErrWeakPassword
	}
	s.rootLogin = login

	var email *string
	if trimmed := strings.TrimSpace(admin.Email); trimmed != "" {
		normalized, err := NormalizeEmail(trimmed)
		if err != nil {
			return 0, err
		}
		email = &normalized
	}
	name := strings.TrimSpace(admin.DisplayName)
	if name == "" {
		name = login
	}

	user, err := s.repo.UserByLogin(ctx, login)
	switch {
	case errors.Is(err, ErrUserNotFound):

		if _, err := NormalizeLogin(login); err != nil {
			return 0, fmt.Errorf("главный админ: %w", err)
		}
		hash, err := createHash(ctx, admin.Password)
		if err != nil {
			return 0, fmt.Errorf("хеширование пароля: %w", err)
		}
		user = &User{ID: uuid.New(), Login: &login, Email: email, DisplayName: name, Role: RoleAdmin, CreatedAt: time.Now()}
		if err := s.repo.CreateUserWithPassword(ctx, user, hash); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:

		if strings.TrimSpace(admin.DisplayName) == "" {
			name = user.DisplayName
		}
		if err := s.repo.UpdateProfile(ctx, user.ID, RoleAdmin, email, name); err != nil {
			return 0, err
		}
		if err := s.syncRootPassword(ctx, user.ID, admin.Password); err != nil {
			return 0, err
		}
	}

	return s.repo.DemoteAdmins(ctx, user.ID)
}

func (s *Service) syncRootPassword(ctx context.Context, userID uuid.UUID, password string) error {
	stored, err := s.repo.PasswordOf(ctx, userID)
	if err == nil {
		same, err := compareHash(ctx, password, stored.Hash)
		if err != nil {
			return fmt.Errorf("проверка пароля: %w", err)
		}
		if same {
			return nil
		}
	} else if !errors.Is(err, ErrNoPassword) {
		return err
	}

	hash, err := createHash(ctx, password)
	if err != nil {
		return fmt.Errorf("хеширование пароля: %w", err)
	}
	if err := s.repo.SetPassword(ctx, userID, hash); err != nil {
		return err
	}

	return s.repo.RevokeAllForUser(ctx, userID)
}

func (s *Service) isRoot(ctx context.Context, userID uuid.UUID) (bool, error) {
	if s.rootLogin == "" {
		return false, nil
	}
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	return user.Login != nil && normalizeLogin(*user.Login) == s.rootLogin, nil
}
