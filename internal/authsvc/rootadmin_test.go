package authsvc_test

import (
	"context"
	"errors"
	"testing"

	"lifelog-api/internal/authsvc"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
)

type rootRepo struct {
	authsvc.Repository

	users     map[uuid.UUID]*authsvc.User
	passwords map[uuid.UUID]string
	revoked   []uuid.UUID
}

func newRootRepo(users ...*authsvc.User) *rootRepo {
	repo := &rootRepo{users: map[uuid.UUID]*authsvc.User{}, passwords: map[uuid.UUID]string{}}
	for _, user := range users {
		repo.users[user.ID] = user
	}
	return repo
}

func (r *rootRepo) UserByLogin(_ context.Context, login string) (*authsvc.User, error) {
	for _, user := range r.users {
		if user.Login != nil && *user.Login == login {
			return user, nil
		}
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *rootRepo) UserByID(_ context.Context, id uuid.UUID) (*authsvc.User, error) {
	if user, ok := r.users[id]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *rootRepo) CreateUserWithPassword(_ context.Context, user *authsvc.User, hash string) error {
	r.users[user.ID] = user
	r.passwords[user.ID] = hash
	return nil
}

func (r *rootRepo) UpdateProfile(_ context.Context, id uuid.UUID, role authsvc.Role, email *string, name string) error {
	user := r.users[id]
	user.Role, user.Email, user.DisplayName = role, email, name
	return nil
}

func (r *rootRepo) DemoteAdmins(_ context.Context, keep uuid.UUID) (int64, error) {
	var n int64
	for id, user := range r.users {
		if id != keep && user.Role == authsvc.RoleAdmin {
			user.Role = authsvc.RoleUser
			n++
		}
	}
	return n, nil
}

func (r *rootRepo) PasswordOf(_ context.Context, id uuid.UUID) (*authsvc.Password, error) {
	hash, ok := r.passwords[id]
	if !ok {
		return nil, authsvc.ErrNoPassword
	}
	return &authsvc.Password{UserID: id, Hash: hash}, nil
}

func (r *rootRepo) SetPassword(_ context.Context, id uuid.UUID, hash string) error {
	r.passwords[id] = hash
	return nil
}

func (r *rootRepo) RevokeAllForUser(_ context.Context, id uuid.UUID) error {
	r.revoked = append(r.revoked, id)
	return nil
}

func named(login string, role authsvc.Role) *authsvc.User {
	value := login
	return &authsvc.User{ID: uuid.New(), Login: &value, DisplayName: login, Role: role}
}

func matches(t *testing.T, password, hash string) bool {
	t.Helper()
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestГлавныйАдминСоздаётсяИСнимаетРольСОстальных(t *testing.T) {
	old := named("boombstick", authsvc.RoleAdmin)
	repo := newRootRepo(old)

	demoted, err := newService(repo).EnsureRootAdmin(context.Background(), authsvc.RootAdmin{
		Login: "Admin", Password: "очень-длинный-пароль", Email: "Root@Example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	root, err := repo.UserByLogin(context.Background(), "admin")
	if err != nil {
		t.Fatalf("учётка admin не создана: %v", err)
	}
	if root.Role != authsvc.RoleAdmin || root.Email == nil || *root.Email != "root@example.com" {
		t.Errorf("учётка admin: роль %q, почта %v", root.Role, root.Email)
	}
	if !matches(t, "очень-длинный-пароль", repo.passwords[root.ID]) {
		t.Error("пароль из конфига не подходит")
	}
	if demoted != 1 || old.Role != authsvc.RoleUser {
		t.Errorf("снято ролей %d, у boombstick роль %q", demoted, old.Role)
	}
}

func TestПарольГлавногоАдминаМеняетсяТолькоКогдаДругой(t *testing.T) {
	root := named("admin", authsvc.RoleUser)
	repo := newRootRepo(root)
	hash, _ := argon2id.CreateHash("старый-пароль-1", argon2id.DefaultParams)
	repo.passwords[root.ID] = hash
	service := newService(repo)

	if _, err := service.EnsureRootAdmin(context.Background(), authsvc.RootAdmin{Login: "admin", Password: "старый-пароль-1"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.revoked) != 0 || repo.passwords[root.ID] != hash {
		t.Error("тот же пароль переписан, и входы погашены на пустом месте")
	}
	if root.Role != authsvc.RoleAdmin {
		t.Error("существующей учётке admin не выдана роль")
	}

	if _, err := service.EnsureRootAdmin(context.Background(), authsvc.RootAdmin{Login: "admin", Password: "новый-пароль-22"}); err != nil {
		t.Fatal(err)
	}
	if !matches(t, "новый-пароль-22", repo.passwords[root.ID]) || len(repo.revoked) != 1 {
		t.Errorf("пароль не сменился или входы не погашены: %v", repo.revoked)
	}
}

func TestПарольГлавногоАдминаНеМеняетсяИзИнтерфейса(t *testing.T) {
	repo := newRootRepo()
	service := newService(repo)
	if _, err := service.EnsureRootAdmin(context.Background(), authsvc.RootAdmin{Login: "admin", Password: "пароль-из-конфига"}); err != nil {
		t.Fatal(err)
	}
	root, _ := repo.UserByLogin(context.Background(), "admin")

	err := service.ChangePassword(context.Background(), root.ID, "пароль-из-конфига", "другой-пароль-33", uuid.New())
	if !errors.Is(err, authsvc.ErrManagedPassword) {
		t.Errorf("смена своего пароля: %v, ожидалась ErrManagedPassword", err)
	}
	if err := service.ResetPassword(context.Background(), root.ID, "другой-пароль-33"); !errors.Is(err, authsvc.ErrManagedPassword) {
		t.Errorf("сброс админом: %v, ожидалась ErrManagedPassword", err)
	}
	if !matches(t, "пароль-из-конфига", repo.passwords[root.ID]) {
		t.Error("пароль всё-таки сменился")
	}

	user := named("masha", authsvc.RoleUser)
	repo.users[user.ID] = user
	if err := service.ResetPassword(context.Background(), user.ID, "новый-пароль-44"); err != nil {
		t.Errorf("сброс обычному пользователю: %v", err)
	}
}

func TestКороткийПарольГлавногоАдминаОтклоняется(t *testing.T) {
	_, err := newService(newRootRepo()).EnsureRootAdmin(context.Background(), authsvc.RootAdmin{Login: "admin", Password: "short"})
	if !errors.Is(err, authsvc.ErrWeakPassword) {
		t.Errorf("ошибка %v, ожидалась ErrWeakPassword", err)
	}
}

func TestRootAdminKeepsProfileNameWithoutConfiguredOne(t *testing.T) {
	root := named("admin", authsvc.RoleAdmin)
	root.DisplayName = "Женя"
	repo := newRootRepo(root)
	service := newService(repo)
	ctx := context.Background()

	if _, err := service.EnsureRootAdmin(ctx, authsvc.RootAdmin{Login: "admin", Password: "очень-длинный-пароль"}); err != nil {
		t.Fatal(err)
	}
	if root.DisplayName != "Женя" {
		t.Fatalf("name overwritten with %q", root.DisplayName)
	}

	if _, err := service.EnsureRootAdmin(ctx, authsvc.RootAdmin{Login: "admin", Password: "очень-длинный-пароль", DisplayName: "Root"}); err != nil {
		t.Fatal(err)
	}
	if root.DisplayName != "Root" {
		t.Fatalf("configured name not applied: %q", root.DisplayName)
	}
}
