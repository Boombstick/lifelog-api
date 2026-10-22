package middleware

import "testing"

func TestBearerToken(t *testing.T) {
	cases := []struct {
		header string
		want   string
		ok     bool
	}{
		{"Bearer abc", "abc", true},
		{"bearer abc", "abc", true},
		{"BEARER  abc  ", "abc", true},
		{"Bearer ", "", false},
		{"Bearer    ", "", false},
		{"Basic abc", "", false},
		{"Bearerabc", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := BearerToken(tc.header)
		if got != tc.want || ok != tc.ok {
			t.Errorf("BearerToken(%q) = %q, %v; want %q, %v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}

func TestInternalTokenMatches(t *testing.T) {
	if InternalTokenMatches("", "") {
		t.Error("an unconfigured token must reject everything, even an empty header")
	}
	if InternalTokenMatches("secret", "") {
		t.Error("an unconfigured token must reject everything")
	}
	if InternalTokenMatches("secre", "secret") || InternalTokenMatches("secret!", "secret") {
		t.Error("a near miss matched")
	}
	if !InternalTokenMatches("secret", "secret") {
		t.Error("the right token was rejected")
	}
}
