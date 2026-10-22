package authsvc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

type blockKey struct {
	blocker uuid.UUID
	blocked uuid.UUID
}

type blocksRepo struct {
	*subscriptionsRepo
	blocks map[blockKey]time.Time
}

func newBlocksRepo(users ...*authsvc.User) *blocksRepo {
	return &blocksRepo{subscriptionsRepo: newSubscriptionsRepo(users...), blocks: map[blockKey]time.Time{}}
}

func (r *blocksRepo) Block(_ context.Context, block *authsvc.Block) error {
	key := blockKey{block.BlockerID, block.BlockedID}
	if _, exists := r.blocks[key]; !exists {
		r.blocks[key] = block.CreatedAt
	}
	delete(r.rows, subscriptionKey{block.BlockerID, block.BlockedID})
	delete(r.rows, subscriptionKey{block.BlockedID, block.BlockerID})
	return nil
}

func (r *blocksRepo) Unblock(_ context.Context, blocker, blocked uuid.UUID) error {
	delete(r.blocks, blockKey{blocker, blocked})
	return nil
}

func (r *blocksRepo) BlockState(_ context.Context, viewer, other uuid.UUID) (bool, bool, error) {
	_, byOther := r.blocks[blockKey{other, viewer}]
	_, byViewer := r.blocks[blockKey{viewer, other}]
	return byOther, byViewer, nil
}

func (r *blocksRepo) BlocksOf(_ context.Context, blocker uuid.UUID) ([]authsvc.BlockView, error) {
	var views []authsvc.BlockView
	for key, created := range r.blocks {
		if key.blocker != blocker {
			continue
		}
		for _, user := range r.users {
			if user.ID == key.blocked {
				views = append(views, authsvc.BlockView{UserID: user.ID, Login: *user.Login, DisplayName: user.DisplayName, CreatedAt: created})
			}
		}
	}
	return views, nil
}

func TestBlockRemovesSubscriptionsBothWays(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	repo := newBlocksRepo(alice, bob)
	svc := newService(repo)
	ctx := context.Background()

	if _, err := svc.Subscribe(ctx, alice.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Subscribe(ctx, bob.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	view, err := svc.Block(ctx, alice.ID, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if view.UserID != bob.ID || view.Login != "bob" {
		t.Errorf("unexpected block view %+v", view)
	}
	if len(repo.rows) != 0 {
		t.Errorf("subscriptions must be gone in both directions, left %d", len(repo.rows))
	}
}

func TestBlockValidation(t *testing.T) {
	t.Parallel()
	alice := newUser("alice", false)
	svc := newService(newBlocksRepo(alice))
	ctx := context.Background()

	if _, err := svc.Block(ctx, alice.ID, "alice"); !errors.Is(err, authsvc.ErrSelfBlock) {
		t.Errorf("self block: want ErrSelfBlock, got %v", err)
	}
	if _, err := svc.Block(ctx, alice.ID, "nobody"); !errors.Is(err, authsvc.ErrUserNotFound) {
		t.Errorf("unknown login: want ErrUserNotFound, got %v", err)
	}
}

func TestSubscribeRespectsBlocks(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	repo := newBlocksRepo(alice, bob)
	svc := newService(repo)
	ctx := context.Background()

	if _, err := svc.Block(ctx, alice.ID, "bob"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Subscribe(ctx, bob.ID, "alice"); !errors.Is(err, authsvc.ErrUserNotFound) {
		t.Errorf("blocked person subscribing: want ErrUserNotFound, got %v", err)
	}
	if _, err := svc.Subscribe(ctx, alice.ID, "bob"); !errors.Is(err, authsvc.ErrUserBlocked) {
		t.Errorf("blocker subscribing: want ErrUserBlocked, got %v", err)
	}

	if err := svc.Unblock(ctx, alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.rows) != 0 {
		t.Error("unblocking must not restore subscriptions")
	}
	if _, err := svc.Subscribe(ctx, bob.ID, "alice"); err != nil {
		t.Errorf("after unblock subscribing must work, got %v", err)
	}
}

func TestBlocksListIsNeverNull(t *testing.T) {
	t.Parallel()
	alice := newUser("alice", false)
	blocks, err := newService(newBlocksRepo(alice)).Blocks(context.Background(), alice.ID)
	if err != nil || blocks == nil {
		t.Errorf("empty list must be [], got %v, %v", blocks, err)
	}
}

type recordingNotifier struct {
	calls [][]uuid.UUID

	mu         sync.Mutex
	blocks     [][2]uuid.UUID
	failBlocks int
}

func (n *recordingNotifier) NotifyMerge(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (n *recordingNotifier) NotifyBlock(_ context.Context, a, b uuid.UUID) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failBlocks > 0 {
		n.failBlocks--
		return errors.New("catalog down")
	}
	n.blocks = append(n.blocks, [2]uuid.UUID{a, b})
	return nil
}

func (n *recordingNotifier) deliveredBlocks() [][2]uuid.UUID {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([][2]uuid.UUID(nil), n.blocks...)
}

func (n *recordingNotifier) InvalidateFollows(_ context.Context, userIDs ...uuid.UUID) error {
	n.calls = append(n.calls, userIDs)
	return nil
}

func (n *recordingNotifier) invalidated(id uuid.UUID) bool {
	for _, call := range n.calls {
		for _, got := range call {
			if got == id {
				return true
			}
		}
	}
	return false
}

func serviceWithNotifier(repo authsvc.Repository, notifier authsvc.MergeNotifier) *authsvc.Service {
	return authsvc.NewService(repo, nil, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour, "bot")
}

func TestBlockInvalidatesBothSides(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	repo := newBlocksRepo(alice, bob)
	notifier := &recordingNotifier{}
	svc := serviceWithNotifier(repo, notifier)
	ctx := context.Background()

	if _, err := svc.Block(ctx, alice.ID, "Bob"); err != nil {
		t.Fatal(err)
	}
	if !notifier.invalidated(alice.ID) || !notifier.invalidated(bob.ID) {
		t.Fatalf("both sides must be invalidated: %v", notifier.calls)
	}
}

func TestSubscriptionChangesInvalidateTheFollower(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", true)
	repo := newBlocksRepo(alice, bob)
	notifier := &recordingNotifier{}
	svc := serviceWithNotifier(repo, notifier)
	ctx := context.Background()

	if _, err := svc.Subscribe(ctx, alice.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApproveSubscriber(ctx, bob.ID, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unsubscribe(ctx, alice.ID, bob.ID); err != nil {
		t.Fatal(err)
	}
	if len(notifier.calls) != 3 {
		t.Fatalf("want 3 invalidations, got %v", notifier.calls)
	}
	for _, call := range notifier.calls {
		if len(call) != 1 || call[0] != alice.ID {
			t.Fatalf("only the follower's set changes: %v", notifier.calls)
		}
	}
}

func TestInvalidationFailureDoesNotFailTheChange(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	svc := serviceWithNotifier(newBlocksRepo(alice, bob), failingNotifier{})

	if _, err := svc.Subscribe(context.Background(), alice.ID, "bob"); err != nil {
		t.Fatalf("subscribe failed because of the catalog: %v", err)
	}
}

type failingNotifier struct{}

func (failingNotifier) NotifyMerge(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (failingNotifier) InvalidateFollows(context.Context, ...uuid.UUID) error {
	return errors.New("catalog down")
}

func (failingNotifier) NotifyBlock(context.Context, uuid.UUID, uuid.UUID) error {
	return errors.New("catalog down")
}

func TestBlockSignalsTheCatalog(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	notifier := &recordingNotifier{}
	svc := serviceWithNotifier(newBlocksRepo(alice, bob), notifier)

	if _, err := svc.Block(context.Background(), alice.ID, "bob"); err != nil {
		t.Fatal(err)
	}
	got := notifier.deliveredBlocks()
	if len(got) != 1 || got[0] != [2]uuid.UUID{alice.ID, bob.ID} {
		t.Fatalf("block signals = %v, want one for (alice, bob)", got)
	}
}

func TestBlockSignalIsRetried(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	notifier := &recordingNotifier{failBlocks: 2}
	svc := serviceWithNotifier(newBlocksRepo(alice, bob), notifier)

	if _, err := svc.Block(context.Background(), alice.ID, "bob"); err != nil {
		t.Fatalf("a failing catalog must not fail the block: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(notifier.deliveredBlocks()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the third attempt never delivered the block signal")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBlockSurvivesLostSignal(t *testing.T) {
	t.Parallel()
	alice, bob := newUser("alice", false), newUser("bob", false)
	svc := serviceWithNotifier(newBlocksRepo(alice, bob), failingNotifier{})

	if _, err := svc.Block(context.Background(), alice.ID, "bob"); err != nil {
		t.Fatalf("a lost signal must not fail the block: %v", err)
	}
}
