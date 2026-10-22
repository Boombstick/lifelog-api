package textutil

import (
	"testing"
	"unicode/utf8"
)

func TestTruncateCutsRunesNotBytes(t *testing.T) {
	t.Parallel()

	long := "Мозилла/5.0 очень длинная строка агента пользователя, которая не влезает"

	got := Truncate(long, 10)

	if !utf8.ValidString(got) {
		t.Fatalf("строка перестала быть валидным UTF-8: %q", got)
	}
	if runes := []rune(got); len(runes) != 10 {
		t.Errorf("символов %d, ожидалось 10: %q", len(runes), got)
	}
	if short := Truncate("коротко", 50); short != "коротко" {
		t.Errorf("короткая строка изменилась: %q", short)
	}
}

func TestFoldYo(t *testing.T) {
	if got := FoldYo("Вильнёв ЁЖ"); got != "Вильнев ЕЖ" {
		t.Fatalf("FoldYo = %q", got)
	}
}
