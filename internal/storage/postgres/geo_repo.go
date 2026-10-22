package postgres

import (
	"context"

	"lifelog-api/internal/geo"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GeoRepository struct {
	db *gorm.DB
}

func NewGeoRepository(db *gorm.DB) *GeoRepository {
	return &GeoRepository{db: db}
}

func (r *GeoRepository) SaveCountries(ctx context.Context, countries []geo.Country) error {
	if len(countries) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns([]string{"name_en", "name_ru", "continent"}),
		}).
		CreateInBatches(countries, 200).Error
}

func (r *GeoRepository) SaveCities(ctx context.Context, cities []geo.City) error {
	if len(cities) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Omit(clause.Associations).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"country_code", "name_en", "name_ru", "latitude", "longitude", "population",
			}),
		}).
		CreateInBatches(cities, 500).Error
}

func (r *GeoRepository) Countries(ctx context.Context, query string) ([]geo.Country, error) {
	db := r.db.WithContext(ctx).Model(&geo.Country{})
	if query != "" {
		like := "%" + query + "%"
		db = db.Where("name_ru ILIKE ? OR name_en ILIKE ? OR code ILIKE ?", like, like, query+"%")
	}

	var countries []geo.Country
	if err := db.Order("name_ru ASC").Find(&countries).Error; err != nil {
		return nil, err
	}
	return countries, nil
}

func (r *GeoRepository) Cities(ctx context.Context, filter geo.CityFilter) ([]geo.City, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	db := r.db.WithContext(ctx).Model(&geo.City{})
	if filter.CountryCode != "" {
		db = db.Where("country_code = ?", filter.CountryCode)
	}
	if filter.Query != "" {
		like := filter.Query + "%"
		db = db.Where("name_ru ILIKE ? OR name_en ILIKE ?", like, like)
	}
	if filter.AfterID > 0 {
		var after geo.City
		if err := r.db.WithContext(ctx).First(&after, filter.AfterID).Error; err != nil {
			return nil, err
		}
		db = db.Where("(population, id) < (?, ?)", after.Population, after.ID)
	}

	var cities []geo.City
	if err := db.Order("population DESC, id DESC").Limit(limit).Find(&cities).Error; err != nil {
		return nil, err
	}
	return cities, nil
}

func (r *GeoRepository) CityByID(ctx context.Context, id uint) (*geo.City, error) {
	var city geo.City
	if err := r.db.WithContext(ctx).First(&city, id).Error; err != nil {
		return nil, err
	}
	return &city, nil
}

func (r *GeoRepository) NearestCity(ctx context.Context, lat, lon float64) (*geo.City, error) {
	const query = `
		SELECT * FROM (
			SELECT *,
			       6371 * acos(
			           LEAST(1, cos(radians(?)) * cos(radians(latitude)) *
			                    cos(radians(longitude) - radians(?)) +
			                    sin(radians(?)) * sin(radians(latitude)))
			       ) AS distance_km
			FROM cities
			WHERE latitude BETWEEN ? - 3 AND ? + 3
			  AND longitude BETWEEN ? - 3 AND ? + 3
		) nearby
		ORDER BY (distance_km <= 25) DESC,
		         CASE WHEN distance_km <= 25 THEN population END DESC,
		         distance_km ASC
		LIMIT 1`

	var city geo.City
	err := r.db.WithContext(ctx).
		Raw(query, lat, lon, lat, lat, lat, lon, lon).
		Scan(&city).Error
	if err != nil {
		return nil, err
	}
	if city.ID == 0 {
		return nil, nil
	}
	return &city, nil
}
