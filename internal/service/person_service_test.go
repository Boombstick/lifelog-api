package service

import (
	"testing"
	"time"

	"lifelog-api/internal/film"
)

func credit(filmID uint, role film.CreditRole, title string, year int) film.FilmCredit {
	card := film.Film{ID: filmID, TitleRu: title, Kind: film.KindMovie}
	if year > 0 {
		card.ReleaseDate = time.Date(year, time.March, 1, 0, 0, 0, 0, time.UTC)
	}
	return film.FilmCredit{FilmID: filmID, Role: role, Film: card}
}

func TestФильмографияСхлопываетРолиОдногоФильма(t *testing.T) {
	t.Parallel()

	items, roles := collectFilmography([]film.FilmCredit{
		credit(1, film.CreditRoleDirector, "Гравитация", 2013),
		credit(1, film.CreditRoleActor, "Гравитация", 2013),
		credit(2, film.CreditRoleActor, "Рома", 2018),
	})

	if len(items) != 2 {
		t.Fatalf("фильмов %d, ожидалось 2: %+v", len(items), items)
	}
	if len(items[0].Roles) != 2 {
		t.Errorf("у первого фильма роли %v, ожидались обе", items[0].Roles)
	}
	if len(items[1].Roles) != 1 || items[1].Roles[0] != "actor" {
		t.Errorf("у второго фильма роли %v, ожидалась одна актёрская", items[1].Roles)
	}
	if len(roles) != 2 {
		t.Errorf("роли человека %v, ожидались обе", roles)
	}
}

func TestФильмографияСохраняетПорядок(t *testing.T) {
	t.Parallel()

	items, _ := collectFilmography([]film.FilmCredit{
		credit(7, film.CreditRoleActor, "Третий", 2024),
		credit(3, film.CreditRoleActor, "Второй", 2020),
		credit(3, film.CreditRoleDirector, "Второй", 2020),
		credit(5, film.CreditRoleActor, "Первый", 0),
	})

	titles := []string{items[0].Title, items[1].Title, items[2].Title}
	want := []string{"Третий", "Второй", "Первый"}
	for i := range want {
		if titles[i] != want[i] {
			t.Errorf("на месте %d стоит %q, ожидался %q", i, titles[i], want[i])
		}
	}

	if items[2].Year != 0 {
		t.Errorf("у фильма без даты год %d, ожидался пустой", items[2].Year)
	}
}

func TestФильмографияНеДублируетРоль(t *testing.T) {
	t.Parallel()

	items, roles := collectFilmography([]film.FilmCredit{
		credit(1, film.CreditRoleActor, "Престиж", 2006),
		credit(1, film.CreditRoleActor, "Престиж", 2006),
	})
	if len(items) != 1 || len(items[0].Roles) != 1 || len(roles) != 1 {
		t.Errorf("фильмы %+v, роли %v; ожидались один фильм и одна роль", items, roles)
	}
}

func TestФильмографияПустая(t *testing.T) {
	t.Parallel()

	items, roles := collectFilmography(nil)
	if len(items) != 0 || len(roles) != 0 {
		t.Errorf("пустые титры дали %+v и %v", items, roles)
	}
}
