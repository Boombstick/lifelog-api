package textutil

import (
	"strings"
	"unicode/utf8"
)

func Truncate(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

var yoFolder = strings.NewReplacer("ё", "е", "Ё", "Е")

func FoldYo(value string) string {
	return yoFolder.Replace(value)
}
