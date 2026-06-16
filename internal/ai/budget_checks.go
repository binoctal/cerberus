package ai

import (
	"fmt"
)

// checkBudgetExhausted verifies the token budget is not exhausted.
// Returns error if budget is exhausted.
func checkBudgetExhausted(budget *TokenBudget) error {
	if budget.Exhausted() {
		return fmt.Errorf("token budget exhausted")
	}
	return nil
}

// checkBudgetCapacity verifies the budget can afford a call.
// Returns error if remaining budget is insufficient for PerCallLimit.
func checkBudgetCapacity(budget *TokenBudget) error {
	if !budget.CanSpend(budget.PerCallLimit) {
		return fmt.Errorf("insufficient budget: remaining %d, need up to %d",
			budget.Remaining(), budget.PerCallLimit)
	}
	return nil
}

// checkBudget validates both exhaustion and capacity in sequence.
// This is the standard budget check before making LLM calls.
func checkBudget(budget *TokenBudget) error {
	if err := checkBudgetExhausted(budget); err != nil {
		return err
	}
	return checkBudgetCapacity(budget)
}
