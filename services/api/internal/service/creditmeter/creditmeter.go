// Package creditmeter holds the pure billing arithmetic that splits a credit
// charge against a business's running balance. It has ZERO dependencies (no DB,
// no service, no repository) so it is safe to import from the repository layer
// that owns the credit_ledger SQL.
//
// The rule: consume as many credits as the balance allows (down to 0), and
// record whatever is left as overage. The balance never goes negative.
package creditmeter

import "github.com/f1xgun/onevoice/pkg/domain"

// Result is the shape of the credit_ledger row a single charge produces.
type Result struct {
	// Delta is the signed change applied to the balance (always <= 0 for a
	// charge — it is the portion drawn from the existing balance).
	Delta int
	// BalanceAfter is the running balance after the charge; never negative.
	BalanceAfter int
	// OverageCredits is the portion of the charge that exceeded the balance.
	OverageCredits int
	// Reason is domain.CreditReasonConsume when the charge was fully covered by
	// the balance, or domain.CreditReasonOverage when it spilled past zero.
	Reason string
}

// Compute splits a `consume`-credit charge against prevBalance.
//
//   - prevBalance >= consume  → fully covered: Delta = -consume,
//     BalanceAfter = prevBalance-consume, no overage, reason "consume".
//   - prevBalance <  consume  → partially covered: draw the remaining balance
//     down to 0 (Delta = -prevBalance) and record the rest as overage,
//     BalanceAfter = 0, reason "overage".
//
// A non-positive consume is treated as a no-op consume row (Delta 0) so callers
// never have to guard the zero case.
func Compute(prevBalance, consume int) Result {
	if consume <= 0 {
		return Result{Delta: 0, BalanceAfter: max(prevBalance, 0), OverageCredits: 0, Reason: domain.CreditReasonConsume}
	}
	if prevBalance >= consume {
		return Result{
			Delta:          -consume,
			BalanceAfter:   prevBalance - consume,
			OverageCredits: 0,
			Reason:         domain.CreditReasonConsume,
		}
	}
	drawn := prevBalance
	if drawn < 0 {
		drawn = 0
	}
	return Result{
		Delta:          -drawn,
		BalanceAfter:   0,
		OverageCredits: consume - drawn,
		Reason:         domain.CreditReasonOverage,
	}
}
