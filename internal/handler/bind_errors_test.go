package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog-api/internal/httpapi"

	"github.com/gin-gonic/gin"
)

type bindProbe struct {
	Email string `json:"email,omitempty" binding:"omitempty,email"`
}

func bindProbeRequest(t *testing.T, body string, limit int64) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if limit > 0 {
		c.Request.Body = http.MaxBytesReader(recorder, c.Request.Body, limit)
	}
	var dst bindProbe
	return recorder, bindJSONRequest(c, &dst)
}

func TestBindAnswers413WhenBodyHitsLimit(t *testing.T) {
	recorder, ok := bindProbeRequest(t, `{"email":"`+strings.Repeat("a", 100)+`@b.ru"}`, 16)
	if ok {
		t.Fatal("bound a body over the limit")
	}
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", recorder.Code)
	}
	if got := errorOf(t, recorder); got != httpapi.BodyTooLargeText {
		t.Fatalf("error %q, want %q", got, httpapi.BodyTooLargeText)
	}
}

func TestBindNamesInvalidFieldByJSONKey(t *testing.T) {
	recorder, ok := bindProbeRequest(t, `{"email":"not-an-address"}`, 0)
	if ok {
		t.Fatal("bound an invalid email")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", recorder.Code)
	}
	if got, want := errorOf(t, recorder), "Некорректное значение поля «email»"; got != want {
		t.Fatalf("error %q, want %q", got, want)
	}
}
