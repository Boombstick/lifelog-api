package handler

import (
	"log/slog"
	"net/http"

	"lifelog-api/internal/service"

	"github.com/gin-gonic/gin"
)

type GeoHandler struct {
	logger  *slog.Logger
	service *service.GeoService
}

func NewGeoHandler(logger *slog.Logger, geoService *service.GeoService) *GeoHandler {
	return &GeoHandler{logger: logger, service: geoService}
}

func (gh *GeoHandler) Register(rg *gin.RouterGroup) {
	geoGroup := rg.Group("/geo")
	geoGroup.GET("/countries", gh.countries)
	geoGroup.GET("/cities", gh.cities)

	geoGroup.GET("/cities/nearest", gh.nearestCity)
}

func (gh *GeoHandler) countries(ctx *gin.Context) {
	countries, err := gh.service.Countries(ctx, ctx.Query("q"))
	if err != nil {
		gh.logger.Error("справочник стран не выдан", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Header("Cache-Control", cachePublicReference)
	ctx.JSON(http.StatusOK, countries)
}

func (gh *GeoHandler) cities(ctx *gin.Context) {
	limit, ok := queryInt(ctx, "limit")
	if !ok {
		return
	}

	page, err := gh.service.Cities(ctx, service.CityListRequest{
		CountryCode: ctx.Query("country"),
		Query:       ctx.Query("q"),
		Cursor:      ctx.Query("cursor"),
		Limit:       limit,
	})
	if err != nil {
		gh.logger.Error("справочник городов не выдан", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	ctx.Header("Cache-Control", cachePublicReference)
	ctx.JSON(http.StatusOK, page)
}

func (gh *GeoHandler) nearestCity(ctx *gin.Context) {
	lat, ok := gh.requiredFloat(ctx, "lat", -90, 90)
	if !ok {
		return
	}
	lon, ok := gh.requiredFloat(ctx, "lon", -180, 180)
	if !ok {
		return
	}

	city, err := gh.service.NearestCity(ctx, lat, lon)
	if err != nil {
		gh.logger.Error("город по координатам не найден", "error", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": internalErrorText})
		return
	}
	if city == nil {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "поблизости нет городов из справочника"})
		return
	}
	ctx.JSON(http.StatusOK, city)
}

func (gh *GeoHandler) requiredFloat(ctx *gin.Context, name string, min, max float64) (float64, bool) {
	if ctx.Query(name) == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "параметр " + name + " обязателен"})
		return 0, false
	}
	value, ok := queryFloat(ctx, name)
	if !ok {
		return 0, false
	}
	if value < min || value > max {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": name + " вне допустимого диапазона"})
		return 0, false
	}
	return value, true
}
