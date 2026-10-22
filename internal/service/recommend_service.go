package service

import (
	"context"
	"errors"
	"strings"

	"lifelog-api/internal/film"
	"lifelog-api/internal/search"
)

const (
	SourceTMDB = "tmdb"
	SourceOwn  = "own"
)

var ErrUnknownSource = errors.New("неизвестный источник похожих")

func ValidSource(source string) bool {
	return source == "" || source == SourceTMDB || source == SourceOwn
}

type SimilarEngine interface {
	SimilarTo(ctx context.Context, filmID uint, limit int) (search.Page[search.FilmDoc], error)
	RecommendFor(ctx context.Context, profile search.TasteProfile, limit int) (search.Page[search.FilmDoc], error)
}

type RecommendRepository interface {
	RecommendedFilms(ctx context.Context, filmID uint, limit int) ([]film.Film, error)
	RatedFilms(ctx context.Context, userID uint, minRate int16, limit int, kind film.Kind) ([]film.Film, error)
	SeenFilmIDs(ctx context.Context, userID uint) ([]uint, error)
}

type RecommendService struct {
	repo   RecommendRepository
	engine SimilarEngine
}

func NewRecommendService(repo RecommendRepository, engine SimilarEngine) *RecommendService {
	return &RecommendService{repo: repo, engine: engine}
}

type SimilarFilms struct {
	Source string           `json:"source"`
	Items  []FilmSearchItem `json:"items"`
}

type RecommendedFilms struct {
	Items []FilmSearchItem `json:"items"`
}

func (s *RecommendService) Similar(ctx context.Context, filmID uint, source string, limit int) (SimilarFilms, error) {
	if !ValidSource(source) {
		return SimilarFilms{}, ErrUnknownSource
	}
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	if source == "" {
		source = SourceTMDB
	}

	if source == SourceTMDB {
		films, err := s.repo.RecommendedFilms(ctx, filmID, limit)
		if err != nil {
			return SimilarFilms{}, err
		}
		if len(films) > 0 {
			return SimilarFilms{Source: SourceTMDB, Items: catalogItems(films)}, nil
		}
	}

	page, err := s.engine.SimilarTo(ctx, filmID, limit)
	if err != nil {
		return SimilarFilms{}, err
	}
	items := make([]FilmSearchItem, 0, len(page.Items))
	for _, doc := range page.Items {
		items = append(items, filmSearchItem(doc))
	}
	return SimilarFilms{Source: SourceOwn, Items: items}, nil
}

const minRateForTaste = 7

const tasteSampleSize = 50

func (s *RecommendService) ForUser(ctx context.Context, userID uint, kind film.Kind, limit int) (RecommendedFilms, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if kind == "" {
		kind = film.KindMovie
	}

	rated, err := s.repo.RatedFilms(ctx, userID, minRateForTaste, tasteSampleSize, kind)
	if err != nil {
		return RecommendedFilms{}, err
	}

	if len(rated) == 0 {
		return RecommendedFilms{Items: []FilmSearchItem{}}, nil
	}

	watched, err := s.repo.SeenFilmIDs(ctx, userID)
	if err != nil {
		return RecommendedFilms{}, err
	}

	profile := buildTasteProfile(rated, watched)
	profile.Kind = string(kind)

	page, err := s.engine.RecommendFor(ctx, profile, limit)
	if err != nil {
		return RecommendedFilms{}, err
	}
	items := make([]FilmSearchItem, 0, len(page.Items))
	for _, doc := range page.Items {
		items = append(items, filmSearchItem(doc))
	}
	return RecommendedFilms{Items: items}, nil
}

func buildTasteProfile(rated []film.Film, watched []uint) search.TasteProfile {
	profile := search.TasteProfile{
		Genres:     map[string]int{},
		Directors:  map[string]int{},
		Cast:       map[string]int{},
		ExcludeIDs: watched,
	}

	for _, card := range rated {
		for _, genre := range card.Genres {
			profile.Genres[genre.ID]++
		}
		for _, credit := range card.Credits {
			name := strings.ToLower(strings.TrimSpace(credit.Person.Name))
			if name == "" {
				continue
			}
			switch credit.Role {
			case film.CreditRoleDirector, film.CreditRoleCreator:
				profile.Directors[name]++
			case film.CreditRoleActor:
				profile.Cast[name]++
			}
		}
	}
	return profile
}
