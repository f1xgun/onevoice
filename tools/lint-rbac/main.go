package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
)

func main() {
	var allowlistPath string
	flag.StringVar(&allowlistPath, "allowlist", ".rbac-migration-allowlist", "path to JSON allowlist file")
	flag.Parse()

	targets := flag.Args()
	if len(targets) == 0 {
		targets = []string{"services/api/internal/router/router.go"}
	}

	now := time.Now()
	allowed, err := ParseAllowlist(allowlistPath, now)
	if err != nil {
		log.Fatalf("lint-rbac: %v", err)
	}
	activeAllowlist = allowed

	fset := token.NewFileSet()
	var hadDiagnostic bool

	for _, target := range targets {
		if !strings.HasSuffix(target, ".go") {
			log.Fatalf("lint-rbac: target %s is not a .go file", target)
		}
		file, err := parser.ParseFile(fset, target, nil, parser.AllErrors)
		if err != nil {
			log.Fatalf("lint-rbac: parse %s: %v", target, err)
		}

		pass := &analysis.Pass{
			Analyzer: Analyzer,
			Fset:     fset,
			Files:    []*ast.File{file},
			Report: func(d analysis.Diagnostic) {
				pos := fset.Position(d.Pos)
				fmt.Fprintf(os.Stderr, "%s:%d:%d: %s\n", target, pos.Line, pos.Column, d.Message)
				hadDiagnostic = true
			},
			ResultOf: map[*analysis.Analyzer]any{},
		}

		if _, err := Analyzer.Run(pass); err != nil {
			log.Fatalf("lint-rbac: analyze %s: %v", target, err)
		}
	}

	if hadDiagnostic {
		os.Exit(1)
	}
	fmt.Println("lint-rbac: clean")
}
