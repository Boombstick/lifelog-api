package search

import "time"

type FilmDoc struct {
	ID      uint   `json:"id"`
	TMDBID  int    `json:"tmdb_id"`
	IMDBID  string `json:"imdb_id,omitempty"`
	TitleRu string `json:"title_ru,omitempty"`
	TitleEn string `json:"title_en,omitempty"`

	TitleSort   string     `json:"title_sort,omitempty"`
	Kind        string     `json:"kind"`
	Year        int        `json:"year,omitempty"`
	ReleaseDate *time.Time `json:"release_date,omitempty"`
	Synopsis    string     `json:"synopsis,omitempty"`
	PosterURL   string     `json:"poster_url,omitempty"`
	Genres      []string   `json:"genres,omitempty"`
	GenreNames  []string   `json:"genre_names,omitempty"`

	GenreTitles []string `json:"genre_titles,omitempty"`
	Directors   []string `json:"directors,omitempty"`
	Cast        []string `json:"cast,omitempty"`

	DirectorsOriginal []string `json:"directors_original,omitempty"`
	CastOriginal      []string `json:"cast_original,omitempty"`
	Popularity        float32  `json:"popularity"`

	Runtime int `json:"runtime,omitempty"`

	TMDBRating  float32 `json:"tmdb_rating,omitempty"`
	TMDBVotes   int     `json:"tmdb_votes"`
	LocalRating float32 `json:"local_rating,omitempty"`
	LocalVotes  int     `json:"local_votes"`
}

type PersonDoc struct {
	ID           uint     `json:"id"`
	TMDBID       int      `json:"tmdb_id"`
	Name         string   `json:"name"`
	OriginalName string   `json:"original_name,omitempty"`
	PhotoURL     string   `json:"photo_url,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	FilmCount    int      `json:"film_count"`
	Popularity   float32  `json:"popularity"`
	KnownFor     []string `json:"known_for,omitempty"`

	IndexedAt time.Time `json:"indexed_at"`
}
