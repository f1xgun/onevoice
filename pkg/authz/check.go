package authz

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// BusinessContext is the per-request authorization payload that
// RequireBusinessAccess attaches to ctx. Handlers retrieve it via
// BusinessContextFromCtx; the runtime check Can() reads Permissions to
// answer permission lookups in O(n) on a tiny slice.
type BusinessContext struct {
	BusinessID  uuid.UUID
	UserID      uuid.UUID
	RoleID      uuid.UUID
	Permissions []Permission
}

// businessContextKey is unexported so callers cannot put a BusinessContext
// into ctx via context.WithValue directly; they must go through
// WithBusinessContext.
type businessContextKey struct{}

// WithBusinessContext returns a ctx carrying bc.
func WithBusinessContext(ctx context.Context, bc BusinessContext) context.Context {
	return context.WithValue(ctx, businessContextKey{}, bc)
}

// BusinessContextFromCtx retrieves the per-request BusinessContext.
// Returns (zero, false) when absent (e.g. handler not under
// /businesses/{id}/... — programmer error caught by lint-rbac).
func BusinessContextFromCtx(ctx context.Context) (BusinessContext, bool) {
	bc, ok := ctx.Value(businessContextKey{}).(BusinessContext)
	return bc, ok
}

// Can reports whether the request's BusinessContext grants perm.
// Returns false when (a) ctx has no BusinessContext, (b) the role's
// permissions slice does not contain perm. Always emits exactly one
// slog line + one rbac_check_total increment per call.
func Can(ctx context.Context, perm Permission) bool {
	bc, ok := BusinessContextFromCtx(ctx)
	if !ok {
		metrics.IncRBACCheck("missing")
		slog.LogAttrs(ctx, slog.LevelDebug, "rbac_check",
			slog.Bool("rbac.checked", true),
			slog.String("perm", string(perm)),
			slog.String("result", "missing"),
		)
		return false
	}
	for _, p := range bc.Permissions {
		if p == perm {
			metrics.IncRBACCheck("allow")
			slog.LogAttrs(ctx, slog.LevelDebug, "rbac_check",
				slog.Bool("rbac.checked", true),
				slog.String("perm", string(perm)),
				slog.String("result", "allow"),
				slog.String("business_id", bc.BusinessID.String()),
				slog.String("user_id", bc.UserID.String()),
			)
			return true
		}
	}
	metrics.IncRBACCheck("deny")
	slog.LogAttrs(ctx, slog.LevelDebug, "rbac_check",
		slog.Bool("rbac.checked", true),
		slog.String("perm", string(perm)),
		slog.String("result", "deny"),
		slog.String("business_id", bc.BusinessID.String()),
		slog.String("user_id", bc.UserID.String()),
	)
	return false
}
