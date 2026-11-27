package engine

import (
	"context"
	"testing"
)

func TestResultBudgetContext(t *testing.T) {
	ctx := context.Background()
	if got := ResultBudget(ctx); got != DefaultResultBudgetMB<<20 {
		t.Fatalf("default budget = %d", got)
	}
	ctx = WithResultBudget(ctx, 12)
	if got := ResultBudget(ctx); got != 12<<20 {
		t.Fatalf("12MB budget = %d", got)
	}
	if got := ResultBudget(WithResultBudget(context.Background(), -3)); got != MinResultBudgetMB<<20 {
		t.Fatalf("clamp min = %d", got)
	}
	if got := ResultBudget(WithResultBudget(context.Background(), 10000)); got != MaxResultBudgetMB<<20 {
		t.Fatalf("clamp max = %d", got)
	}
}

// 预算挂在 ctx 上，同一进程里的多次执行互不影响。
func TestResultBudgetIsolation(t *testing.T) {
	a := WithResultBudget(context.Background(), 3)
	b := WithResultBudget(context.Background(), 5)
	if got := ResultBudget(a); got != 3<<20 {
		t.Fatalf("a budget = %d", got)
	}
	if got := ResultBudget(b); got != 5<<20 {
		t.Fatalf("b budget = %d", got)
	}
	if got := ResultBudget(context.Background()); got != DefaultResultBudgetMB<<20 {
		t.Fatalf("default budget was mutated: %d", got)
	}
}
