package imagegen

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReserveTurnSlot_NoBudget_AlwaysAllows proves the cap is a no-op when no
// budget is attached — library callers and isolated tests are never blocked.
func TestReserveTurnSlot_NoBudget_AlwaysAllows(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		assert.True(t, ReserveTurnSlot(ctx, 2), "no budget in ctx must never cap")
	}
}

// TestReserveTurnSlot_EnforcesMax proves that with a budget attached the first
// max reservations pass and every one after is denied.
func TestReserveTurnSlot_EnforcesMax(t *testing.T) {
	ctx := WithTurnBudget(context.Background())
	assert.True(t, ReserveTurnSlot(ctx, 2), "1st image within cap")
	assert.True(t, ReserveTurnSlot(ctx, 2), "2nd image within cap")
	assert.False(t, ReserveTurnSlot(ctx, 2), "3rd image exceeds cap")
	assert.False(t, ReserveTurnSlot(ctx, 2), "4th image still capped (monotonic)")
}

// TestReserveTurnSlot_MaxDisablesCap proves max <= 0 disables the cap even when
// a budget is present.
func TestReserveTurnSlot_MaxDisablesCap(t *testing.T) {
	ctx := WithTurnBudget(context.Background())
	for i := 0; i < 5; i++ {
		assert.True(t, ReserveTurnSlot(ctx, 0), "max=0 disables the cap")
	}
	for i := 0; i < 5; i++ {
		assert.True(t, ReserveTurnSlot(ctx, -1), "negative max disables the cap")
	}
}

// TestReserveTurnSlot_ConcurrentBatch proves the shared atomic counter admits
// exactly max reservations across a parallel tool-call batch (go test -race).
func TestReserveTurnSlot_ConcurrentBatch(t *testing.T) {
	ctx := WithTurnBudget(context.Background())
	const maxPerTurn = 2
	const callers = 20

	var mu sync.Mutex
	admitted := 0
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ReserveTurnSlot(ctx, maxPerTurn) {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, maxPerTurn, admitted, "exactly maxPerTurn reservations may be admitted concurrently")
}

// TestWithTurnBudget_PerTurnIsolation proves each WithTurnBudget call starts a
// fresh count — one turn's exhaustion does not leak into the next.
func TestWithTurnBudget_PerTurnIsolation(t *testing.T) {
	base := context.Background()

	turn1 := WithTurnBudget(base)
	assert.True(t, ReserveTurnSlot(turn1, 1))
	assert.False(t, ReserveTurnSlot(turn1, 1), "turn 1 exhausted")

	turn2 := WithTurnBudget(base)
	assert.True(t, ReserveTurnSlot(turn2, 1), "turn 2 starts fresh")
}
