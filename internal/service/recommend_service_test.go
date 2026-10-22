package service

import (
	"testing"

	"lifelog-api/internal/film"
)

func ratedFilm(genres []string, director, actor string) film.Film {
	card := film.Film{}
	for _, slug := range genres {
		card.Genres = append(card.Genres, film.Genre{ID: slug})
	}
	if director != "" {
		card.Credits = append(card.Credits, film.FilmCredit{
			Role:   film.CreditRoleDirector,
			Person: film.Person{Name: director},
		})
	}
	if actor != "" {
		card.Credits = append(card.Credits, film.FilmCredit{
			Role:   film.CreditRoleActor,
			Person: film.Person{Name: actor},
		})
	}
	return card
}

func TestBuildTasteProfile(t *testing.T) {
	t.Parallel()

	profile := buildTasteProfile([]film.Film{
		ratedFilm([]string{"drama", "thriller"}, "Кристофер Нолан", "Киллиан Мёрфи"),
		ratedFilm([]string{"drama"}, "Кристофер Нолан", "Мэттью Макконахи"),
	}, []uint{7, 9})

	if profile.Directors["кристофер нолан"] != 2 {
		t.Errorf("вес режиссёра %d, ожидалось 2: %v", profile.Directors["кристофер нолан"], profile.Directors)
	}
	if profile.Genres["drama"] != 2 || profile.Genres["thriller"] != 1 {
		t.Errorf("веса жанров неверны: %v", profile.Genres)
	}
	if len(profile.Cast) != 2 {
		t.Errorf("актёров в профиле %d, ожидалось 2: %v", len(profile.Cast), profile.Cast)
	}

	if _, upper := profile.Directors["Кристофер Нолан"]; upper {
		t.Error("имя режиссёра не приведено к нижнему регистру")
	}
	if len(profile.ExcludeIDs) != 2 {
		t.Errorf("просмотренное не попало в исключения: %v", profile.ExcludeIDs)
	}
}

func TestBuildTasteProfileCountsCreators(t *testing.T) {
	t.Parallel()

	series := film.Film{Kind: film.KindSeries, Credits: []film.FilmCredit{
		{Role: film.CreditRoleCreator, Person: film.Person{Name: "Винс Гиллиган"}},
	}}
	profile := buildTasteProfile([]film.Film{series, series}, nil)
	if profile.Directors["винс гиллиган"] != 2 {
		t.Errorf("creator weight %d, want 2: %v", profile.Directors["винс гиллиган"], profile.Directors)
	}
}

func TestBuildTasteProfileПустой(t *testing.T) {
	t.Parallel()

	if !buildTasteProfile(nil, nil).Empty() {
		t.Error("профиль без оценок должен быть пустым")
	}

	if !buildTasteProfile([]film.Film{{}}, nil).Empty() {
		t.Error("фильм без признаков не должен давать профиль")
	}
}

func TestValidSource(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"", SourceTMDB, SourceOwn} {
		if !ValidSource(source) {
			t.Errorf("источник %q должен быть допустим", source)
		}
	}
	for _, source := range []string{"imdb", "свой"} {
		if ValidSource(source) {
			t.Errorf("источник %q не должен быть допустим", source)
		}
	}
}
