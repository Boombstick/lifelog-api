package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"lifelog-api/internal/trips"

	"github.com/google/uuid"
)

func TestTripsRoutesRegister(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	registered := map[string]bool{}
	for _, r := range env.engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/dictionary/countries", "GET /api/dictionary/currencies",
		"GET /api/trips", "GET /api/trips/active", "POST /api/trips", "POST /api/trips/end",
		"POST /api/trips/arrive", "PUT /api/trips/currency", "PUT /api/trips/timezone",
		"GET /api/trips/photos", "POST /api/trips/photos",
		"GET /api/trips/photos/:photoId", "DELETE /api/trips/photos/:photoId",
		"DELETE /api/trips/:tripId",
		"POST /api/expenses", "GET /api/expenses", "GET /api/expenses/total",
		"GET /api/expenses/report", "GET /api/expenses/report/builder",
		"GET /api/expenses/duplicates", "POST /api/expenses/:id/not-duplicate", "DELETE /api/expenses/:id",
		"GET /api/expenses/categories", "POST /api/expenses/categories", "PUT /api/expenses/categories/:id",
		"POST /api/sync", "POST /api/internal/users/merge", "POST /api/internal/bot/identify",
	} {
		if !registered[want] {
			t.Errorf("route %s is not registered", want)
		}
	}
}

func TestTripsBearerMode(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)

	cases := []struct {
		name      string
		headers   map[string]string
		challenge string
	}{
		{"no token", nil, "Bearer"},
		{"invalid token", map[string]string{"Authorization": "Bearer nope"}, `Bearer error="invalid_token"`},
		{"sub is not a uuid", map[string]string{"Authorization": "Bearer bad-sub"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/dictionary/countries", headers: tc.headers})
			if rec.Code != http.StatusUnauthorized || rec.Body.Len() != 0 {
				t.Fatalf("got %d %q, want 401 with an empty body", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.challenge {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, tc.challenge)
			}
		})
	}

	rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: bearer()})
	if rec.Code != http.StatusOK {
		t.Fatalf("valid bearer: got %d", rec.Code)
	}
	if env.trips.actor.Mode != trips.ModeApp || env.trips.actor.UserID != testAppUserID || env.identity.displayName != "Ann" {
		t.Fatalf("actor = %+v, name %q", env.trips.actor, env.identity.displayName)
	}

	env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: map[string]string{"Authorization": "Bearer nameless"}})
	if env.identity.displayName != "user-0198a1b2" {
		t.Fatalf("fallback display name = %q", env.identity.displayName)
	}
}

func TestTripsBotMode(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)

	rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: botHeaders()})
	if rec.Code != http.StatusOK {
		t.Fatalf("bot mode: got %d %s", rec.Code, rec.Body)
	}
	if env.trips.actor.Mode != trips.ModeBot || env.identity.telegramID != 123456 ||
		env.identity.authUserID != testAuthUserID || env.identity.displayName != "Иван Петров" {
		t.Fatalf("actor %+v, identity %+v", env.trips.actor, env.identity)
	}

	nameless := botHeaders()
	delete(nameless, "X-Telegram-Name")
	env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: nameless})
	if env.identity.displayName != "tg-123456" {
		t.Fatalf("display name without a header = %q", env.identity.displayName)
	}

	missingID := botHeaders()
	delete(missingID, "X-Telegram-Id")
	missingAuth := botHeaders()
	delete(missingAuth, "X-Auth-User-Id")
	onlyToken := map[string]string{"X-Internal-Token": tripsTestInternalToken}

	rejected := map[string]map[string]string{
		"wrong token":          withHeader(withHeader(botHeaders(), "X-Internal-Token", "guess"), "Authorization", "Bearer good"),
		"empty token":          withHeader(botHeaders(), "X-Internal-Token", ""),
		"telegram id missing":  missingID,
		"telegram id garbage":  withHeader(botHeaders(), "X-Telegram-Id", "12a"),
		"auth user id missing": missingAuth,
		"token only":           onlyToken,
	}
	for name, headers := range rejected {
		t.Run(name, func(t *testing.T) {
			rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: headers})
			if rec.Code != http.StatusUnauthorized || rec.Body.Len() != 0 {
				t.Fatalf("got %d %q, want an empty 401", rec.Code, rec.Body.String())
			}
		})
	}

	unconfigured := newTripsTestEnv(t, "")
	rec = unconfigured.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: withHeader(botHeaders(), "X-Internal-Token", "")})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unconfigured internal token must reject everything, got %d", rec.Code)
	}
}

func TestTripsIdentityFailureIs500(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	env.identity.err = errors.New("db down")
	rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: bearer()})
	assertTripsError(t, rec, http.StatusInternalServerError, trips.MsgInternal)
}

func TestTripsInternalRoutes(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	mergeBody := `{"from":"0198a1b2-0000-7000-8000-00000000000f","into":"0198a1b2-0000-7000-8000-00000000000e"}`

	for name, headers := range map[string]map[string]string{
		"no auth":     nil,
		"bearer":      bearer(),
		"wrong token": {"X-Internal-Token": "guess"},
	} {
		rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/internal/users/merge", body: jsonBody(mergeBody), headers: headers})
		if rec.Code != http.StatusUnauthorized || rec.Body.Len() != 0 {
			t.Fatalf("merge with %s: got %d %q", name, rec.Code, rec.Body.String())
		}
	}
	if env.merge.calls != 0 {
		t.Fatal("merge ran without the internal token")
	}

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/internal/users/merge", body: jsonBody(mergeBody),
		headers: map[string]string{"X-Internal-Token": tripsTestInternalToken}})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("merge: got %d %q", rec.Code, rec.Body.String())
	}
	if env.merge.request.From.String() != "0198a1b2-0000-7000-8000-00000000000f" || env.merge.request.Into.String() != "0198a1b2-0000-7000-8000-00000000000e" {
		t.Fatalf("merge request = %+v", env.merge.request)
	}

	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/internal/bot/identify", headers: bearer()})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("identify with a bearer: got %d", rec.Code)
	}
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/internal/bot/identify", headers: botHeaders()})
	want := `{"user_id":"` + testBotUserID.String() + `","telegram_id":123456,"username":"Иван Петров"}`
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("identify: got %d %s, want %s", rec.Code, rec.Body, want)
	}
}

func TestTripsCategoryWritesByMode(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	body := `{"name":"Спорт","keywords":["фитнес"]}`

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/expenses/categories", body: jsonBody(body), headers: bearer()})
	assertTripsError(t, rec, http.StatusForbidden, trips.MsgForbidden)

	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/expenses/categories", body: jsonBody(body),
		headers: map[string]string{"X-Internal-Token": tripsTestInternalToken}})
	if rec.Code != http.StatusOK || rec.Body.String() != "11" || env.dictionary.actor.Mode != trips.ModeInternal {
		t.Fatalf("internal token: got %d %s, mode %v", rec.Code, rec.Body, env.dictionary.actor.Mode)
	}

	rec = env.do(tripsRequest{method: http.MethodPut, path: "/api/expenses/categories/3", body: jsonBody(`{"Keywords":["кофе"]}`), headers: botHeaders()})
	if rec.Code != http.StatusOK || rec.Body.String() != "{}" || env.dictionary.actor.Mode != trips.ModeBot ||
		env.dictionary.categoryID != 3 || len(env.dictionary.keywords) != 1 {
		t.Fatalf("bot mode: got %d %s, dictionary %+v", rec.Code, rec.Body, env.dictionary)
	}

	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/categories?include_keywords=TRUE", headers: bearer()})
	if rec.Code != http.StatusOK || rec.Body.String() != "[]" || !env.dictionary.includeKeywords {
		t.Fatalf("categories: got %d %s", rec.Code, rec.Body)
	}
	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/categories"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("categories without auth: got %d", rec.Code)
	}
}

func TestTripsStatusMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		body   string
	}{
		{trips.Validation(trips.MsgNoActiveTrips), http.StatusBadRequest, trips.MsgNoActiveTrips},
		{trips.Validation("Сумма должна быть больше нуля"), http.StatusBadRequest, "Сумма должна быть больше нуля"},
		{trips.NotFound(trips.MsgTripNotFound), http.StatusNotFound, trips.MsgTripNotFound},
		{trips.Conflict(trips.MsgSequenceMoreThanOne), http.StatusConflict, trips.MsgSequenceMoreThanOne},
		{trips.NoSnapshots(trips.MsgNoSnapshotsLatest), http.StatusConflict, trips.MsgNoSnapshotsLatest},
		{trips.Forbidden(trips.MsgForbidden), http.StatusForbidden, trips.MsgForbidden},
		{errors.New("pq: connection refused"), http.StatusInternalServerError, trips.MsgInternal},
	}
	for _, tc := range cases {
		env := newTripsTestEnv(t, tripsTestInternalToken)
		env.trips.err = tc.err
		rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/end", headers: bearer()})
		assertTripsError(t, rec, tc.status, tc.body)
	}
}

func TestTripsValidationEnvelope(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)

	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		errorKey string
	}{
		{"empty body", http.MethodPost, "/api/trips", "", ""},
		{"null body", http.MethodPost, "/api/trips", "null", "request"},
		{"malformed JSON", http.MethodPost, "/api/trips", `{"name":`, "$"},
		{"missing name", http.MethodPost, "/api/trips", `{"city":"Пхукет"}`, "name"},
		{"empty name", http.MethodPost, "/api/trips", `{"name":""}`, "name"},
		{"wrong type", http.MethodPost, "/api/trips", `{"name":42}`, "$.name"},
		{"missing description", http.MethodPost, "/api/expenses", `{"amount":350}`, "description"},
		{"bad amount", http.MethodPost, "/api/expenses", `{"amount":"много","description":"кофе"}`, "$"},
		{"bad date", http.MethodPost, "/api/expenses", `{"amount":1,"description":"кофе","date":"вчера"}`, "$"},
		{"missing country", http.MethodPost, "/api/trips/arrive", `{"city":"Пхукет"}`, "country_name"},
		{"missing currency", http.MethodPut, "/api/trips/currency", `{}`, "currency"},
		{"bad tripId in body", http.MethodPut, "/api/trips/currency", `{"currency":"USD","trip_id":"x"}`, "$"},
		{"empty timezone body", http.MethodPut, "/api/trips/timezone", ``, ""},
		{"bad tripId query", http.MethodGet, "/api/expenses?trip_id=abc", "", "trip_id"},
		{"bad categoryId query", http.MethodGet, "/api/expenses?category_id=x", "", "category_id"},
		{"bad date query", http.MethodGet, "/api/expenses/total?from=16-09-2026", "", "from"},
		{"bad photos tripId", http.MethodGet, "/api/trips/photos?trip_id=nope", "", "trip_id"},
		{"bad bool", http.MethodGet, "/api/expenses/categories?include_keywords=yes", "", "include_keywords"},
		{"empty bool", http.MethodGet, "/api/expenses/report/builder?include_expenses=", "", "include_expenses"},
		{"keywords missing", http.MethodPost, "/api/expenses/categories", `{"name":"Спорт"}`, "keywords"},
		{"category id not a number", http.MethodPut, "/api/expenses/categories/abc", `{"keywords":[]}`, "id"},
		{"sync not JSON", http.MethodPost, "/api/sync", `operations`, "$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.do(tripsRequest{method: tc.method, path: tc.path, body: jsonBody(tc.body), headers: bearer()})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d %s, want 400", rec.Code, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
				t.Fatalf("Content-Type = %q", ct)
			}
			var problem trips.ValidationProblem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if problem.Type != trips.ValidationProblemType || problem.Title != trips.ValidationProblemTitle ||
				problem.Status != 400 || !strings.HasPrefix(problem.TraceID, "00-") {
				t.Fatalf("envelope = %+v", problem)
			}
			if _, ok := problem.Errors[tc.errorKey]; !ok {
				t.Fatalf("errors = %v, want key %q", problem.Errors, tc.errorKey)
			}
		})
	}

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/expenses/categories", body: jsonBody(`{"name":"Спорт","keywords":[]}`), headers: botHeaders()})
	if rec.Code != http.StatusOK {
		t.Fatalf("empty keywords: got %d %s", rec.Code, rec.Body)
	}

	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses?trip_id=&category_id=", headers: bearer()})
	if rec.Code != http.StatusOK || env.expenses.list.TripID != nil || env.expenses.list.CategoryID != nil {
		t.Fatalf("empty query values: got %d, query %+v", rec.Code, env.expenses.list)
	}
}

func TestTripsCaseInsensitiveBinding(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/trips", headers: bearer(),
		body: jsonBody(`{"NAME":"Asia 2026","COUNTRY_NAME":"Таиланд","City":"Пхукет","BUDGET":"5000","currency_code":"thb","unknown":1}`)})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: got %d %s", rec.Code, rec.Body)
	}
	cmd := env.trips.create
	if cmd.Name != "Asia 2026" || *cmd.CountryName != "Таиланд" || *cmd.City != "Пхукет" ||
		cmd.Budget.String() != "5000" || *cmd.CurrencyCode != "thb" || cmd.Source != nil {
		t.Fatalf("command = %+v", cmd)
	}

	rec = env.do(tripsRequest{method: http.MethodPut, path: "/api/trips/timezone", headers: bearer(),
		body: jsonBody(`{"Latitude":"7.88","LONGTITUDE":98.39}`)})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"timezone":"UTC+7"}` ||
		env.trips.timezone.Latitude != 7.88 || env.trips.timezone.Longitude != 98.39 {
		t.Fatalf("timezone: got %d %s, command %+v", rec.Code, rec.Body, env.trips.timezone)
	}

	env.do(tripsRequest{method: http.MethodPut, path: "/api/trips/timezone", headers: bearer(), body: jsonBody(`{"latitude":7.88,"longitude":98.39}`)})
	if env.trips.timezone.Longitude != 0 {
		t.Fatalf("longitude = %v, want 0", env.trips.timezone.Longitude)
	}

	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/expenses", headers: botHeaders(),
		body: jsonBody(`{"Amount":350,"CURRENCY":"THB","Description":"кофе","date":"2026-09-16T00:00:00"}`)})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"category_name":"Кафе и рестораны","amount":350,"currency":"THB","id":241}` {
		t.Fatalf("add expense: got %d %s", rec.Code, rec.Body)
	}
	if add := env.expenses.add; add.Date == nil || add.Date.Format("2006-01-02T15:04:05Z07:00") != "2026-09-16T00:00:00Z" || *add.Currency != "THB" {
		t.Fatalf("add command = %+v", add)
	}

	tripID := uuid.New()
	env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses?TRIP_ID=" + tripID.String() + "&Category_Id=3", headers: bearer()})
	if env.expenses.list.TripID == nil || *env.expenses.list.TripID != tripID || *env.expenses.list.CategoryID != 3 {
		t.Fatalf("list query = %+v", env.expenses.list)
	}

	env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/total?from=2026-09-01&TO=2026-09-16", headers: bearer()})
	if env.expenses.total.From.String() != "2026-09-01" || env.expenses.total.To.String() != "2026-09-16" {
		t.Fatalf("total range = %+v", env.expenses.total)
	}

	env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/report/builder?dimensions=Trip,%20city&include_expenses=True", headers: bearer()})
	if env.reports.builder.Dimensions != "Trip, city" || !env.reports.builder.IncludeExpenses {
		t.Fatalf("builder query = %+v", env.reports.builder)
	}
}

func TestTripsRouteConstraintsAndUnknownPaths(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	for _, r := range []tripsRequest{
		{method: http.MethodDelete, path: "/api/expenses/abc"},
		{method: http.MethodPost, path: "/api/expenses/1.5/not-duplicate"},
		{method: http.MethodGet, path: "/api/trips/photos/not-a-guid"},
		{method: http.MethodDelete, path: "/api/trips/photos/not-a-guid"},
		{method: http.MethodDelete, path: "/api/trips/not-a-guid"},

		{method: http.MethodDelete, path: "/api/trips/photos"},
		{method: http.MethodGet, path: "/api/nothing-here"},
	} {
		rec := env.do(r)
		if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
			t.Fatalf("%s %s: got %d %q, want an empty 404", r.method, r.path, rec.Code, rec.Body.String())
		}
	}

	rec := env.do(tripsRequest{method: http.MethodDelete, path: "/api/expenses/42", headers: bearer()})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || env.expenses.deletedID != 42 {
		t.Fatalf("delete: got %d %q, id %d", rec.Code, rec.Body.String(), env.expenses.deletedID)
	}
	env.expenses.err = trips.NotFound(trips.MsgExpenseNotFound)
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/expenses/7/not-duplicate", headers: bearer()})
	assertTripsError(t, rec, http.StatusNotFound, trips.MsgExpenseNotFound)

	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/duplicates", headers: bearer()})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("duplicates error: got %d", rec.Code)
	}
	env.expenses.err = nil
	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/expenses/duplicates", headers: bearer()})
	if rec.Code != http.StatusOK || rec.Body.String() != "[]" {
		t.Fatalf("duplicates: got %d %s", rec.Code, rec.Body)
	}
}

func TestTripsCORS(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	rec := env.do(tripsRequest{method: http.MethodOptions, path: "/api/trips", headers: map[string]string{
		"Origin":                         "https://example.org",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	}})
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "POST" ||
		rec.Header().Get("Access-Control-Allow-Headers") != "authorization,content-type" {
		t.Fatalf("preflight: got %d %v", rec.Code, rec.Header())
	}
	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/trips", headers: withHeader(bearer(), "Origin", "https://example.org")})
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("simple request: got %d %v", rec.Code, rec.Header())
	}
}

func multipartPhoto(t *testing.T, fields map[string]string, fileField, fileName, contentType string, content []byte) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if fileField != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+fileField+`"; filename="`+fileName+`"`)
		header.Set("Content-Type", contentType)
		part, err := w.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestTripsPhotoUpload(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	tripID := uuid.New()

	body, ct := multipartPhoto(t, map[string]string{"TRIP_ID": tripID.String(), "caption": "Пляж"}, "File", "photos/beach.PNG", "IMAGE/PNG", []byte("png-bytes"))
	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: body, headers: withHeader(bearer(), "Content-Type", ct)})
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: got %d %s", rec.Code, rec.Body)
	}
	add := env.photos.add
	if add.TripID == nil || *add.TripID != tripID || add.Caption == nil || *add.Caption != "Пляж" ||
		add.ContentType != "IMAGE/PNG" || add.Size != 9 || string(env.photos.addedBody) != "png-bytes" || add.FileName != "beach.PNG" {
		t.Fatalf("command = %+v body %q", add, env.photos.addedBody)
	}

	body, ct = multipartPhoto(t, map[string]string{"caption": "x"}, "", "", "", nil)
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: body, headers: withHeader(bearer(), "Content-Type", ct)})
	assertEnvelopeKey(t, rec, "file")
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: jsonBody(`{}`), headers: withHeader(bearer(), "Content-Type", "application/json")})
	assertEnvelopeKey(t, rec, "file")

	body, ct = multipartPhoto(t, map[string]string{"trip_id": "nope"}, "file", "a.jpg", "image/jpeg", []byte("x"))
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: body, headers: withHeader(bearer(), "Content-Type", ct)})
	assertEnvelopeKey(t, rec, "trip_id")

	env.photos.err = trips.Validation("Файл больше 15 МБ")
	body, ct = multipartPhoto(t, nil, "file", "a.jpg", "image/jpeg", []byte("x"))
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: body, headers: withHeader(bearer(), "Content-Type", ct)})
	assertTripsError(t, rec, http.StatusBadRequest, "Файл больше 15 МБ")
}

func TestTripsPhotoUploadOverRequestLimit(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	big := bytes.Repeat([]byte{0xff}, int(trips.MaxPhotoRequestBytes)+1)
	body, ct := multipartPhoto(t, nil, "file", "big.jpg", "image/jpeg", big)
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/trips/photos", body: bytes.NewReader(raw), headers: withHeader(bearer(), "Content-Type", ct)})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared length: got %d %s", rec.Code, rec.Body)
	}

	req, _ := http.NewRequest(http.MethodPost, "/api/trips/photos", io.MultiReader(bytes.NewReader(raw)))
	req.ContentLength = -1
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Authorization", "Bearer good")
	rec2 := newRecorder()
	env.engine.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked: got %d %s", rec2.Code, rec2.Body)
	}
	if env.photos.add.Size != 0 {
		t.Fatal("the service saw an oversized upload")
	}
}

func TestTripsPhotoDownload(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	env.photos.download = trips.PhotoDownload{ContentType: "image/png", FileName: "photo.png", Body: io.NopCloser(strings.NewReader("png"))}
	rec := env.do(tripsRequest{method: http.MethodGet, path: "/api/trips/photos/" + uuid.NewString(), headers: bearer()})
	if rec.Code != http.StatusOK || rec.Body.String() != "png" || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("Content-Disposition") != "attachment; filename=photo.png; filename*=UTF-8''photo.png" {
		t.Fatalf("download: got %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	env.photos.download = trips.PhotoDownload{}
	env.photos.err = trips.NotFound(trips.MsgPhotoFileLost)
	rec = env.do(tripsRequest{method: http.MethodGet, path: "/api/trips/photos/" + uuid.NewString(), headers: bearer()})
	assertTripsError(t, rec, http.StatusNotFound, trips.MsgPhotoFileLost)

	env.photos.err = nil
	rec = env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/photos/" + uuid.NewString(), headers: bearer()})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestContentDisposition(t *testing.T) {
	for name, want := range map[string]string{
		"photo.png":     "attachment; filename=photo.png; filename*=UTF-8''photo.png",
		"my photo.png":  `attachment; filename="my photo.png"; filename*=UTF-8''my%20photo.png`,
		"фото.jpg":      "attachment; filename=____.jpg; filename*=UTF-8''%D1%84%D0%BE%D1%82%D0%BE.jpg",
		`say "hi".heic`: `attachment; filename="say \"hi\".heic"; filename*=UTF-8''say%20%22hi%22.heic`,
	} {
		if got := contentDisposition(name); got != want {
			t.Errorf("contentDisposition(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestTripsSyncWiring(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	opID := uuid.New()
	env.sync.response = trips.SyncResponse{Results: []json.RawMessage{json.RawMessage(`{"op_id":"` + opID.String() + `","status":"blocked"}`)}}

	body := `{"Operations":[{"op_id":"` + opID.String() + `","type":"expense.add","occurred_at":"2026-09-16T10:00:00+07:00","payload":{"client_id":"` + uuid.NewString() + `","amount":"350"}}]}`
	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/sync", body: jsonBody(body), headers: bearer()})
	want := `{"results":[{"op_id":"` + opID.String() + `","status":"blocked"}],"active_trip":null}`
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("sync: got %d %s, want %s", rec.Code, rec.Body, want)
	}
	ops := env.sync.request.Operations
	if ops == nil || len(*ops) != 1 || (*ops)[0].OpID != opID || (*ops)[0].Type != trips.OpExpenseAdd ||
		(*ops)[0].OccurredAt == nil || !strings.Contains(string((*ops)[0].Payload), `"amount":"350"`) || env.sync.actor.Mode != trips.ModeApp {
		t.Fatalf("request = %+v", env.sync.request)
	}
	if _, offset := (*ops)[0].OccurredAt.Zone(); offset != 7*3600 {
		t.Fatalf("occurredAt lost its offset: %v", (*ops)[0].OccurredAt)
	}

	env.sync.response = trips.SyncResponse{}
	env.do(tripsRequest{method: http.MethodPost, path: "/api/sync", body: jsonBody(`{}`), headers: bearer()})
	if env.sync.request.Operations != nil {
		t.Fatal("a missing operations must reach the service as nil")
	}
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/sync", body: jsonBody(`{"operations":[]}`), headers: bearer()})
	if rec.Body.String() != `{"results":[],"active_trip":null}` {
		t.Fatalf("empty sync: %s", rec.Body)
	}

	env.sync.err = trips.Validation("Нет списка операций")
	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/sync", body: jsonBody(`{"operations":null}`), headers: bearer()})
	assertTripsError(t, rec, http.StatusBadRequest, "Нет списка операций")
}

func assertTripsError(t *testing.T, rec *recorder, status int, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body, status)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	want, _ := json.Marshal(trips.ErrorResponse{Error: message})
	if rec.Body.String() != string(want) {
		t.Fatalf("body = %s, want %s", rec.Body, want)
	}
}

func assertEnvelopeKey(t *testing.T, rec *recorder, key string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", rec.Code, rec.Body)
	}
	var problem trips.ValidationProblem
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if _, ok := problem.Errors[key]; !ok {
		t.Fatalf("errors = %v, want key %q", problem.Errors, key)
	}
}

func TestTripsDeleteTrip(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)
	tripID := uuid.New()

	rec := env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/" + tripID.String(), headers: bearer()})
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("got %d %q, want an empty 204", rec.Code, rec.Body.String())
	}
	if env.trips.deletedID != tripID || env.trips.actor.Mode != trips.ModeApp {
		t.Fatalf("id %s, actor %+v", env.trips.deletedID, env.trips.actor)
	}

	rec = env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/" + tripID.String(), headers: botHeaders()})
	if rec.Code != http.StatusNoContent || env.trips.actor.Mode != trips.ModeBot {
		t.Fatalf("bot mode: got %d, actor %+v", rec.Code, env.trips.actor)
	}

	env.trips.err = trips.NotFound(trips.MsgTripNotFound)
	rec = env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/" + uuid.New().String(), headers: bearer()})
	assertTripsError(t, rec, http.StatusNotFound, trips.MsgTripNotFound)

	env.trips.err = errors.New("disk on fire")
	rec = env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/" + tripID.String(), headers: bearer()})
	assertTripsError(t, rec, http.StatusInternalServerError, trips.MsgInternal)

	rec = env.do(tripsRequest{method: http.MethodDelete, path: "/api/trips/" + tripID.String()})
	if rec.Code != http.StatusUnauthorized || rec.Body.Len() != 0 {
		t.Fatalf("anonymous: got %d %q, want an empty 401", rec.Code, rec.Body.String())
	}
}

func TestTripsCreateStartDate(t *testing.T) {
	env := newTripsTestEnv(t, tripsTestInternalToken)

	rec := env.do(tripsRequest{method: http.MethodPost, path: "/api/trips", headers: bearer(),
		body: jsonBody(`{"name":"Asia 2026","start_date":"2026-09-01"}`)})
	if rec.Code != http.StatusOK {
		t.Fatalf("create: got %d %s", rec.Code, rec.Body)
	}
	if got := env.trips.create.StartDate; got == nil || *got != trips.NewDate(2026, 9, 1) {
		t.Fatalf("start date = %v", got)
	}

	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips", headers: bearer(),
		body: jsonBody(`{"name":"Asia 2026"}`)})
	if rec.Code != http.StatusOK || env.trips.create.StartDate != nil {
		t.Fatalf("absent: got %d, start date %v", rec.Code, env.trips.create.StartDate)
	}

	rec = env.do(tripsRequest{method: http.MethodPost, path: "/api/trips", headers: bearer(),
		body: jsonBody(`{"name":"Asia 2026","start_date":"01-09-2026"}`)})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"start_date":["`+trips.MsgStartDateFormat+`"]`) {
		t.Fatalf("bad date: got %d %s", rec.Code, rec.Body)
	}
}
