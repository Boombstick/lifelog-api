package model

type EntityNotFoundError struct {
	EntityName string
}

var notFoundTexts = map[string]string{
	"film":             "Фильм не найден",
	"film entry":       "Отметка о фильме не найдена",
	"person":           "Человек не найден",
	"place":            "Место не найдено",
	"share link":       "Ссылка не найдена или истекла",
	"shared list":      "Общий список не найден",
	"shared list item": "Фильма нет в общем списке",
	"user":             "Пользователь не найден",
}

func (e *EntityNotFoundError) Error() string {
	if text, ok := notFoundTexts[e.EntityName]; ok {
		return text
	}
	return "Не найдено"
}

func NewEntityNotFound(entity string) error {
	return &EntityNotFoundError{EntityName: entity}
}
