package authsvc_test

import (
	"context"
	"errors"
	"testing"

	"lifelog-api/internal/authsvc"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
)

type passwordRepo struct {
	authsvc.Repository

	stored      *authsvc.Password
	user        *authsvc.User
	written     string
	revokedKeep *uuid.UUID
	revokedAll  bool
}

func (r *passwordRepo) PasswordOf(context.Context, uuid.UUID) (*authsvc.Password, error) {
	if r.stored == nil {
		return nil, authsvc.ErrNoPassword
	}
	return r.stored, nil
}

func (r *passwordRepo) SetPassword(_ context.Context, _ uuid.UUID, hash string) error {
	r.written = hash
	return nil
}

func (r *passwordRepo) UserByID(context.Context, uuid.UUID) (*authsvc.User, error) {
	if r.user == nil {
		return nil, authsvc.ErrUserNotFound
	}
	return r.user, nil
}

func (r *passwordRepo) RevokeOtherFamilies(_ context.Context, _ uuid.UUID, keep uuid.UUID) error {
	r.revokedKeep = &keep
	return nil
}

func (r *passwordRepo) RevokeAllForUser(context.Context, uuid.UUID) error {
	r.revokedAll = true
	return nil
}

func hashOf(t *testing.T, password string) *authsvc.Password {
	t.Helper()
	hash, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		t.Fatalf("подготовка хеша: %v", err)
	}
	return &authsvc.Password{UserID: uuid.New(), Hash: hash}
}

func TestСменаПароляТребуетТекущий(t *testing.T) {
	repo := &passwordRepo{stored: hashOf(t, "прежний-пароль")}
	service := newService(repo)

	err := service.ChangePassword(context.Background(), uuid.New(), "не тот", "новый-пароль", uuid.New())
	if !errors.Is(err, authsvc.ErrInvalidCredentials) {
		t.Fatalf("ошибка: %v, ожидалась ErrInvalidCredentials", err)
	}
	if repo.written != "" {
		t.Error("пароль записан, хотя текущий не подошёл")
	}
	if repo.revokedKeep != nil {
		t.Error("сессии погашены, хотя смена не состоялась")
	}
}

func TestСменаПароляГаситОстальныеУстройства(t *testing.T) {
	repo := &passwordRepo{stored: hashOf(t, "прежний-пароль")}
	service := newService(repo)
	current := uuid.New()

	if err := service.ChangePassword(context.Background(), uuid.New(), "прежний-пароль", "новый-пароль", current); err != nil {
		t.Fatalf("смена пароля: %v", err)
	}
	if repo.written == "" {
		t.Fatal("новый пароль не записан")
	}
	if repo.written == repo.stored.Hash {
		t.Error("записан прежний хеш")
	}
	if repo.revokedKeep == nil || *repo.revokedKeep != current {
		t.Errorf("текущий вход не сохранён при гашении: %v", repo.revokedKeep)
	}
}

func TestКороткийПарольОтвергается(t *testing.T) {
	repo := &passwordRepo{stored: hashOf(t, "прежний-пароль")}
	service := newService(repo)

	err := service.ChangePassword(context.Background(), uuid.New(), "прежний-пароль", "корот", uuid.New())
	if !errors.Is(err, authsvc.ErrWeakPassword) {
		t.Fatalf("ошибка: %v, ожидалась ErrWeakPassword", err)
	}
	if repo.written != "" {
		t.Error("короткий пароль записан")
	}
}

func TestАдминскийСбросНеСпрашиваетПрежний(t *testing.T) {
	repo := &passwordRepo{user: &authsvc.User{ID: uuid.New()}}
	service := newService(repo)

	if err := service.ResetPassword(context.Background(), repo.user.ID, "назначенный-пароль"); err != nil {
		t.Fatalf("сброс пароля: %v", err)
	}
	if repo.written == "" {
		t.Fatal("пароль не записан")
	}
	if !repo.revokedAll {
		t.Error("входы не погашены после сброса")
	}
}

func TestАдминскийСбросНесуществующемуОтвергается(t *testing.T) {
	repo := &passwordRepo{}
	service := newService(repo)

	err := service.ResetPassword(context.Background(), uuid.New(), "назначенный-пароль")
	if !errors.Is(err, authsvc.ErrUserNotFound) {
		t.Fatalf("ошибка: %v, ожидалась ErrUserNotFound", err)
	}
	if repo.written != "" {
		t.Error("пароль записан несуществующему пользователю")
	}
}

func TestАдминскийСбросПроверяетДлину(t *testing.T) {
	repo := &passwordRepo{user: &authsvc.User{ID: uuid.New()}}
	service := newService(repo)

	err := service.ResetPassword(context.Background(), repo.user.ID, "корот")
	if !errors.Is(err, authsvc.ErrWeakPassword) {
		t.Fatalf("ошибка: %v, ожидалась ErrWeakPassword", err)
	}
	if repo.revokedAll {
		t.Error("входы погашены, хотя пароль не сменился")
	}
}

func TestГраницаДлиныПароля(t *testing.T) {
	cases := []struct {
		name     string
		password string
		accept   bool
	}{
		{"семь символов", "семисим", false},
		{"восемь символов", "восемьси", true},
		{"восемь букв кириллицей", "парольчи", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &passwordRepo{stored: hashOf(t, "прежний-пароль")}
			err := newService(repo).ChangePassword(context.Background(), uuid.New(), "прежний-пароль", c.password, uuid.New())
			if c.accept {
				if err != nil {
					t.Fatalf("пароль отвергнут: %v", err)
				}
				if repo.written == "" {
					t.Error("пароль не записан")
				}
				return
			}
			if !errors.Is(err, authsvc.ErrWeakPassword) {
				t.Fatalf("ошибка: %v, ожидалась ErrWeakPassword", err)
			}
			if repo.written != "" {
				t.Error("короткий пароль записан")
			}
		})
	}
}
