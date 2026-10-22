package service

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"lifelog-api/internal/model"
	"lifelog-api/internal/place"
)

type CreatePlaceRequest struct {
	Name     string `json:"name" binding:"required,max=128" example:"Alex"`
	Address  string `json:"address"  binding:"required,max=128" example:"Ленина"`
	Category string `json:"category"  binding:"required,max=32" example:"Кафе"`

	Kind string `json:"kind" binding:"omitempty" example:"food"`

	CountryCode *string  `json:"country_code" binding:"omitempty,len=2" example:"TH"`
	CityID      *uint    `json:"city_id" binding:"omitempty"`
	Lat         *float64 `json:"lat" binding:"omitempty"`
	Lon         *float64 `json:"lon" binding:"omitempty"`
}
type UpdatePlaceRequest struct {
	CreatePlaceRequest
}
type AddReviewRequest struct {
	Description string `json:"description"`
	Rate        int16  `json:"rate" binding:"required,gte=1,lte=5" `
}
type PlaceResponse struct {
	Id               uint                   `json:"id"`
	Name             string                 `json:"name"`
	Address          string                 `json:"address"`
	Category         string                 `json:"category"`
	Kind             string                 `json:"kind"`
	ModerationStatus model.ModerationStatus `json:"moderation_status"`
	CountryCode      *string                `json:"country_code,omitempty"`
	CountryName      string                 `json:"country_name,omitempty"`
	CityID           *uint                  `json:"city_id,omitempty"`
	CityName         string                 `json:"city_name,omitempty"`
	Lat              *float64               `json:"lat,omitempty"`
	Lon              *float64               `json:"lon,omitempty"`
}

type PlacePage struct {
	Items      []PlaceResponse `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type PlaceListRequest struct {
	CountryCode string
	CityID      uint
	Kind        string
	Query       string
	Cursor      string
	Limit       int
	Viewer      place.Viewer
}
type CreatePlaceResponse struct {
	Id uint `json:"id"`
}

type PlaceRepository interface {
	FindAll(ctx context.Context, filter place.Filter) ([]place.Place, error)
	FindById(ctx context.Context, id uint, viewer place.Viewer) (*place.Place, error)
	Create(ctx context.Context, place *place.Place) error
	Update(ctx context.Context, place *place.Place) error
	Delete(ctx context.Context, id uint) error
	AddReview(ctx context.Context, review *place.PlaceReview) error
}
type PlaceService struct {
	repo PlaceRepository
}

func NewPlaceService(repo PlaceRepository) *PlaceService {
	return &PlaceService{repo}
}

var ErrUnknownKind = errors.New("неизвестный тип места")

var ErrPlaceNotYours = errors.New("изменять место может только его автор")

var ErrReviewTooLong = fmt.Errorf("отзыв длиннее %d символов", MaxPlaceReviewLength)

const MaxPlaceReviewLength = 255

func (s *PlaceService) Create(ctx context.Context, authorID uint, req *CreatePlaceRequest) (*place.Place, error) {
	kind, err := parseKind(req.Kind)
	if err != nil {
		return nil, err
	}

	created := place.Place{
		Address:     req.Address,
		Category:    req.Category,
		Name:        req.Name,
		Kind:        kind,
		CountryCode: req.CountryCode,
		CityID:      req.CityID,
		Latitude:    req.Lat,
		Longitude:   req.Lon,
		CreatedBy:   &authorID,
	}
	if err := s.repo.Create(ctx, &created); err != nil {
		return nil, err
	}
	return &created, nil
}
func (s *PlaceService) Update(ctx context.Context, id uint, viewer place.Viewer, req *UpdatePlaceRequest) error {
	kind, err := parseKind(req.Kind)
	if err != nil {
		return err
	}

	stored, err := s.editable(ctx, id, viewer)
	if err != nil {
		return err
	}
	stored.Name = req.Name
	stored.Category = req.Category
	stored.Address = req.Address
	stored.Kind = kind
	stored.CountryCode = req.CountryCode
	stored.CityID = req.CityID

	stored.Latitude = req.Lat
	stored.Longitude = req.Lon
	stored.ModerationStatus = model.ModerationStatusPending

	return s.repo.Update(ctx, stored)
}

func (s *PlaceService) editable(ctx context.Context, id uint, viewer place.Viewer) (*place.Place, error) {
	stored, err := s.repo.FindById(ctx, id, viewer)
	if err != nil {
		return nil, err
	}
	if !viewer.CanEdit(*stored) {
		return nil, ErrPlaceNotYours
	}
	return stored, nil
}

func (s *PlaceService) FindById(ctx context.Context, id uint, viewer place.Viewer) (*PlaceResponse, error) {
	stored, err := s.repo.FindById(ctx, id, viewer)
	if err != nil {
		return nil, err
	}
	response := placeResponse(*stored)
	return &response, nil
}

func (s *PlaceService) FindAll(ctx context.Context, req PlaceListRequest) (PlacePage, error) {
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	places, err := s.repo.FindAll(ctx, place.Filter{
		CountryCode: req.CountryCode,
		CityID:      req.CityID,
		Kind:        req.Kind,
		Query:       req.Query,
		AfterID:     decodePlaceCursor(req.Cursor),
		Limit:       limit,
		Viewer:      req.Viewer,
	})
	if err != nil {
		return PlacePage{}, err
	}

	page := PlacePage{Items: make([]PlaceResponse, 0, len(places))}
	for _, item := range places {
		page.Items = append(page.Items, placeResponse(item))
	}

	if len(places) == limit {
		page.NextCursor = encodePlaceCursor(places[len(places)-1].ID)
	}
	return page, nil
}

func placeResponse(item place.Place) PlaceResponse {
	response := PlaceResponse{
		Id:               item.ID,
		Lat:              item.Latitude,
		Lon:              item.Longitude,
		Category:         item.Category,
		Kind:             string(item.Kind),
		Name:             item.Name,
		Address:          item.Address,
		ModerationStatus: item.ModerationStatus,
		CountryCode:      item.CountryCode,
		CityID:           item.CityID,
	}
	if item.Country != nil {
		response.CountryName = item.Country.NameRu
	}
	if item.City != nil {
		response.CityName = item.City.Name()
	}
	return response
}

func parseKind(value string) (place.Kind, error) {
	if value == "" {
		return place.KindOther, nil
	}
	kind := place.Kind(value)
	if !kind.Valid() {
		return "", ErrUnknownKind
	}
	return kind, nil
}

func (s *PlaceService) Delete(ctx context.Context, id uint, viewer place.Viewer) error {
	if _, err := s.editable(ctx, id, viewer); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func (s *PlaceService) AddReview(ctx context.Context, id uint, viewer place.Viewer, req *AddReviewRequest) (*place.PlaceReview, error) {
	if utf8.RuneCountInString(req.Description) > MaxPlaceReviewLength {
		return nil, ErrReviewTooLong
	}
	if _, err := s.repo.FindById(ctx, id, viewer); err != nil {
		return nil, err
	}

	userID := viewer.UserID
	review := place.PlaceReview{PlaceID: id, UserID: &userID, Description: req.Description, Rate: req.Rate}
	if err := s.repo.AddReview(ctx, &review); err != nil {
		return nil, err
	}
	return &review, nil
}
