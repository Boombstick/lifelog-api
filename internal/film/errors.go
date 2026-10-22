package film

import "errors"

var ErrRatedCannotUnwatch = errors.New("Сначала уберите оценку, потом снимайте отметку «просмотрено»")

var ErrUnknownEntrySet = errors.New("Неизвестный набор: watchlist, watched, watching или rated")

var ErrSortNotApplicable = errors.New("Такая сортировка недоступна для этого набора")
