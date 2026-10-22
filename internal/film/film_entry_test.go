package film

import "testing"

func TestEntrySetValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  EntrySet
		want bool
	}{
		{name: "список к просмотру", set: EntrySetWatchlist, want: true},
		{name: "просмотренные", set: EntrySetWatched, want: true},
		{name: "оценённые", set: EntrySetRated, want: true},
		{name: "пустая строка", set: EntrySet(""), want: false},
		{name: "выдуманный набор", set: EntrySet("favourites"), want: false},
		{name: "другой регистр", set: EntrySet("Watchlist"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.set.Valid(); got != tc.want {
				t.Errorf("EntrySet(%q).Valid() = %v, ожидалось %v", tc.set, got, tc.want)
			}
		})
	}
}

func TestEntrySortAllowedIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sort EntrySort
		set  EntrySet
		want bool
	}{
		{name: "дата добавления есть у всех записей", sort: EntrySortAdded, set: EntrySetWatchlist, want: true},
		{name: "название есть у всех фильмов", sort: EntrySortTitle, set: EntrySetWatchlist, want: true},
		{name: "дата просмотра в просмотренных", sort: EntrySortWatched, set: EntrySetWatched, want: true},
		{name: "дата просмотра в оценённых", sort: EntrySortWatched, set: EntrySetRated, want: true},
		{name: "дата просмотра в списке к просмотру", sort: EntrySortWatched, set: EntrySetWatchlist, want: false},
		{name: "оценка в оценённых", sort: EntrySortRate, set: EntrySetRated, want: true},
		{name: "оценка в просмотренных", sort: EntrySortRate, set: EntrySetWatched, want: false},
		{name: "оценка в списке к просмотру", sort: EntrySortRate, set: EntrySetWatchlist, want: false},
		{name: "выдуманная сортировка", sort: EntrySort("random"), set: EntrySetRated, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.sort.AllowedIn(tc.set); got != tc.want {
				t.Errorf("EntrySort(%q).AllowedIn(%q) = %v, ожидалось %v", tc.sort, tc.set, got, tc.want)
			}
		})
	}
}

func TestDefaultEntrySortIsAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  EntrySet
		want EntrySort
	}{
		{name: "список к просмотру — по добавлению", set: EntrySetWatchlist, want: EntrySortAdded},
		{name: "просмотренные — по дате просмотра", set: EntrySetWatched, want: EntrySortWatched},
		{name: "оценённые — по оценке", set: EntrySetRated, want: EntrySortRate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DefaultEntrySort(tc.set)
			if got != tc.want {
				t.Errorf("DefaultEntrySort(%q) = %q, ожидалось %q", tc.set, got, tc.want)
			}
			if !got.AllowedIn(tc.set) {
				t.Errorf("порядок по умолчанию %q недопустим в наборе %q", got, tc.set)
			}
		})
	}
}
