package note

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	Create(ctx context.Context, note *Note) error
	Get(ctx context.Context, userID uuid.UUID, id uint) (Note, error)
	List(ctx context.Context, filter Filter) ([]Note, error)
	Update(ctx context.Context, userID uuid.UUID, id uint, changes Changes) error
	Delete(ctx context.Context, userID uuid.UUID, id uint) error
	Tags(ctx context.Context, userID uuid.UUID) ([]TagCount, error)
}

type NoteResponse struct {
	ID        uint      `json:"id"`
	Title     *string   `json:"title"`
	Body      string    `json:"body"`
	Day       *string   `json:"day"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NotesPage struct {
	Items      []NoteResponse `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type TagCountResponse struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

type TagsResponse struct {
	Items []TagCountResponse `json:"items"`
}

type CreateInput struct {
	Title *string
	Body  string
	Day   *string
	Tags  []string
}

type UpdateInput struct {
	SetTitle bool
	Title    *string
	Body     *string
	SetDay   bool
	Day      *string
	SetTags  bool
	Tags     []string
}

type ListInput struct {
	Tags   []string
	Cursor string
	Limit  int
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func toResponse(n Note) NoteResponse {
	response := NoteResponse{
		ID:        n.ID,
		Title:     n.Title,
		Body:      n.Body,
		Tags:      n.Tags,
		CreatedAt: n.CreatedAt.UTC(),
		UpdatedAt: n.UpdatedAt.UTC(),
	}
	if response.Tags == nil {
		response.Tags = []string{}
	}
	if n.Day != nil {
		day := n.Day.Format(DayLayout)
		response.Day = &day
	}
	return response
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, input CreateInput) (NoteResponse, error) {
	title, err := NormalizeTitle(input.Title)
	if err != nil {
		return NoteResponse{}, err
	}
	body, err := NormalizeBody(input.Body)
	if err != nil {
		return NoteResponse{}, err
	}
	day, err := ParseDay(input.Day)
	if err != nil {
		return NoteResponse{}, err
	}
	tags, err := NormalizeTags(input.Tags)
	if err != nil {
		return NoteResponse{}, err
	}
	created := Note{UserID: userID, Title: title, Body: body, Day: day, Tags: tags}
	if err := s.repo.Create(ctx, &created); err != nil {
		return NoteResponse{}, err
	}
	return toResponse(created), nil
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID, id uint) (NoteResponse, error) {
	found, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return NoteResponse{}, err
	}
	return toResponse(found), nil
}

func (s *Service) Update(ctx context.Context, userID uuid.UUID, id uint, input UpdateInput) (NoteResponse, error) {
	changes, err := input.changes()
	if err != nil {
		return NoteResponse{}, err
	}

	if changes.empty() {
		return s.Get(ctx, userID, id)
	}
	if err := s.repo.Update(ctx, userID, id, changes); err != nil {
		return NoteResponse{}, err
	}
	return s.Get(ctx, userID, id)
}

func (input UpdateInput) changes() (Changes, error) {
	var changes Changes
	if input.SetTitle {
		title, err := NormalizeTitle(input.Title)
		if err != nil {
			return Changes{}, err
		}
		changes.SetTitle, changes.Title = true, title
	}
	if input.Body != nil {
		body, err := NormalizeBody(*input.Body)
		if err != nil {
			return Changes{}, err
		}
		changes.Body = &body
	}
	if input.SetDay {
		day, err := ParseDay(input.Day)
		if err != nil {
			return Changes{}, err
		}
		changes.SetDay, changes.Day = true, day
	}
	if input.SetTags {
		tags, err := NormalizeTags(input.Tags)
		if err != nil {
			return Changes{}, err
		}
		changes.SetTags, changes.Tags = true, tags
	}
	return changes, nil
}

func (s *Service) Delete(ctx context.Context, userID uuid.UUID, id uint) error {
	return s.repo.Delete(ctx, userID, id)
}

func filterTags(raw []string) (tags []string, ok bool) {
	for _, value := range raw {
		tag, err := NormalizeTag(value)
		if errors.Is(err, ErrTagEmpty) {
			continue
		}
		if err != nil {
			return nil, false
		}
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	if len(tags) > MaxTags {
		return nil, false
	}
	return tags, true
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, input ListInput) (NotesPage, error) {
	page := NotesPage{Items: []NoteResponse{}}
	after, err := DecodeCursor(input.Cursor)
	if err != nil {
		return NotesPage{}, err
	}
	limit := input.Limit
	if limit < 1 || limit > MaxPageSize {
		return NotesPage{}, ErrBadLimit
	}
	tags, ok := filterTags(input.Tags)
	if !ok {
		return page, nil
	}

	notes, err := s.repo.List(ctx, Filter{
		UserID: userID,
		Tags:   tags,
		After:  after,
		Limit:  limit + 1,
	})
	if err != nil {
		return NotesPage{}, err
	}
	if len(notes) > limit {
		notes = notes[:limit]
		last := notes[limit-1]
		cursor := EncodeCursor(Position{Day: last.Day, UpdatedAt: last.UpdatedAt, ID: last.ID})
		page.NextCursor = &cursor
	}
	for _, n := range notes {
		page.Items = append(page.Items, toResponse(n))
	}
	return page, nil
}

func (s *Service) Tags(ctx context.Context, userID uuid.UUID) (TagsResponse, error) {
	counts, err := s.repo.Tags(ctx, userID)
	if err != nil {
		return TagsResponse{}, err
	}
	response := TagsResponse{Items: make([]TagCountResponse, 0, len(counts))}
	for _, count := range counts {
		response.Items = append(response.Items, TagCountResponse{Tag: count.Tag, Count: count.Count})
	}
	return response, nil
}
