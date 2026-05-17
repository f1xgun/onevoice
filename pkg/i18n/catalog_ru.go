package i18n

// ru is the canonical (default + fallback) catalog. Every translatable key
// MUST exist here. Keys missing from en/ fall back to this map at runtime.
//
// Phase A1 ships a single sentinel entry; real strings get migrated in
// Phase C of `.planning/i18n-readiness/PLAN.md`.
var ru = map[string]string{
	"test.hello": "Привет, %s",
}
