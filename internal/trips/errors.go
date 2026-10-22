package trips

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Kind int

const (
	KindValidation Kind = iota + 1

	KindNotFound

	KindConflict

	KindUnauthorized

	KindForbidden
)

func (k Kind) HTTPStatus() int {
	switch k {
	case KindValidation:
		return http.StatusBadRequest
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

type Error struct {
	Kind    Kind
	Message string

	Retryable bool
}

func (e *Error) Error() string { return e.Message }

func Validation(msg string) *Error { return &Error{Kind: KindValidation, Message: msg} }
func NotFound(msg string) *Error   { return &Error{Kind: KindNotFound, Message: msg} }
func Conflict(msg string) *Error   { return &Error{Kind: KindConflict, Message: msg} }
func Forbidden(msg string) *Error  { return &Error{Kind: KindForbidden, Message: msg} }

func Validationf(format string, args ...any) *Error {
	return Validation(fmt.Sprintf(format, args...))
}

func FieldTooLong(field string, max int) *Error {
	return Validationf("Поле %s не может быть длиннее %d символов", field, max)
}

func UnknownCurrency(code string) *Error {
	return Validationf(MsgCurrencyNotFound, code)
}

func NoSnapshots(msg string) *Error {
	return &Error{Kind: KindConflict, Message: msg, Retryable: true}
}

func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func StatusOf(err error) int {
	if e, ok := AsError(err); ok {
		return e.Kind.HTTPStatus()
	}
	return http.StatusInternalServerError
}

func PublicMessage(err error) string {
	if e, ok := AsError(err); ok {
		return e.Message
	}
	return MsgInternal
}

type IntegrityError struct {
	SQLState   string
	Constraint string
	Message    string
}

func (e *IntegrityError) Error() string { return e.Message }

func (e *IntegrityError) IsUniqueViolation() bool { return e.SQLState == "23505" }

func AsIntegrityError(err error) (*IntegrityError, bool) {
	var e *IntegrityError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func IsIntegrityClass(sqlState string) bool { return strings.HasPrefix(sqlState, "23") }

func IsDataExceptionClass(sqlState string) bool { return strings.HasPrefix(sqlState, "22") }

func sqlStateOf(err error) (string, bool) {
	if integrity, ok := AsIntegrityError(err); ok {
		return integrity.SQLState, true
	}
	var stater interface{ SQLState() string }
	if errors.As(err, &stater) {
		return stater.SQLState(), true
	}
	return "", false
}

func DataExceptionMessage(sqlState string) string {
	if sqlState == "22003" {
		return "Число вне допустимого диапазона"
	}
	return "Некорректное значение в данных операции"
}

var ErrActiveTripExists = errors.New("trips: user already has an active trip")

const (
	MsgInternal = "Внутренняя ошибка сервиса"

	MsgTripNotFound        = "Поездка не найдена"
	MsgNoActiveTrip        = "У вас нет активной поездки"
	MsgNoActiveTrips       = "Нет активных поездок"
	MsgSequenceMoreThanOne = "Найдено несколько записей там, где ожидалась одна"

	MsgActiveTripExists   = "У вас уже есть активная поездка '%s'. Завершите её командой /trip_end перед созданием новой."
	MsgCountryUnsupported = "Выбранная страна не поддерживается или не существует"

	MsgCurrencyNotFound = "Валюта %s не найдена"

	MsgExpenseNotFound = "Трата не найдена"
	MsgPhotoNotFound   = "Фотография не найдена"
	MsgPhotoFileLost   = "Файл фотографии потерян"

	MsgNoSnapshotsLatest  = "Курсы валют недоступны: в базе их нет, а сервис курсов не отвечает"
	MsgNoSnapshotsClosest = "Курсы валют недоступны: в базе их нет"

	MsgForbidden = "Недостаточно прав"

	MsgSyncRetry = "Временная ошибка сервиса, операция будет повторена"
)

func IntegrityMessage(e *IntegrityError) string {
	switch e.SQLState {
	case "23505":
		return "Такая запись уже есть"
	case "23503":
		return "Связанная запись не найдена"
	case "23502":
		return "Не заполнено обязательное поле"
	case "23514":
		return "Значение вне допустимого диапазона"
	default:
		return "Данные не прошли проверку"
	}
}
