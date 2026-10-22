package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseSearchFiltersRatingBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		query  string
		status int
		want   float64
	}{
		{"rating_from=7.5", http.StatusOK, 7.5},
		{"local_from=0", http.StatusOK, 0},
		{"rating_from=Inf", http.StatusBadRequest, 0},
		{"local_from=Infinity", http.StatusBadRequest, 0},
		{"rating_from=-Inf", http.StatusBadRequest, 0},
		{"rating_from=NaN", http.StatusBadRequest, 0},
		{"rating_from=11", http.StatusBadRequest, 0},
		{"local_from=-1", http.StatusBadRequest, 0},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/films?"+tc.query, nil)

		filters, ok := parseSearchFilters(c)
		if tc.status == http.StatusOK {
			if !ok {
				t.Errorf("%s: rejected with %d", tc.query, recorder.Code)
				continue
			}
			if got := filters.RatingFrom + filters.LocalFrom; got != tc.want {
				t.Errorf("%s: parsed %v, want %v", tc.query, got, tc.want)
			}
			continue
		}
		if ok || recorder.Code != tc.status {
			t.Errorf("%s: ok=%v status=%d, want %d", tc.query, ok, recorder.Code, tc.status)
		}
	}
}
