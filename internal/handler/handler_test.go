package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestWriteContextOutlivesClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reqCtx, hangUp := context.WithCancel(context.WithValue(context.Background(), struct{}{}, "kept"))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/notes", nil).WithContext(reqCtx)

	ctx, cancel := writeContext(c)
	defer cancel()
	hangUp()

	if ctx.Err() != nil {
		t.Fatalf("write context died with the client: %v", ctx.Err())
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("write context has no deadline")
	}
	if ctx.Value(struct{}{}) != "kept" {
		t.Fatal("request values were lost")
	}
}

func TestParamRejectsGarbageWithFixedText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{"abc", "0", "-1", "1.5", "99999999999999999999"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Params = gin.Params{{Key: "id", Value: raw}}
		if _, err := (handler{}).paramID(c); err == nil || err.Error() != badIDText {
			t.Errorf("%q: err = %v, want %q", raw, err, badIDText)
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "id", Value: "42"}}
	if id, err := (handler{}).paramID(c); err != nil || id != 42 {
		t.Fatalf("42: %d, %v", id, err)
	}
}
