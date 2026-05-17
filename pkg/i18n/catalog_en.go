package i18n

// en is the English catalog. Keys may be omitted while migration is in
// progress — TrTag falls back to the ru catalog when a key is missing here.
//
// Phase A1 ships a single sentinel entry; real strings get migrated in
// Phase C of `.planning/i18n-readiness/PLAN.md`.
var en = map[string]string{
	"test.hello": "Hello, %s",
}
