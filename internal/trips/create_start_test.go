package trips

import "testing"

func TestCreateTripRequestParseStartDate(t *testing.T) {
	day := NewDate(2026, 9, 1)
	cases := []struct {
		name string
		raw  *string
		want *Date
		ok   bool
	}{
		{"absent", nil, nil, true},
		{"blank", ptr("  "), nil, true},
		{"date", ptr("2026-09-01"), &day, true},
		{"padded", ptr(" 2026-09-01 "), &day, true},
		{"day first", ptr("01-09-2026"), nil, false},
		{"with time", ptr("2026-09-01T10:00:00Z"), nil, false},
		{"impossible", ptr("2026-02-30"), nil, false},
	}
	for _, tc := range cases {
		got, ok := CreateTripRequest{StartDate: tc.raw}.ParseStartDate()
		if ok != tc.ok || (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%s: got %v %v, want %v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}
