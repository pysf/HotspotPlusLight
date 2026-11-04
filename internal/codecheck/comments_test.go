// Package codecheck holds a test that keeps the Go comment rules from drifting
// (CLAUDE.md, "Go comments").
package codecheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// enforced lists the directories (relative to the repo root) whose comments are
// checked. TODO(phase14): add each group of packages once it has been swept;
// the last group makes this "cmd", "internal", "e2e" and "perf" as a whole.
// TODO(phase14): cmd/ is package main and needs its own package-comment case
// ("Command hotspotpluslight ..." as Go convention says, or "Package main");
// group 3 decides, keeping it simple.
var enforced = []string{
	"internal/domain",
	"internal/auth",
	"internal/session",
	"internal/radius",
	"internal/store",
	"internal/member",
	"internal/plan",
	"internal/voucher",
	"internal/settings",
	"internal/usage",
}

// TestCommentRules checks, for every enforced package (test files and generated
// files excluded), that:
//   - the package has exactly one "Package <name>" comment;
//   - every top-level declaration (exported or not) that has a doc comment starts it with its name.
func TestCommentRules(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range enforced {
		checkDir(t, root, dir)
	}
}

func checkDir(t *testing.T, root, dir string) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_templ.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(root, dir, n), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatalf("%s: no Go files", dir)
	}
	var docs []token.Pos
	name := files[0].Name.Name
	for _, f := range files {
		if f.Doc != nil {
			docs = append(docs, f.Doc.Pos())
			if w := strings.Fields(f.Doc.Text()); len(w) < 2 || w[0] != "Package" || w[1] != name {
				t.Errorf("%s: package comment must start with %q", fset.Position(f.Doc.Pos()), "Package "+name)
			}
		}
		// Only "//" comments are allowed, except a package comment.
		for _, c := range f.Comments {
			if c != f.Doc && strings.HasPrefix(c.List[0].Text, "/*") {
				t.Errorf("%s: use // comments, not /* */", fset.Position(c.Pos()))
			}
		}
		for _, d := range f.Decls {
			checkDecl(t, fset, d)
		}
	}
	switch {
	case len(docs) == 0:
		t.Errorf("%s: package %s has no package comment", dir, name)
	case len(docs) > 1:
		for _, p := range docs {
			t.Errorf("%s: package %s has more than one package comment", fset.Position(p), name)
		}
	}
}

func checkDecl(t *testing.T, fset *token.FileSet, d ast.Decl) {
	t.Helper()
	switch d := d.(type) {
	case *ast.FuncDecl:
		checkDoc(t, fset, d.Name, d.Doc)
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				checkDoc(t, fset, s.Name, specDoc(d, s.Doc))
			case *ast.ValueSpec:
				for _, n := range s.Names {
					checkDoc(t, fset, n, specDoc(d, s.Doc))
				}
			}
		}
	}
}

// specDoc returns the doc of a spec: its own, or the declaration's when the
// declaration has no parentheses. The doc of a group is not checked.
func specDoc(d *ast.GenDecl, own *ast.CommentGroup) *ast.CommentGroup {
	if own != nil {
		return own
	}
	if !d.Lparen.IsValid() {
		return d.Doc
	}
	return nil
}

func checkDoc(t *testing.T, fset *token.FileSet, name *ast.Ident, doc *ast.CommentGroup) {
	t.Helper()
	if doc == nil {
		return
	}
	var first string
	if w := strings.Fields(doc.Text()); len(w) > 0 {
		first = strings.TrimRight(w[0], ".,:")
	}
	if first != name.Name {
		t.Errorf("%s: doc comment of %s must start with %q", fset.Position(doc.Pos()), name.Name, name.Name)
	}
}
