package authsvc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lifelog-api/internal/authsvc"

	"github.com/google/uuid"
)

func TestNormalizeLogin(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		err  error
	}{
		{"trims and lowers", "  MaShA ", "masha", nil},
		{"cyrillic is fine", "Маша_88", "маша_88", nil},
		{"64 runes is the limit", strings.Repeat("я", 64), strings.Repeat("я", 64), nil},
		{"65 runes", strings.Repeat("я", 65), "", authsvc.ErrLoginLength},
		{"empty", "   ", "", authsvc.ErrLoginRequired},
		{"at sign", "masha@home", "", authsvc.ErrLoginHasAt},
		{"inner space", "ma sha", "", authsvc.ErrLoginSymbols},
		{"control", "ma\x00sha", "", authsvc.ErrLoginSymbols},
		{"zero-width space", "ma​sha", "", authsvc.ErrLoginSymbols},
		{"invalid utf-8", "ma\xffsha", "", authsvc.ErrLoginSymbols},
	}
	for _, tc := range cases {
		got, err := authsvc.NormalizeLogin(tc.in)
		if !errors.Is(err, tc.err) || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q, %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}

func TestNormalizeDisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		err  error
	}{
		{"plain", "Женя", "Женя", nil},
		{"trimmed", "  Женя  ", "Женя", nil},
		{"inner spaces collapse", "Женя   Долганов", "Женя Долганов", nil},
		{"tab and newline are whitespace", "Женя\t\nД.\r\n", "Женя Д.", nil},
		{"non-breaking space collapses", "Женя  Д.", "Женя Д.", nil},
		{"one rune", "Ж", "Ж", nil},
		{"64 runes", strings.Repeat("ж", 64), strings.Repeat("ж", 64), nil},
		{"64 runes after collapsing", " " + strings.Repeat("ж", 32) + "    " + strings.Repeat("ж", 31) + " ", strings.Repeat("ж", 32) + " " + strings.Repeat("ж", 31), nil},
		{"empty", "", "", authsvc.ErrDisplayNameLength},
		{"only spaces", "   \t ", "", authsvc.ErrDisplayNameLength},
		{"65 runes", strings.Repeat("ж", 65), "", authsvc.ErrDisplayNameLength},
		{"nul", "Же\x00ня", "", authsvc.ErrDisplayNameControl},
		{"escape", "\x1b[31mЖеня", "", authsvc.ErrDisplayNameControl},
		{"delete", "Женя\x7f", "", authsvc.ErrDisplayNameControl},
		{"next line is whitespace", "Женя\u0085x", "Женя x", nil},
		{"c1 control", "Женя\u0080", "", authsvc.ErrDisplayNameControl},
		{"invalid utf-8", "Же\xffня", "", authsvc.ErrDisplayNameControl},
		{"bidi override", "Женя" + string(rune(0x202e)) + "яннА", "", authsvc.ErrDisplayNameControl},
		{"zero-width space", "Же" + string(rune(0x200b)) + "ня", "", authsvc.ErrDisplayNameControl},
		{"byte order mark", string(rune(0xfeff)) + "Женя", "", authsvc.ErrDisplayNameControl},
		{"emoji joiner stays", "Женя " + emojiFamily, "Женя " + emojiFamily, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := authsvc.NormalizeDisplayName(tc.in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, tc.err)
			}
			if err == nil && tc.want != "" && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

type displayNameRepo struct {
	authsvc.Repository
	stored map[uuid.UUID]string
}

func (r *displayNameRepo) SetDisplayName(_ context.Context, id uuid.UUID, name string) error {
	r.stored[id] = name
	return nil
}

func (r *displayNameRepo) UserByID(_ context.Context, id uuid.UUID) (*authsvc.User, error) {
	return &authsvc.User{ID: id, DisplayName: r.stored[id]}, nil
}

func TestSetDisplayNameStoresNormalisedName(t *testing.T) {
	repo := &displayNameRepo{stored: map[uuid.UUID]string{}}
	service := newService(repo)
	id := uuid.New()

	user, err := service.SetDisplayName(context.Background(), id, "  Женя   Д. ")
	if err != nil {
		t.Fatal(err)
	}
	if repo.stored[id] != "Женя Д." || user.DisplayName != "Женя Д." {
		t.Fatalf("stored %q, returned %q", repo.stored[id], user.DisplayName)
	}

	if _, err := service.SetDisplayName(context.Background(), id, "Же"+string(rune(0x202e))+"ня"); !errors.Is(err, authsvc.ErrDisplayNameControl) {
		t.Fatalf("bidi override: %v", err)
	}
	if repo.stored[id] != "Женя Д." {
		t.Fatalf("a refused name was written: %q", repo.stored[id])
	}
}

var emojiFamily = string([]rune{0x1f468, 0x200d, 0x1f469, 0x200d, 0x1f467})
