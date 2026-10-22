package indexer

import (
	"slices"
	"testing"

	"lifelog-api/internal/film"
)

func TestFilmToDocKeepsOriginalNames(t *testing.T) {
	t.Parallel()
	item := film.Film{
		ID: 1,
		Credits: []film.FilmCredit{
			{Role: film.CreditRoleDirector, Person: film.Person{Name: "Дени Вильнев", OriginalName: "Denis Villeneuve"}},

			{Role: film.CreditRoleActor, Person: film.Person{Name: "Rebecca Ferguson", OriginalName: "Rebecca Ferguson"}},
			{Role: film.CreditRoleActor, Person: film.Person{Name: "Тимоти Шаламе", OriginalName: " Timothée Chalamet "}},
			{Role: film.CreditRoleActor, Person: film.Person{Name: "Зендея"}},
		},
	}

	doc := filmToDoc(item)

	if !slices.Equal(doc.Directors, []string{"Дени Вильнев"}) || !slices.Equal(doc.DirectorsOriginal, []string{"Denis Villeneuve"}) {
		t.Errorf("directors %v / %v", doc.Directors, doc.DirectorsOriginal)
	}
	if !slices.Equal(doc.Cast, []string{"Rebecca Ferguson", "Тимоти Шаламе", "Зендея"}) {
		t.Errorf("displayed cast must stay as is: %v", doc.Cast)
	}
	if !slices.Equal(doc.CastOriginal, []string{"Timothée Chalamet"}) {
		t.Errorf("original cast %v", doc.CastOriginal)
	}
}

func TestFilmToDocDropsRatingsWithoutVotes(t *testing.T) {
	t.Parallel()

	doc := filmToDoc(film.Film{TMDBVoteAverage: 6, TMDBVoteCount: 0, LocalRating: 7, LocalRatingCount: 0})
	if doc.TMDBRating != 0 || doc.LocalRating != 0 {
		t.Errorf("ratings without votes kept: tmdb %v, local %v", doc.TMDBRating, doc.LocalRating)
	}
	doc = filmToDoc(film.Film{TMDBVoteAverage: 6, TMDBVoteCount: 10, LocalRating: 7, LocalRatingCount: 1})
	if doc.TMDBRating != 6 || doc.LocalRating != 7 {
		t.Errorf("ratings with votes lost: tmdb %v, local %v", doc.TMDBRating, doc.LocalRating)
	}
}
