package service

import (
	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

type FilmCatalogItem struct {
	ID          uint    `json:"id"`
	Title       string  `json:"title"`
	Year        int     `json:"year,omitempty"`
	Kind        string  `json:"kind"`
	PosterURL   string  `json:"poster_url,omitempty"`
	TMDBRating  float32 `json:"tmdb_rating"`
	LocalRating float32 `json:"local_rating"`
	LocalVotes  int     `json:"local_votes"`
}

type FilmRow struct {
	FilmCatalogItem
	Runtime int      `json:"runtime,omitempty"`
	Genres  []string `json:"genres,omitempty"`
}

type FilmSearchItem struct {
	FilmRow

	TitleOrig string   `json:"-"`
	Directors []string `json:"directors,omitempty"`
}

func filmTitles(ru, en string) (title, original string) {
	if ru == "" {
		return en, ""
	}
	if en == ru {
		return ru, ""
	}
	return ru, en
}

func filmRow(card film.Film) FilmRow {
	title, _ := filmTitles(card.TitleRu, card.TitleEn)
	row := FilmRow{
		FilmCatalogItem: FilmCatalogItem{
			ID:          card.ID,
			Title:       title,
			Kind:        string(card.Kind),
			PosterURL:   card.PosterURL,
			TMDBRating:  card.TMDBVoteAverage,
			LocalRating: card.LocalRating,
			LocalVotes:  card.LocalRatingCount,
		},
		Runtime: card.Runtime,
	}

	if !card.ReleaseDate.IsZero() && card.ReleaseDate.Year() > 1 {
		row.Year = card.ReleaseDate.Year()
	}
	for _, genre := range card.Genres {
		name := genre.NameRu
		if name == "" {
			name = genre.NameEn
		}
		row.Genres = append(row.Genres, name)
	}
	return row
}

func filmCatalogItem(doc search.FilmDoc) FilmCatalogItem {
	title, _ := filmTitles(doc.TitleRu, doc.TitleEn)
	return FilmCatalogItem{
		ID:          doc.ID,
		Title:       title,
		Year:        doc.Year,
		Kind:        doc.Kind,
		PosterURL:   doc.PosterURL,
		TMDBRating:  doc.TMDBRating,
		LocalRating: doc.LocalRating,
		LocalVotes:  doc.LocalVotes,
	}
}

func filmSearchItem(doc search.FilmDoc) FilmSearchItem {
	_, original := filmTitles(doc.TitleRu, doc.TitleEn)
	return FilmSearchItem{
		FilmRow: FilmRow{
			FilmCatalogItem: filmCatalogItem(doc),
			Runtime:         doc.Runtime,
			Genres:          doc.GenreTitles,
		},
		TitleOrig: original,
		Directors: doc.Directors,
	}
}

func catalogItems(films []film.Film) []FilmSearchItem {
	items := make([]FilmSearchItem, 0, len(films))
	for _, card := range films {
		items = append(items, FilmSearchItem{FilmRow: filmRow(card)})
	}
	return items
}
