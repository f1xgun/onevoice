// Package lockout implements a Redis-backed per-(email_hash, /16 IP) failure
// counter for brute-force / credential-stuffing defense on /auth/login.
//
// Layered defense:
//
//	0–3 failures  → TierNormal  (login proceeds normally)
//	4–9 failures  → TierCaptcha (handler enforces SmartCaptcha challenge)
//	10+ failures  → TierLocked  (middleware short-circuits with 423)
//
// Key format: "onevoice:lockout:" + sha256_hex(lowercase(email)) + ":" + net16(ip).
// The email is hashed so a Redis dump cannot enumerate the registered email
// allowlist. The IP is bucketed at the /16 to absorb shared-NAT collisions
// while still binding to the network neighborhood — an attacker from a
// different /16 cannot lock out a legitimate user.
//
// Self-unlock: on successful password-reset the service calls
// ClearAllForEmail to remove every per-/16 variant of the email_hash key.
//
// Atomicity: RecordFailure runs INCR and the conditional PEXPIRE in a single
// Lua round trip, so a lost EXPIRE can never leave the counter locked with no
// TTL — the lock always carries Config.Duration and auto-unlocks. A key found
// TTL-less by an earlier missing expiry self-heals on the next failure. Tested
// with 50 concurrent goroutines yielding count==50
// (TestRecordFailure_IncrementsAtomically) and with a TTL-less heal path
// (TestRecordFailure_HealsMissingTTL).
package lockout
