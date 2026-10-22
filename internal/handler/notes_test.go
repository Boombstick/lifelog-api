package handler

import (
	"encoding/json"
	"errors"
	"testing"
)

func decodeNotePatch(t *testing.T, raw string) noteUpdateBody {
	t.Helper()
	var body noteUpdateBody
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestNotePatchAbsentVersusNull(t *testing.T) {
	input, err := decodeNotePatch(t, `{}`).input()
	if err != nil || input.SetTitle || input.SetDay || input.SetTags || input.Body != nil {
		t.Fatalf("empty PATCH must change nothing: %+v, %v", input, err)
	}

	input, err = decodeNotePatch(t, `{"title": null, "day": null}`).input()
	if err != nil || !input.SetTitle || input.Title != nil || !input.SetDay || input.Day != nil {
		t.Fatalf("null must clear: %+v, %v", input, err)
	}

	input, err = decodeNotePatch(t, `{"title": "x", "day": "2026-10-02", "tags": ["a"], "body": "b"}`).input()
	if err != nil || *input.Title != "x" || *input.Day != "2026-10-02" || input.Tags[0] != "a" || *input.Body != "b" {
		t.Fatalf("values: %+v, %v", input, err)
	}
}

func TestNotePatchRejects(t *testing.T) {
	if _, err := decodeNotePatch(t, `{"body": null}`).input(); !errors.Is(err, errNoteBodyNull) {
		t.Fatalf("body null: %v", err)
	}
	if _, err := decodeNotePatch(t, `{"title": 5}`).input(); !errors.Is(err, errNoteField) {
		t.Fatalf("title number: %v", err)
	}
	if _, err := decodeNotePatch(t, `{"tags": "a"}`).input(); !errors.Is(err, errNoteField) {
		t.Fatalf("tags string: %v", err)
	}
}
