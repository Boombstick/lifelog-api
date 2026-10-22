package adminui

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog-api/internal/authsvc"
)

func pages(t *testing.T) *template.Template {
	t.Helper()
	parsed, err := template.New("").Funcs(helpers).ParseFS(files, "templates/*.html")
	if err != nil {
		t.Fatalf("разбор шаблонов: %v", err)
	}
	return parsed
}

func TestШаблоныСобираютсяИРисуются(t *testing.T) {
	parsed := pages(t)

	cases := []struct {
		page string
		data map[string]any
		want string
	}{
		{"login.html", map[string]any{"Prefix": "/admin", "Error": "Неверный логин или пароль."}, "Неверный логин"},
		{"sync.html", map[string]any{
			"Prefix": "/admin", "User": "Женя", "CSRF": "csrf-1",
			"YearFrom": 2016, "YearTo": 2026,
			"Status":  &SyncStatus{State: "running", RunID: "r-1", YearFrom: 2016, YearTo: 2026},
			"Refresh": true,
		}, "csrf-1"},
		{"password.html", map[string]any{"Prefix": "/admin", "User": "Женя", "CSRF": "csrf-2", "Done": "own"}, "Остальные устройства разлогинены"},
	}

	for _, item := range cases {
		buffer := bytes.Buffer{}
		if err := parsed.ExecuteTemplate(&buffer, item.page, item.data); err != nil {
			t.Errorf("%s: %v", item.page, err)
			continue
		}
		if !strings.Contains(buffer.String(), item.want) {
			t.Errorf("%s: в выводе нет %q", item.page, item.want)
		}
	}
}

func TestИдущийПрогонПроситСтраницуОбновляться(t *testing.T) {
	buffer := bytes.Buffer{}
	err := pages(t).ExecuteTemplate(&buffer, "sync.html", map[string]any{
		"Prefix": "/admin", "User": "Женя", "Refresh": true,
		"Status": &SyncStatus{State: "running"},
	})
	if err != nil {
		t.Fatalf("отрисовка: %v", err)
	}
	if !strings.Contains(buffer.String(), `http-equiv="refresh"`) {
		t.Error("страница не просит обновляться, пока прогон идёт")
	}
	if !strings.Contains(buffer.String(), "disabled") {
		t.Error("кнопка запуска не заблокирована при идущем прогоне")
	}
}

func TestЗавершённыйПрогонСтраницуНеОбновляет(t *testing.T) {
	buffer := bytes.Buffer{}
	err := pages(t).ExecuteTemplate(&buffer, "sync.html", map[string]any{
		"Prefix": "/admin", "User": "Женя",
		"Status": &SyncStatus{State: "succeeded"},
	})
	if err != nil {
		t.Fatalf("отрисовка: %v", err)
	}
	if strings.Contains(buffer.String(), `http-equiv="refresh"`) {
		t.Error("страница продолжает обновляться после завершения")
	}
}

func TestТокенАдминаУходитВКаталог(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"idle"}`))
	}))
	defer server.Close()

	if _, err := newFilmsClient(server.URL).status(context.Background(), "access-1"); err != nil {
		t.Fatalf("запрос состояния: %v", err)
	}
	if seen != "Bearer access-1" {
		t.Errorf("заголовок авторизации: %q", seen)
	}
}

func TestЗанятоНеСчитаетсяОшибкой(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"state":"running","run_id":"r-7"}`))
	}))
	defer server.Close()

	status, err := newFilmsClient(server.URL).start(context.Background(), "access-1", 2016, 2026)
	if err != nil {
		t.Fatalf("ошибка вместо состояния: %v", err)
	}
	if !status.Running() || status.RunID != "r-7" {
		t.Errorf("состояние идущего прогона не разобрано: %+v", status)
	}
}

func TestОшибкаКаталогаПоказываетсяЕгоТекстом(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"недопустимые годы: 2030 позже 2020"}`))
	}))
	defer server.Close()

	_, err := newFilmsClient(server.URL).start(context.Background(), "access-1", 2030, 2020)
	if err == nil {
		t.Fatal("ошибка каталога проглочена")
	}
	if !strings.Contains(err.Error(), "недопустимые годы") {
		t.Errorf("текст ошибки потерян: %v", err)
	}
}

func TestБезАдресаКаталогаОшибкаОбъясняет(t *testing.T) {
	_, err := newFilmsClient("").status(context.Background(), "access-1")
	if err == nil || !strings.Contains(err.Error(), "AUTH_FILMS_URL") {
		t.Errorf("ошибка не называет переменную: %v", err)
	}
}

func TestОшибкиСервисаПереводятсяВПонятныйТекст(t *testing.T) {
	cases := map[error]string{
		authsvc.ErrInvalidCredentials: "Текущий пароль не подошёл.",
		authsvc.ErrWeakPassword:       "Пароль короче восьми символов.",
		authsvc.ErrUserNotFound:       "Такой учётной записи нет.",
	}
	for err, want := range cases {
		if got := describe(err); got != want {
			t.Errorf("%v → %q, ожидалось %q", err, got, want)
		}
	}

	if got := describe(errors.New("что-то своё")); got != "что-то своё" {
		t.Errorf("незнакомая ошибка переписана: %q", got)
	}
	if got := describe(nil); got != "" {
		t.Errorf("nil дал текст %q", got)
	}
}

func TestКодОтветаКаталогаДоходитДоСтраницы(t *testing.T) {
	cases := []struct {
		catalog int
		want    int
	}{
		{http.StatusBadRequest, http.StatusBadRequest},
		{http.StatusForbidden, http.StatusForbidden},
		{http.StatusServiceUnavailable, http.StatusBadGateway},
		{http.StatusInternalServerError, http.StatusBadGateway},
	}

	for _, item := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(item.catalog)
			_, _ = w.Write([]byte(`{"error":"беда"}`))
		}))

		_, err := newFilmsClient(server.URL).start(context.Background(), "access-1", 2016, 2026)
		server.Close()
		if err == nil {
			t.Fatalf("%d: ошибка проглочена", item.catalog)
		}
		if got := statusOf(err); got != item.want {
			t.Errorf("каталог ответил %d → страница %d, ожидалось %d", item.catalog, got, item.want)
		}
	}
}

func TestНедоступныйКаталогЭто502(t *testing.T) {
	_, err := newFilmsClient("http://127.0.0.1:1").status(context.Background(), "access-1")
	if err == nil {
		t.Fatal("недоступный каталог не дал ошибки")
	}
	if got := statusOf(err); got != http.StatusBadGateway {
		t.Errorf("код страницы %d, ожидался 502", got)
	}
}
