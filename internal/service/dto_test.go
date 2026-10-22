package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"lifelog-api/internal/geo"
	"lifelog-api/internal/model"
	"lifelog-api/internal/place"
	"lifelog-api/internal/search"

	"gorm.io/gorm"
)

func TestFilmSearchItemTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		doc          search.FilmDoc
		wantTitle    string
		wantOriginal string
	}{
		{
			name:         "разные названия показываются оба",
			doc:          search.FilmDoc{TitleRu: "Дюна", TitleEn: "Dune"},
			wantTitle:    "Дюна",
			wantOriginal: "Dune",
		},
		{
			name:         "пустое русское название заменяется английским",
			doc:          search.FilmDoc{TitleRu: "", TitleEn: "Dune"},
			wantTitle:    "Dune",
			wantOriginal: "",
		},
		{
			name:         "одинаковые названия не дублируются",
			doc:          search.FilmDoc{TitleRu: "Tenet", TitleEn: "Tenet"},
			wantTitle:    "Tenet",
			wantOriginal: "",
		},
		{
			name:         "оба названия пустые",
			doc:          search.FilmDoc{},
			wantTitle:    "",
			wantOriginal: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := filmSearchItem(tc.doc)
			if got.Title != tc.wantTitle {
				t.Errorf("Title = %q, ожидалось %q", got.Title, tc.wantTitle)
			}
			if got.TitleOrig != tc.wantOriginal {
				t.Errorf("TitleOrig = %q, ожидалось %q", got.TitleOrig, tc.wantOriginal)
			}
		})
	}
}

func TestFilmSearchItemMapsGenreTitles(t *testing.T) {
	t.Parallel()

	doc := search.FilmDoc{
		ID:          7,
		TitleRu:     "Дюна",
		TitleEn:     "Dune",
		Genres:      []string{"science-fiction", "adventure"},
		GenreTitles: []string{"фантастика", "приключения"},
		GenreNames:  []string{"фантастика", "science fiction"},
		Runtime:     155,
		LocalRating: 8.5,
		LocalVotes:  3,
	}

	got := filmSearchItem(doc)

	if !reflect.DeepEqual(got.Genres, doc.GenreTitles) {
		t.Errorf("Genres = %v, ожидалось %v (русские названия, не смесь языков)", got.Genres, doc.GenreTitles)
	}
	if got.ID != doc.ID || got.LocalRating != doc.LocalRating {
		t.Errorf("скалярные поля потерялись: %+v", got)
	}
}

func TestParseKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    place.Kind
		wantErr error
	}{
		{name: "пустой тип означает прочее", value: "", want: place.KindOther},
		{name: "еда", value: "food", want: place.KindFood},
		{name: "культура", value: "culture", want: place.KindCulture},
		{name: "парк", value: "park", want: place.KindPark},
		{name: "жильё", value: "stay", want: place.KindStay},
		{name: "явное прочее", value: "other", want: place.KindOther},
		{name: "неизвестный тип", value: "shopping", wantErr: ErrUnknownKind},

		{name: "верхний регистр не принимается", value: "FOOD", wantErr: ErrUnknownKind},
		{name: "пробел вокруг значения не срезается", value: " food", wantErr: ErrUnknownKind},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseKind(tc.value)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("parseKind(%q) вернул ошибку %v, ожидалась %v", tc.value, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("parseKind(%q) = %q, ожидалось %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestPlaceResponse(t *testing.T) {
	t.Parallel()

	lat, lon := 55.75, 37.61

	tests := []struct {
		name  string
		in    place.Place
		check func(t *testing.T, got PlaceResponse)
	}{
		{
			name: "место без географии",
			in: place.Place{
				Model:            gorm.Model{ID: 1},
				Name:             "Кофейня",
				Address:          "Ленина, 1",
				Category:         "Кафе",
				Kind:             place.KindFood,
				ModerationStatus: model.ModerationStatusPending,
			},
			check: func(t *testing.T, got PlaceResponse) {
				if got.CountryName != "" || got.CityName != "" {
					t.Errorf("названия страны и города должны быть пустыми: %+v", got)
				}
				if got.Kind != "food" {
					t.Errorf("Kind = %q, ожидалось %q", got.Kind, "food")
				}
				if got.Id != 1 || got.Name != "Кофейня" {
					t.Errorf("основные поля не перенеслись: %+v", got)
				}
			},
		},
		{
			name: "место со страной и городом",
			in: place.Place{
				Model:     gorm.Model{ID: 2},
				Country:   &geo.Country{Code: "RU", NameRu: "Россия", NameEn: "Russia"},
				City:      &geo.City{ID: 524901, NameRu: "Москва", NameEn: "Moscow"},
				Latitude:  &lat,
				Longitude: &lon,
			},
			check: func(t *testing.T, got PlaceResponse) {
				if got.CountryName != "Россия" {
					t.Errorf("CountryName = %q, ожидалось %q", got.CountryName, "Россия")
				}
				if got.CityName != "Москва" {
					t.Errorf("CityName = %q, ожидалось %q", got.CityName, "Москва")
				}
				if got.Lat == nil || *got.Lat != lat || got.Lon == nil || *got.Lon != lon {
					t.Errorf("координаты потерялись: %+v", got)
				}
			},
		},
		{
			name: "город без русского имени показывается английским",
			in: place.Place{
				Model: gorm.Model{ID: 3},
				City:  &geo.City{ID: 1, NameEn: "Krabi"},
			},
			check: func(t *testing.T, got PlaceResponse) {
				if got.CityName != "Krabi" {
					t.Errorf("CityName = %q, ожидалось %q", got.CityName, "Krabi")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.check(t, placeResponse(tc.in))
		})
	}
}

type stubPlaceRepo struct {
	places     []place.Place
	lastFilter place.Filter

	card     *place.Place
	updated  *place.Place
	deleted  uint
	reviewed *place.PlaceReview
}

func (s *stubPlaceRepo) FindAll(_ context.Context, filter place.Filter) ([]place.Place, error) {
	s.lastFilter = filter
	return s.places, nil
}
func (s *stubPlaceRepo) FindById(context.Context, uint, place.Viewer) (*place.Place, error) {
	if s.card == nil {
		return nil, model.NewEntityNotFound("place")
	}
	copied := *s.card
	return &copied, nil
}
func (s *stubPlaceRepo) Create(context.Context, *place.Place) error { return nil }
func (s *stubPlaceRepo) Update(_ context.Context, p *place.Place) error {
	s.updated = p
	return nil
}
func (s *stubPlaceRepo) Delete(_ context.Context, id uint) error {
	s.deleted = id
	return nil
}
func (s *stubPlaceRepo) AddReview(_ context.Context, r *place.PlaceReview) error {
	s.reviewed = r
	return nil
}

func placesWithIDs(ids ...uint) []place.Place {
	out := make([]place.Place, 0, len(ids))
	for _, id := range ids {
		out = append(out, place.Place{Model: gorm.Model{ID: id}})
	}
	return out
}

func TestPlaceServiceFindAllLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{name: "ноль означает значение по умолчанию", limit: 0, wantLimit: 20},
		{name: "отрицательный лимит приводится к умолчанию", limit: -3, wantLimit: 20},
		{name: "слишком большой обрезается", limit: 51, wantLimit: 20},
		{name: "предельно допустимый проходит", limit: 50, wantLimit: 50},
		{name: "обычный проходит как есть", limit: 10, wantLimit: 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &stubPlaceRepo{}
			svc := NewPlaceService(repo)

			if _, err := svc.FindAll(context.Background(), PlaceListRequest{Limit: tc.limit}); err != nil {
				t.Fatalf("FindAll: %v", err)
			}
			if repo.lastFilter.Limit != tc.wantLimit {
				t.Errorf("в хранилище ушёл limit %d, ожидался %d", repo.lastFilter.Limit, tc.wantLimit)
			}
		})
	}
}

func TestPlaceServiceFindAllCursor(t *testing.T) {
	t.Parallel()

	t.Run("полная страница отдаёт курсор последнего элемента", func(t *testing.T) {
		t.Parallel()

		repo := &stubPlaceRepo{places: placesWithIDs(30, 29, 28)}
		page, err := NewPlaceService(repo).FindAll(context.Background(), PlaceListRequest{Limit: 3})
		if err != nil {
			t.Fatalf("FindAll: %v", err)
		}
		if page.NextCursor == "" {
			t.Fatal("страница заполнена целиком, курсор обязан быть")
		}
		if got := decodePlaceCursor(page.NextCursor); got != 28 {
			t.Errorf("курсор указывает на %d, ожидалось 28", got)
		}
	})

	t.Run("неполная страница курсора не даёт", func(t *testing.T) {
		t.Parallel()

		repo := &stubPlaceRepo{places: placesWithIDs(30, 29)}
		page, err := NewPlaceService(repo).FindAll(context.Background(), PlaceListRequest{Limit: 3})
		if err != nil {
			t.Fatalf("FindAll: %v", err)
		}

		if page.NextCursor != "" {
			t.Errorf("NextCursor = %q, ожидалась пустая строка", page.NextCursor)
		}
	})

	t.Run("курсор из запроса доезжает до хранилища", func(t *testing.T) {
		t.Parallel()

		repo := &stubPlaceRepo{}
		svc := NewPlaceService(repo)

		if _, err := svc.FindAll(context.Background(), PlaceListRequest{
			Cursor: encodePlaceCursor(17),
		}); err != nil {
			t.Fatalf("FindAll: %v", err)
		}
		if repo.lastFilter.AfterID != 17 {
			t.Errorf("AfterID = %d, ожидалось 17", repo.lastFilter.AfterID)
		}
	})

	t.Run("испорченный курсор даёт первую страницу", func(t *testing.T) {
		t.Parallel()

		repo := &stubPlaceRepo{}
		if _, err := NewPlaceService(repo).FindAll(context.Background(), PlaceListRequest{
			Cursor: "явно-не-курсор!!",
		}); err != nil {
			t.Fatalf("FindAll: %v", err)
		}
		if repo.lastFilter.AfterID != 0 {
			t.Errorf("AfterID = %d, ожидался 0", repo.lastFilter.AfterID)
		}
	})

	t.Run("пустой список даёт пустой массив, а не nil", func(t *testing.T) {
		t.Parallel()

		page, err := NewPlaceService(&stubPlaceRepo{}).FindAll(context.Background(), PlaceListRequest{})
		if err != nil {
			t.Fatalf("FindAll: %v", err)
		}

		if page.Items == nil {
			t.Error("Items = nil, ожидался пустой срез")
		}
	})
}
