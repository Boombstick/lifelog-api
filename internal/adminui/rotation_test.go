package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func rotationEngine(t *testing.T, dir string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &Handler{templates: pages(t), prefix: "/admin", rotationDir: dir}
	engine := gin.New()
	guarded := engine.Group("/admin", func(c *gin.Context) {
		c.Set(principalKey, principal{UserID: uuid.New(), Name: "Женя"})
		c.Set(csrfKey, "csrf-1")
	})
	guarded.GET("/rotate", h.rotationPage)
	guarded.POST("/rotate", h.startRotation)
	guarded.POST("/rotate/reveal", h.revealSecrets)
	return engine
}

func post(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-1"})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestСменаБезСловаНеЗаказывается(t *testing.T) {
	dir := t.TempDir()
	engine := rotationEngine(t, dir)

	answer := post(engine, "/admin/rotate", url.Values{"csrf": {"csrf-1"}, "confirm": {"да"}})

	if answer.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался 400", answer.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, rotationRequest)); err == nil {
		t.Error("заявка записана без подтверждения")
	}
}

func TestСменаБезCSRFНеЗаказывается(t *testing.T) {
	dir := t.TempDir()
	engine := rotationEngine(t, dir)

	answer := post(engine, "/admin/rotate", url.Values{"csrf": {"чужой"}, "confirm": {rotationConfirm}})

	if answer.Code != http.StatusForbidden {
		t.Errorf("код %d, ожидался 403", answer.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, rotationRequest)); err == nil {
		t.Error("заявка записана с чужим CSRF")
	}
}

func TestСменаЗаказываетсяОдинРаз(t *testing.T) {
	dir := t.TempDir()
	engine := rotationEngine(t, dir)
	form := url.Values{"csrf": {"csrf-1"}, "confirm": {rotationConfirm}}

	if answer := post(engine, "/admin/rotate", form); answer.Code != http.StatusSeeOther {
		t.Fatalf("код %d, ожидался 303", answer.Code)
	}
	data, err := os.ReadFile(filepath.Join(dir, rotationRequest))
	if err != nil || !strings.Contains(string(data), "Женя") {
		t.Fatalf("заявка: %s, %v", data, err)
	}
	if answer := post(engine, "/admin/rotate", form); answer.Code != http.StatusConflict {
		t.Errorf("вторая заявка: код %d, ожидался 409", answer.Code)
	}
}

func TestПокаСменаИдётВтораяНеЗаказывается(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, rotationStatus), []byte(`{"state":"running"}`), 0o600)
	engine := rotationEngine(t, dir)

	answer := post(engine, "/admin/rotate", url.Values{"csrf": {"csrf-1"}, "confirm": {rotationConfirm}})

	if answer.Code != http.StatusConflict {
		t.Errorf("код %d, ожидался 409", answer.Code)
	}
}

func TestНовыеПаролиПоказываютсяОдинРаз(t *testing.T) {
	dir := t.TempDir()
	secrets := filepath.Join(dir, rotationSecrets)
	os.WriteFile(secrets, []byte("  Grafana          admin / s3cret-value\n"), 0o600)
	engine := rotationEngine(t, dir)

	first := post(engine, "/admin/rotate/reveal", url.Values{"csrf": {"csrf-1"}})
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "s3cret-value") {
		t.Fatalf("первый показ: код %d", first.Code)
	}
	if first.Header().Get("Cache-Control") != "no-store" {
		t.Error("страница с паролями может осесть в кеше браузера")
	}
	if _, err := os.Stat(secrets); err == nil {
		t.Error("файл с паролями остался после показа")
	}

	second := post(engine, "/admin/rotate/reveal", url.Values{"csrf": {"csrf-1"}})
	if second.Code != http.StatusGone || strings.Contains(second.Body.String(), "s3cret-value") {
		t.Errorf("повторный показ: код %d", second.Code)
	}
}

func TestПаролиБезCSRFНеПоказываются(t *testing.T) {
	dir := t.TempDir()
	secrets := filepath.Join(dir, rotationSecrets)
	os.WriteFile(secrets, []byte("secret"), 0o600)
	engine := rotationEngine(t, dir)

	answer := post(engine, "/admin/rotate/reveal", url.Values{"csrf": {"чужой"}})

	if answer.Code != http.StatusForbidden || strings.Contains(answer.Body.String(), "secret\n") {
		t.Errorf("код %d", answer.Code)
	}
	if _, err := os.Stat(secrets); err != nil {
		t.Error("файл удалён чужим запросом — пароли потеряны без показа")
	}
}

func TestСтраницаСменыПокаИдёт(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, rotationStatus), []byte(`{"state":"running","requested_by":"Женя"}`), 0o600)
	os.WriteFile(filepath.Join(dir, rotationLog), []byte("=== Postgres\n=== Elasticsearch\n"), 0o600)
	engine := rotationEngine(t, dir)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/rotate", nil))
	body := recorder.Body.String()

	for _, want := range []string{`http-equiv="refresh"`, "=== Elasticsearch", "идёт", "disabled"} {
		if !strings.Contains(body, want) {
			t.Errorf("на странице нет %q", want)
		}
	}
	if strings.Contains(body, "Показать новые пароли") {
		t.Error("кнопка показа паролей при отсутствии файла")
	}
}
