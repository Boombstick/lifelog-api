package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"lifelog-api/internal/adminui"
	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/config"
	"lifelog-api/internal/handler"
	"lifelog-api/internal/health"
	"lifelog-api/internal/httpapi"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/storage/postgres"
	"lifelog-api/internal/token"
	"lifelog-api/internal/visits"

	"github.com/gin-gonic/gin"
)

func run() error {
	logger := logging.Setup("auth")
	configuration, err := config.New()
	if err != nil {
		return err
	}

	pgConfig := postgres.ConfigFrom(configuration, configuration.AuthDatabase, logger)
	database, err := postgres.NewAuthPostgres(pgConfig)
	if err != nil {
		return err
	}

	signer, err := token.LoadOrCreateSigner(
		configuration.AuthPrivateKeyPath,
		configuration.AuthIssuer,
		configuration.AuthAudience,
		configuration.AuthAccessTTL,
	)
	if err != nil {
		return err
	}

	repo := postgres.NewIdentityRepository(database)
	notifier := authsvc.NewHTTPMergeNotifier(
		strings.Split(configuration.AuthMergeTargets, ","),
		strings.Split(configuration.AuthFilmsURL, ","),
		configuration.AuthInternalToken,
		10*time.Second,
	)
	service := authsvc.NewService(repo, signer, notifier, logger, configuration.AuthRefreshTTL, configuration.AuthBotUsername)

	var mailer authsvc.Mailer = authsvc.NewLogMailer(logger)
	if configuration.AuthSMTPHost != "" {
		mailer = authsvc.NewSMTPMailer(configuration.AuthSMTPHost, configuration.AuthSMTPPort, configuration.AuthSMTPUser, configuration.AuthSMTPPassword, configuration.AuthMailFrom)
	} else {
		logger.Warn("SMTP is not configured: verification codes are only written to the log")
	}
	var mailbox *authsvc.CaptureMailer
	if configuration.AuthMailCapture {
		mailbox = authsvc.NewCaptureMailer(mailer)
		mailer = mailbox
	}
	service.SetMailer(mailer)

	service.SetRootLogin(configuration.AuthRootAdminLogin)
	if configuration.AuthRootAdminPassword != "" {
		rootCtx, cancelRoot := context.WithTimeout(context.Background(), 30*time.Second)
		demoted, err := service.EnsureRootAdmin(rootCtx, authsvc.RootAdmin{
			Login:       configuration.AuthRootAdminLogin,
			Password:    configuration.AuthRootAdminPassword,
			Email:       configuration.AuthRootAdminEmail,
			DisplayName: configuration.AuthRootAdminName,
		})
		cancelRoot()
		if err != nil {
			logger.Error("root admin not synced with config", "error", err)
		} else {
			logger.Info("root admin synced with config", "login", configuration.AuthRootAdminLogin, "demoted", demoted)
		}
	}

	resumeCtx, cancelResume := context.WithTimeout(context.Background(), 30*time.Second)
	if err := service.ResumeMerges(resumeCtx); err != nil {
		logger.Warn("unfinished merges not resumed", "error", err)
	}
	cancelResume()

	ledger := visits.NewLedger(postgres.NewVisitRepository(database))
	var recorder *visits.Recorder
	if configuration.VisitsEnabled {
		recorder = visits.NewRecorder("auth", ledger, logger)
	}

	identityHandler := handler.NewIdentityHandler(logger, service, signer)
	identityHandler.SetMailbox(mailbox)
	admin, err := adminui.New(service, signer, adminui.Options{
		FilmsURL:    configuration.AuthFilmsURL,
		AccessTTL:   configuration.AuthAccessTTL,
		RefreshTTL:  configuration.AuthRefreshTTL,
		RotationDir: configuration.AuthRotationDir,
		Visits:      ledger,
	})
	if err != nil {
		return err
	}

	checks, err := healthChecks(database, signer, configuration)
	if err != nil {
		return err
	}
	engine := buildEngine(identityHandler, checks, configuration.AuthInternalToken, logger)
	engine.POST("/internal/visits", handler.RequireInternalToken(configuration.AuthInternalToken), handler.VisitsIngest(ledger, logger))
	admin.Register(engine)

	server := httpapi.NewServer(configuration.BindHost+":"+configuration.AuthPort, visits.Wrap(engine, recorder))
	jobs := append(recorder.Jobs(), visitsPrune(ledger, logger))
	return httpapi.Serve(server, logger, append(jobs, func(ctx context.Context) error {
		cleanup := expiredCleanup(repo, logger)
		if err := cleanup(ctx); err != nil {
			logger.Warn(cleanupFailed, "error", err)
		}
		return httpapi.Every(ctx, time.Hour, logger, cleanupFailed, cleanup)
	})...)
}

func visitsPrune(ledger *visits.Ledger, logger *slog.Logger) func(context.Context) error {
	prune := func(ctx context.Context) error { return ledger.Prune(ctx, time.Now()) }
	return func(ctx context.Context) error {
		if err := prune(ctx); err != nil {
			logger.Warn(visitsPruneFailed, "error", err)
		}
		return httpapi.Every(ctx, 15*time.Minute, logger, visitsPruneFailed, prune)
	}
}

const visitsPruneFailed = "visit statistics prune failed"

type expiredCleaner interface {
	DeleteExpired(ctx context.Context) (int64, error)
}

const cleanupFailed = "expired sessions cleanup failed"

func expiredCleanup(cleaner expiredCleaner, logger *slog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		removed, err := cleaner.DeleteExpired(ctx)
		if err == nil && removed > 0 {
			logger.Info("expired records removed", "count", removed)
		}
		return err
	}
}

func buildEngine(h *handler.IdentityHandler, checks *health.Registry, internalToken string, logger *slog.Logger) *gin.Engine {
	engine := httpapi.NewEngine(httpapi.EngineOptions{Service: "auth", Logger: logger, Health: checks.Handler()})
	engine.GET("/.well-known/jwks.json", h.JWKS)

	public := engine.Group("/auth")
	public.POST("/register", h.Register)
	public.POST("/register/confirm", h.ConfirmRegistration)
	public.POST("/password/forgot", h.ForgotPassword)
	public.POST("/password/reset", h.ResetPasswordWithCode)
	public.POST("/login", h.Login)
	public.POST("/refresh", h.Refresh)
	public.POST("/logout", h.Logout)

	private := engine.Group("/auth", h.RequireToken())
	private.GET("/me", h.Me)
	private.PATCH("/me", h.UpdateMe)
	private.POST("/password", h.ChangePassword)
	private.POST("/email", h.AttachEmail)
	private.POST("/email/confirm", h.ConfirmEmail)
	private.POST("/telegram/link", h.StartTelegramLink)
	private.DELETE("/telegram/link", h.UnlinkTelegram)
	private.GET("/subscriptions", h.Subscriptions)
	private.POST("/subscriptions", h.Subscribe)
	private.DELETE("/subscriptions/:id", h.Unsubscribe)
	private.GET("/subscribers", h.Subscribers)
	private.POST("/subscribers/:id/approve", h.ApproveSubscriber)
	private.POST("/subscribers/:id/decline", h.DeclineSubscriber)
	private.PUT("/privacy", h.SetPrivacy)
	private.GET("/blocks", h.Blocks)
	private.POST("/blocks", h.Block)
	private.DELETE("/blocks/:id", h.Unblock)
	private.GET("/sessions", h.Sessions)
	private.DELETE("/sessions", h.RevokeAllSessions)
	private.DELETE("/sessions/:family", h.RevokeSession)

	admin := engine.Group("/auth/users", h.RequireToken(), h.RequireAdmin())
	admin.POST("/:id/password", h.ResetPassword)

	internal := engine.Group("/internal", handler.RequireInternalToken(internalToken))
	internal.POST("/telegram/ensure", h.EnsureTelegramUser)
	internal.GET("/telegram/:id", h.ResolveTelegram)
	internal.POST("/telegram/ids", h.TelegramIDs)
	internal.POST("/telegram/confirm", h.ConfirmTelegramLink)
	internal.POST("/merges/resume", h.ResumeMerges)
	internal.GET("/follows/:id", h.Following)
	internal.POST("/follows/check", h.FollowersAmong)
	internal.GET("/blocks/:id", h.BlockRelations)
	internal.GET("/mail/last", h.LastMail)

	return engine
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
