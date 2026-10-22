package model

import (
	"regexp"
	"testing"
)

func TestNotFoundTextsAreRussian(t *testing.T) {
	latin := regexp.MustCompile(`[A-Za-z]`)
	for _, entity := range []string{"film", "film entry", "person", "place", "share link", "shared list", "shared list item", "user", "something new"} {
		text := NewEntityNotFound(entity).Error()
		if latin.MatchString(text) {
			t.Errorf("%q: %q has Latin letters", entity, text)
		}
	}
	if got := NewEntityNotFound("film").Error(); got != "Фильм не найден" {
		t.Errorf("film: %q", got)
	}
}
