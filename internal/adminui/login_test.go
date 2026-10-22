package adminui

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/token"

	"github.com/alexedwards/argon2id"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type loginRepo struct {
	authsvc.Repository
	users     map[string]*authsvc.User
	hash      string
	sessions  map[string]*authsvc.Session
	loggedOut int
	hits      map[string]int
}

func (r *loginRepo) UserByLogin(_ context.Context, login string) (*authsvc.User, error) {
	if user, ok := r.users[login]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *loginRepo) PasswordOf(_ context.Context, userID uuid.UUID) (*authsvc.Password, error) {
	return &authsvc.Password{UserID: userID, Hash: r.hash}, nil
}

func (r *loginRepo) CreateSession(_ context.Context, session *authsvc.Session) error {
	r.sessions[session.RefreshHash] = session
	return nil
}

func (r *loginRepo) SessionByRefreshHash(_ context.Context, hash string) (*authsvc.Session, error) {
	if session, ok := r.sessions[hash]; ok {
		return session, nil
	}
	return nil, authsvc.ErrRefreshInvalid
}

func (r *loginRepo) RevokeFamily(context.Context, uuid.UUID) error {
	r.loggedOut++
	return nil
}

func TestNonAdminLoginLooksLikeWrongPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hash, err := argon2id.CreateHash("Passw0rd!", argon2id.DefaultParams)
	if err != nil {
		t.Fatal(err)
	}
	login := "masha"
	repo := &loginRepo{
		users:    map[string]*authsvc.User{login: {ID: uuid.New(), Login: &login, DisplayName: "Маша", Role: authsvc.RoleUser}},
		hash:     hash,
		sessions: map[string]*authsvc.Session{},
	}
	signer, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "lifelog", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := authsvc.NewService(repo, signer, nil, logger, time.Hour, "")
	handler, err := New(service, signer, Options{})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	handler.Register(engine)

	post := func(password string) (int, string) {
		form := url.Values{"login": {login}, "password": {password}}
		request := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	wrongCode, wrongBody := post("not-the-password")
	rightCode, rightBody := post("Passw0rd!")

	if wrongCode != http.StatusUnauthorized || rightCode != http.StatusUnauthorized {
		t.Fatalf("codes: wrong password %d, non-admin %d; want 401 for both", wrongCode, rightCode)
	}
	if wrongBody != rightBody {
		t.Error("the non-admin page differs from the wrong-password page")
	}
	if !strings.Contains(rightBody, "Неверный логин или пароль.") {
		t.Error("the page lacks the wrong-credentials text")
	}
	if repo.loggedOut != 1 {
		t.Errorf("the non-admin session must be revoked at once, revoked %d", repo.loggedOut)
	}
}

func (r *loginRepo) HitRateCounter(_ context.Context, key string, _ time.Time, _ time.Duration) (int, error) {
	if r.hits == nil {
		r.hits = map[string]int{}
	}
	r.hits[key]++
	return r.hits[key], nil
}

func (r *loginRepo) ResetRateCounter(_ context.Context, key string) error {
	delete(r.hits, key)
	return nil
}

func TestAdminLoginLockedAfterTenFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &loginRepo{users: map[string]*authsvc.User{}, sessions: map[string]*authsvc.Session{}}
	signer, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "lifelog", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := New(authsvc.NewService(repo, signer, nil, logger, time.Hour, ""), signer, Options{})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	handler.Register(engine)

	post := func() (int, string) {
		form := url.Values{"login": {"nobody"}, "password": {"guess"}}
		request := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	for i := range 10 {
		if code, _ := post(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i+1, code)
		}
	}
	code, body := post()
	if code != http.StatusTooManyRequests {
		t.Fatalf("11th attempt: %d, want 429", code)
	}
	if !strings.Contains(body, "Слишком много попыток входа") {
		t.Error("the page lacks the lockout text")
	}
}
