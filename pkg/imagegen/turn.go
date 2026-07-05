package imagegen

import (
	"context"
	"sync/atomic"
)

type turnBudgetKey struct{}

// turnBudget counts the images reserved within a single agent turn. A pointer to
// it is stored in the context so the tool-dispatch goroutines of one parallel
// tool-call batch share a single atomic counter — the per-turn cap therefore
// holds even when the model emits several generate_image calls at once.
type turnBudget struct{ used atomic.Int64 }

// WithTurnBudget returns a context carrying a fresh per-turn image budget. The
// orchestrator attaches one at the start of every agent turn (fresh run and
// resume) so the generate_image executor can enforce a per-turn image cap
// without owning any turn-scoped state of its own. A context with no budget
// attached leaves the cap a no-op (ReserveTurnSlot returns true), which keeps
// library callers and isolated tests unaffected.
func WithTurnBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, turnBudgetKey{}, &turnBudget{})
}

// ReserveTurnSlot atomically claims one image slot for the current turn and
// reports whether the reservation is within maxPerTurn. It returns true
// (allowed) when no budget is attached to ctx or when maxPerTurn <= 0 (cap
// disabled); otherwise it increments the shared counter and returns false once
// the count would exceed maxPerTurn. Because the increment happens even for a
// denied call, a turn that hits the cap stays capped for every subsequent call
// — the comparison is monotonic.
func ReserveTurnSlot(ctx context.Context, maxPerTurn int) bool {
	b, ok := ctx.Value(turnBudgetKey{}).(*turnBudget)
	if !ok || maxPerTurn <= 0 {
		return true
	}
	return b.used.Add(1) <= int64(maxPerTurn)
}
