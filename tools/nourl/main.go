// Package main implements a Go analyzer that flags inline HTTP/HTTPS URL
// string literals outside of `const` declarations.
//
// Rationale: URLs are exactly the kind of value that should live in a single
// named place. Inline-ing them across files invites drift (e.g., one site
// updates an OAuth endpoint, another keeps the old value) and makes test
// substitution awkward. This analyzer enforces the convention by rejecting
// `"https://..."` / `"http://..."` literals anywhere except:
//
//   - file-level `const` declarations (preferred)
//   - file-level `var` declarations (for slice/map URL collections that
//     can't be expressed as a const)
//   - `*_test.go` files (test fixtures may legitimately inline URLs)
//
// To override on a single line, add the directive comment `//nourl:allow`
// on the same line as the literal, with a brief reason.
//
// Run as a standalone `go vet`-style tool:
//
//	go run ./tools/nourl ./...
//
// or via the `make lint-urls` target, which iterates Go workspace modules.
package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/singlechecker"
)

var urlPattern = regexp.MustCompile(`(?i)^https?://`)

const allowDirective = "//nourl:allow"

// Analyzer rejects inline URL string literals outside const declarations.
var Analyzer = &analysis.Analyzer{
	Name: "nourl",
	Doc:  "rejects inline http(s):// string literals outside const declarations",
	Run:  run,
}

func main() {
	singlechecker.Main(Analyzer)
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		filename := pass.Fset.File(file.Pos()).Name()
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}

		constSpans := collectConstSpans(file)

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}

			value := strings.Trim(lit.Value, "`\"")
			if !urlPattern.MatchString(value) {
				return true
			}

			if positionInSpans(lit.Pos(), constSpans) {
				return true
			}

			if hasAllowDirective(file, pass.Fset, lit) {
				return true
			}

			pass.Reportf(
				lit.Pos(),
				"inline URL %q — move to a named const (or add `%s <reason>` on this line)",
				value,
				allowDirective,
			)
			return true
		})
	}
	return nil, nil
}

// collectConstSpans returns the position ranges of every file-level `const`
// or `var` declaration in the file (both single-line and parenthesized
// blocks). A literal whose position falls inside any span is part of a
// declaration and is exempt. Function-local `var` declarations are wrapped
// in *ast.DeclStmt and never appear in file.Decls, so they remain flagged.
func collectConstSpans(file *ast.File) []span {
	var spans []span
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if gd.Tok != token.CONST && gd.Tok != token.VAR {
			continue
		}
		spans = append(spans, span{start: gd.Pos(), end: gd.End()})
	}
	return spans
}

type span struct{ start, end token.Pos }

func positionInSpans(p token.Pos, spans []span) bool {
	for _, s := range spans {
		if p >= s.start && p <= s.end {
			return true
		}
	}
	return false
}

// hasAllowDirective scans the comments associated with the literal's line for
// the `//nourl:allow ...` directive. We use a coarse line-match against all
// file comments rather than CommentMap because go/ast doesn't tie standalone
// trailing line-comments to the specific BasicLit node reliably.
func hasAllowDirective(file *ast.File, fset *token.FileSet, lit *ast.BasicLit) bool {
	litLine := fset.Position(lit.Pos()).Line
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			if fset.Position(c.Pos()).Line != litLine {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(c.Text), allowDirective) {
				return true
			}
		}
	}
	return false
}
