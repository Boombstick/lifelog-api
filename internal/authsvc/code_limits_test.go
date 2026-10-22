package authsvc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

var testClient = authsvc.Client{UserAgent: "test"}

func TestCodeTriesSurviveResend(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()
	const key = "register:a@b.ru"

	for round := 0; round < 4; round++ {
		if _, err := svc.RegisterStart(ctx, "a@b.ru", "alice", "Passw0rd!", ""); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		for i := 0; i < 5; i++ {
			if _, err := svc.RegisterConfirm(ctx, "a@b.ru", "wrong!", testClient); !errors.Is(err, authsvc.ErrCodeInvalid) {
				t.Fatalf("round %d try %d: want ErrCodeInvalid, got %v", round, i, err)
			}
		}
		repo.codes[key].CreatedAt = time.Now().Add(-2 * time.Minute)
	}

	if _, err := svc.RegisterStart(ctx, "a@b.ru", "alice", "Passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	fresh := mailer.sent[len(mailer.sent)-1].Code
	if _, err := svc.RegisterConfirm(ctx, "a@b.ru", fresh, testClient); !errors.Is(err, authsvc.ErrCodeLocked) {
		t.Fatalf("after 20 tries today even the right code must be locked, got %v", err)
	}
	if repo.created != nil {
		t.Error("an account was created past the daily limit")
	}
}

func TestParallelGuessesSpendOnlyTheAttemptsLeft(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, mailer := emailService(repo)
	ctx := context.Background()

	if _, err := svc.RegisterStart(ctx, "p@b.ru", "parallel", "Passw0rd!", ""); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.RegisterConfirm(ctx, "p@b.ru", "wrong!", testClient)
		}()
	}
	wg.Wait()

	if got := repo.codes["register:p@b.ru"].Attempts; got != 5 {
		t.Errorf("attempts = %d, want exactly 5", got)
	}
	if _, err := svc.RegisterConfirm(ctx, "p@b.ru", mailer.sent[0].Code, testClient); !errors.Is(err, authsvc.ErrCodeInvalid) {
		t.Errorf("an exhausted code must stay refused, got %v", err)
	}
}

func TestCodeLockHidesUnknownAddresses(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	svc, _ := emailService(repo)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		if err := svc.ResetPasswordWithCode(ctx, "ghost@b.ru", "000000", "NewPassw0rd"); !errors.Is(err, authsvc.ErrCodeInvalid) {
			t.Fatalf("try %d: want ErrCodeInvalid, got %v", i, err)
		}
	}
	if err := svc.ResetPasswordWithCode(ctx, "ghost@b.ru", "000000", "NewPassw0rd"); !errors.Is(err, authsvc.ErrCodeLocked) {
		t.Errorf("21st try: want ErrCodeLocked, got %v", err)
	}
}

func TestAttachTakenAddressIsRateLimited(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	repo.emails["taken@b.ru"] = &authsvc.User{ID: uuid.New()}
	svc, mailer := emailService(repo)
	ctx := context.Background()
	me := uuid.New()

	for i := 0; i < 5; i++ {
		if err := svc.AttachEmailStart(ctx, me, "taken@b.ru"); !errors.Is(err, authsvc.ErrEmailTaken) {
			t.Fatalf("try %d: want ErrEmailTaken, got %v", i, err)
		}
	}
	if err := svc.AttachEmailStart(ctx, me, "taken@b.ru"); !errors.Is(err, authsvc.ErrAttachLimit) {
		t.Errorf("6th try: want ErrAttachLimit, got %v", err)
	}
	if err := svc.AttachEmailStart(ctx, me, "free@b.ru"); !errors.Is(err, authsvc.ErrAttachLimit) {
		t.Errorf("the limit is per account, not per address, got %v", err)
	}
	if len(mailer.sent) != 0 {
		t.Errorf("no mail may leave, sent %d", len(mailer.sent))
	}
	if err := svc.AttachEmailStart(ctx, uuid.New(), "free@b.ru"); err != nil {
		t.Errorf("another account has its own budget, got %v", err)
	}
}

func TestAttachCooldownPrecedesTakenCheck(t *testing.T) {
	t.Parallel()
	repo := newEmailRepo()
	repo.emails["taken@b.ru"] = &authsvc.User{ID: uuid.New()}
	svc, _ := emailService(repo)
	ctx := context.Background()
	me := uuid.New()

	if err := svc.AttachEmailStart(ctx, me, "free@b.ru"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachEmailStart(ctx, me, "taken@b.ru"); !errors.Is(err, authsvc.ErrCodeTooSoon) {
		t.Errorf("within the cooldown: want ErrCodeTooSoon, got %v", err)
	}
}
