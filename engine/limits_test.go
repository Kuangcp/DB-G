package engine

import "testing"

func TestResultBudgetDefaultAndClamp(t *testing.T) {
	t.Cleanup(func() { SetResultBudgetMB(DefaultResultBudgetMB) })
	if got := ResultBudgetBytes(); got != DefaultResultBudgetMB<<20 {
		t.Fatalf("default budget = %d", got)
	}
	SetResultBudgetMB(12)
	if got := ResultBudgetBytes(); got != 12<<20 {
		t.Fatalf("12MB budget = %d", got)
	}
	SetResultBudgetMB(-3)
	if got := ResultBudgetBytes(); got != MinResultBudgetMB<<20 {
		t.Fatalf("clamp min = %d", got)
	}
	SetResultBudgetMB(10000)
	if got := ResultBudgetBytes(); got != MaxResultBudgetMB<<20 {
		t.Fatalf("clamp max = %d", got)
	}
}
