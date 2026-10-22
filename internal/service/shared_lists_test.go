package service

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"lifelog-api/internal/sharedlist"

	"github.com/google/uuid"
)

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"  Кино на выходные ", "Кино на выходные", true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("я", 80), strings.Repeat("я", 80), true},
		{strings.Repeat("я", 81), "", false},
	}
	for _, c := range cases {
		got, err := normalizeTitle(c.raw)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("normalizeTitle(%q) = %q, %v; want %q", c.raw, got, err, c.want)
		}
		if !c.ok && !errors.Is(err, ErrSharedListTitle) {
			t.Errorf("normalizeTitle(%q) error = %v, want ErrSharedListTitle", c.raw, err)
		}
	}
}

func TestParseMembers(t *testing.T) {
	owner := uuid.New()
	a, b := uuid.New(), uuid.New()

	got, err := parseMembers(owner, []string{a.String(), b.String(), a.String(), " " + b.String()})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []uuid.UUID{a, b}) {
		t.Errorf("duplicates must fold keeping first order, got %v", got)
	}

	if _, err := parseMembers(owner, []string{owner.String()}); !errors.Is(err, ErrSharedListSelfMember) {
		t.Errorf("owner as member: %v", err)
	}
	if _, err := parseMembers(owner, []string{"nope"}); err == nil {
		t.Error("malformed id accepted")
	} else if _, ok := errors.AsType[*BadMemberIDError](err); !ok {
		t.Errorf("malformed id: %T", err)
	}

	many := make([]string, 0, sharedlist.MaxMembers+1)
	for range sharedlist.MaxMembers {
		many = append(many, uuid.NewString())
	}
	if _, err := parseMembers(owner, many); err != nil {
		t.Errorf("exactly %d members rejected: %v", sharedlist.MaxMembers, err)
	}

	if _, err := parseMembers(owner, append(many, many[0])); err != nil {
		t.Errorf("duplicate pushed past the limit: %v", err)
	}
	if _, err := parseMembers(owner, append(many, uuid.NewString())); !errors.Is(err, ErrSharedListTooMany) {
		t.Errorf("%d members: %v", sharedlist.MaxMembers+1, err)
	}

	empty, err := parseMembers(owner, nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no members must give an empty non-nil set, got %v %v", empty, err)
	}
}

func TestDrawSize(t *testing.T) {
	intp := func(v int) *int { return &v }
	cases := []struct {
		mode  string
		count *int
		want  int
		err   error
	}{
		{"one", nil, 1, nil},
		{"one", intp(7), 1, nil},
		{"top", intp(3), 3, nil},
		{"top", intp(10), 10, nil},
		{"top", intp(2), 0, ErrSharedListBadCount},
		{"top", intp(11), 0, ErrSharedListBadCount},
		{"top", nil, 0, ErrSharedListBadCount},
		{"", nil, 0, ErrSharedListBadMode},
		{"all", intp(3), 0, ErrSharedListBadMode},
	}
	for _, c := range cases {
		got, err := drawSize(c.mode, c.count)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("drawSize(%q, %v) = %d, %v; want %d, %v", c.mode, c.count, got, err, c.want, c.err)
		}
	}
}

func testRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

func TestDrawFilmsBoundsAndDistinct(t *testing.T) {
	ids := []uint{11, 22, 33, 44, 55, 66, 77}
	original := slices.Clone(ids)
	rng := testRNG(1)

	for n := 1; n <= 10; n++ {
		got := drawFilms(ids, n, rng)
		if want := min(n, len(ids)); len(got) != want {
			t.Fatalf("n=%d: got %d films, want %d", n, len(got), want)
		}
		seen := map[uint]bool{}
		for _, id := range got {
			if !slices.Contains(ids, id) {
				t.Fatalf("n=%d: %d is not from the list", n, id)
			}
			if seen[id] {
				t.Fatalf("n=%d: %d drawn twice", n, id)
			}
			seen[id] = true
		}
	}
	if !slices.Equal(ids, original) {
		t.Error("input slice was modified")
	}
}

func TestDrawFilmsTopLargerThanList(t *testing.T) {
	ids := []uint{1, 2, 3}
	got := drawFilms(ids, 10, testRNG(2))
	sorted := slices.Clone(got)
	slices.Sort(sorted)
	if !slices.Equal(sorted, ids) {
		t.Errorf("got %v, want a permutation of %v", got, ids)
	}
}

func TestDrawFilmsSingle(t *testing.T) {
	if got := drawFilms([]uint{42}, 1, testRNG(3)); !slices.Equal(got, []uint{42}) {
		t.Errorf("got %v", got)
	}
}

func TestDrawFilmsUniform(t *testing.T) {
	ids := []uint{1, 2, 3, 4, 5, 6, 7, 8}
	const rounds = 80_000
	rng := testRNG(4)

	first := map[uint]int{}
	anywhere := map[uint]int{}
	for range rounds {
		got := drawFilms(ids, 3, rng)
		first[got[0]]++
		for _, id := range got {
			anywhere[id]++
		}
	}

	check := func(name string, counts map[uint]int, expected float64) {
		chi := 0.0
		for _, id := range ids {
			diff := float64(counts[id]) - expected
			chi += diff * diff / expected
		}

		if chi > 24.3 || math.IsNaN(chi) {
			t.Errorf("%s: chi-square %.1f, counts %v", name, chi, counts)
		}
	}
	check("first position", first, rounds/float64(len(ids)))
	check("any position", anywhere, rounds*3/float64(len(ids)))
}

func TestDrawUsesCryptoSeed(t *testing.T) {
	a := cryptoSeededRNG().Uint64()
	b := cryptoSeededRNG().Uint64()
	if a == b {
		t.Error("two crypto-seeded generators produced the same first value")
	}
}

func TestUserRefName(t *testing.T) {
	named, bare, missing := uuid.New(), uuid.New(), uuid.New()
	names := map[uuid.UUID]string{named: "Женя", bare: bare.String()}

	if ref := userRef(named, names); ref.Name == nil || *ref.Name != "Женя" {
		t.Errorf("named: %+v", ref)
	}
	if ref := userRef(bare, names); ref.Name != nil {
		t.Errorf("uuid placeholder must not pass for a name: %v", *ref.Name)
	}
	if ref := userRef(missing, names); ref.Name != nil || ref.AuthUserID != missing {
		t.Errorf("missing: %+v", ref)
	}
}

func TestNotSubscribersErrorListsIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	msg := (&NotSubscribersError{IDs: []uuid.UUID{a, b}}).Error()
	if !strings.HasPrefix(msg, "участник должен быть вашим подписчиком") ||
		!strings.Contains(msg, a.String()) || !strings.Contains(msg, b.String()) {
		t.Errorf("message: %s", msg)
	}
}

type stubFollowerCheck struct {
	followers map[uuid.UUID]bool
	err       error
	asked     []uuid.UUID
}

func (s *stubFollowerCheck) FollowersAmong(_ context.Context, _ uuid.UUID, candidates []uuid.UUID) (map[uuid.UUID]bool, error) {
	s.asked = append(s.asked, candidates...)
	if s.err != nil {
		return nil, s.err
	}
	return s.followers, nil
}

func TestValidateMembersChecksOnlyAdded(t *testing.T) {
	owner, kept, added, stranger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	check := &stubFollowerCheck{followers: map[uuid.UUID]bool{added: true}}
	svc := NewSharedListService(nil, check, nil, nil)
	ctx := context.Background()

	members, err := svc.validateMembers(ctx, owner, []string{kept.String(), added.String()}, []uuid.UUID{kept})
	if err != nil {
		t.Fatalf("an unsubscribed member already in the list must stay: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members = %v", members)
	}
	if len(check.asked) != 1 || check.asked[0] != added {
		t.Fatalf("only the added member is checked, asked %v", check.asked)
	}

	_, err = svc.validateMembers(ctx, owner, []string{stranger.String()}, nil)
	if notSub, ok := errors.AsType[*NotSubscribersError](err); !ok || len(notSub.IDs) != 1 || notSub.IDs[0] != stranger {
		t.Fatalf("a non-subscriber must be rejected, got %v", err)
	}

	check.asked = nil
	if _, err := svc.validateMembers(ctx, owner, []string{kept.String()}, []uuid.UUID{kept}); err != nil || len(check.asked) != 0 {
		t.Fatalf("no new members means no call to auth: %v %v", err, check.asked)
	}

	down := NewSharedListService(nil, &stubFollowerCheck{err: errors.New("auth down")}, nil, nil)
	if _, err := down.validateMembers(ctx, owner, []string{added.String()}, nil); !errors.Is(err, ErrFollowsUnavailable) {
		t.Fatalf("auth down must refuse the change, got %v", err)
	}
}
