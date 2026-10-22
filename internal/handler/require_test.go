package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"lifelog-api/internal/auth"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func identityContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(recorder)
	engine.ContextWithFallback = true
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, recorder
}

func TestRequirePrincipal(t *testing.T) {
	c, recorder := identityContext(t)
	if _, ok := requirePrincipal(c, quietLogger()); ok {
		t.Fatal("no principal reported as present")
	}
	if recorder.Code != http.StatusInternalServerError || errorOf(t, recorder) != internalErrorText {
		t.Fatalf("status %d body %q", recorder.Code, recorder.Body.String())
	}

	c, recorder = identityContext(t)
	want := auth.Principal{UserID: 4, Role: auth.RoleUser, AuthUserID: uuid.New()}
	c.Request = c.Request.WithContext(auth.WithPrincipal(c.Request.Context(), want))
	if got, ok := requirePrincipal(c, quietLogger()); !ok || got != want || recorder.Body.Len() != 0 {
		t.Fatalf("got %+v, %v, body %q", got, ok, recorder.Body.String())
	}
}

func TestRequireAccount(t *testing.T) {
	c, recorder := identityContext(t)
	if _, ok := requireAccount(c, quietLogger()); ok {
		t.Fatal("no account reported as present")
	}
	if recorder.Code != http.StatusInternalServerError || errorOf(t, recorder) != internalErrorText {
		t.Fatalf("status %d body %q", recorder.Code, recorder.Body.String())
	}

	c, recorder = identityContext(t)
	want := uuid.New()
	c.Set("auth_user_id", want)
	if got, ok := requireAccount(c, quietLogger()); !ok || got != want || recorder.Body.Len() != 0 {
		t.Fatalf("got %v, %v, body %q", got, ok, recorder.Body.String())
	}
}
