package logging

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"lifelog-api/internal/auth"

	"github.com/gin-gonic/gin"
)

func Setup(service string) *slog.Logger {
	return SetupLevel(service, slog.LevelInfo)
}

func SetupLevel(service string, level slog.Leveler) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler).With("service", service)
	slog.SetDefault(logger)

	log.SetFlags(0)
	log.SetOutput(Writer(logger.With("source", "stdlog"), slog.LevelWarn))

	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	gin.DefaultWriter = Writer(logger.With("source", "gin"), slog.LevelInfo)
	gin.DefaultErrorWriter = Writer(logger.With("source", "gin"), slog.LevelError)
	return logger
}

func Writer(logger *slog.Logger, level slog.Level) io.Writer {
	return slog.NewLogLogger(logger.Handler(), level).Writer()
}

func Recovery(logger *slog.Logger, respond gin.RecoveryFunc) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		logger.ErrorContext(c, "panic recovered",
			"method", c.Request.Method,
			"path", loggedPath(c),
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()))
		if respond != nil {
			respond(c, recovered)
			return
		}
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}

func Requests(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		fields := []any{
			"method", c.Request.Method,
			"route", route,
			"path", loggedPath(c),
			"status", c.Writer.Status(),
			"duration_ms", float64(time.Since(started).Microseconds()) / 1000,
			"ip", c.ClientIP(),
		}

		if principal, ok := auth.PrincipalFromContext(c); ok {
			fields = append(fields, "user_id", principal.UserID)
		}
		if len(c.Errors) > 0 {
			fields = append(fields, "errors", c.Errors.String())
		}

		switch {
		case c.Writer.Status() >= 500:
			logger.Error("запрос обработан", fields...)
		case c.Writer.Status() >= 400:
			logger.Warn("запрос обработан", fields...)
		default:
			logger.Info("запрос обработан", fields...)
		}
	}
}

var secretParams = map[string]bool{"token": true}

func loggedPath(c *gin.Context) string {
	path := c.Request.URL.Path
	for _, param := range c.Params {
		if !secretParams[param.Key] || param.Value == "" {
			continue
		}
		path = strings.ReplaceAll(path, param.Value, redactSecret(param.Value))
	}
	return path
}

func redactSecret(value string) string {
	const keep = 4
	if len([]rune(value)) <= keep {
		return "…"
	}
	return string([]rune(value)[:keep]) + "…"
}
