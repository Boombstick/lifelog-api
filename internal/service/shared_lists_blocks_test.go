package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"lifelog-api/internal/model"
	"lifelog-api/internal/sharedlist"

	"github.com/google/uuid"
)

type memListRepo struct {
	SharedListRepository
	lists   map[uint]sharedlist.List
	members map[uint][]uuid.UUID
	removed [][2]uuid.UUID
	applied [][2]uuid.UUID
}

func newMemListRepo() *memListRepo {
	return &memListRepo{lists: map[uint]sharedlist.List{}, members: map[uint][]uuid.UUID{}}
}

func (r *memListRepo) add(id uint, owner uuid.UUID, members ...uuid.UUID) {
	r.lists[id] = sharedlist.List{ID: id, OwnerAuthID: owner, Title: "list"}
	r.members[id] = members
}

func (r *memListRepo) participant(list sharedlist.List, caller uuid.UUID) bool {
	return list.OwnerAuthID == caller || slices.Contains(r.members[list.ID], caller)
}

func (r *memListRepo) Access(_ context.Context, listID uint, caller uuid.UUID) (sharedlist.List, error) {
	list, ok := r.lists[listID]
	if !ok || !r.participant(list, caller) {
		return sharedlist.List{}, model.NewEntityNotFound("shared list")
	}
	return list, nil
}

func (r *memListRepo) ListsOf(_ context.Context, caller uuid.UUID) ([]sharedlist.List, error) {
	var result []sharedlist.List
	for id := uint(1); id <= uint(len(r.lists)); id++ {
		if list, ok := r.lists[id]; ok && r.participant(list, caller) {
			result = append(result, list)
		}
	}
	return result, nil
}

func (r *memListRepo) Members(_ context.Context, ids []uint) (map[uint][]uuid.UUID, error) {
	result := map[uint][]uuid.UUID{}
	for _, id := range ids {
		result[id] = slices.Clone(r.members[id])
	}
	return result, nil
}

func (r *memListRepo) ItemCounts(context.Context, []uint) (map[uint]int, error) {
	return map[uint]int{}, nil
}

func (r *memListRepo) LastDraws(context.Context, []uint) (map[uint]sharedlist.Draw, error) {
	return map[uint]sharedlist.Draw{}, nil
}

func (r *memListRepo) Items(context.Context, uint) ([]sharedlist.Item, error) { return nil, nil }

func (r *memListRepo) Draws(context.Context, uint, int) ([]sharedlist.Draw, error) { return nil, nil }

func (r *memListRepo) Names(context.Context, []uuid.UUID) (map[uuid.UUID]string, error) {
	return map[uuid.UUID]string{}, nil
}

func (r *memListRepo) RemoveMembers(_ context.Context, owner uuid.UUID, members []uuid.UUID) error {
	for id, list := range r.lists {
		if list.OwnerAuthID != owner {
			continue
		}
		r.members[id] = slices.DeleteFunc(r.members[id], func(m uuid.UUID) bool { return slices.Contains(members, m) })
	}
	for _, member := range members {
		r.removed = append(r.removed, [2]uuid.UUID{owner, member})
	}
	return nil
}

func (r *memListRepo) RemoveBlocked(_ context.Context, a, b uuid.UUID) (int64, error) {
	r.applied = append(r.applied, [2]uuid.UUID{a, b})
	var removed int64
	for id, list := range r.lists {
		before := len(r.members[id])
		switch list.OwnerAuthID {
		case a:
			r.members[id] = slices.DeleteFunc(r.members[id], func(m uuid.UUID) bool { return m == b })
		case b:
			r.members[id] = slices.DeleteFunc(r.members[id], func(m uuid.UUID) bool { return m == a })
		}
		removed += int64(before - len(r.members[id]))
	}
	return removed, nil
}

type stubBlocks struct {
	pairs map[[2]uuid.UUID]bool
	down  bool
}

func (s *stubBlocks) block(a, b uuid.UUID) {
	if s.pairs == nil {
		s.pairs = map[[2]uuid.UUID]bool{}
	}
	s.pairs[[2]uuid.UUID{a, b}] = true
}

func (s *stubBlocks) Blocked(_ context.Context, user uuid.UUID) (map[uuid.UUID]struct{}, bool) {
	if s.down {
		return nil, false
	}
	result := map[uuid.UUID]struct{}{}
	for pair := range s.pairs {
		if pair[0] == user {
			result[pair[1]] = struct{}{}
		}
		if pair[1] == user {
			result[pair[0]] = struct{}{}
		}
	}
	return result, true
}

func memberIDs(card SharedListCard) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(card.Members))
	for _, ref := range card.Members {
		ids = append(ids, ref.AuthUserID)
	}
	return ids
}

func listIDs(page SharedListsPage) []uint {
	ids := make([]uint, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestBlockSignalLostMemberLosesAccessOnRead(t *testing.T) {
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob, carol)
	repo.add(2, bob, alice)
	blocks := &stubBlocks{}
	blocks.block(alice, bob)
	svc := NewSharedListService(repo, nil, blocks, nil)
	ctx := context.Background()

	if _, err := svc.Get(ctx, 1, bob); !isNotFound(err) {
		t.Fatalf("a blocked member must get 404 on the owner's list, got %v", err)
	}
	if slices.Contains(repo.members[1], bob) {
		t.Fatal("the read must remove the blocked membership")
	}

	page, err := svc.Lists(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if got := listIDs(page); !slices.Equal(got, []uint{1}) {
		t.Fatalf("alice sees %v, want only her own list", got)
	}
	if slices.Contains(repo.members[2], alice) {
		t.Fatal("the lists page must remove alice from bob's list")
	}
	if page.Items[0].MembersCount != 1 {
		t.Fatalf("members_count = %d, want 1 (carol)", page.Items[0].MembersCount)
	}
}

func TestBlockSignalLostOwnerSeesMemberGone(t *testing.T) {
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob, carol)
	blocks := &stubBlocks{}
	blocks.block(bob, alice)
	svc := NewSharedListService(repo, nil, blocks, nil)
	ctx := context.Background()

	card, err := svc.Get(ctx, 1, alice)
	if err != nil {
		t.Fatal(err)
	}
	if got := memberIDs(card); !slices.Equal(got, []uuid.UUID{carol}) {
		t.Fatalf("owner's card members = %v, want only carol", got)
	}
	if !slices.Contains(repo.removed, [2]uuid.UUID{alice, bob}) {
		t.Fatal("the owner's card must remove the blocked member")
	}

	repo.members[1] = []uuid.UUID{bob, carol}
	card, err = svc.Get(ctx, 1, carol)
	if err != nil {
		t.Fatal(err)
	}
	if got := memberIDs(card); !slices.Equal(got, []uuid.UUID{carol}) {
		t.Fatalf("member's card members = %v, want only carol", got)
	}
}

func TestBlockedMemberCannotWrite(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob)
	blocks := &stubBlocks{}
	blocks.block(alice, bob)
	svc := NewSharedListService(repo, nil, blocks, nil)

	if _, err := svc.Draw(context.Background(), 1, bob, "one", nil); !isNotFound(err) {
		t.Fatalf("draw by a blocked member: got %v, want 404", err)
	}
	if _, _, err := svc.AddItem(context.Background(), 1, bob, 7); !isNotFound(err) {
		t.Fatalf("add by a blocked member: got %v, want 404", err)
	}
}

func TestBlocksUnknownFailsClosedForOthersOnly(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob)
	svc := NewSharedListService(repo, nil, &stubBlocks{down: true}, nil)
	ctx := context.Background()

	if _, err := svc.Get(ctx, 1, bob); !errors.Is(err, ErrBlocksUnavailable) {
		t.Fatalf("member read with blocks unknown: got %v, want ErrBlocksUnavailable", err)
	}
	page, err := svc.Lists(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("someone else's list must be hidden while blocks are unknown, got %v", listIDs(page))
	}

	card, err := svc.Get(ctx, 1, alice)
	if err != nil {
		t.Fatalf("the owner always gets their own list: %v", err)
	}
	if got := memberIDs(card); !slices.Equal(got, []uuid.UUID{bob}) {
		t.Fatalf("owner's members = %v, want stored set", got)
	}
	if len(repo.removed) != 0 || !slices.Contains(repo.members[1], bob) {
		t.Fatal("nothing may be removed while blocks are unknown")
	}
}

func TestUpdateRechecksBlockedMember(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob)
	blocks := &stubBlocks{}
	blocks.block(alice, bob)
	check := &stubFollowerCheck{followers: map[uuid.UUID]bool{}}
	svc := NewSharedListService(repo, check, blocks, nil)

	_, err := svc.Update(context.Background(), 1, alice, nil, []string{bob.String()})
	if notSub, ok := errors.AsType[*NotSubscribersError](err); !ok || !slices.Equal(notSub.IDs, []uuid.UUID{bob}) {
		t.Fatalf("got %v, want bob rejected as a non-subscriber", err)
	}
	if !slices.Equal(check.asked, []uuid.UUID{bob}) {
		t.Fatalf("auth asked about %v, want bob", check.asked)
	}
}

func TestApplyBlockRemovesBothDirections(t *testing.T) {
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	repo := newMemListRepo()
	repo.add(1, alice, bob, carol)
	repo.add(2, bob, alice, carol)
	repo.add(3, carol, alice, bob)
	svc := NewSharedListService(repo, nil, nil, nil)

	removed, err := svc.ApplyBlock(context.Background(), alice, bob)
	if err != nil || removed != 2 {
		t.Fatalf("removed %d, %v; want 2", removed, err)
	}
	if !slices.Equal(repo.members[1], []uuid.UUID{carol}) || !slices.Equal(repo.members[2], []uuid.UUID{carol}) {
		t.Fatalf("lists 1 and 2 = %v, %v", repo.members[1], repo.members[2])
	}
	if !slices.Equal(repo.members[3], []uuid.UUID{alice, bob}) {
		t.Fatalf("a third person's list is not touched, got %v", repo.members[3])
	}
}
