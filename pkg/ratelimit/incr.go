package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// incrWithHealScript atomically increments a fixed-window counter and
// (re-)stamps its TTL only when the key has none (PTTL < 0). Running INCR and
// the TTL repair in a single Lua round trip closes the window where a separate
// EXPIRE could be lost to a transient Redis error — which would otherwise leave
// the counter TTL-less and throttle the caller forever. The PTTL guard
// preserves fixed-window semantics: a live, in-progress window is never
// extended, only a missing TTL is repaired.
//
// KEYS[1] = counter key. ARGV[1] = window in milliseconds.
// Returns the post-increment count.
var incrWithHealScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if redis.call('PTTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return count
`)

// IncrWithHeal increments the fixed-window counter at key and returns the new
// value. It self-heals a missing TTL in the same atomic round trip, so a lost
// EXPIRE can never leave the counter blocked forever. A Redis/script error is
// returned to the caller so each gate can keep its own fail-open / fail-closed
// behavior rather than having a policy imposed here.
func IncrWithHeal(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error) {
	return incrWithHealScript.Run(ctx, client, []string{key}, window.Milliseconds()).Int64()
}
