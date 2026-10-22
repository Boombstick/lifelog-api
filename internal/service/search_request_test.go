package service

import (
	"errors"
	"testing"
)

func TestSearchRequestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		req     SearchRequest
		wantErr bool
	}{
		{name: "пустой запрос", req: SearchRequest{}},
		{name: "режим и направление", req: SearchRequest{Sort: "title", Order: "asc"}},
		{name: "только направление", req: SearchRequest{Order: "desc"}},
		{
			name: "устаревший режим rating",
			req:  SearchRequest{Sort: "rating"},
		},
		{
			name:    "рейтинг IMDB",
			req:     SearchRequest{Sort: "imdb_rating"},
			wantErr: true,
		},
		{name: "выдуманный режим", req: SearchRequest{Sort: "как-нибудь"}, wantErr: true},
		{name: "выдуманное направление", req: SearchRequest{Sort: "year", Order: "вверх"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.req.Validate()
			if tc.wantErr {
				if !errors.Is(err, ErrUnknownSort) {
					t.Fatalf("ожидалась ErrUnknownSort, получено %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("неожиданная ошибка: %v", err)
			}
		})
	}
}

func TestSearchRequestHideWatched(t *testing.T) {
	t.Parallel()

	if err := (SearchRequest{HideWatched: true}).Validate(); !errors.Is(err, ErrAnonymousHide) {
		t.Errorf("аноним со скрытием: ожидалась ErrAnonymousHide, получено %v", err)
	}
	if err := (SearchRequest{HideWatched: true, UserID: 4}).Validate(); err != nil {
		t.Errorf("вошедший со скрытием: неожиданная ошибка %v", err)
	}
	if err := (SearchRequest{UserID: 0}).Validate(); err != nil {
		t.Errorf("аноним без скрытия: неожиданная ошибка %v", err)
	}
}
