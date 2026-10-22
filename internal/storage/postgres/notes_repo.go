package postgres

import (
	"context"
	"errors"
	"slices"

	"lifelog-api/internal/note"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NotesRepository struct {
	db *gorm.DB
}

func NewNotesRepository(db *gorm.DB) *NotesRepository {
	return &NotesRepository{db: db}
}

type noteTag struct {
	NoteID uint
	UserID uuid.UUID `gorm:"type:uuid"`
	Tag    string
}

func (noteTag) TableName() string { return "note_tags" }

func (r *NotesRepository) Create(ctx context.Context, n *note.Note) error {
	now := dbNow()
	n.CreatedAt, n.UpdatedAt = now, now
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(n).Error; err != nil {
			return err
		}
		return insertNoteTags(tx, n.ID, n.UserID, n.Tags)
	})
}

func insertNoteTags(tx *gorm.DB, noteID uint, userID uuid.UUID, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	rows := make([]noteTag, 0, len(tags))
	for _, tag := range tags {
		rows = append(rows, noteTag{NoteID: noteID, UserID: userID, Tag: tag})
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

func (r *NotesRepository) Get(ctx context.Context, userID uuid.UUID, id uint) (note.Note, error) {
	var found note.Note
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&found).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return found, note.ErrNotFound
	}
	if err != nil {
		return found, err
	}
	notes := []note.Note{found}
	if err := r.attachTags(ctx, notes); err != nil {
		return found, err
	}
	return notes[0], nil
}

func (r *NotesRepository) List(ctx context.Context, filter note.Filter) ([]note.Note, error) {
	query := r.db.WithContext(ctx).Where("user_id = ?", filter.UserID)
	if len(filter.Tags) > 0 {
		query = query.Where(`id IN (
			SELECT note_id FROM note_tags
			WHERE user_id = ? AND tag IN ?
			GROUP BY note_id HAVING count(*) = ?)`,
			filter.UserID, filter.Tags, len(filter.Tags))
	}
	if after := filter.After; after != nil {
		if after.Day != nil {
			query = query.Where(`(day < ? OR day IS NULL OR (day = ? AND (updated_at, id) < (?, ?)))`,
				*after.Day, *after.Day, after.UpdatedAt, after.ID)
		} else {
			query = query.Where(`day IS NULL AND (updated_at, id) < (?, ?)`, after.UpdatedAt, after.ID)
		}
	}
	var notes []note.Note
	err := query.
		Order("day DESC NULLS LAST, updated_at DESC, id DESC").
		Limit(filter.Limit).
		Find(&notes).Error
	if err != nil {
		return nil, err
	}
	if err := r.attachTags(ctx, notes); err != nil {
		return nil, err
	}
	return notes, nil
}

func (r *NotesRepository) attachTags(ctx context.Context, notes []note.Note) error {
	if len(notes) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	var rows []noteTag
	if err := r.db.WithContext(ctx).Where("note_id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	byNote := make(map[uint][]string, len(notes))
	for _, row := range rows {
		byNote[row.NoteID] = append(byNote[row.NoteID], row.Tag)
	}

	for i := range notes {
		tags := byNote[notes[i].ID]
		slices.Sort(tags)
		notes[i].Tags = tags
	}
	return nil
}

func (r *NotesRepository) Update(ctx context.Context, userID uuid.UUID, id uint, changes note.Changes) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing note.Note
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("id = ? AND user_id = ?", id, userID).
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return note.ErrNotFound
		}
		if err != nil {
			return err
		}

		values := map[string]any{"updated_at": dbNow()}
		if changes.SetTitle {
			values["title"] = changes.Title
		}
		if changes.Body != nil {
			values["body"] = *changes.Body
		}
		if changes.SetDay {
			values["day"] = changes.Day
		}
		if err := tx.Model(&note.Note{}).Where("id = ?", id).Updates(values).Error; err != nil {
			return err
		}

		if !changes.SetTags {
			return nil
		}
		if err := tx.Where("note_id = ?", id).Delete(&noteTag{}).Error; err != nil {
			return err
		}
		return insertNoteTags(tx, id, userID, changes.Tags)
	})
}

func (r *NotesRepository) Delete(ctx context.Context, userID uuid.UUID, id uint) error {
	result := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&note.Note{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return note.ErrNotFound
	}
	return nil
}

func (r *NotesRepository) Tags(ctx context.Context, userID uuid.UUID) ([]note.TagCount, error) {
	var counts []note.TagCount
	err := r.db.WithContext(ctx).
		Model(&noteTag{}).
		Select("tag, count(*) AS count").
		Where("user_id = ?", userID).
		Group("tag").
		Order(`count DESC, tag COLLATE "C"`).
		Scan(&counts).Error
	return counts, err
}
