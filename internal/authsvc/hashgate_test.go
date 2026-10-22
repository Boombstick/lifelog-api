package authsvc

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHashSlotGivesUpWhenContextEnds(t *testing.T) {
	for range cap(hashSlots) {
		hashSlots <- struct{}{}
	}
	defer func() {
		for range cap(hashSlots) {
			<-hashSlots
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	called := false
	_, err := withHashSlot(ctx, func() (bool, error) {
		called = true
		return true, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if called {
		t.Fatal("fn ran without a free slot")
	}
}

func TestHashSlotReleasesAfterUse(t *testing.T) {
	for range cap(hashSlots) * 3 {
		if _, err := withHashSlot(context.Background(), func() (int, error) { return 1, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if len(hashSlots) != 0 {
		t.Fatalf("%d slots still held", len(hashSlots))
	}
}
