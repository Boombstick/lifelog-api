package authsvc_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

var errStop = errors.New("stop before issuing tokens")

type emailRepo struct {
	authsvc.Repository
	mu       sync.Mutex
	codes    map[string]*authsvc.EmailCode
	counters map[string]*rateCounter
	logins   map[string]*authsvc.User
	emails   map[string]*authsvc.User
	created  *authsvc.User
	password string
}

type rateCounter struct {
	hits  int
	start time.Time
}

func newEmailRepo() *emailRepo {
	return &emailRepo{
		codes:    map[string]*authsvc.EmailCode{},
		counters: map[string]*rateCounter{},
		logins:   map[string]*authsvc.User{},
		emails:   map[string]*authsvc.User{},
	}
}

func (r *emailRepo) UserByLogin(_ context.Context, login string) (*authsvc.User, error) {
	if user, ok := r.logins[login]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *emailRepo) UserByEmail(_ context.Context, email string) (*authsvc.User, error) {
	if user, ok := r.emails[email]; ok {
		return user, nil
	}
	return nil, authsvc.ErrUserNotFound
}

func (r *emailRepo) EmailCode(_ context.Context, key string) (*authsvc.EmailCode, error) {
	if code, ok := r.codes[key]; ok {
		copied := *code
		return &copied, nil
	}
	return nil, authsvc.ErrCodeInvalid
}

func (r *emailRepo) SaveEmailCode(_ context.Context, code *authsvc.EmailCode) error {
	copied := *code
	r.codes[code.Key] = &copied
	return nil
}

func (r *emailRepo) DeleteEmailCode(_ context.Context, key string) error {
	delete(r.codes, key)
	return nil
}

func (r *emailRepo) TakeCodeAttempt(_ context.Context, key string, maxAttempts int, now time.Time) (*authsvc.EmailCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	code, ok := r.codes[key]
	if !ok || code.CodeHash == "" || code.Attempts >= maxAttempts || !now.Before(code.ExpiresAt) {
		return nil, authsvc.ErrCodeInvalid
	}
	code.Attempts++
	copied := *code
	return &copied, nil
}

func (r *emailRepo) HitRateCounter(_ context.Context, key string, now time.Time, window time.Duration) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counter, ok := r.counters[key]
	if !ok || !counter.start.After(now.Add(-window)) {
		counter = &rateCounter{start: now}
		r.counters[key] = counter
	}
	counter.hits++
	return counter.hits, nil
}

func (r *emailRepo) CreateUserFromCode(_ context.Context, key, codeHash string, user *authsvc.User, passwordHash string) error {
	if r.codes[key] == nil || r.codes[key].CodeHash != codeHash {
		return authsvc.ErrCodeInvalid
	}
	r.created = user
	r.password = passwordHash
	return errStop
}

type recordingMailer struct{ sent []authsvc.Mail }

func (m *recordingMailer) Send(_ context.Context, mail authsvc.Mail) error {
	m.sent = append(m.sent, mail)
	return nil
}

func emailService(repo *emailRepo) (*authsvc.Service, *recordingMailer) {
	svc := newService(repo)
	mailer := &recordingMailer{}
	svc.SetMailer(mailer)
	return svc, mailer
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"  Masha@Example.COM ": "masha@example.com",
		"a.b+tag@mail.ru":      "a.b+tag@mail.ru",
	}
	for in, want := range valid {
		got, err := authsvc.NormalizeEmail(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "masha", "masha@localhost", "Masha <masha@example.com>", "@example.com", "a@b@c.com"} {
		if _, err := authsvc.NormalizeEmail(in); !errors.Is(err, authsvc.ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) accepted", in)
		}
	}
}

func TestRegisterStartValidation(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	repo.logins["taken"] = &authsvc.User{}
	svc, mailer := emailService(repo)
	ctx := context.Background()

	cases := []struct {
		name, email, login, password string
		want                         error
	}{
		{"invalid email", "nope", "fresh", "Passw0rd!", authsvc.ErrInvalidEmail},
		{"empty login", "a@b.ru", "  ", "Passw0rd!", authsvc.ErrLoginRequired},
		{"weak password", "a@b.ru", "fresh", "short", authsvc.ErrWeakPassword},
		{"login taken", "a@b.ru", "Taken", "Passw0rd!", authsvc.ErrLoginTaken},
		{"login with @", "a@b.ru", "me@x.io", "Passw0rd!", authsvc.ErrLoginHasAt},
		{"login too long", "a@b.ru", strings.Repeat("a", 65), "Passw0rd!", authsvc.ErrLoginLength},
		{"login with space", "a@b.ru", "ivan petrov", "Passw0rd!", authsvc.ErrLoginSymbols},
	}
	for _, tc := range cases {
		if _, err := svc.RegisterStart(ctx, tc.email, tc.login, tc.password, ""); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", tc.name, tc.want, err)
		}
	}
	if len(mailer.sent) != 0 {
		t.Errorf("rejected registrations must not send mail, sent %d", len(mailer.sent))
	}
}

func TestRegisterSendsCodeAndConfirmCreatesAccount(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()

	expires, err := svc.RegisterStart(ctx, "Masha@Example.com", "Masha", "Passw0rd!", "")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(expires); d < 14*time.Minute || d > 15*time.Minute {
		t.Errorf("code lifetime %v", d)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("want one mail, got %d", len(mailer.sent))
	}
	code := mailer.sent[0].Code
	if !regexp.MustCompile(`^\d{6}$`).MatchString(code) || mailer.sent[0].To != "masha@example.com" {
		t.Fatalf("unexpected mail %+v", mailer.sent[0])
	}
	stored := repo.codes["register:masha@example.com"]
	if stored.CodeHash == "" || stored.CodeHash == code || stored.PasswordHash == "Passw0rd!" {
		t.Fatal("code and password must be stored hashed")
	}

	if _, err := svc.RegisterStart(ctx, "masha@example.com", "masha", "Passw0rd!", ""); !errors.Is(err, authsvc.ErrCodeTooSoon) {
		t.Errorf("resend within a minute: want ErrCodeTooSoon, got %v", err)
	}

	if _, err := svc.RegisterConfirm(ctx, "masha@example.com", "000000x", authsvc.Client{UserAgent: "test"}); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Errorf("wrong code: want ErrCodeInvalid, got %v", err)
	}
	if repo.codes["register:masha@example.com"].Attempts != 1 {
		t.Error("a wrong code must cost an attempt")
	}

	if _, err := svc.RegisterConfirm(ctx, " MASHA@example.com ", code, authsvc.Client{UserAgent: "test"}); !errors.Is(err, errStop) {
		t.Fatalf("correct code: want to reach account creation, got %v", err)
	}
	if *repo.created.Email != "masha@example.com" || *repo.created.Login != "masha" || repo.created.DisplayName != "masha" {
		t.Errorf("unexpected account %+v", repo.created)
	}
}

func TestConfirmRejectsExhaustedCode(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if _, err := svc.RegisterStart(ctx, "a@b.ru", "alice", "Passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, _ = svc.RegisterConfirm(ctx, "a@b.ru", "wrong!", authsvc.Client{UserAgent: "test"})
	}
	if _, err := svc.RegisterConfirm(ctx, "a@b.ru", mailer.sent[0].Code, authsvc.Client{UserAgent: "test"}); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Errorf("after five wrong attempts the right code must fail too, got %v", err)
	}
}

func TestRegisterForExistingEmailSendsNoticeWithoutCode(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	repo.emails["owner@b.ru"] = &authsvc.User{ID: uuid.New()}
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if _, err := svc.RegisterStart(ctx, "owner@b.ru", "intruder", "Passw0rd!", ""); err != nil {
		t.Fatalf("existing email must look like a normal registration, got %v", err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].Code != "" {
		t.Fatalf("want one notice without a code, got %+v", mailer.sent)
	}
	for _, code := range []string{"", "000000", "123456"} {
		if _, err := svc.RegisterConfirm(ctx, "owner@b.ru", code, authsvc.Client{UserAgent: "test"}); !errors.Is(err, authsvc.ErrCodeInvalid) {
			t.Errorf("confirming a notice with %q: want ErrCodeInvalid, got %v", code, err)
		}
	}
}

func TestForgotPasswordHidesUnknownEmails(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if err := svc.ForgotPassword(ctx, "ghost@b.ru"); err != nil {
		t.Fatalf("unknown email: want silence, got %v", err)
	}
	if len(mailer.sent) != 0 {
		t.Error("no mail may go to an unknown address")
	}

	if err := svc.ForgotPassword(ctx, "ghost@b.ru"); !errors.Is(err, authsvc.ErrCodeTooSoon) {
		t.Errorf("second request: want ErrCodeTooSoon, got %v", err)
	}
	if err := svc.ResetPasswordWithCode(ctx, "ghost@b.ru", "000000", "NewPassw0rd"); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Errorf("reset for unknown email: want ErrCodeInvalid, got %v", err)
	}
}

func TestCaptureMailerKeepsLastPerRecipient(t *testing.T) {
	t.Parallel()
	capture := authsvc.NewCaptureMailer(nil)
	ctx := context.Background()
	_ = capture.Send(ctx, authsvc.Mail{To: "a@b.ru", Subject: "first", Code: "111111"})
	_ = capture.Send(ctx, authsvc.Mail{To: "a@b.ru", Subject: "second", Code: "222222"})

	last, ok := capture.Last(" A@B.ru ")
	if !ok || last.Code != "222222" || last.Subject != "second" {
		t.Errorf("unexpected capture %+v %v", last, ok)
	}
	if _, ok := capture.Last("other@b.ru"); ok {
		t.Error("no mail was sent to other@b.ru")
	}
}

func TestConfirmRejectsExpiredCode(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if _, err := svc.RegisterStart(ctx, "late@b.ru", "late", "Passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	code := mailer.sent[0].Code
	stored := repo.codes["register:late@b.ru"]
	stored.ExpiresAt = time.Now().Add(-time.Second)

	if _, err := svc.RegisterConfirm(ctx, "late@b.ru", code, authsvc.Client{UserAgent: "test"}); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Fatalf("expired code: want ErrCodeInvalid, got %v", err)
	}
	if repo.created != nil {
		t.Error("an account was created from an expired code")
	}

	if repo.codes["register:late@b.ru"].Attempts != 0 {
		t.Errorf("attempts = %d, want 0", repo.codes["register:late@b.ru"].Attempts)
	}
}

func TestResetRejectsExpiredCode(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	owner := &authsvc.User{ID: uuid.New()}
	repo.emails["owner@b.ru"] = owner
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if err := svc.ForgotPassword(ctx, "owner@b.ru"); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].Code == "" {
		t.Fatalf("want a reset code, got %+v", mailer.sent)
	}
	repo.codes["reset:owner@b.ru"].ExpiresAt = time.Now().Add(-time.Second)

	if err := svc.ResetPasswordWithCode(ctx, "owner@b.ru", mailer.sent[0].Code, "NewPassw0rd"); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Fatalf("expired reset code: want ErrCodeInvalid, got %v", err)
	}
}
