package router

import (
	"log/slog"

	"lifelog-api/internal/handler"
	"lifelog-api/internal/httpapi"

	"github.com/gin-gonic/gin"
	ginFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Router struct {
	placeHandler    *handler.PlaceHandler
	filmHandler     *handler.FilmHandler
	searchHandler   *handler.SearchHandler
	geoHandler      *handler.GeoHandler
	internalHandler *handler.InternalHandler
	adminHandler    *handler.AdminHandler
	sharingHandler  *handler.SharingHandler
	personHandler   *handler.PersonHandler
	sharedLists     *handler.SharedListHandler
	imports         *handler.ImportHandler
	tmdbAdd         *handler.TMDBAddHandler
}

func NewRouter(placeHandler *handler.PlaceHandler,
	filmHandler *handler.FilmHandler,
	searchHandler *handler.SearchHandler,
	geoHandler *handler.GeoHandler,
	internalHandler *handler.InternalHandler,
	adminHandler *handler.AdminHandler,
	sharingHandler *handler.SharingHandler,
	personHandler *handler.PersonHandler,
	sharedLists *handler.SharedListHandler,
	imports *handler.ImportHandler,
	tmdbAdd *handler.TMDBAddHandler) *Router {
	return &Router{placeHandler: placeHandler,
		filmHandler:     filmHandler,
		searchHandler:   searchHandler,
		geoHandler:      geoHandler,
		internalHandler: internalHandler,
		adminHandler:    adminHandler,
		sharingHandler:  sharingHandler,
		personHandler:   personHandler,
		sharedLists:     sharedLists,
		imports:         imports,
		tmdbAdd:         tmdbAdd,
	}
}

func (router *Router) SetupRoutes(health, authMiddleware, optionalAuth, internalGuard, adminGuard gin.HandlerFunc) *gin.Engine {
	r := httpapi.NewEngine(httpapi.EngineOptions{
		Service:             "films",
		Logger:              slog.Default(),
		Health:              health,
		ContextWithFallback: true,
	})

	r.GET("/s/:token", router.sharingHandler.SharedPage)

	commonGroup := r.Group("/api/v1")
	router.placeHandler.Register(commonGroup, authMiddleware, optionalAuth)
	router.filmHandler.Register(commonGroup, authMiddleware, optionalAuth)

	router.searchHandler.Register(commonGroup, optionalAuth)

	router.personHandler.Register(commonGroup)

	router.geoHandler.Register(commonGroup)

	router.sharingHandler.Register(commonGroup, authMiddleware)
	router.sharedLists.Register(commonGroup, authMiddleware)
	router.imports.Register(commonGroup, authMiddleware)
	router.tmdbAdd.Register(commonGroup, authMiddleware)
	router.internalHandler.Register(commonGroup, internalGuard)
	router.adminHandler.Register(commonGroup, authMiddleware, adminGuard)

	commonGroup.GET("/swagger/*any", ginSwagger.WrapHandler(ginFiles.Handler))

	return r
}
