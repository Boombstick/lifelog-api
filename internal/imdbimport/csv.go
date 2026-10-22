package imdbimport

import (
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ListKind string

const (
	ListRatings   ListKind = "ratings"
	ListWatchlist ListKind = "watchlist"
)

const MaxRows = 20000

var (
	ErrNotIMDb     = errors.New("это не экспорт IMDb")
	ErrTooManyRows = errors.New("в файле больше 20 000 строк")
	ErrBadIMDbID   = errors.New("битый IMDb ID")
	ErrBadRating   = errors.New("оценка не от 1 до 10")

	imdbIDPattern = regexp.MustCompile(`^tt\d+$`)
)

const (
	utf8BOM         = "\uFEFF"
	dateLayout      = "2006-01-02"
	headerConst     = "const"
	headerRating    = "your rating"
	headerDateRated = "date rated"
	headerPosition  = "position"
	headerCreated   = "created"
	headerTitle     = "title"
	headerOriginal  = "original title"
	headerTitleType = "title type"
	headerYear      = "year"
)

type Row struct {
	Line      int
	IMDbID    string
	Title     string
	Year      int
	TitleType string

	Rating    int16
	DateRated *time.Time
	Created   *time.Time
	Err       error
}

func Parse(r io.Reader) (ListKind, []Row, error) {
	reader := csv.NewReader(r)

	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false

	header, err := reader.Read()
	if err != nil {
		return "", nil, ErrNotIMDb
	}
	columns := make(map[string]int, len(header))
	for i, name := range header {
		if i == 0 {
			name = strings.TrimPrefix(name, utf8BOM)
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if _, dup := columns[name]; !dup {
			columns[name] = i
		}
	}

	kind, ok := detectKind(columns)
	if !ok {
		return "", nil, ErrNotIMDb
	}

	var rows []Row
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var parseErr *csv.ParseError
			if !errors.As(err, &parseErr) {
				return "", nil, err
			}

			rows = append(rows, Row{Line: parseErr.StartLine, Err: err})
		} else {
			if blankRecord(record) {
				continue
			}
			line, _ := reader.FieldPos(0)
			rows = append(rows, parseRow(kind, columns, record, line))
		}
		if len(rows) > MaxRows {
			return "", nil, ErrTooManyRows
		}
	}
	return kind, rows, nil
}

func detectKind(columns map[string]int) (ListKind, bool) {
	has := func(name string) bool { _, ok := columns[name]; return ok }
	if !has(headerConst) {
		return "", false
	}
	switch {
	case has(headerPosition) && has(headerCreated):
		return ListWatchlist, true
	case has(headerRating) && has(headerDateRated):
		return ListRatings, true
	}
	return "", false
}

func blankRecord(record []string) bool {
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			return false
		}
	}
	return true
}

func parseRow(kind ListKind, columns map[string]int, record []string, line int) Row {
	field := func(name string) string {
		i, ok := columns[name]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	row := Row{
		Line:      line,
		IMDbID:    field(headerConst),
		Title:     field(headerTitle),
		TitleType: field(headerTitleType),
	}
	if row.Title == "" {
		row.Title = field(headerOriginal)
	}
	if year, err := strconv.Atoi(field(headerYear)); err == nil && year > 0 {
		row.Year = year
	}

	if !imdbIDPattern.MatchString(row.IMDbID) {
		row.Err = ErrBadIMDbID
		return row
	}

	switch kind {
	case ListRatings:
		rating, ok := parseRating(field(headerRating))
		if !ok {
			row.Err = ErrBadRating
			return row
		}
		row.Rating = rating
		row.DateRated = parseDate(field(headerDateRated))
	case ListWatchlist:
		row.Created = parseDate(field(headerCreated))
	}
	return row
}

func parseRating(value string) (int16, bool) {
	rating, err := strconv.Atoi(value)
	if err != nil {
		parsed, ferr := strconv.ParseFloat(value, 64)
		if ferr != nil || parsed != float64(int(parsed)) {
			return 0, false
		}
		rating = int(parsed)
	}
	if rating < 1 || rating > 10 {
		return 0, false
	}
	return int16(rating), true
}

func parseDate(value string) *time.Time {
	if len(value) < len(dateLayout) {
		return nil
	}
	parsed, err := time.Parse(dateLayout, value[:len(dateLayout)])
	if err != nil {
		return nil
	}
	return &parsed
}
