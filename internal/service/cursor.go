package service

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	"lifelog-api/internal/film"
)

func encodeCursor(after []any) string {
	if len(after) == 0 {
		return ""
	}
	payload, err := json.Marshal(after)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCursor(cursor string) []any {
	if cursor == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil
	}
	var after []any
	if err := json.Unmarshal(raw, &after); err != nil {
		return nil
	}
	return after
}

type searchCursor struct {
	After []any `json:"a"`
	Total int   `json:"t"`
}

func encodeSearchCursor(after []any, total int) string {
	if len(after) == 0 {
		return ""
	}
	payload, err := json.Marshal(searchCursor{After: after, Total: total})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeSearchCursor(cursor string) (after []any, total int) {
	if cursor == "" {
		return nil, 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, 0
	}
	var c searchCursor
	if err := json.Unmarshal(raw, &c); err == nil && len(c.After) > 0 {
		return c.After, max(c.Total, 0)
	}
	return decodeCursor(cursor), 0
}

func encodePlaceCursor(id uint) string {
	if id == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(id), 10)))
}

func encodeEntryCursor(value any, filmID uint) string {
	if value == nil || filmID == 0 {
		return ""
	}
	return encodeCursor([]any{value, filmID})
}

func decodeEntryCursor(cursor string, sortMode film.EntrySort) (any, uint) {
	parts := decodeCursor(cursor)
	if len(parts) != 2 {
		return nil, 0
	}
	rawID, ok := parts[1].(float64)
	if !ok || rawID < 1 {
		return nil, 0
	}
	filmID := uint(rawID)

	switch sortMode {
	case film.EntrySortRate:
		rate, ok := parts[0].(float64)
		if !ok {
			return nil, 0
		}
		return rate, filmID
	case film.EntrySortTitle:
		title, ok := parts[0].(string)
		if !ok {
			return nil, 0
		}
		return title, filmID
	default:
		raw, ok := parts[0].(string)
		if !ok {
			return nil, 0
		}
		moment, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, 0
		}
		return moment, filmID
	}
}

func decodePlaceCursor(cursor string) uint {
	if cursor == "" {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return 0
	}
	return uint(value)
}
