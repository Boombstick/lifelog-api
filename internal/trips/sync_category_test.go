package trips

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

type txSpySearch struct {
	store    *m4Store
	calls    int
	insideTx bool
}

func (s *txSpySearch) Search(ctx context.Context, description string) (*CategoryRef, error) {
	s.calls++
	if s.store.txDepth > 0 {
		s.insideTx = true
	}
	return &CategoryRef{ID: 3, Name: "Кафе и рестораны"}, nil
}

func TestSyncExpenseCategoryResolvedOutsideTransaction(t *testing.T) {
	f := m4NewSync()
	spy := &txSpySearch{store: f.store}
	f.svc.expenses.categories = spy

	start := NewDate(2026, 9, 10)
	trip := f.store.putTrip(Trip{UserID: f.actor.UserID, Name: "server", CurrencyCode: ptr("THB"), StartDate: &start})
	op := m4Op(t, OpExpenseAdd, "2026-09-16T11:00:00+07:00", map[string]any{
		"client_id": uuid.New(), "trip": map[string]any{"id": trip.ID}, "amount": 350, "description": "кофе",
	})

	resp := f.run(t, op)
	result := m4WantStatus(t, resp.Results[0], SyncStatusOK)
	m4Expect(t, "search calls", spy.calls, 1)
	if spy.insideTx {
		t.Fatal("category search ran inside the transaction")
	}
	if result.Expense == nil {
		t.Fatalf("no expense in result: %s", resp.Results[0])
	}
}

type blockingSearch struct{}

func (blockingSearch) Search(ctx context.Context, description string) (*CategoryRef, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestResolveCategoryFallsBackOnSlowSearch(t *testing.T) {
	svc := &ExpenseService{categories: blockingSearch{}, logger: quietLogger}
	started := time.Now()
	got := svc.ResolveCategory(context.Background(), "кофе")
	if elapsed := time.Since(started); elapsed > 5*categorySearchTimeout {
		t.Fatalf("search took %v", elapsed)
	}
	m4Expect(t, "fallback", got, CategoryRef{ID: OtherCategoryID, Name: OtherCategoryName})
}
