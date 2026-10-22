package imdbimport

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const ratingsExport = "\uFEFF" + `Const,Your Rating,Date Rated,Title,Original Title,URL,Title Type,IMDb Rating,Runtime (mins),Year,Genres,Num Votes,Release Date,Directors
tt0816692,9,2024-03-01,Interstellar,Interstellar,https://www.imdb.com/title/tt0816692/,Movie,8.7,169,2014,"Adventure, Drama, Sci-Fi",2200000,2014-10-26,Christopher Nolan
tt0903747,10,2023-11-20,Breaking Bad,Breaking Bad,https://www.imdb.com/title/tt0903747/,TV Series,9.5,49,2008,"Crime, Drama, Thriller",2100000,2008-01-20,
tt0959621,8,2023-11-21,"Pilot",Pilot,https://www.imdb.com/title/tt0959621/,TV Episode,9.0,58,2008,"Crime, Drama",40000,2008-01-20,Vince Gilligan
`

const ratingsExportOld = `Title,Const,Title Type,Your Rating,Date Rated,Year
"Inception",tt1375666,movie,8,2019-05-04,2010
"Chernobyl",tt7366338,tvMiniSeries,9,2019-06-10,2019
`

const watchlistExport = `Position,Const,Created,Modified,Description,Title,Original Title,URL,Title Type,IMDb Rating,Runtime (mins),Year,Genres,Num Votes,Release Date,Directors,Your Rating,Date Rated
1,tt1375666,2021-01-15,2021-01-15,,Inception,Inception,https://www.imdb.com/title/tt1375666/,Movie,8.8,148,2010,"Action, Adventure",2500000,2010-07-08,Christopher Nolan,7,2021-02-01
2,tt0944947,2021-02-10T08:12:00Z,2021-02-10,,Game of Thrones,Game of Thrones,https://www.imdb.com/title/tt0944947/,TV Series,9.2,57,2011,"Action, Adventure",2300000,2011-04-17,,,
`

func TestParseRatingsExport(t *testing.T) {
	t.Parallel()

	kind, rows, err := Parse(strings.NewReader(ratingsExport))
	if err != nil {
		t.Fatal(err)
	}
	if kind != ListRatings || len(rows) != 3 {
		t.Fatalf("kind %s rows %d", kind, len(rows))
	}
	first := rows[0]
	if first.IMDbID != "tt0816692" || first.Rating != 9 || first.Title != "Interstellar" || first.Year != 2014 || first.TitleType != "Movie" || first.Err != nil {
		t.Errorf("first row %+v", first)
	}
	if first.DateRated == nil || !first.DateRated.Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("date rated %v", first.DateRated)
	}
	if first.Line != 2 {
		t.Errorf("line %d, want 2", first.Line)
	}
	if rows[1].TitleType != "TV Series" || rows[2].TitleType != "TV Episode" {
		t.Errorf("types %q %q", rows[1].TitleType, rows[2].TitleType)
	}
}

func TestParseColumnsByName(t *testing.T) {
	t.Parallel()

	kind, rows, err := Parse(strings.NewReader(ratingsExportOld))
	if err != nil {
		t.Fatal(err)
	}
	if kind != ListRatings || len(rows) != 2 {
		t.Fatalf("kind %s rows %d", kind, len(rows))
	}
	if rows[0].IMDbID != "tt1375666" || rows[0].Rating != 8 || rows[0].TitleType != "movie" || rows[0].Title != "Inception" {
		t.Errorf("row %+v", rows[0])
	}
	if rows[1].TitleType != "tvMiniSeries" || rows[1].Rating != 9 {
		t.Errorf("row %+v", rows[1])
	}
}

func TestParseWatchlistIgnoresRating(t *testing.T) {
	t.Parallel()

	kind, rows, err := Parse(strings.NewReader(watchlistExport))
	if err != nil {
		t.Fatal(err)
	}
	if kind != ListWatchlist || len(rows) != 2 {
		t.Fatalf("kind %s rows %d", kind, len(rows))
	}
	if rows[0].Rating != 0 || rows[0].DateRated != nil {
		t.Errorf("a watchlist row must carry no rating: %+v", rows[0])
	}
	if rows[0].Created == nil || rows[0].Created.Format(dateLayout) != "2021-01-15" {
		t.Errorf("created %v", rows[0].Created)
	}

	if rows[1].Created == nil || rows[1].Created.Format(dateLayout) != "2021-02-10" {
		t.Errorf("created %v", rows[1].Created)
	}
}

func TestParseGarbageLines(t *testing.T) {
	t.Parallel()

	input := `Const,Your Rating,Date Rated,Title,Title Type,Year
tt0816692,9,2024-03-01,Interstellar,Movie,2014
not-an-id,7,2024-03-02,Broken,Movie,2000

tt1375666,11,2024-03-03,Out of range,Movie,2010
tt0000001,"8,2024-03-04,Unclosed quote,Movie,1999
tt0133093,8,,The Matrix,Movie
tt0110912,8.0,2020-01-01,Pulp Fiction,Movie,1994
`
	kind, rows, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if kind != ListRatings {
		t.Fatalf("kind %s", kind)
	}
	var valid, failed []Row
	for _, row := range rows {
		if row.Err != nil {
			failed = append(failed, row)
		} else {
			valid = append(valid, row)
		}
	}

	if len(failed) < 3 {
		t.Errorf("failed rows %d, want at least 3: %+v", len(failed), failed)
	}
	if !errors.Is(failed[0].Err, ErrBadIMDbID) || !errors.Is(failed[1].Err, ErrBadRating) {
		t.Errorf("errors %v, %v", failed[0].Err, failed[1].Err)
	}
	if len(valid) < 1 || valid[0].IMDbID != "tt0816692" {
		t.Errorf("valid rows %+v", valid)
	}
}

func TestParseMissingOptionalColumns(t *testing.T) {
	t.Parallel()

	input := "Const,Your Rating,Date Rated\ntt0133093,8,\ntt0110912,8.0,2020-01-01\n"
	_, rows, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	if rows[0].Err != nil || rows[0].DateRated != nil || rows[0].Title != "" || rows[0].Year != 0 {
		t.Errorf("row %+v", rows[0])
	}
	if rows[1].Rating != 8 {
		t.Errorf("8.0 must read as 8: %+v", rows[1])
	}
}

func TestParseRejectsOtherFiles(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]string{
		"empty":            "",
		"letterboxd":       "Date,Name,Year,Letterboxd URI,Rating\n2024-01-01,Heat,1995,https://boxd.it/x,4.5\n",
		"no const":         "Your Rating,Date Rated,Title\n9,2024-01-01,Heat\n",
		"const only":       "Const,Title\ntt0113277,Heat\n",
		"binary":           "\x00\x01\x02PNG",
		"kinopoisk-ish id": "id,rating,date\n12345,8,2024-01-01\n",
	} {
		if _, _, err := Parse(strings.NewReader(input)); !errors.Is(err, ErrNotIMDb) {
			t.Errorf("%s: err %v, want ErrNotIMDb", name, err)
		}
	}
}

func TestParseRowLimit(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("Const,Your Rating,Date Rated\n")
	for i := range MaxRows + 1 {
		fmt.Fprintf(&b, "tt%07d,7,2024-01-01\n", i+1)
	}
	if _, _, err := Parse(strings.NewReader(b.String())); !errors.Is(err, ErrTooManyRows) {
		t.Errorf("err %v, want ErrTooManyRows", err)
	}

	var exact strings.Builder
	exact.WriteString("Const,Your Rating,Date Rated\n")
	for i := range MaxRows {
		fmt.Fprintf(&exact, "tt%07d,7,2024-01-01\n", i+1)
	}
	if _, rows, err := Parse(strings.NewReader(exact.String())); err != nil || len(rows) != MaxRows {
		t.Errorf("exactly %d rows: %d, %v", MaxRows, len(rows), err)
	}
}
