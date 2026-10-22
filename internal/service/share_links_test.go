package service

import (
	"errors"
	"testing"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/randtoken"
)

func TestФормаТокенаСсылки(t *testing.T) {
	valid := "st2u4flFV0aCNhqekI8ukT79L8Ju1wcQLqqUjBdliQs"
	if len(valid) != 43 {
		t.Fatalf("образец токена длиной %d, ожидалось 43", len(valid))
	}

	cases := map[string]bool{
		valid:            true,
		"":               false,
		"короткий":       false,
		valid + "x":      false,
		valid[:42]:       false,
		valid[:42] + "+": false,
		valid[:42] + "/": false,
		valid[:42] + "=": false,
		valid[:42] + " ": false,
		"нетакогонетакогонетакогонетакогонетакогоабв": false,
	}

	for token, want := range cases {
		if got := validToken(token); got != want {
			t.Errorf("validToken(%q) = %v, ожидалось %v", token, got, want)
		}
	}
}

func TestТокеныНеПовторяются(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		token := randtoken.New(shareTokenBytes)
		if !validToken(token) {
			t.Fatalf("сгенерирован токен, не проходящий свою же проверку: %q", token)
		}
		if seen[token] {
			t.Fatalf("токен повторился на шаге %d", i)
		}
		seen[token] = true
	}
}

func TestShareTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		minutes int
		want    time.Duration
		wantErr bool
	}{
		{name: "не указан — сутки", minutes: 0, want: DefaultShareTTL},
		{name: "минута", minutes: 1, want: time.Minute},
		{name: "два часа", minutes: 120, want: 2 * time.Hour},
		{name: "ровно потолок", minutes: 2880, want: MaxShareTTL},

		{name: "больше потолка", minutes: 2881, wantErr: true},
		{name: "неделя", minutes: 10080, wantErr: true},
		{name: "отрицательный", minutes: -5, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := shareTTL(tc.minutes)
			if tc.wantErr {
				if !errors.Is(err, ErrShareTTLOutOfRange) {
					t.Fatalf("ожидалась ErrShareTTLOutOfRange, получено %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
			if got != tc.want {
				t.Errorf("срок %v, ожидался %v", got, tc.want)
			}
		})
	}
}

func TestShareLinkAlive(t *testing.T) {
	t.Parallel()

	now := time.Now()
	revoked := now.Add(-time.Hour)

	tests := []struct {
		name string
		link auth.ShareLink
		want bool
	}{
		{name: "живая", link: auth.ShareLink{ExpiresAt: now.Add(time.Hour)}, want: true},
		{name: "просрочена", link: auth.ShareLink{ExpiresAt: now.Add(-time.Minute)}},
		{name: "отозвана", link: auth.ShareLink{ExpiresAt: now.Add(time.Hour), RevokedAt: &revoked}},
		{name: "и то и другое", link: auth.ShareLink{ExpiresAt: now.Add(-time.Hour), RevokedAt: &revoked}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.link.Alive(now); got != tc.want {
				t.Errorf("Alive = %v, ожидалось %v", got, tc.want)
			}
		})
	}
}
