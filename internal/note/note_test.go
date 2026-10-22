package note

import (
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNormalizeTag(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		err  error
	}{
		{raw: "книги", want: "книги"},
		{raw: "  #Книги ", want: "книги"},
		{raw: "Ёлка", want: "елка"},
		{raw: "новый   \t год", want: "новый год"},
		{raw: "#", err: ErrTagEmpty},
		{raw: "   ", err: ErrTagEmpty},
		{raw: "##два", want: "#два"},
		{raw: strings.Repeat("я", MaxTagLength), want: strings.Repeat("я", MaxTagLength)},
		{raw: strings.Repeat("я", MaxTagLength+1), err: ErrTagTooLong},

		{raw: " #" + strings.Repeat("Я", MaxTagLength) + " ", want: strings.Repeat("я", MaxTagLength)},
	}
	for _, tc := range cases {
		got, err := NormalizeTag(tc.raw)
		if !errors.Is(err, tc.err) {
			t.Errorf("NormalizeTag(%q) error = %v, want %v", tc.raw, err, tc.err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestNormalizeTagsSortsAndFolds(t *testing.T) {
	got, err := NormalizeTags([]string{"тренировки", "#Книги", "книги", "books", "ёж", "еж"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"books", "еж", "книги", "тренировки"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeTagsLimitAfterDedup(t *testing.T) {
	raw := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "#A", "B "}
	got, err := NormalizeTags(raw)
	if err != nil {
		t.Fatalf("12 spellings of 10 tags must pass: %v", err)
	}
	if len(got) != MaxTags {
		t.Fatalf("got %d tags", len(got))
	}
	if _, err := NormalizeTags(append(raw, "k")); !errors.Is(err, ErrTooManyTags) {
		t.Fatalf("11 distinct tags: error = %v", err)
	}
}

func TestNormalizeTagsEmpty(t *testing.T) {
	got, err := NormalizeTags(nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil slice", got, err)
	}
	if _, err := NormalizeTags([]string{"ok", " # "}); !errors.Is(err, ErrTagEmpty) {
		t.Fatalf("blank tag: error = %v", err)
	}
}

func ptr(s string) *string { return &s }

func TestNormalizeTitle(t *testing.T) {
	if got, err := NormalizeTitle(nil); got != nil || err != nil {
		t.Fatalf("nil: %v, %v", got, err)
	}
	if got, err := NormalizeTitle(ptr("   ")); got != nil || err != nil {
		t.Fatalf("blank must become null: %v, %v", got, err)
	}
	if got, _ := NormalizeTitle(ptr("  Дневник ")); got == nil || *got != "Дневник" {
		t.Fatalf("trim: %v", got)
	}
	if _, err := NormalizeTitle(ptr(strings.Repeat("ж", MaxTitleLength))); err != nil {
		t.Fatalf("120 runes must pass: %v", err)
	}
	if _, err := NormalizeTitle(ptr(strings.Repeat("ж", MaxTitleLength+1))); !errors.Is(err, ErrTitleTooLong) {
		t.Fatalf("121 runes: %v", err)
	}
}

func TestNormalizeBody(t *testing.T) {
	got, err := NormalizeBody("\n  первая строка\n\n    отступ\n  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "первая строка\n\n    отступ" {
		t.Fatalf("inner line breaks and indentation must survive: %q", got)
	}
	if _, err := NormalizeBody(" \n\t "); !errors.Is(err, ErrBodyEmpty) {
		t.Fatalf("whitespace body: %v", err)
	}
	if _, err := NormalizeBody(strings.Repeat("ё", MaxBodyLength) + "   "); err != nil {
		t.Fatalf("20000 runes plus trailing spaces must pass: %v", err)
	}
	if _, err := NormalizeBody(strings.Repeat("ё", MaxBodyLength+1)); !errors.Is(err, ErrBodyTooLong) {
		t.Fatalf("20001 runes: %v", err)
	}
}

func TestParseDay(t *testing.T) {
	day, err := ParseDay(ptr("2026-10-02"))
	if err != nil || day == nil || day.Format(DayLayout) != "2026-10-02" || day.Location() != time.UTC {
		t.Fatalf("got %v, %v", day, err)
	}
	for _, bad := range []string{"2026-02-30", "02.10.2026", "2026-10-02T00:00:00Z", ""} {
		if _, err := ParseDay(ptr(bad)); !errors.Is(err, ErrBadDay) {
			t.Errorf("ParseDay(%q) error = %v", bad, err)
		}
	}
	if day, err := ParseDay(nil); day != nil || err != nil {
		t.Fatalf("nil: %v, %v", day, err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	day := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 10, 2, 9, 30, 15, 123456000, time.FixedZone("x", 3*3600))
	for _, position := range []Position{
		{Day: &day, UpdatedAt: updated, ID: 42},
		{Day: nil, UpdatedAt: updated, ID: 7},
	} {
		got, err := DecodeCursor(EncodeCursor(position))
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != position.ID || !got.UpdatedAt.Equal(position.UpdatedAt) {
			t.Fatalf("got %+v, want %+v", got, position)
		}
		if (got.Day == nil) != (position.Day == nil) || (got.Day != nil && !got.Day.Equal(*position.Day)) {
			t.Fatalf("day: got %v, want %v", got.Day, position.Day)
		}
	}
}

func TestDecodeCursor(t *testing.T) {
	if got, err := DecodeCursor(""); got != nil || err != nil {
		t.Fatalf("empty cursor: %v, %v", got, err)
	}
	fractionalID := base64.RawURLEncoding.EncodeToString([]byte(`[null,"2026-10-02T00:00:00Z",1.5]`))
	for _, bad := range []string{"!!!", "bm90IGpzb24", "WzEsMl0", "WyIyMDI2LTEwLTAyIiwiMjAyNi0xMC0wMlQwMDowMDowMFoiLDBd", fractionalID} {
		if _, err := DecodeCursor(bad); !errors.Is(err, ErrBadCursor) {
			t.Errorf("DecodeCursor(%q) error = %v", bad, err)
		}
	}
}

func TestPageSize(t *testing.T) {
	if got, err := PageSize(""); got != DefaultPageSize || err != nil {
		t.Fatalf("absent: %d, %v", got, err)
	}
	if got, err := PageSize("50"); got != 50 || err != nil {
		t.Fatalf("50: %d, %v", got, err)
	}
	for _, bad := range []string{"0", "51", "-1", "abc"} {
		if _, err := PageSize(bad); !errors.Is(err, ErrBadLimit) {
			t.Errorf("PageSize(%q) error = %v", bad, err)
		}
	}
}

func TestFilterTags(t *testing.T) {
	tags, ok := filterTags([]string{"#Книги", "", "книги", "Ёж"})
	if !ok || !slices.Equal(tags, []string{"еж", "книги"}) {
		t.Fatalf("got %q, %v", tags, ok)
	}
	if _, ok := filterTags([]string{strings.Repeat("x", MaxTagLength+1)}); ok {
		t.Fatal("a tag too long to exist must short-circuit to an empty page")
	}
	many := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}
	if _, ok := filterTags(many); ok {
		t.Fatal("more distinct tags than a note may carry must short-circuit")
	}
}

func TestUpdateChanges(t *testing.T) {
	changes, err := UpdateInput{}.changes()
	if err != nil || !changes.empty() {
		t.Fatalf("empty PATCH: %+v, %v", changes, err)
	}
	changes, err = UpdateInput{SetTitle: true, SetDay: true}.changes()
	if err != nil || changes.empty() || changes.Title != nil || changes.Day != nil {
		t.Fatalf("null title and day must clear: %+v, %v", changes, err)
	}
	if _, err := (UpdateInput{Body: ptr("  ")}).changes(); !errors.Is(err, ErrBodyEmpty) {
		t.Fatalf("blank body: %v", err)
	}
	changes, err = UpdateInput{SetTags: true}.changes()
	if err != nil || !changes.SetTags || changes.Tags == nil || len(changes.Tags) != 0 {
		t.Fatalf("null tags must clear the set: %+v, %v", changes, err)
	}
}
