package note

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRepo struct {
	notes      []Note
	lastFilter Filter
	updates    int
}

func (r *fakeRepo) Create(_ context.Context, n *Note) error { n.ID = 1; return nil }
func (r *fakeRepo) Get(_ context.Context, _ uuid.UUID, id uint) (Note, error) {
	return Note{ID: id, Body: "текст"}, nil
}
func (r *fakeRepo) List(_ context.Context, filter Filter) ([]Note, error) {
	r.lastFilter = filter
	return r.notes[:min(filter.Limit, len(r.notes))], nil
}
func (r *fakeRepo) Update(context.Context, uuid.UUID, uint, Changes) error {
	r.updates++
	return nil
}
func (r *fakeRepo) Delete(context.Context, uuid.UUID, uint) error       { return nil }
func (r *fakeRepo) Tags(context.Context, uuid.UUID) ([]TagCount, error) { return nil, nil }

func storedNotes(n int) []Note {
	notes := make([]Note, n)
	for i := range notes {
		notes[i] = Note{ID: uint(100 - i), Body: "x", UpdatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	}
	return notes
}

func TestListCursorOnlyWhenMoreRemain(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{notes: storedNotes(3)}
	s := NewService(repo)

	page, err := s.List(ctx, uuid.New(), ListInput{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastFilter.Limit != 3 || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("limit asked %d, items %d, cursor %v", repo.lastFilter.Limit, len(page.Items), page.NextCursor)
	}
	position, err := DecodeCursor(*page.NextCursor)
	if err != nil || position.ID != 99 {
		t.Fatalf("cursor points at %+v, %v; want the last shown note 99", position, err)
	}

	page, err = s.List(ctx, uuid.New(), ListInput{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 || page.NextCursor != nil {
		t.Fatalf("exact last page: items %d, cursor %v", len(page.Items), page.NextCursor)
	}
}

func TestListRejectsLimitOutsideRange(t *testing.T) {
	s := NewService(&fakeRepo{})
	for _, limit := range []int{0, MaxPageSize + 1} {
		if _, err := s.List(context.Background(), uuid.New(), ListInput{Limit: limit}); !errors.Is(err, ErrBadLimit) {
			t.Errorf("limit %d: %v", limit, err)
		}
	}
}

func TestListIgnoresBlankFilterTag(t *testing.T) {
	repo := &fakeRepo{notes: storedNotes(1)}
	page, err := NewService(repo).List(context.Background(), uuid.New(), ListInput{Limit: 5, Tags: []string{" ", "Книги"}})
	if err != nil || len(page.Items) != 1 || len(repo.lastFilter.Tags) != 1 || repo.lastFilter.Tags[0] != "книги" {
		t.Fatalf("items %d, tags %q, err %v", len(page.Items), repo.lastFilter.Tags, err)
	}
}

func TestEmptyUpdateWritesNothing(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := NewService(repo).Update(context.Background(), uuid.New(), 7, UpdateInput{}); err != nil {
		t.Fatal(err)
	}
	if repo.updates != 0 {
		t.Fatalf("empty PATCH wrote %d times", repo.updates)
	}
}

func TestResponseTagsAreNeverNull(t *testing.T) {
	raw, err := json.Marshal(toResponse(Note{ID: 1, Body: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tags":[]`) {
		t.Fatalf("tags not an empty list: %s", raw)
	}
}
