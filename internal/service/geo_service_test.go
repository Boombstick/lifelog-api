package service

import (
	"context"
	"testing"

	"lifelog-api/internal/geo"
)

type geoRepoFake struct {
	countryCalls, cityCalls int
}

func (r *geoRepoFake) Countries(context.Context, string) ([]geo.Country, error) {
	r.countryCalls++
	return []geo.Country{{Code: "TH"}}, nil
}

func (r *geoRepoFake) Cities(context.Context, geo.CityFilter) ([]geo.City, error) {
	r.cityCalls++
	return []geo.City{{ID: 1}}, nil
}

func (r *geoRepoFake) CityByID(context.Context, uint) (*geo.City, error) { return nil, nil }

func (r *geoRepoFake) NearestCity(context.Context, float64, float64) (*geo.City, error) {
	return nil, nil
}

func TestGeoReferenceCaching(t *testing.T) {
	ctx := context.Background()
	repo := &geoRepoFake{}
	s := NewGeoService(repo)

	for range 3 {
		if _, err := s.Countries(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	if repo.countryCalls != 1 {
		t.Fatalf("full country list read %d times, want 1", repo.countryCalls)
	}
	_, _ = s.Countries(ctx, "та")
	_, _ = s.Countries(ctx, "та")
	if repo.countryCalls != 3 {
		t.Fatalf("filtered lists must not be cached: %d reads, want 3", repo.countryCalls)
	}

	for range 2 {
		_, _ = s.Cities(ctx, CityListRequest{CountryCode: "TH"})
	}
	_, _ = s.Cities(ctx, CityListRequest{CountryCode: "RU"})
	if repo.cityCalls != 2 {
		t.Fatalf("city pages read %d times, want 2 (one per country)", repo.cityCalls)
	}
	for range 2 {
		_, _ = s.Cities(ctx, CityListRequest{CountryCode: "TH", Query: "Пху"})
	}
	if repo.cityCalls != 4 {
		t.Fatalf("name queries must not be cached: %d reads, want 4", repo.cityCalls)
	}
}
