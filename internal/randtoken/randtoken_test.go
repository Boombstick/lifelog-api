package randtoken

import (
	"encoding/base64"
	"testing"
)

func TestNewLengthAndUniqueness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		token := New(32)
		raw, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(raw) != 32 {
			t.Fatalf("token %q: %d bytes, err %v", token, len(raw), err)
		}
		if seen[token] {
			t.Fatalf("token repeated: %q", token)
		}
		seen[token] = true
	}
}

func TestHashIsHexSHA256(t *testing.T) {
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := Hash("hello"); got != want {
		t.Fatalf("Hash(hello) = %s", got)
	}
}
