package importer

import (
	"testing"

	"lifelog-api/internal/tmdb"
)

func TestRussianTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		titles []tmdb.AlternativeTitle
		want   string
	}{
		{name: "пусто", titles: nil, want: ""},
		{
			name: "российское название кириллицей",
			titles: []tmdb.AlternativeTitle{
				{CountryCode: "US", Title: "Working Title"},
				{CountryCode: "RU", Title: "Прибытие"},
			},
			want: "Прибытие",
		},
		{
			name: "российская транслитерация не берётся",
			titles: []tmdb.AlternativeTitle{
				{CountryCode: "RU", Title: "Pribytie"},
			},
			want: "",
		},
		{
			name: "кириллица чужой страны не берётся",
			titles: []tmdb.AlternativeTitle{
				{CountryCode: "BG", Title: "Пристигане"},
			},
			want: "",
		},
		{
			name: "первое подходящее из нескольких",
			titles: []tmdb.AlternativeTitle{
				{CountryCode: "RU", Title: "Pribytie"},
				{CountryCode: "RU", Title: "Прибытие"},
				{CountryCode: "RU", Title: "Другое"},
			},
			want: "Прибытие",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := russianTitle(tc.titles); got != tc.want {
				t.Errorf("russianTitle = %q, ожидалось %q", got, tc.want)
			}
		})
	}
}

func TestRussianName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variants []string
		want     string
	}{
		{name: "пусто", variants: nil, want: ""},
		{
			name:     "только латиница",
			variants: []string{"Christopher Nolan", "Chris Nolan"},
			want:     "",
		},
		{
			name:     "кириллица среди прочих",
			variants: []string{"クリストファー・ノーラン", "Кристофер Нолан", "Christopher Nolan"},
			want:     "Кристофер Нолан",
		},
		{
			name:     "первое кириллическое написание",
			variants: []string{"Крістофер Нолан", "Кристофер Нолан"},
			want:     "Крістофер Нолан",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := russianName(tc.variants); got != tc.want {
				t.Errorf("russianName = %q, ожидалось %q", got, tc.want)
			}
		})
	}
}

func TestHasCyrillic(t *testing.T) {
	t.Parallel()

	yes := []string{"Нолан", "Nolan и Ко", "Ё"}
	no := []string{"", "Nolan", "ノーラン", "123"}

	for _, value := range yes {
		if !hasCyrillic(value) {
			t.Errorf("%q должно считаться кириллицей", value)
		}
	}
	for _, value := range no {
		if hasCyrillic(value) {
			t.Errorf("%q не должно считаться кириллицей", value)
		}
	}
}
