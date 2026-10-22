package service

import (
	"context"
	"testing"
	"time"

	"lifelog-api/internal/auth"
	"lifelog-api/internal/film"

	"github.com/google/uuid"
)

type stubReviewGate struct {
	open map[uint]bool

	watchedClosed map[uint]bool
	subscribed    map[[2]uuid.UUID]bool

	asked *[]uint
}

func (g stubReviewGate) Visibilities(_ context.Context, userIDs []uint) (map[uint]auth.ListVisibility, error) {
	out := map[uint]auth.ListVisibility{}
	for _, userID := range userIDs {
		if g.asked != nil {
			*g.asked = append(*g.asked, userID)
		}
		v := auth.DefaultVisibility(userID)
		if !g.open[userID] {
			v.Rated = auth.VisibilityPrivate
		}
		if g.watchedClosed[userID] {
			v.Watched = auth.VisibilityPrivate
		}
		out[userID] = v
	}
	return out, nil
}

func (g stubReviewGate) Following(_ context.Context, viewer uuid.UUID) map[uuid.UUID]struct{} {
	set := map[uuid.UUID]struct{}{}
	for pair, ok := range g.subscribed {
		if ok && pair[0] == viewer {
			set[pair[1]] = struct{}{}
		}
	}
	return set
}

func ptrInt16(v int16) *int16    { return &v }
func ptrString(v string) *string { return &v }

func TestFilmCardMapping(t *testing.T) {
	t.Parallel()

	released := time.Date(2021, 9, 15, 0, 0, 0, 0, time.UTC)
	stored := film.Film{
		ID:              7,
		TitleRu:         "",
		TitleEn:         "Dune",
		Kind:            film.KindMovie,
		ReleaseDate:     released,
		Runtime:         155,
		TMDBID:          438631,
		TMDBVoteAverage: 7.8,
		Genres: []film.Genre{
			{ID: "science-fiction", NameRu: "фантастика"},
			{ID: "adventure", NameRu: "", NameEn: "Adventure"},
		},

		Credits: []film.FilmCredit{
			{PersonID: 2, Role: film.CreditRoleActor, BillingOrder: 0, Person: film.Person{Name: "Тимоти Шаламе"}},
			{PersonID: 3, Role: film.CreditRoleActor, BillingOrder: 1, Person: film.Person{Name: "Ребекка Фергюсон"}},
			{PersonID: 1, Role: film.CreditRoleDirector, Person: film.Person{Name: "Дени Вильнёв", PhotoURL: "p.jpg"}},
		},
	}

	card := filmCard(stored)

	if card.Title != "Dune" || card.TitleOrig != "Dune" {
		t.Errorf("без перевода оба названия оригинальные, пришло %q / %q", card.Title, card.TitleOrig)
	}
	if card.Year != 2021 || card.ReleaseDate != "2021-09-15" {
		t.Errorf("дата выхода: year=%d release_date=%q", card.Year, card.ReleaseDate)
	}
	if len(card.Genres) != 2 || card.Genres[1] != "Adventure" || card.GenreSlugs[1] != "adventure" {
		t.Errorf("жанры: %v / %v", card.Genres, card.GenreSlugs)
	}
	if len(card.Credits) != 3 || card.Credits[0].Role != "director" || card.Credits[0].PersonID != 1 {
		t.Fatalf("режиссёр обязан идти первым, пришло %+v", card.Credits)
	}
	if card.Credits[1].PersonID != 2 || card.Credits[2].PersonID != 3 {
		t.Errorf("актёры не в порядке billing_order: %+v", card.Credits[1:])
	}
	if card.Credits[0].PhotoURL != "p.jpg" {
		t.Errorf("портрет потерян: %+v", card.Credits[0])
	}
	if card.MyEntry != nil || card.Reviews == nil || len(card.Reviews) != 0 {
		t.Errorf("без спрашивающего: my_entry=%v reviews=%v", card.MyEntry, card.Reviews)
	}
}

func TestFilmCardEmptyDate(t *testing.T) {
	t.Parallel()

	card := filmCard(film.Film{TitleRu: "Без даты"})
	if card.Year != 0 || card.ReleaseDate != "" {
		t.Errorf("нулевое время не должно превращаться в первый год: %d %q", card.Year, card.ReleaseDate)
	}
	if card.Genres == nil || card.Credits == nil {
		t.Error("пустые списки обязаны сериализоваться как [], а не null")
	}
}

func TestFilmCardReviewsVisibility(t *testing.T) {
	t.Parallel()

	viewerAuth, openAuth, closedAuth, strangerAuth := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	viewer := auth.Principal{UserID: 1, AuthUserID: viewerAuth}
	watched := time.Date(2026, 3, 10, 18, 0, 0, 0, time.UTC)

	stored := &film.Film{
		ID: 7,
		Entries: []film.FilmEntry{
			{UserID: 1, Rate: ptrInt16(9), Watchlisted: false, WatchedAt: &watched},

			{UserID: 2, Rate: ptrInt16(8), Description: ptrString("Долго, но красиво"), WatchedAt: &watched,
				User: auth.User{Username: "masha", AuthUserID: &openAuth}},

			{UserID: 3, Rate: ptrInt16(5), User: auth.User{Username: "stranger", AuthUserID: &strangerAuth}},

			{UserID: 4, Rate: ptrInt16(10), User: auth.User{Username: "closed", AuthUserID: &closedAuth}},

			{UserID: 5, Watchlisted: true, User: auth.User{Username: "planner", AuthUserID: &openAuth}},

			{UserID: 6, Rate: ptrInt16(7), User: auth.User{Username: "legacy"}},
		},
	}
	var asked []uint
	gate := stubReviewGate{
		open:  map[uint]bool{2: true, 3: true, 5: true},
		asked: &asked,
		subscribed: map[[2]uuid.UUID]bool{
			{viewerAuth, openAuth}:   true,
			{viewerAuth, closedAuth}: true,
		},
	}
	svc := NewFilmService(&stubFilmRepo{card: stored}, nil, gate, nil)

	card, err := svc.Card(context.Background(), 7, &viewer)
	if err != nil {
		t.Fatal(err)
	}
	if card.MyEntry == nil || card.MyEntry.Rate == nil || *card.MyEntry.Rate != 9 {
		t.Fatalf("своя отметка не попала в my_entry: %+v", card.MyEntry)
	}
	if len(card.Reviews) != 1 {
		t.Fatalf("ожидался ровно один видимый отзыв, пришло %+v", card.Reviews)
	}
	review := card.Reviews[0]
	if review.UserName != "masha" || *review.Rate != 8 || *review.Description != "Долго, но красиво" {
		t.Errorf("не тот отзыв: %+v", review)
	}

	if len(asked) != 2 || asked[0] != 2 || asked[1] != 4 {
		t.Errorf("visibility read for %v, want [2 4]", asked)
	}

	anonymous, err := svc.Card(context.Background(), 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if anonymous.MyEntry != nil || len(anonymous.Reviews) != 0 {
		t.Errorf("без токена личных полей быть не должно: %+v %+v", anonymous.MyEntry, anonymous.Reviews)
	}
}

func TestFilmCardWithoutGate(t *testing.T) {
	t.Parallel()

	other := uuid.New()
	stored := &film.Film{Entries: []film.FilmEntry{
		{UserID: 2, Rate: ptrInt16(8), User: auth.User{Username: "masha", AuthUserID: &other}},
	}}
	svc := NewFilmService(&stubFilmRepo{card: stored}, nil, nil, nil)

	card, err := svc.Card(context.Background(), 1, &auth.Principal{UserID: 1, AuthUserID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Reviews) != 0 {
		t.Errorf("без правил видимости отзывы обязаны быть закрыты: %+v", card.Reviews)
	}
}

func TestFilmCardReviewHidesWatchedDateWhenWatchedIsPrivate(t *testing.T) {
	t.Parallel()

	viewerAuth, openAuth, privateAuth := uuid.New(), uuid.New(), uuid.New()
	watched := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	stored := &film.Film{ID: 7, Entries: []film.FilmEntry{
		{UserID: 2, Rate: ptrInt16(8), WatchedAt: &watched, UpdatedAt: watched.Add(time.Hour),
			User: auth.User{Username: "open", AuthUserID: &openAuth}},
		{UserID: 3, Rate: ptrInt16(6), WatchedAt: &watched, UpdatedAt: watched,
			User: auth.User{Username: "private", AuthUserID: &privateAuth}},
	}}
	gate := stubReviewGate{
		open:          map[uint]bool{2: true, 3: true},
		watchedClosed: map[uint]bool{3: true},
		subscribed: map[[2]uuid.UUID]bool{
			{viewerAuth, openAuth}:    true,
			{viewerAuth, privateAuth}: true,
		},
	}
	svc := NewFilmService(&stubFilmRepo{card: stored}, nil, gate, nil)

	card, err := svc.Card(context.Background(), 7, &auth.Principal{UserID: 1, AuthUserID: viewerAuth})
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Reviews) != 2 {
		t.Fatalf("both reviews are visible by the rated setting, got %+v", card.Reviews)
	}
	if card.Reviews[0].UserName != "open" || card.Reviews[0].WatchedAt == nil {
		t.Errorf("an open watched set keeps the date: %+v", card.Reviews[0])
	}
	if card.Reviews[1].UserName != "private" || card.Reviews[1].WatchedAt != nil || card.Reviews[1].Rate == nil {
		t.Errorf("a private watched set hides only the date: %+v", card.Reviews[1])
	}
}
