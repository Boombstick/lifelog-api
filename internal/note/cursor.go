package note

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

func EncodeCursor(p Position) string {
	var day any
	if p.Day != nil {
		day = p.Day.Format(DayLayout)
	}
	payload, err := json.Marshal([]any{day, p.UpdatedAt.UTC().Format(time.RFC3339Nano), p.ID})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeCursor(cursor string) (*Position, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrBadCursor
	}
	var parts []any
	if err := json.Unmarshal(raw, &parts); err != nil || len(parts) != 3 {
		return nil, ErrBadCursor
	}

	var position Position
	switch value := parts[0].(type) {
	case nil:
	case string:
		day, err := time.Parse(DayLayout, value)
		if err != nil {
			return nil, ErrBadCursor
		}
		position.Day = &day
	default:
		return nil, ErrBadCursor
	}

	updated, ok := parts[1].(string)
	if !ok {
		return nil, ErrBadCursor
	}
	moment, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return nil, ErrBadCursor
	}
	position.UpdatedAt = moment

	id, ok := parts[2].(float64)
	if !ok || id < 1 || id != float64(uint(id)) {
		return nil, ErrBadCursor
	}
	position.ID = uint(id)
	return &position, nil
}

func PageSize(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultPageSize, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > MaxPageSize {
		return 0, ErrBadLimit
	}
	return limit, nil
}
