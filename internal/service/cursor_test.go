package service

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeDecodeCursorRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after []any

		want []any
	}{
		{
			name:  "популярность и id",
			after: []any{float64(12.5), float64(42)},
			want:  []any{12.5, float64(42)},
		},
		{
			name:  "строка и число",
			after: []any{"Нолан", float64(7)},
			want:  []any{"Нолан", float64(7)},
		},
		{
			name:  "null внутри значений сортировки",
			after: []any{nil, float64(3)},
			want:  []any{nil, float64(3)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cursor := encodeCursor(tc.after)
			if cursor == "" {
				t.Fatal("непустые значения сортировки дали пустой курсор")
			}

			if strings.ContainsAny(cursor, "+/=") {
				t.Errorf("курсор %q не url-safe", cursor)
			}

			got := decodeCursor(cursor)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("decodeCursor(encodeCursor(%v)) = %#v, ожидалось %#v", tc.after, got, tc.want)
			}
		})
	}
}

func TestEncodeCursorEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		after []any
	}{
		{name: "nil", after: nil},
		{name: "пустой срез", after: []any{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := encodeCursor(tc.after); got != "" {
				t.Errorf("encodeCursor(%v) = %q, ожидалась пустая строка", tc.after, got)
			}
		})
	}
}

func TestDecodeCursorGarbage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cursor string
	}{
		{name: "пустая строка", cursor: ""},
		{name: "не base64", cursor: "это-не-курсор!!!"},
		{name: "base64 не от json", cursor: base64.RawURLEncoding.EncodeToString([]byte("просто текст"))},
		{name: "json-объект вместо массива", cursor: base64.RawURLEncoding.EncodeToString([]byte(`{"id":1}`))},
		{name: "json-число вместо массива", cursor: base64.RawURLEncoding.EncodeToString([]byte("42"))},
		{name: "обрезанный json", cursor: base64.RawURLEncoding.EncodeToString([]byte("[1, 2"))},
		{name: "курсор с padding из стандартного base64", cursor: base64.StdEncoding.EncodeToString([]byte("[1,2]"))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := decodeCursor(tc.cursor); got != nil {
				t.Errorf("decodeCursor(%q) = %#v, ожидался nil", tc.cursor, got)
			}
		})
	}
}

func TestPlaceCursorRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   uint
	}{
		{name: "обычный идентификатор", id: 42},
		{name: "единица", id: 1},
		{name: "большой идентификатор", id: 4294967295},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cursor := encodePlaceCursor(tc.id)
			if cursor == "" {
				t.Fatalf("encodePlaceCursor(%d) дал пустой курсор", tc.id)
			}
			if got := decodePlaceCursor(cursor); got != tc.id {
				t.Errorf("decodePlaceCursor(%q) = %d, ожидалось %d", cursor, got, tc.id)
			}
		})
	}
}

func TestPlaceCursorEdges(t *testing.T) {
	t.Parallel()

	t.Run("нулевой id даёт пустой курсор", func(t *testing.T) {
		t.Parallel()

		if got := encodePlaceCursor(0); got != "" {
			t.Errorf("encodePlaceCursor(0) = %q, ожидалась пустая строка", got)
		}
	})

	tests := []struct {
		name   string
		cursor string
	}{
		{name: "пустая строка", cursor: ""},
		{name: "не base64", cursor: "курсор?"},
		{name: "base64 не от числа", cursor: base64.RawURLEncoding.EncodeToString([]byte("abc"))},
		{name: "отрицательное число", cursor: base64.RawURLEncoding.EncodeToString([]byte("-5"))},
		{name: "дробное число", cursor: base64.RawURLEncoding.EncodeToString([]byte("1.5"))},
		{name: "число за пределами uint64", cursor: base64.RawURLEncoding.EncodeToString([]byte("99999999999999999999999"))},
	}

	for _, tc := range tests {
		t.Run("мусор: "+tc.name, func(t *testing.T) {
			t.Parallel()

			if got := decodePlaceCursor(tc.cursor); got != 0 {
				t.Errorf("decodePlaceCursor(%q) = %d, ожидался 0", tc.cursor, got)
			}
		})
	}
}
