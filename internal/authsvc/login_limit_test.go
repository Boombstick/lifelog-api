package authsvc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

type limitRepo struct {
	*refreshRepo
	hits          map[string]int
	passwordReads int
}

func newLimitRepo(t *testing.T) *limitRepo {
	return &limitRepo{refreshRepo: signInRepo(t), hits: map[string]int{}}
}

func (r *limitRepo) HitRateCounter(_ context.Context, key string, _ time.Time, _ time.Duration) (int, error) {
	r.hits[key]++
	return r.hits[key], nil
}

func (r *limitRepo) ResetRateCounter(_ context.Context, key string) error {
	delete(r.hits, key)
	return nil
}

func (r *limitRepo) PasswordOf(ctx context.Context, id uuid.UUID) (*authsvc.Password, error) {
	r.passwordReads++
	return r.refreshRepo.PasswordOf(ctx, id)
}

func (r *limitRepo) UserByEmail(context.Context, string) (*authsvc.User, error) {
	return nil, authsvc.ErrUserNotFound
}

func signIn(service *authsvc.Service, identifier, password string) error {
	_, err := service.SignIn(context.Background(), identifier, password, authsvc.Client{UserAgent: "ua"})
	return err
}

func TestTenFailedLoginsLockTheIdentifier(t *testing.T) {
	repo := newLimitRepo(t)
	service := newTokenService(repo, &stubIssuer{})

	for i := range 10 {
		if err := signIn(service, "someone", "не тот пароль"); !errors.Is(err, authsvc.ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	reads := repo.passwordReads

	if err := signIn(service, "  SomeOne ", "пароль-подольше"); !errors.Is(err, authsvc.ErrLoginLocked) {
		t.Fatalf("11th attempt: %v, want ErrLoginLocked", err)
	}
	if repo.passwordReads != reads {
		t.Error("a locked attempt still checked the password")
	}
}

func TestUnknownIdentifiersLockTheSameWay(t *testing.T) {
	repo := newLimitRepo(t)
	service := newTokenService(repo, &stubIssuer{})

	for range 10 {
		_ = signIn(service, "Nobody@Example.com", "пароль")
	}
	if err := signIn(service, "nobody@example.com", "пароль"); !errors.Is(err, authsvc.ErrLoginLocked) {
		t.Fatalf("unknown email after 10 failures: %v, want ErrLoginLocked", err)
	}
	if _, ok := repo.hits["login:nobody@example.com"]; !ok {
		t.Errorf("counter keys %v, want login:nobody@example.com", repo.hits)
	}
}

func TestSuccessfulLoginClearsFailures(t *testing.T) {
	repo := newLimitRepo(t)
	service := newTokenService(repo, &stubIssuer{})

	for range 9 {
		_ = signIn(service, "someone", "не тот пароль")
	}
	if err := signIn(service, "someone", "пароль-подольше"); err != nil {
		t.Fatalf("right password after 9 failures: %v", err)
	}
	for i := range 10 {
		if err := signIn(service, "someone", "не тот пароль"); !errors.Is(err, authsvc.ErrInvalidCredentials) {
			t.Fatalf("failure %d after a success: %v, want ErrInvalidCredentials", i+1, err)
		}
	}
	if err := signIn(service, "someone", "пароль-подольше"); !errors.Is(err, authsvc.ErrLoginLocked) {
		t.Fatalf("after 10 fresh failures: %v, want ErrLoginLocked", err)
	}
}

func TestLockIsPerIdentifier(t *testing.T) {
	repo := newLimitRepo(t)
	service := newTokenService(repo, &stubIssuer{})

	for range 11 {
		_ = signIn(service, "victim", "пароль")
	}
	if err := signIn(service, "someone", "пароль-подольше"); err != nil {
		t.Fatalf("another login after a lock: %v", err)
	}
}
