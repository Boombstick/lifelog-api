package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/metrics"
	"lifelog-api/internal/middleware"
	"lifelog-api/internal/token"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

type EngineOptions struct {
	Service  string
	Logger   *slog.Logger
	Health   gin.HandlerFunc
	Recovery gin.RecoveryFunc

	ContextWithFallback bool

	MaxBodyBytes int64
}

const DefaultMaxBodyBytes = 1 << 20

const BodyTooLargeText = "Слишком большой запрос"

func NewEngine(opts EngineOptions) *gin.Engine {
	engine := gin.New()
	engine.ContextWithFallback = opts.ContextWithFallback
	engine.Use(logging.Recovery(opts.Logger, opts.Recovery))
	engine.Use(metrics.Middleware(opts.Service))
	engine.Use(logging.Requests(opts.Logger))
	engine.Use(limitBody(opts.MaxBodyBytes))
	engine.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	engine.GET("/health", opts.Health)
	engine.GET("/metrics", metrics.Handler())
	return engine
}

func limitBody(limit int64) gin.HandlerFunc {
	if limit <= 0 {
		limit = DefaultMaxBodyBytes
	}
	return func(c *gin.Context) {
		if c.Request.Body == nil || strings.HasPrefix(c.ContentType(), "multipart/") {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": BodyTooLargeText})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
	}
}

const shutdownTimeout = 5 * time.Second

func Serve(server *http.Server, logger *slog.Logger, jobs ...func(context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		logger.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	for _, job := range jobs {
		g.Go(func() error { return job(gCtx) })
	}
	g.Go(func() error {
		<-gCtx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	})

	return g.Wait()
}

func Every(ctx context.Context, period time.Duration, logger *slog.Logger, msg string, job func(context.Context) error) error {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, period)
			if err := job(runCtx); err != nil {
				logger.Error(msg, "error", err)
			}
			cancel()
		}
	}
}

func Verifier(cfg *config.Config, logger *slog.Logger) middleware.TokenVerifier {
	if cfg.AuthJWKSURL == "" {
		logger.Warn("AUTH_JWKS_URL is empty, every bearer token will be rejected")
		return nil
	}
	return token.NewVerifier(cfg.AuthJWKSURL, cfg.AuthIssuer, cfg.AuthAudience, cfg.AuthJWKSCacheTTL)
}

func WarmVerifier(verifier middleware.TokenVerifier, logger *slog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		if v, ok := verifier.(*token.Verifier); ok {
			v.Warm(ctx, logger)
		}
		return nil
	}
}
