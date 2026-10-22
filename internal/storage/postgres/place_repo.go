package postgres

import (
	"context"
	"errors"
	"lifelog-api/internal/model"
	"lifelog-api/internal/place"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type PlaceRepository struct {
	db *gorm.DB
}

func ProvidePlaceRepository(gdb *gorm.DB) *PlaceRepository {
	return &PlaceRepository{db: gdb}
}

func visiblePlaces(viewer place.Viewer) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		switch {
		case viewer.Admin:
			return db
		case viewer.UserID != 0:
			return db.Where("(places.moderation_status = ? OR places.created_by = ?)", model.ModerationStatusApproved, viewer.UserID)
		default:
			return db.Where("places.moderation_status = ?", model.ModerationStatusApproved)
		}
	}
}

func (r *PlaceRepository) FindAll(ctx context.Context, filter place.Filter) ([]place.Place, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	db := r.db.WithContext(ctx).Model(&place.Place{}).
		Scopes(visiblePlaces(filter.Viewer)).
		Preload("City").
		Preload("Country")

	if filter.CountryCode != "" {
		db = db.Where("country_code = ?", filter.CountryCode)
	}
	if filter.CityID > 0 {
		db = db.Where("city_id = ?", filter.CityID)
	}
	if filter.Kind != "" {
		db = db.Where("kind = ?", filter.Kind)
	}
	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		db = db.Where("name ILIKE ? OR address ILIKE ? OR category ILIKE ?", like, like, like)
	}
	if filter.AfterID > 0 {
		db = db.Where("id < ?", filter.AfterID)
	}

	var result []place.Place
	if err := db.Order("id DESC").Limit(limit).Find(&result).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *PlaceRepository) FindById(ctx context.Context, id uint, viewer place.Viewer) (*place.Place, error) {
	result := &place.Place{}
	err := r.db.WithContext(ctx).Model(&place.Place{}).
		Preload("City").
		Preload("Country").
		Scopes(visiblePlaces(viewer)).
		Where("places.id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.NewEntityNotFound("place")
		}
		return nil, err
	}
	return result, nil
}

func (r *PlaceRepository) Create(ctx context.Context, created *place.Place) error {
	return placeWriteError(r.db.WithContext(ctx).Omit("City", "Country").Create(created).Error)
}

func (r *PlaceRepository) Update(ctx context.Context, updated *place.Place) error {
	return placeWriteError(r.db.WithContext(ctx).Omit("City", "Country", "Reviews").Save(updated).Error)
}

func (r *PlaceRepository) Delete(ctx context.Context, id uint) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&place.Place{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return model.NewEntityNotFound("place")
	}
	return nil
}

func (r *PlaceRepository) AddReview(ctx context.Context, review *place.PlaceReview) error {
	err := r.db.WithContext(ctx).Create(review).Error
	if isForeignKeyViolation(err) {
		return model.NewEntityNotFound("place")
	}
	return err
}

func placeWriteError(err error) error {
	if isForeignKeyViolation(err) {
		return place.ErrUnknownGeography
	}
	return err
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
