package service

import "testing"

func TestTopBlock(t *testing.T) {
	t.Parallel()

	films := []FilmSearchItem{
		titled("Гравитация", "Gravity"),
		titled("Мстители: Финал", "Avengers: Endgame"),
	}
	people := []PersonSearchItem{
		{Name: "Кристофер Нолан", OriginalName: "Christopher Nolan"},
		{Name: "Сандра Буллок"},
	}

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "точное название", text: "Гравитация", want: TopFilms},
		{name: "точное название другим регистром", text: "гРАВИТАЦИЯ", want: TopFilms},
		{name: "точное оригинальное название", text: "gravity", want: TopFilms},

		{name: "название без двоеточия", text: "мстители финал", want: TopFilms},
		{name: "точное имя", text: "Кристофер Нолан", want: TopPeople},
		{name: "точное оригинальное имя", text: "christopher nolan", want: TopPeople},

		{name: "частичный ввод", text: "нолан", want: TopPeople},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := topBlock(tc.text, films, people); got != tc.want {
				t.Errorf("top = %q, ожидалось %q", got, tc.want)
			}
		})
	}
}

func TestTopBlockНазваниеПеревешиваетИмя(t *testing.T) {
	t.Parallel()

	films := []FilmSearchItem{titled("Малкольм Икс", "")}
	people := []PersonSearchItem{{Name: "Малкольм Икс"}}

	if got := topBlock("Малкольм Икс", films, people); got != TopFilms {
		t.Errorf("top = %q, ожидалось %q", got, TopFilms)
	}
}

func TestTopBlockБезЛюдей(t *testing.T) {
	t.Parallel()

	if got := topBlock("что-нибудь", []FilmSearchItem{titled("Другое", "")}, nil); got != TopFilms {
		t.Errorf("top = %q, ожидалось %q", got, TopFilms)
	}
	if got := topBlock("", nil, []PersonSearchItem{{Name: "Кто-то"}}); got != TopFilms {
		t.Errorf("пустой запрос дал top = %q, ожидалось %q", got, TopFilms)
	}
}

func TestTopBlockОдинЧеловек(t *testing.T) {
	t.Parallel()

	if got := topBlock("нолан", []FilmSearchItem{titled("Довод", "Tenet")}, []PersonSearchItem{{Name: "Кристофер Нолан"}}); got != TopPeople {
		t.Errorf("top = %q, ожидалось %q", got, TopPeople)
	}
}

func titled(title, original string) FilmSearchItem {
	item := FilmSearchItem{TitleOrig: original}
	item.Title = title
	return item
}
