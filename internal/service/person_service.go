package service

import (
	"context"

	"lifelog-api/internal/film"
)

type PersonRepository interface {
	FindByID(ctx context.Context, id uint) (*film.Person, error)
	Filmography(ctx context.Context, personID uint) ([]film.FilmCredit, error)
}

type PersonService struct {
	repo PersonRepository
}

func NewPersonService(repo PersonRepository) *PersonService {
	return &PersonService{repo: repo}
}

type FilmographyItem struct {
	FilmRow
	Roles []string `json:"roles"`
}

type PersonCard struct {
	ID           uint              `json:"id"`
	Name         string            `json:"name"`
	PhotoURL     string            `json:"photo_url,omitempty"`
	OriginalName string            `json:"original_name,omitempty"`
	Roles        []string          `json:"roles"`
	FilmCount    int               `json:"film_count"`
	Films        []FilmographyItem `json:"films"`
}

func (s *PersonService) Card(ctx context.Context, id uint) (*PersonCard, error) {
	person, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	credits, err := s.repo.Filmography(ctx, id)
	if err != nil {
		return nil, err
	}

	card := PersonCard{
		ID:           person.ID,
		Name:         person.Name,
		PhotoURL:     person.PhotoURL,
		OriginalName: person.OriginalName,
		Roles:        []string{},
		Films:        []FilmographyItem{},
	}
	if card.OriginalName == card.Name {
		card.OriginalName = ""
	}
	card.Films, card.Roles = collectFilmography(credits)
	card.FilmCount = len(card.Films)
	return &card, nil
}

func collectFilmography(credits []film.FilmCredit) ([]FilmographyItem, []string) {
	items := make([]FilmographyItem, 0, len(credits))
	positions := make(map[uint]int, len(credits))
	personRoles := make([]string, 0, 2)

	for _, credit := range credits {
		role := string(credit.Role)
		if !containsString(personRoles, role) {
			personRoles = append(personRoles, role)
		}

		if at, seen := positions[credit.FilmID]; seen {
			if !containsString(items[at].Roles, role) {
				items[at].Roles = append(items[at].Roles, role)
			}
			continue
		}
		positions[credit.FilmID] = len(items)
		items = append(items, filmographyItem(credit))
	}
	return items, personRoles
}

func filmographyItem(credit film.FilmCredit) FilmographyItem {
	return FilmographyItem{FilmRow: filmRow(credit.Film), Roles: []string{string(credit.Role)}}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
