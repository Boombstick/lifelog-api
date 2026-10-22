package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"lifelog-api/internal/httpclient"
	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	InternalTokenHeader = httpclient.InternalTokenHeader
	TelegramIDHeader    = "X-Telegram-Id"
	AuthUserIDHeader    = "X-Auth-User-Id"
	TelegramNameHeader  = "X-Telegram-Name"
)

const tripsActorKey = "trips_actor"

type TripsIdentityResolver interface {
	ByAuthUser(ctx context.Context, authUserID uuid.UUID, displayName string) (trips.Actor, error)
	ByTelegram(ctx context.Context, telegramID int64, authUserID uuid.UUID, displayName string) (trips.Actor, error)
}

type TripsAccess int

const (
	TripsUser TripsAccess = iota + 1

	TripsUserOrInternal

	TripsBot

	TripsInternal
)

func TripsAuthenticate(verifier TokenVerifier, resolver TripsIdentityResolver, internalToken string, access TripsAccess, logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		if _, present := c.Request.Header[InternalTokenHeader]; present {
			if !InternalTokenMatches(c.GetHeader(InternalTokenHeader), internalToken) {
				emptyUnauthorized(c, "")
				return
			}
			if access == TripsInternal {
				setTripsActor(c, trips.Actor{Mode: trips.ModeInternal})
				return
			}
			if access == TripsUserOrInternal && !hasTelegramHeaders(c) {
				setTripsActor(c, trips.Actor{Mode: trips.ModeInternal})
				return
			}
			botMode(c, resolver, logger)
			return
		}

		if access == TripsInternal || access == TripsBot {
			emptyUnauthorized(c, "")
			return
		}
		bearerMode(c, verifier, resolver, logger)
	}
}

func bearerMode(c *gin.Context, verifier TokenVerifier, resolver TripsIdentityResolver, logger *slog.Logger) {
	raw, ok := BearerToken(c.GetHeader("Authorization"))
	if !ok || raw == "" {
		emptyUnauthorized(c, "Bearer")
		return
	}
	if verifier == nil {
		emptyUnauthorized(c, `Bearer error="invalid_token"`)
		return
	}
	claims, err := verifier.Parse(c.Request.Context(), raw)
	if err != nil {
		emptyUnauthorized(c, `Bearer error="invalid_token"`)
		return
	}
	authUserID, err := uuid.Parse(claims.Subject)
	if err != nil {
		emptyUnauthorized(c, "")
		return
	}

	name := trips.DisplayNameForAuth(claims.Name, "", authUserID)
	actor, err := resolver.ByAuthUser(c.Request.Context(), authUserID, name)
	if err != nil {
		identityFailed(c, logger, err)
		return
	}
	setTripsActor(c, actor)
}

func botMode(c *gin.Context, resolver TripsIdentityResolver, logger *slog.Logger) {
	telegramID, err := strconv.ParseInt(strings.TrimSpace(c.GetHeader(TelegramIDHeader)), 10, 64)
	if err != nil {
		emptyUnauthorized(c, "")
		return
	}
	authUserID, err := uuid.Parse(strings.TrimSpace(c.GetHeader(AuthUserIDHeader)))
	if err != nil {
		emptyUnauthorized(c, "")
		return
	}
	actor, err := resolver.ByTelegram(c.Request.Context(), telegramID, authUserID, telegramName(c.GetHeader(TelegramNameHeader), telegramID))
	if err != nil {
		identityFailed(c, logger, err)
		return
	}
	setTripsActor(c, actor)
}

func telegramName(header string, telegramID int64) string {
	name := header
	if decoded, err := url.PathUnescape(header); err == nil {
		name = decoded
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "tg-" + strconv.FormatInt(telegramID, 10)
	}
	return name
}

func hasTelegramHeaders(c *gin.Context) bool {
	_, id := c.Request.Header[TelegramIDHeader]
	_, auth := c.Request.Header[AuthUserIDHeader]
	return id || auth
}

func emptyUnauthorized(c *gin.Context, challenge string) {
	if challenge != "" {
		c.Header("WWW-Authenticate", challenge)
	}
	c.AbortWithStatus(http.StatusUnauthorized)
}

func identityFailed(c *gin.Context, logger *slog.Logger, err error) {
	logger.ErrorContext(c.Request.Context(), "trips: cannot resolve the caller", "error", err)
	c.AbortWithStatusJSON(trips.StatusOf(err), trips.ErrorResponse{Error: trips.PublicMessage(err)})
}

func setTripsActor(c *gin.Context, actor trips.Actor) {
	c.Set(tripsActorKey, actor)
	c.Next()
}

func TripsActor(c *gin.Context) (trips.Actor, bool) {
	raw, ok := c.Get(tripsActorKey)
	if !ok {
		return trips.Actor{}, false
	}
	actor, ok := raw.(trips.Actor)
	return actor, ok
}
