package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"lifelog-api/internal/trips"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxJSONBodyBytes = 30 * 1000 * 1000

type problems map[string][]string

func (p problems) add(key, message string) { p[key] = append(p[key], message) }

func (p problems) required(property, value string) {
	if value == "" {
		p.add(property, fmt.Sprintf("Поле %s обязательно", property))
	}
}

func (p problems) requiredList(property string, value []string) {
	if value == nil {
		p.add(property, fmt.Sprintf("Поле %s обязательно", property))
	}
}

func (p problems) writeIfAny(c *gin.Context) bool {
	if len(p) == 0 {
		return false
	}
	writeValidationProblem(c, p)
	return true
}

func writeValidationProblem(c *gin.Context, errs problems) {
	body, err := json.Marshal(trips.ValidationProblem{
		Type:    trips.ValidationProblemType,
		Title:   trips.ValidationProblemTitle,
		Status:  http.StatusBadRequest,
		Errors:  errs,
		TraceID: traceID(),
	})
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Abort()
	c.Data(http.StatusBadRequest, "application/problem+json; charset=utf-8", body)
}

func traceID() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return "00-" + hex.EncodeToString(b[:16]) + "-" + hex.EncodeToString(b[16:]) + "-00"
}

func bindJSON(c *gin.Context, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return false
		}
		writeValidationProblem(c, problems{"$": {"Не удалось прочитать тело запроса"}})
		return false
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		writeValidationProblem(c, problems{"": {"Тело запроса не может быть пустым"}})
		return false
	}

	if bytes.Equal(trimmed, []byte("null")) {
		writeValidationProblem(c, problems{"request": {"Тело запроса не может быть null"}})
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeValidationProblem(c, jsonProblem(err))
		return false
	}
	return true
}

func jsonProblem(err error) problems {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return problems{"$." + typeErr.Field: {fmt.Sprintf("Значение $.%s не подходит: ожидался тип %s", typeErr.Field, typeErr.Type)}}
	}
	return problems{"$": {"Тело запроса не разбирается как JSON: " + strings.TrimPrefix(err.Error(), "json: ")}}
}

type query struct {
	values url.Values
	errs   problems
}

func newQuery(c *gin.Context) *query {
	return &query{values: c.Request.URL.Query(), errs: problems{}}
}

func (q *query) get(name string) (string, bool) {
	if v, ok := q.values[name]; ok && len(v) > 0 {
		return v[0], true
	}
	for key, v := range q.values {
		if strings.EqualFold(key, name) && len(v) > 0 {
			return v[0], true
		}
	}
	return "", false
}

func (q *query) invalid(name, raw string) {
	q.errs.add(name, fmt.Sprintf("Значение '%s' не подходит для %s", raw, name))
}

func (q *query) guid(name string) *uuid.UUID {
	raw, ok := q.get(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		q.invalid(name, raw)
		return nil
	}
	return &id
}

func (q *query) long(name string) *int64 {
	raw, ok := q.get(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		q.invalid(name, raw)
		return nil
	}
	return &n
}

func (q *query) day(name string) *trips.Date {
	raw, ok := q.get(name)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil
	}
	d, err := trips.ParseDate(strings.TrimSpace(raw))
	if err != nil {
		q.invalid(name, raw)
		return nil
	}
	return &d
}

func (q *query) flag(name string) bool {
	raw, ok := q.get(name)
	if !ok {
		return false
	}
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil || !isDotNetBool(strings.TrimSpace(raw)) {
		if strings.TrimSpace(raw) == "" {
			q.errs.add(name, fmt.Sprintf("Значение '%s' недопустимо", raw))
		} else {
			q.invalid(name, raw)
		}
		return false
	}
	return b
}

func isDotNetBool(s string) bool {
	return strings.EqualFold(s, "true") || strings.EqualFold(s, "false")
}

func (q *query) text(name string) string {
	raw, _ := q.get(name)
	return raw
}

func (q *query) writeIfInvalid(c *gin.Context) bool { return q.errs.writeIfAny(c) }
