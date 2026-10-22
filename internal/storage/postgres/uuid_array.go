package postgres

import (
	"strings"

	"github.com/google/uuid"
)

func uuidArray(ids []uuid.UUID) string {
	var b strings.Builder
	b.Grow(len(ids)*37 + 2)
	b.WriteByte('{')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(id.String())
	}
	b.WriteByte('}')
	return b.String()
}
