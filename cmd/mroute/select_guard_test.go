package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/alaborderie/jev-model-router/"

// TestSelectionNeverImportsLegacyRouting makes "never the legacy safe target" structural:
// the selection path cannot reach the router or the user target catalogue, and uses
// internal/classifier only for its endpoint constant.
func TestSelectionNeverImportsLegacyRouting(t *testing.T) {
	files, err := filepath.Glob("../../internal/selection/*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "select.go")
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		checked++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		classifierName := ""
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			switch p {
			case modulePath + "internal/router", modulePath + "internal/config":
				t.Errorf("%s imports %s", path, p)
			case modulePath + "internal/classifier":
				classifierName = "classifier"
				if imp.Name != nil {
					classifierName = imp.Name.Name
				}
			}
		}
		if classifierName == "" {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == classifierName && sel.Sel.Name != "JevURL" {
				t.Errorf("%s uses classifier.%s; only classifier.JevURL is allowed", path, sel.Sel.Name)
			}
			return true
		})
	}
	if checked < 4 {
		t.Fatalf("guard checked only %d files", checked)
	}
}
