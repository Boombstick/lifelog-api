package service

import (
	"context"
	"fmt"

	"lifelog-api/internal/geo"
	"lifelog-api/internal/memcache"
)

type GeoRepository interface {
	Countries(ctx context.Context, query string) ([]geo.Country, error)
	Cities(ctx context.Context, filter geo.CityFilter) ([]geo.City, error)
	CityByID(ctx context.Context, id uint) (*geo.City, error)
	NearestCity(ctx context.Context, lat, lon float64) (*geo.City, error)
}

type GeoService struct {
	repo GeoRepository

	countries *memcache.Loader[[]CountryResponse]
	cityPages *memcache.Loader[CityPage]
}

func NewGeoService(repo GeoRepository) *GeoService {
	return &GeoService{
		repo:      repo,
		countries: memcache.NewLoader[[]CountryResponse](referenceTTL, 1),
		cityPages: memcache.NewLoader[CityPage](referenceTTL, cityPagesCacheSize),
	}
}

type CountryResponse struct {
	Code   string `json:"code"`
	NameRu string `json:"name_ru"`
	NameEn string `json:"name_en"`
}

type CityResponse struct {
	ID          uint    `json:"id"`
	NameRu      string  `json:"name_ru,omitempty"`
	NameEn      string  `json:"name_en"`
	CountryCode string  `json:"country_code"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Population  int     `json:"population"`
}

type CityPage struct {
	Items      []CityResponse `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type CityListRequest struct {
	CountryCode string
	Query       string
	Cursor      string
	Limit       int
}

func (s *GeoService) Countries(ctx context.Context, query string) ([]CountryResponse, error) {
	if query == "" {
		return s.countries.Get(ctx, "", func(ctx context.Context) ([]CountryResponse, error) {
			return s.loadCountries(ctx, "")
		})
	}
	return s.loadCountries(ctx, query)
}

func (s *GeoService) loadCountries(ctx context.Context, query string) ([]CountryResponse, error) {
	countries, err := s.repo.Countries(ctx, query)
	if err != nil {
		return nil, err
	}

	response := make([]CountryResponse, 0, len(countries))
	for _, item := range countries {
		response = append(response, CountryResponse{
			Code:   item.Code,
			NameRu: item.NameRu,
			NameEn: item.NameEn,
		})
	}
	return response, nil
}

func (s *GeoService) Cities(ctx context.Context, req CityListRequest) (CityPage, error) {
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	filter := geo.CityFilter{
		CountryCode: req.CountryCode,
		Query:       req.Query,
		AfterID:     decodePlaceCursor(req.Cursor),
		Limit:       limit,
	}

	if filter.Query != "" || len(filter.CountryCode) > 2 {
		return s.loadCities(ctx, filter)
	}
	key := fmt.Sprintf("%s|%d|%d", filter.CountryCode, filter.AfterID, filter.Limit)
	return s.cityPages.Get(ctx, key, func(ctx context.Context) (CityPage, error) {
		return s.loadCities(ctx, filter)
	})
}

func (s *GeoService) loadCities(ctx context.Context, filter geo.CityFilter) (CityPage, error) {
	limit := filter.Limit
	cities, err := s.repo.Cities(ctx, filter)
	if err != nil {
		return CityPage{}, err
	}

	page := CityPage{Items: make([]CityResponse, 0, len(cities))}
	for _, item := range cities {
		page.Items = append(page.Items, cityResponse(item))
	}
	if len(cities) == limit {
		page.NextCursor = encodePlaceCursor(cities[len(cities)-1].ID)
	}
	return page, nil
}

func (s *GeoService) NearestCity(ctx context.Context, lat, lon float64) (*CityResponse, error) {
	city, err := s.repo.NearestCity(ctx, lat, lon)
	if err != nil || city == nil {
		return nil, err
	}
	response := cityResponse(*city)
	return &response, nil
}

func cityResponse(item geo.City) CityResponse {
	return CityResponse{
		ID:          item.ID,
		NameRu:      item.NameRu,
		NameEn:      item.NameEn,
		CountryCode: item.CountryCode,
		Lat:         item.Latitude,
		Lon:         item.Longitude,
		Population:  item.Population,
	}
}
