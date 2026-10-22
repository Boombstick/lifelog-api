package note

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"lifelog-api/internal/textutil"

	"github.com/google/uuid"
)

const (
	MaxTitleLength = 120
	MaxBodyLength  = 20000
	MaxTagLength   = 30
	MaxTags        = 10

	DefaultPageSize = 20
	MaxPageSize     = 50

	DayLayout = "2006-01-02"
)

var (
	ErrTitleTooLong = fmt.Errorf("заголовок — не длиннее %d символов", MaxTitleLength)
	ErrBodyEmpty    = errors.New("текст заметки не может быть пустым")
	ErrBodyTooLong  = fmt.Errorf("текст заметки — не длиннее %d символов", MaxBodyLength)
	ErrTagEmpty     = errors.New("тег не может быть пустым")
	ErrTagTooLong   = fmt.Errorf("тег — не длиннее %d символов", MaxTagLength)
	ErrTooManyTags  = fmt.Errorf("тегов не больше %d", MaxTags)
	ErrBadDay       = errors.New("day — дата в формате YYYY-MM-DD")
	ErrNotFound     = errors.New("заметка не найдена")
	ErrBadCursor    = errors.New("cursor: неверный курсор")
	ErrBadLimit     = fmt.Errorf("limit — целое число от 1 до %d", MaxPageSize)
)

type Note struct {
	ID     uint      `gorm:"primaryKey"`
	UserID uuid.UUID `gorm:"type:uuid"`
	Title  *string
	Body   string

	Day       *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
	Tags      []string `gorm:"-"`
}

func (Note) TableName() string { return "notes" }

type Position struct {
	Day       *time.Time
	UpdatedAt time.Time
	ID        uint
}

type Filter struct {
	UserID uuid.UUID

	Tags  []string
	After *Position
	Limit int
}

type Changes struct {
	SetTitle bool
	Title    *string
	Body     *string
	SetDay   bool
	Day      *time.Time
	SetTags  bool
	Tags     []string
}

func (c Changes) empty() bool {
	return !c.SetTitle && c.Body == nil && !c.SetDay && !c.SetTags
}

type TagCount struct {
	Tag   string
	Count int
}

func NormalizeTag(raw string) (string, error) {
	tag := strings.TrimSpace(raw)
	tag = strings.TrimPrefix(tag, "#")
	tag = strings.Join(strings.FieldsFunc(tag, unicode.IsSpace), " ")
	tag = strings.ToLower(tag)
	tag = textutil.FoldYo(tag)
	length := utf8.RuneCountInString(tag)
	if length == 0 {
		return "", ErrTagEmpty
	}
	if length > MaxTagLength {
		return "", ErrTagTooLong
	}
	return tag, nil
}

func NormalizeTags(raw []string) ([]string, error) {
	tags := make([]string, 0, len(raw))
	for _, value := range raw {
		tag, err := NormalizeTag(value)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	tags = slices.Compact(tags)
	if len(tags) > MaxTags {
		return nil, ErrTooManyTags
	}
	return tags, nil
}

func NormalizeTitle(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	title := strings.TrimSpace(*raw)
	if title == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(title) > MaxTitleLength {
		return nil, ErrTitleTooLong
	}
	return &title, nil
}

func NormalizeBody(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", ErrBodyEmpty
	}
	if utf8.RuneCountInString(body) > MaxBodyLength {
		return "", ErrBodyTooLong
	}
	return body, nil
}

func ParseDay(raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	day, err := time.Parse(DayLayout, strings.TrimSpace(*raw))
	if err != nil {
		return nil, ErrBadDay
	}
	return &day, nil
}
