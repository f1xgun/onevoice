package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const businessRoutePrefix = "/businesses/{id}"

// activeAllowlist holds the parsed allowlist entries that the analyzer
// consults before emitting a diagnostic. main.go assigns this from the
// CLI flag's parsed entries before invoking Analyzer.Run; tests assign
// directly. nil/empty means "no entries — every diagnostic is reported"
// (HI-02 wire-up; was previously unused dead code).
var activeAllowlist []AllowlistEntry

// Analyzer is the go/analysis.Analyzer that enforces AUTHZ-09.
// No Requires — the check is purely syntactic (no type info needed for
// the route-prefix detection per CONTEXT D-12 steps 1-3).
var Analyzer = &analysis.Analyzer{
	Name: "rbac",
	Doc:  "Detects business-scoped chi routes that skip authz.RequireBusinessAccess / authz.BusinessContextFromCtx / authz.Can.",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isChiRouteToBusinessSubtree(call) {
				return true
			}
			checkBusinessSubroute(pass, call)
			return true
		})
	}
	return nil, nil
}

// isChiRouteToBusinessSubtree returns true for r.Route("/businesses/{id}", func(...) { ... }).
func isChiRouteToBusinessSubtree(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Route" {
		return false
	}
	if len(call.Args) < 2 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.Trim(lit.Value, "\"") == businessRoutePrefix
}

// checkBusinessSubroute walks the func-literal body of r.Route(...) and
// confirms exactly one r.Use(authz.RequireBusinessAccess(...)) appears
// before any handler registration. If absent, every handler call is
// reported.
//
// NOTE: This check only scans the top-level statement list of the Route
// func literal. Handlers nested inside r.Group() blocks within the same
// Route are not inspected. If the router adopts r.Group() nesting under
// /businesses/{id}, extend this function with recursive AST descent.
func checkBusinessSubroute(pass *analysis.Pass, call *ast.CallExpr) {
	funcLit, ok := call.Args[1].(*ast.FuncLit)
	if !ok || funcLit.Body == nil {
		return
	}

	hasRequireBusinessAccess := false
	for _, stmt := range funcLit.Body.List {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if isAuthzRequireBusinessAccessUse(es.X) {
			hasRequireBusinessAccess = true
			break
		}
	}

	if hasRequireBusinessAccess {
		return
	}

	// Missing chokepoint — emit a diagnostic on every handler registration in
	// the body, UNLESS the route is in the allowlist (HI-02). Allowlist
	// entries are parsed and expiry-checked in allowlist.go; the matcher
	// here gates the report.
	for _, stmt := range funcLit.Body.List {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		if c, ok := es.X.(*ast.CallExpr); ok {
			if name := chiHandlerName(c); name != "" {
				if IsAllowed(name, activeAllowlist) {
					continue
				}
				pass.Reportf(c.Pos(),
					"handler %s registered under /businesses/{id}/... must reference authz.BusinessContextFromCtx or authz.Can (or be added to .rbac-migration-allowlist)",
					name,
				)
			}
		}
	}
}

// isAuthzRequireBusinessAccessUse identifies r.Use(authz.RequireBusinessAccess(...)).
func isAuthzRequireBusinessAccessUse(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Use" {
		return false
	}
	if len(call.Args) < 1 {
		return false
	}
	inner, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	innerSel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if innerSel.Sel.Name != "RequireBusinessAccess" {
		return false
	}
	if pkg, ok := innerSel.X.(*ast.Ident); ok && pkg.Name == "authz" {
		return true
	}
	return false
}

// chiHandlerName returns a "Verb /pattern" string for r.Get/.../r.Post etc.
// Returns empty string for non-handler calls (like r.Use, r.Route).
func chiHandlerName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	switch sel.Sel.Name {
	case "Get", "Post", "Put", "Patch", "Delete", "Head", "Options":
		if len(call.Args) > 0 {
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				return fmt.Sprintf("%s %s", sel.Sel.Name, strings.Trim(lit.Value, "\""))
			}
		}
		return sel.Sel.Name
	}
	return ""
}
