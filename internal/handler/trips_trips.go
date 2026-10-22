package handler

import (
	"net/http"
	"strings"

	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *TripsHandler) countries(c *gin.Context) {
	result, err := h.services.Dictionary.Countries(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", cachePrivateReference)
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) currencies(c *gin.Context) {
	result, err := h.services.Dictionary.Currencies(c.Request.Context())
	if err != nil {
		h.fail(c, err)
		return
	}
	c.Header("Cache-Control", cachePrivateReference)
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) tripHistory(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	result, err := h.services.Trips.History(c.Request.Context(), actor)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) activeTrip(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	result, err := h.services.Trips.Active(c.Request.Context(), actor)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) createTrip(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.CreateTripRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	errs.required("name", req.Name)
	startDate, ok := req.ParseStartDate()
	if !ok {
		errs.add("start_date", trips.MsgStartDateFormat)
	}
	if errs.writeIfAny(c) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Trips.Create(ctx, actor, trips.CreateTripCommand{
		Name:         req.Name,
		CountryName:  req.CountryName,
		City:         req.City,
		Budget:       req.Budget,
		CurrencyCode: req.CurrencyCode,
		StartDate:    startDate,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) endTrip(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Trips.End(ctx, actor, trips.EndTripCommand{})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) arrive(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.ArriveRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	errs.required("country_name", req.CountryName)
	if errs.writeIfAny(c) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Trips.Arrive(ctx, actor, trips.ArriveCommand{
		CountryName: req.CountryName,
		City:        req.City,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) setCurrency(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.SetCurrencyRequest
	if !bindJSON(c, &req) {
		return
	}
	errs := problems{}
	errs.required("currency", req.Currency)
	if errs.writeIfAny(c) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Trips.SetCurrency(ctx, actor, trips.SetCurrencyCommand{
		Currency: req.Currency,
		TripID:   req.TripID,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TripsHandler) deleteTrip(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	tripID, _ := uuid.Parse(strings.TrimSpace(c.Param("tripId")))
	ctx, cancel := writeContext(c)
	defer cancel()
	if _, err := h.services.Trips.Delete(ctx, actor, tripID); err != nil {
		h.fail(c, err)
		return
	}
	noContent(c)
}

func (h *TripsHandler) setTimezone(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var req trips.SetTimezoneRequest
	if !bindJSON(c, &req) {
		return
	}
	ctx, cancel := writeContext(c)
	defer cancel()
	result, err := h.services.Trips.SetTimezone(ctx, actor, trips.SetTimezoneCommand{
		Latitude:  float64(req.Latitude),
		Longitude: float64(req.Longtitude),
		TripID:    req.TripID,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
