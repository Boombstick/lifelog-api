package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"lifelog-api/internal/middleware"
	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TripsIdentityService interface {
	middleware.TripsIdentityResolver
	Identify(actor trips.Actor) trips.BotIdentifyResponse
}

type TripsTripService interface {
	History(ctx context.Context, actor trips.Actor) (trips.TripHistoryResponse, error)
	Active(ctx context.Context, actor trips.Actor) (trips.ActiveTripResponse, error)
	Create(ctx context.Context, actor trips.Actor, cmd trips.CreateTripCommand) (trips.CreateTripResponse, error)
	End(ctx context.Context, actor trips.Actor, cmd trips.EndTripCommand) (trips.EndTripResponse, error)
	Arrive(ctx context.Context, actor trips.Actor, cmd trips.ArriveCommand) (trips.ArriveResponse, error)
	SetCurrency(ctx context.Context, actor trips.Actor, cmd trips.SetCurrencyCommand) (trips.SetCurrencyResponse, error)
	SetTimezone(ctx context.Context, actor trips.Actor, cmd trips.SetTimezoneCommand) (trips.SetTimezoneResponse, error)
	Delete(ctx context.Context, actor trips.Actor, tripID uuid.UUID) (trips.DeletedTrip, error)
}

type TripsExpenseService interface {
	Add(ctx context.Context, actor trips.Actor, cmd trips.AddExpenseCommand) (trips.AddExpenseResponse, error)
	List(ctx context.Context, actor trips.Actor, q trips.ListExpensesQuery) (trips.ExpenseListResponse, error)
	Total(ctx context.Context, actor trips.Actor, r trips.DateRange) (trips.TotalResponse, error)
	Duplicates(ctx context.Context, actor trips.Actor) ([]trips.DuplicatePair, error)
	NotDuplicate(ctx context.Context, actor trips.Actor, id int64) error
	Delete(ctx context.Context, actor trips.Actor, id int64) error
}

type TripsReportService interface {
	Report(ctx context.Context, actor trips.Actor, r trips.DateRange) (trips.ReportResponse, error)
	Builder(ctx context.Context, actor trips.Actor, q trips.ReportBuilderQuery) (trips.ReportBuilderResponse, error)
}

type TripsPhotoService interface {
	List(ctx context.Context, actor trips.Actor, tripID *uuid.UUID) (trips.PhotosResponse, error)
	Add(ctx context.Context, actor trips.Actor, cmd trips.AddPhotoCommand) (trips.AddPhotoResponse, error)
	Open(ctx context.Context, actor trips.Actor, photoID uuid.UUID) (trips.PhotoDownload, error)
	Delete(ctx context.Context, actor trips.Actor, photoID uuid.UUID) error
}

type TripsDictionaryService interface {
	Countries(ctx context.Context) (trips.CountriesResponse, error)
	Currencies(ctx context.Context) (trips.CurrenciesResponse, error)
	Categories(ctx context.Context, includeKeywords bool) ([]trips.CategoryDTO, error)
	CreateCategory(ctx context.Context, actor trips.Actor, req trips.CreateCategoryRequest) (int64, error)
	AddKeywords(ctx context.Context, actor trips.Actor, id int64, keywords []string) error
}

type TripsSyncService interface {
	Process(ctx context.Context, actor trips.Actor, req trips.SyncRequest) (trips.SyncResponse, error)
}

type TripsMergeService interface {
	Merge(ctx context.Context, req trips.MergeRequest) error
}

type TripsServices struct {
	Identity   TripsIdentityService
	Trips      TripsTripService
	Expenses   TripsExpenseService
	Reports    TripsReportService
	Photos     TripsPhotoService
	Dictionary TripsDictionaryService
	Sync       TripsSyncService
	Merge      TripsMergeService
}

type TripsHandler struct {
	logger        *slog.Logger
	services      TripsServices
	verifier      middleware.TokenVerifier
	internalToken string
}

func NewTripsHandler(logger *slog.Logger, services TripsServices, verifier middleware.TokenVerifier, internalToken string) *TripsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TripsHandler{logger: logger, services: services, verifier: verifier, internalToken: internalToken}
}

func (h *TripsHandler) Register(engine *gin.Engine) {
	engine.Use(tripsCORS())
	engine.NoRoute(emptyStatus(http.StatusNotFound))

	user := h.auth(middleware.TripsUser)
	writer := h.auth(middleware.TripsUserOrInternal)
	api := engine.Group("/api")

	api.GET("/dictionary/countries", user, h.countries)
	api.GET("/dictionary/currencies", user, h.currencies)

	api.GET("/trips", user, h.tripHistory)
	api.GET("/trips/active", user, h.activeTrip)
	api.POST("/trips", user, h.createTrip)
	api.POST("/trips/end", user, h.endTrip)
	api.POST("/trips/arrive", user, h.arrive)
	api.PUT("/trips/currency", user, h.setCurrency)
	api.PUT("/trips/timezone", user, h.setTimezone)

	api.DELETE("/trips/:tripId", guidParam("tripId"), user, h.deleteTrip)

	api.GET("/trips/photos", user, h.photos)
	api.POST("/trips/photos", user, h.addPhoto)
	api.GET("/trips/photos/:photoId", guidParam("photoId"), user, h.downloadPhoto)
	api.DELETE("/trips/photos/:photoId", guidParam("photoId"), user, h.deletePhoto)

	api.POST("/expenses", user, h.addExpense)
	api.GET("/expenses", user, h.expenses)
	api.GET("/expenses/total", user, h.total)
	api.GET("/expenses/report", user, h.report)
	api.GET("/expenses/report/builder", user, h.reportBuilder)
	api.GET("/expenses/duplicates", user, h.duplicates)
	api.POST("/expenses/:id/not-duplicate", longParam("id"), user, h.notDuplicate)
	api.DELETE("/expenses/:id", longParam("id"), user, h.deleteExpense)

	api.GET("/expenses/categories", user, h.categories)
	api.POST("/expenses/categories", writer, h.createCategory)
	api.PUT("/expenses/categories/:id", writer, h.addKeywords)

	api.POST("/sync", user, h.sync)

	api.POST("/internal/users/merge", h.auth(middleware.TripsInternal), h.merge)
	api.POST("/internal/bot/identify", h.auth(middleware.TripsBot), h.identify)
}

func (h *TripsHandler) auth(access middleware.TripsAccess) gin.HandlerFunc {
	return middleware.TripsAuthenticate(h.verifier, h.services.Identity, h.internalToken, access, h.logger)
}

func (h *TripsHandler) actor(c *gin.Context) (trips.Actor, bool) {
	actor, ok := middleware.TripsActor(c)
	if !ok {
		h.logger.ErrorContext(c.Request.Context(), "trips: route without an actor", "path", c.FullPath())
		c.AbortWithStatusJSON(http.StatusInternalServerError, trips.ErrorResponse{Error: trips.MsgInternal})
		return trips.Actor{}, false
	}
	return actor, true
}

func (h *TripsHandler) fail(c *gin.Context, err error) {
	status := trips.StatusOf(err)
	if status >= http.StatusInternalServerError {
		h.logger.ErrorContext(c.Request.Context(), "trips request failed", "path", c.FullPath(), "error", err)
	}
	_ = c.Error(err)
	c.AbortWithStatusJSON(status, trips.ErrorResponse{Error: trips.PublicMessage(err)})
}

func noContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

func emptyStatus(status int) gin.HandlerFunc {
	return func(c *gin.Context) { c.AbortWithStatus(status) }
}

func guidParam(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := uuid.Parse(strings.TrimSpace(c.Param(name))); err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

func longParam(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64); err != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		c.Next()
	}
}

func tripsCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", "*")
		method := c.GetHeader("Access-Control-Request-Method")
		if c.Request.Method != http.MethodOptions || method == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Methods", method)
		if headers := c.GetHeader("Access-Control-Request-Headers"); headers != "" {
			c.Header("Access-Control-Allow-Headers", headers)
		}
		c.AbortWithStatus(http.StatusNoContent)
	}
}
