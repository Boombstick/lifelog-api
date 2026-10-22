package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lifelog-api/internal/model"
	"lifelog-api/internal/place"

	"gorm.io/gorm"
)

func placeBy(author *uint) *place.Place {
	return &place.Place{Model: gorm.Model{ID: 7}, Name: "Кофейня", CreatedBy: author}
}

func TestPlaceEditOnlyByAuthorOrAdmin(t *testing.T) {
	author := uint(1)
	tests := []struct {
		name    string
		card    *place.Place
		viewer  place.Viewer
		wantErr error
	}{
		{"author edits own place", placeBy(&author), place.Viewer{UserID: 1}, nil},
		{"stranger is refused", placeBy(&author), place.Viewer{UserID: 2}, ErrPlaceNotYours},
		{"admin edits any place", placeBy(&author), place.Viewer{UserID: 2, Admin: true}, nil},
		{"place without author is admin-only", placeBy(nil), place.Viewer{UserID: 1}, ErrPlaceNotYours},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubPlaceRepo{card: tc.card}
			svc := NewPlaceService(repo)

			err := svc.Update(context.Background(), 7, tc.viewer, &UpdatePlaceRequest{CreatePlaceRequest{Name: "x", Address: "y", Category: "z"}})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Update err = %v, want %v", err, tc.wantErr)
			}
			if (repo.updated != nil) != (tc.wantErr == nil) {
				t.Fatalf("update reached the repo: %v", repo.updated != nil)
			}

			err = svc.Delete(context.Background(), 7, tc.viewer)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Delete err = %v, want %v", err, tc.wantErr)
			}
			if (repo.deleted != 0) != (tc.wantErr == nil) {
				t.Fatalf("delete reached the repo: %d", repo.deleted)
			}
		})
	}
}

func TestPlaceHiddenIsNotFound(t *testing.T) {
	svc := NewPlaceService(&stubPlaceRepo{})
	err := svc.Delete(context.Background(), 7, place.Viewer{UserID: 1})
	if _, ok := errors.AsType[*model.EntityNotFoundError](err); !ok {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestPlaceCreateRecordsAuthor(t *testing.T) {
	svc := NewPlaceService(&stubPlaceRepo{})
	created, err := svc.Create(context.Background(), 5, &CreatePlaceRequest{Name: "x", Address: "y", Category: "z"})
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedBy == nil || *created.CreatedBy != 5 {
		t.Fatalf("author = %v, want 5", created.CreatedBy)
	}
}

func TestPlaceReviewLength(t *testing.T) {
	author := uint(1)
	repo := &stubPlaceRepo{card: placeBy(&author)}
	svc := NewPlaceService(repo)

	_, err := svc.AddReview(context.Background(), 7, place.Viewer{UserID: 2}, &AddReviewRequest{Rate: 4, Description: strings.Repeat("я", 256)})
	if !errors.Is(err, ErrReviewTooLong) {
		t.Fatalf("err = %v, want ErrReviewTooLong", err)
	}
	if repo.reviewed != nil {
		t.Fatal("too long review reached the repo")
	}

	if _, err := svc.AddReview(context.Background(), 7, place.Viewer{UserID: 2}, &AddReviewRequest{Rate: 4, Description: strings.Repeat("я", 255)}); err != nil {
		t.Fatalf("255 characters must pass: %v", err)
	}
	if repo.reviewed == nil || *repo.reviewed.UserID != 2 {
		t.Fatalf("review author not recorded: %+v", repo.reviewed)
	}
}

func TestPlaceListPassesViewer(t *testing.T) {
	repo := &stubPlaceRepo{}
	viewer := place.Viewer{UserID: 3}
	if _, err := NewPlaceService(repo).FindAll(context.Background(), PlaceListRequest{Viewer: viewer}); err != nil {
		t.Fatal(err)
	}
	if repo.lastFilter.Viewer != viewer {
		t.Fatalf("viewer lost on the way to the repo: %+v", repo.lastFilter.Viewer)
	}
}
