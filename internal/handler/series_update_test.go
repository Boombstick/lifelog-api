package handler

import (
	"encoding/json"
	"errors"
	"testing"

	"lifelog-api/internal/film"
)

func TestParseSeasonUpdate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		body     string
		state    string
		rateOnly bool
		rate     int16
		setRate  bool
		err      error
	}{
		{name: "empty body removes the entry", body: `{}`},
		{name: "explicit null state removes the entry", body: `{"state":null}`},
		{name: "state without rate keeps the rating", body: `{"state":"watched"}`, state: "watched"},
		{name: "state with rate", body: `{"state":"watched","rate":8}`, state: "watched", rate: 8, setRate: true},
		{name: "state with null rate clears it", body: `{"state":"watched","rate":null}`, state: "watched", setRate: true},
		{name: "null rate alone keeps the state", body: `{"rate":null}`, rateOnly: true, setRate: true},
		{name: "rate alone keeps the state", body: `{"rate":6}`, rateOnly: true, rate: 6, setRate: true},
		{name: "explicit null state with rate is not rate-only", body: `{"state":null,"rate":6}`, rate: 6, setRate: true},
		{name: "rate out of range", body: `{"state":"watched","rate":11}`, err: film.ErrSeasonRate},
		{name: "state of the wrong type", body: `{"state":5}`, err: film.ErrBadSeasonState},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var request seasonStateRequest
			if err := json.Unmarshal([]byte(tc.body), &request); err != nil {
				t.Fatal(err)
			}
			got, err := parseSeasonUpdate(request)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("want %v, got %v", tc.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (got.state == nil) != (tc.state == "") || (got.state != nil && *got.state != tc.state) {
				t.Errorf("state %v, want %q", got.state, tc.state)
			}
			if (got.rate == nil) != (tc.rate == 0) || (got.rate != nil && *got.rate != tc.rate) {
				t.Errorf("rate %v, want %d", got.rate, tc.rate)
			}
			if got.rateOnly != tc.rateOnly || got.setRate != tc.setRate {
				t.Errorf("rateOnly=%v setRate=%v, want %v %v", got.rateOnly, got.setRate, tc.rateOnly, tc.setRate)
			}
		})
	}
}
