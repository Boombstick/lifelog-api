package auth_test

import (
	"testing"
	"time"

	"lifelog-api/internal/auth"
)

func TestShareLinkAlive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	revoked := now.Add(-time.Hour)

	cases := []struct {
		name string
		link auth.ShareLink
		want bool
	}{
		{"still inside its lifetime", auth.ShareLink{ExpiresAt: now.Add(time.Minute)}, true},
		{"expired a minute ago", auth.ShareLink{ExpiresAt: now.Add(-time.Minute)}, false},
		{"expires exactly now", auth.ShareLink{ExpiresAt: now}, false},
		{"revoked but not yet expired", auth.ShareLink{ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}, false},
	}
	for _, c := range cases {
		if got := c.link.Alive(now); got != c.want {
			t.Errorf("%s: Alive = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestListVisibilityForSet(t *testing.T) {
	t.Parallel()

	visibility := auth.ListVisibility{
		Watchlist: auth.VisibilityPrivate,
		Watched:   auth.VisibilitySubscribers,
		Rated:     auth.VisibilityPrivate,
	}
	for set, want := range map[string]string{
		"watched":   auth.VisibilitySubscribers,
		"watching":  auth.VisibilitySubscribers,
		"rated":     auth.VisibilityPrivate,
		"watchlist": auth.VisibilityPrivate,
		"":          auth.VisibilityPrivate,
	} {
		if got := visibility.For(set); got != want {
			t.Errorf("For(%q) = %q, want %q", set, got, want)
		}
	}
}

func TestDefaultVisibilityKeepsPlansPrivate(t *testing.T) {
	t.Parallel()

	defaults := auth.DefaultVisibility(7)
	if defaults.Watchlist != auth.VisibilityPrivate {
		t.Errorf("the watchlist must default to private, got %q", defaults.Watchlist)
	}
	if defaults.Watched != auth.VisibilitySubscribers || defaults.Rated != auth.VisibilitySubscribers {
		t.Errorf("facts default to subscribers: %+v", defaults)
	}
	if !auth.ValidVisibility(defaults.Watchlist) || auth.ValidVisibility("everyone") {
		t.Error("ValidVisibility accepts exactly the two known levels")
	}
}
