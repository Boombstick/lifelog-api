package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/token"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func errorOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not {\"error\"}: %v", recorder.Body.String(), err)
	}
	return body.Error
}

func TestBindErrorsAreShortRussianTexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		body string
		bind func(c *gin.Context) bool
		want string
	}{
		{"missing password", `{"login":"masha"}`, func(c *gin.Context) bool { var r loginRequest; return bindJSONRequest(c, &r) }, "Укажите пароль"},
		{"broken json", `{"login":x}`, func(c *gin.Context) bool { var r loginRequest; return bindJSONRequest(c, &r) }, malformedRequest},
		{"empty body", ``, func(c *gin.Context) bool { var r loginRequest; return bindJSONRequest(c, &r) }, malformedRequest},
		{"wrong type", `{"login":"m","password":5}`, func(c *gin.Context) bool { var r loginRequest; return bindJSONRequest(c, &r) }, "Неверный тип поля «password»"},
		{"missing code", `{"email":"a@b.ru"}`, func(c *gin.Context) bool { var r confirmRegisterRequest; return bindJSONRequest(c, &r) }, "Укажите код"},
		{"missing new password", `{"current_password":"x"}`, func(c *gin.Context) bool { var r changePasswordRequest; return bindJSONRequest(c, &r) }, "Укажите новый пароль"},
		{"unlisted field", `{}`, func(c *gin.Context) bool { var r ensureTelegramRequest; return bindJSONRequest(c, &r) }, "Не заполнено поле «telegram_id»"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")

		if tc.bind(c) {
			t.Errorf("%s: bound a bad body", tc.name)
			continue
		}
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", tc.name, recorder.Code)
		}
		if got := errorOf(t, recorder); got != tc.want {
			t.Errorf("%s: error %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRequireInternalToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		configured, sent string
		want             int
	}{
		{"secret", "secret", http.StatusNoContent},
		{"secret", "secre", http.StatusUnauthorized},
		{"secret", "", http.StatusUnauthorized},
		{"", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		engine := gin.New()
		engine.GET("/internal", RequireInternalToken(tc.configured), func(c *gin.Context) { c.Status(http.StatusNoContent) })
		request := httptest.NewRequest(http.MethodGet, "/internal", nil)
		if tc.sent != "" {
			request.Header.Set("X-Internal-Token", tc.sent)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != tc.want {
			t.Errorf("configured %q, sent %q: status %d, want %d", tc.configured, tc.sent, recorder.Code, tc.want)
		}
	}
}

func TestRequireTokenAcceptsAnyBearerCase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signer, err := token.LoadOrCreateSigner(filepath.Join(t.TempDir(), "key.pem"), "lifelog-auth", "lifelog", 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	access, _, err := signer.Issue(uuid.New(), "user", "Маша", uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	handler := &IdentityHandler{signer: signer}
	engine := gin.New()
	engine.GET("/me", handler.RequireToken(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	cases := map[string]int{
		"Bearer " + access:        http.StatusNoContent,
		"bearer " + access:        http.StatusNoContent,
		"BEARER  " + access + " ": http.StatusNoContent,
		"Bearer ":                 http.StatusUnauthorized,
		"Basic " + access:         http.StatusUnauthorized,
		"Bearer not-a-jwt":        http.StatusUnauthorized,
	}
	for header, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/me", nil)
		request.Header.Set("Authorization", header)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Errorf("Authorization %.20q…: status %d, want %d", header, recorder.Code, want)
		}
	}
}
