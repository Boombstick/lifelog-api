package adminui

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"lifelog-api/internal/authsvc"
	"lifelog-api/internal/token"
	"lifelog-api/internal/visits"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

//go:embed templates/*.html
var files embed.FS

type Options struct {
	Prefix     string
	FilmsURL   string
	AccessTTL  time.Duration
	RefreshTTL time.Duration

	RotationDir string

	Visits *visits.Ledger
}

type Handler struct {
	service     *authsvc.Service
	signer      *token.Signer
	films       *filmsClient
	templates   *template.Template
	prefix      string
	accessTTL   time.Duration
	refreshTTL  time.Duration
	rotationDir string
	visits      *visits.Ledger
}

func New(service *authsvc.Service, signer *token.Signer, options Options) (*Handler, error) {
	if options.Prefix == "" {
		options.Prefix = "/admin"
	}
	pages, err := template.New("").Funcs(helpers).ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Handler{
		service:     service,
		signer:      signer,
		films:       newFilmsClient(options.FilmsURL),
		templates:   pages,
		prefix:      options.Prefix,
		accessTTL:   options.AccessTTL,
		refreshTTL:  options.RefreshTTL,
		rotationDir: options.RotationDir,
		visits:      options.Visits,
	}, nil
}

func (h *Handler) Register(engine *gin.Engine) {
	group := engine.Group(h.prefix)
	group.GET("/login", h.loginForm)
	group.POST("/login", h.login)

	guarded := group.Group("", h.require)
	guarded.GET("", func(c *gin.Context) { c.Redirect(http.StatusSeeOther, h.prefix+"/sync") })
	guarded.POST("/logout", h.logout)
	guarded.GET("/sync", h.syncPage)
	guarded.POST("/sync", h.startSync)
	guarded.GET("/password", h.passwordPage)
	guarded.POST("/password", h.changeOwnPassword)
	guarded.POST("/password/reset", h.resetPassword)
	guarded.GET("/rotate", h.rotationPage)
	guarded.POST("/rotate", h.startRotation)
	guarded.POST("/rotate/reveal", h.revealSecrets)
	guarded.GET("/visits", h.visitsPage)
	guarded.GET("/visits/:day/:visitor", h.visitorPage)
}

func (h *Handler) loginForm(c *gin.Context) {
	h.render(c, http.StatusOK, "login.html", gin.H{})
}

func (h *Handler) login(c *gin.Context) {
	login := c.PostForm("login")
	password := c.PostForm("password")

	rejected := func() {
		h.render(c, http.StatusUnauthorized, "login.html", gin.H{
			"Error": "Неверный логин или пароль.",
			"Login": login,
		})
	}

	pair, err := h.service.SignIn(c.Request.Context(), login, password, authsvc.Client{UserAgent: c.Request.UserAgent()})
	if errors.Is(err, authsvc.ErrLoginLocked) {
		h.render(c, http.StatusTooManyRequests, "login.html", gin.H{
			"Error": "Слишком много попыток входа, попробуйте через 15 минут.",
			"Login": login,
		})
		return
	}
	if err != nil {
		rejected()
		return
	}

	claims, err := h.signer.Parse(pair.AccessToken)
	if err != nil || claims.Role != string(authsvc.RoleAdmin) {
		_ = h.service.Logout(c.Request.Context(), pair.RefreshToken)
		rejected()
		return
	}

	h.setSession(c, pair)
	if _, err := h.ensureCSRF(c); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Redirect(http.StatusSeeOther, h.prefix+"/sync")
}

func (h *Handler) logout(c *gin.Context) {
	if refresh, err := c.Cookie(refreshCookie); err == nil && refresh != "" {
		_ = h.service.Logout(c.Request.Context(), refresh)
	}
	h.clearSession(c)
	c.Redirect(http.StatusSeeOther, h.prefix+"/login")
}

func (h *Handler) syncPage(c *gin.Context) {
	h.renderSync(c, http.StatusOK, "", c.Query("ok") != "")
}

func (h *Handler) startSync(c *gin.Context) {
	if !h.checkCSRF(c) {
		h.renderSync(c, http.StatusForbidden, "Форма устарела, откройте страницу заново.", false)
		return
	}

	from, fromErr := strconv.Atoi(c.PostForm("year_from"))
	to, toErr := strconv.Atoi(c.PostForm("year_to"))
	if fromErr != nil || toErr != nil {
		h.renderSync(c, http.StatusBadRequest, "Годы должны быть числами.", false)
		return
	}

	if _, err := h.films.start(c.Request.Context(), principalOf(c).Access, from, to); err != nil {
		h.renderSync(c, statusOf(err), err.Error(), false)
		return
	}
	c.Redirect(http.StatusSeeOther, h.prefix+"/sync?ok=1")
}

func (h *Handler) renderSync(c *gin.Context, code int, message string, started bool) {
	data := gin.H{"Error": message, "Started": started}

	status, err := h.films.status(c.Request.Context(), principalOf(c).Access)
	if err != nil && message == "" {
		data["Error"] = err.Error()
	}
	if status != nil {
		data["Status"] = status
		data["Refresh"] = status.Running()
	}
	if status == nil || status.YearFrom == 0 {
		data["YearFrom"] = 2016
		data["YearTo"] = time.Now().Year()
	} else {
		data["YearFrom"] = status.YearFrom
		data["YearTo"] = status.YearTo
	}

	h.render(c, code, "sync.html", data)
}

func (h *Handler) passwordPage(c *gin.Context) {
	h.render(c, http.StatusOK, "password.html", gin.H{"Done": c.Query("ok")})
}

func (h *Handler) changeOwnPassword(c *gin.Context) {
	if !h.checkCSRF(c) {
		h.render(c, http.StatusForbidden, "password.html", gin.H{"OwnError": "Форма устарела, откройте страницу заново."})
		return
	}

	next := c.PostForm("new_password")
	if next != c.PostForm("repeat_password") {
		h.render(c, http.StatusBadRequest, "password.html", gin.H{"OwnError": "Пароли не совпадают."})
		return
	}

	person := principalOf(c)
	err := h.service.ChangePassword(c.Request.Context(), person.UserID, c.PostForm("current_password"), next, person.Family)
	if err != nil {
		h.render(c, http.StatusBadRequest, "password.html", gin.H{"OwnError": describe(err)})
		return
	}
	c.Redirect(http.StatusSeeOther, h.prefix+"/password?ok=own")
}

func (h *Handler) resetPassword(c *gin.Context) {
	if !h.checkCSRF(c) {
		h.render(c, http.StatusForbidden, "password.html", gin.H{"ResetError": "Форма устарела, откройте страницу заново."})
		return
	}

	login := c.PostForm("login")
	next := c.PostForm("new_password")

	target, err := h.resolveTarget(c, login)
	if err != nil {
		h.render(c, http.StatusNotFound, "password.html", gin.H{"ResetError": describe(err), "ResetLogin": login})
		return
	}

	if err := h.service.ResetPassword(c.Request.Context(), target, next); err != nil {
		h.render(c, http.StatusBadRequest, "password.html", gin.H{"ResetError": describe(err), "ResetLogin": login})
		return
	}
	c.Redirect(http.StatusSeeOther, h.prefix+"/password?ok=reset")
}

func (h *Handler) resolveTarget(c *gin.Context, value string) (uuid.UUID, error) {
	if id, err := uuid.Parse(value); err == nil {
		return id, nil
	}
	user, err := h.service.UserByLogin(c.Request.Context(), value)
	if err != nil {
		return uuid.Nil, err
	}
	return user.ID, nil
}

func (h *Handler) render(c *gin.Context, code int, page string, data gin.H) {
	data["Prefix"] = h.prefix
	if value, exists := c.Get(csrfKey); exists {
		data["CSRF"] = value
	}
	if person := principalOf(c); person.UserID != uuid.Nil {
		data["User"] = person.Name
	}

	c.Status(code)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(c.Writer, page, data); err != nil {
		c.String(http.StatusInternalServerError, "не удалось собрать страницу: %v", err)
	}
}

func describe(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, authsvc.ErrInvalidCredentials):
		return "Текущий пароль не подошёл."
	case errors.Is(err, authsvc.ErrWeakPassword):
		return "Пароль короче восьми символов."
	case errors.Is(err, authsvc.ErrUserNotFound):
		return "Такой учётной записи нет."
	case errors.Is(err, authsvc.ErrNoPassword):
		return "У этой учётной записи пароля ещё нет — воспользуйтесь сбросом ниже."
	case errors.Is(err, authsvc.ErrManagedPassword):
		return "Пароль главного администратора задаётся конфигом сервиса (AUTH_ROOT_ADMIN_PASSWORD) и меняется только выкладкой."
	default:
		return err.Error()
	}
}

var helpers = template.FuncMap{
	"clock": func(value time.Time) string { return value.Local().Format("15:04:05") },
	"dayLabel": func(day string) string {
		if len(day) == 10 {
			return day[8:]
		}
		return day
	},
	"percent": func(value, max int) int {
		if max <= 0 {
			return 0
		}
		return value * 100 / max
	},
	"moment": func(value *time.Time) string {
		if value == nil {
			return "—"
		}
		return value.Local().Format("02.01.2006 15:04:05")
	},
}
