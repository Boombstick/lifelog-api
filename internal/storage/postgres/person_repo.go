package postgres

import (
	"context"
	"errors"

	"lifelog-api/internal/film"
	"lifelog-api/internal/model"

	"gorm.io/gorm"
)

type PersonRepository struct {
	db *gorm.DB
}

func ProvidePersonRepository(gdb *gorm.DB) *PersonRepository {
	return &PersonRepository{db: gdb}
}

func (r *PersonRepository) FindByID(ctx context.Context, id uint) (*film.Person, error) {
	person := film.Person{}
	err := r.db.WithContext(ctx).First(&person, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("person")
		}
		return nil, err
	}
	return &person, nil
}

func (r *PersonRepository) Filmography(ctx context.Context, personID uint) ([]film.FilmCredit, error) {
	var credits []film.FilmCredit
	err := r.db.WithContext(ctx).
		Model(&film.FilmCredit{}).
		Preload("Film.Genres").
		Joins("JOIN films ON films.id = film_credits.film_id").
		Where("film_credits.person_id = ?", personID).
		Order("films.release_date DESC NULLS LAST").
		Order("films.id DESC").
		Find(&credits).Error
	if err != nil {
		return nil, err
	}
	return credits, nil
}
