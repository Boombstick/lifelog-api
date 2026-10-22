package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"lifelog-api/internal/httpapi"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const malformedRequest = "Некорректный запрос"

var requiredFieldMessages = map[string]string{
	"email":            "Укажите почту",
	"login":            "Укажите логин",
	"password":         "Укажите пароль",
	"new_password":     "Укажите новый пароль",
	"current_password": "Укажите текущий пароль",
	"code":             "Укажите код",
	"refresh_token":    "Нужен refresh_token",
}

func bindJSONRequest(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": httpapi.BodyTooLargeText})
			return false
		}
		badRequest(c, bindErrorMessage(err, dst))
		return false
	}
	return true
}

func bindErrorMessage(err error, dst any) string {
	var invalid validator.ValidationErrors
	if errors.As(err, &invalid) && len(invalid) > 0 {
		first := invalid[0]
		name := jsonFieldName(dst, first.StructField())
		if first.Tag() == "required" {
			if message, ok := requiredFieldMessages[name]; ok {
				return message
			}
			return "Не заполнено поле «" + name + "»"
		}
		return "Некорректное значение поля «" + name + "»"
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return "Неверный тип поля «" + typeErr.Field + "»"
	}
	return malformedRequest
}

func jsonFieldName(dst any, structField string) string {
	t := reflect.TypeOf(dst)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return structField
	}
	field, ok := t.FieldByName(structField)
	if !ok {
		return structField
	}
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "" || name == "-" {
		return structField
	}
	return name
}
