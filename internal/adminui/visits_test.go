package adminui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"lifelog-api/internal/visits"
)

func TestVisitPagesRender(t *testing.T) {
	parsed := pages(t)
	now := time.Now()
	summary := visits.Summary{Max: 4, Days: []visits.DayCount{{Day: "2026-10-21", Visitors: 2, Requests: 9}, {Day: "2026-10-22", Visitors: 4, Requests: 30}}}
	cases := []struct {
		page string
		data map[string]any
		want []string
	}{
		{"visits.html", map[string]any{
			"Prefix": "/admin", "User": "Женя", "Summary": summary, "Day": "2026-10-22", "Requests": 6,
			"Visitors": []visits.VisitorDay{{Day: "2026-10-22", Visitor: "60d66f696b35f3ec", FirstSeen: now, LastSeen: now, Requests: 6, UserAgent: "Mozilla"}},
		}, []string{"/admin/visits/2026-10-22/60d66f696b35f3ec", "height: 100%", "height: 50%", "1 посетителей"}},
		{"visits.html", map[string]any{"Prefix": "/admin", "Summary": summary, "Day": "2026-10-20", "Visitors": []visits.VisitorDay{}}, []string{"посетителей нет"}},
		{"visits.html", map[string]any{"Prefix": "/admin", "Error": "Статистика посещений выключена."}, []string{"выключена"}},
		{"visitor.html", map[string]any{
			"Prefix": "/admin", "Day": "2026-10-22", "Visitor": "60d66f696b35f3ec", "Keep": 100,
			"Requests": []visits.StoredRequest{{At: now, Service: "films", Method: "GET", Path: "/api/v1/films/:id", Status: 502, DurationMS: 12.5}},
		}, []string{"GET /api/v1/films/:id", `class="s5"`, ">12<"}},
	}
	for _, c := range cases {
		var out bytes.Buffer
		if err := parsed.ExecuteTemplate(&out, c.page, c.data); err != nil {
			t.Fatalf("%s: %v", c.page, err)
		}
		for _, want := range c.want {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: no %q in output", c.page, want)
			}
		}
	}
}
