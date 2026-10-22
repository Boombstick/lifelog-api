package authsvc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
)

type refreshRepo struct {
	authsvc.Repository

	session  *authsvc.Session
	user     *authsvc.User
	password *authsvc.Password

	rotatedFrom *uuid.UUID
	rotatedTo   *authsvc.Session
	created     []*authsvc.Session
	revokedFam  *uuid.UUID
	lookedUp    string

	revokedDevices []string

	rotateErr error
}

func (r *refreshRepo) SessionByRefreshHash(_ context.Context, hash string) (*authsvc.Session, error) {
	if r.session == nil || r.session.RefreshHash != hash {
		return nil, authsvc.ErrRefreshInvalid
	}
	copied := *r.session
	return &copied, nil
}

func (r *refreshRepo) UserByID(context.Context, uuid.UUID) (*authsvc.User, error) {
	if r.user == nil {
		return nil, authsvc.ErrUserNotFound
	}
	return r.user, nil
}

func (r *refreshRepo) UserByLogin(_ context.Context, login string) (*authsvc.User, error) {
	r.lookedUp = login
	if r.user == nil {
		return nil, authsvc.ErrUserNotFound
	}
	return r.user, nil
}

func (r *refreshRepo) PasswordOf(context.Context, uuid.UUID) (*authsvc.Password, error) {
	if r.password == nil {
		return nil, authsvc.ErrNoPassword
	}
	return r.password, nil
}

func (r *refreshRepo) RotateSession(_ context.Context, oldID uuid.UUID, next *authsvc.Session) error {
	if r.rotateErr != nil {
		return r.rotateErr
	}
	r.rotatedFrom = &oldID
	r.rotatedTo = next
	return nil
}

func (r *refreshRepo) CreateSession(_ context.Context, session *authsvc.Session) error {
	r.created = append(r.created, session)
	return nil
}

func (r *refreshRepo) RevokeFamily(_ context.Context, familyID uuid.UUID) error {
	r.revokedFam = &familyID
	return nil
}

func (r *refreshRepo) RevokeDevice(_ context.Context, userID uuid.UUID, deviceID string) error {
	r.revokedDevices = append(r.revokedDevices, userID.String()+"/"+deviceID)
	return nil
}

type stubIssuer struct{ families []uuid.UUID }

func (i *stubIssuer) Issue(_ uuid.UUID, _, _ string, family uuid.UUID) (string, time.Time, error) {
	i.families = append(i.families, family)
	return "access." + family.String(), time.Now().Add(time.Minute), nil
}

func newTokenService(repo authsvc.Repository, issuer authsvc.TokenIssuer) *authsvc.Service {
	return authsvc.NewService(repo, issuer, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, "bot")
}

func liveSession(hash string) *authsvc.Session {
	return &authsvc.Session{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		FamilyID:    uuid.New(),
		RefreshHash: hash,
		IssuedAt:    time.Now().Add(-time.Minute),
		ExpiresAt:   time.Now().Add(time.Hour),
	}
}

func TestПросроченныйRefreshОтвергается(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	repo.session.ExpiresAt = time.Now().Add(-time.Second)
	issuer := &stubIssuer{}

	_, err := newTokenService(repo, issuer).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"})
	if !errors.Is(err, authsvc.ErrRefreshInvalid) {
		t.Fatalf("ошибка: %v, ожидалась ErrRefreshInvalid", err)
	}
	if len(issuer.families) != 0 {
		t.Error("по просроченному токену выпущен access")
	}
	if repo.rotatedTo != nil || len(repo.created) != 0 {
		t.Error("просроченный токен не должен порождать новую сессию")
	}
}

func TestОтозванныйRefreshОтвергается(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	revoked := time.Now().Add(-time.Minute)
	repo.session.RevokedAt = &revoked

	_, err := newTokenService(repo, &stubIssuer{}).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"})
	if !errors.Is(err, authsvc.ErrRefreshInvalid) {
		t.Fatalf("ошибка: %v, ожидалась ErrRefreshInvalid", err)
	}
}

func TestОбновлениеРотируетСессиюВТойЖеСемье(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	issuer := &stubIssuer{}
	raw := refreshRawFor(t, repo)

	pair, err := newTokenService(repo, issuer).Refresh(context.Background(), raw, authsvc.Client{UserAgent: "ua"})
	if err != nil {
		t.Fatalf("обновление: %v", err)
	}
	if repo.rotatedFrom == nil || *repo.rotatedFrom != repo.session.ID {
		t.Fatalf("ротирована не та сессия: %v", repo.rotatedFrom)
	}
	if len(repo.created) != 0 {
		t.Error("обновление завело вторую сессию вместо ротации первой")
	}
	if repo.rotatedTo == nil {
		t.Fatal("новая сессия не передана")
	}
	if repo.rotatedTo.FamilyID != repo.session.FamilyID {
		t.Error("новая сессия вышла из другой семьи, экран устройств удвоит строку")
	}
	if repo.rotatedTo.RefreshHash == repo.session.RefreshHash {
		t.Error("refresh-токен не сменился")
	}
	if pair.RefreshToken == raw {
		t.Error("клиенту вернули прежний refresh-токен")
	}
	if len(issuer.families) != 1 || issuer.families[0] != repo.session.FamilyID {
		t.Errorf("access выпущен не в той семье: %v", issuer.families)
	}
}

func TestПовторныйRefreshГаситСемью(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	used := time.Now().Add(-time.Second)
	repo.session.UsedAt = &used

	_, err := newTokenService(repo, &stubIssuer{}).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"})
	if !errors.Is(err, authsvc.ErrRefreshReused) {
		t.Fatalf("ошибка: %v, ожидалась ErrRefreshReused", err)
	}
	if repo.revokedFam == nil || *repo.revokedFam != repo.session.FamilyID {
		t.Error("семья не погашена после повторного использования")
	}
}

func TestПроигравшийГонкуRefreshГаситСемью(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	repo.rotateErr = authsvc.ErrRefreshReused

	pair, err := newTokenService(repo, &stubIssuer{}).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"})
	if !errors.Is(err, authsvc.ErrRefreshReused) {
		t.Fatalf("ошибка: %v, ожидалась ErrRefreshReused", err)
	}
	if pair != nil {
		t.Error("проигравшему гонку выданы токены")
	}
	if repo.revokedFam == nil || *repo.revokedFam != repo.session.FamilyID {
		t.Error("семья не погашена после проигранной гонки")
	}
}

func TestСбойРотацииНеГаситСемью(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	repo.rotateErr = errors.New("db down")

	if _, err := newTokenService(repo, &stubIssuer{}).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"}); err == nil {
		t.Fatal("сбой хранилища проглочен")
	}
	if repo.revokedFam != nil {
		t.Error("семья погашена из-за сбоя базы, а не из-за повтора")
	}
}

func TestВходНормализуетЛогин(t *testing.T) {
	repo := &refreshRepo{user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}

	_, _ = newTokenService(repo, &stubIssuer{}).Login(context.Background(), "  MaShA  ", "неважно", authsvc.Client{UserAgent: "ua"})
	if repo.lookedUp != "masha" {
		t.Errorf("в хранилище ушёл логин %q, ожидался \"masha\"", repo.lookedUp)
	}
}

func signInRepo(t *testing.T) *refreshRepo {
	t.Helper()
	repo := &refreshRepo{user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	hash, err := argon2id.CreateHash("пароль-подольше", argon2id.DefaultParams)
	if err != nil {
		t.Fatalf("подготовка хеша: %v", err)
	}
	repo.password = &authsvc.Password{UserID: repo.user.ID, Hash: hash}
	return repo
}

func TestВходСТогоЖеУстройстваГаситПрежний(t *testing.T) {
	repo := signInRepo(t)
	service := newTokenService(repo, &stubIssuer{})

	if _, err := service.Login(context.Background(), "someone", "пароль-подольше", authsvc.Client{UserAgent: "ua", DeviceID: "phone-1"}); err != nil {
		t.Fatal(err)
	}

	want := repo.user.ID.String() + "/phone-1"
	if len(repo.revokedDevices) != 1 || repo.revokedDevices[0] != want {
		t.Errorf("погашены %v, ожидалось %s", repo.revokedDevices, want)
	}
	if got := repo.created[0].DeviceID; got == nil || *got != "phone-1" {
		t.Errorf("устройство у новой сессии: %v", got)
	}
}

func TestВходБезУстройстваНичегоНеГасит(t *testing.T) {
	repo := signInRepo(t)

	if _, err := newTokenService(repo, &stubIssuer{}).Login(context.Background(), "someone", "пароль-подольше", authsvc.Client{UserAgent: "ua"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.revokedDevices) != 0 {
		t.Errorf("погашены %v без идентификатора устройства", repo.revokedDevices)
	}
	if repo.created[0].DeviceID != nil {
		t.Errorf("пустой идентификатор записан как %q", *repo.created[0].DeviceID)
	}
}

func TestОбновлениеСохраняетУстройство(t *testing.T) {
	repo := &refreshRepo{session: liveSession("hash"), user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	device := "phone-1"
	repo.session.DeviceID = &device

	if _, err := newTokenService(repo, &stubIssuer{}).Refresh(context.Background(), refreshRawFor(t, repo), authsvc.Client{UserAgent: "ua"}); err != nil {
		t.Fatal(err)
	}
	if got := repo.rotatedTo.DeviceID; got == nil || *got != device {
		t.Errorf("устройство после ротации: %v", got)
	}
	if len(repo.revokedDevices) != 0 {
		t.Error("ротация погасила собственный вход")
	}
}

func refreshRawFor(t *testing.T, repo *refreshRepo) string {
	t.Helper()

	issued := &refreshRepo{user: &authsvc.User{ID: uuid.New(), Role: authsvc.RoleUser}}
	hash, err := argon2id.CreateHash("пароль-подольше", argon2id.DefaultParams)
	if err != nil {
		t.Fatalf("подготовка хеша: %v", err)
	}
	issued.password = &authsvc.Password{UserID: issued.user.ID, Hash: hash}

	pair, err := newTokenService(issued, &stubIssuer{}).Login(context.Background(), "someone", "пароль-подольше", authsvc.Client{UserAgent: "ua"})
	if err != nil {
		t.Fatalf("подготовка токена: %v", err)
	}
	repo.session.RefreshHash = issued.created[0].RefreshHash
	return pair.RefreshToken
}

func (r *refreshRepo) HitRateCounter(context.Context, string, time.Time, time.Duration) (int, error) {
	return 1, nil
}

func (r *refreshRepo) ResetRateCounter(context.Context, string) error { return nil }
