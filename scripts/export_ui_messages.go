// Export app-visible backend errors/statuses for localization coverage checks.
// Agent message bodies, tool output, CLI prompts and external browser pages are
// deliberately outside this contract; they are not interface translations.
// The owner-only workstation console has its own UI and is never sent to phones.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type record struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
}

func han(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
func expr(e ast.Expr, index *int) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, err := strconv.Unquote(v.Value)
			if err == nil {
				return s
			}
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return expr(v.X, index) + expr(v.Y, index)
		}
	}
	s := fmt.Sprintf("{%d}", *index)
	*index++
	return s
}
func main() {
	root := "backend/internal"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	records := map[string]string{}
	add := func(e ast.Expr, kind string, printf bool) {
		i := 0
		s := expr(e, &i)
		if !han(s) {
			return
		}
		if printf {
			n := 0
			s = regexp.MustCompile(`%[+#\-0-9.]*[sdvq]`).ReplaceAllStringFunc(s, func(string) string { v := fmt.Sprintf("{%d}", n); n++; return v })
		}
		if strings.Contains(s, "<html") || strings.Contains(s, "<p>") {
			return
		}
		if records[s] != "notice" {
			records[s] = kind
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "console" {
			return filepath.SkipDir
		}
		if strings.HasSuffix(filepath.ToSlash(path), "/server/association.go") {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				if f, ok := v.Fun.(*ast.Ident); ok && f.Name == "emit" && len(v.Args) > 2 {
					if kind, ok := v.Args[0].(*ast.BasicLit); ok {
						k, _ := strconv.Unquote(kind.Value)
						if k == "notice" || k == "status" {
							add(v.Args[2], k, false)
						}
					}
				}
				if f, ok := v.Fun.(*ast.SelectorExpr); ok && len(v.Args) > 0 {
					if p, ok := f.X.(*ast.Ident); ok && ((p.Name == "errors" && f.Sel.Name == "New") || (p.Name == "fmt" && f.Sel.Name == "Errorf")) {
						add(v.Args[0], "error", f.Sel.Name == "Errorf")
					}
				}
			case *ast.KeyValueExpr:
				k := ""
				switch key := v.Key.(type) {
				case *ast.Ident:
					k = key.Name
				case *ast.BasicLit:
					k, _ = strconv.Unquote(key.Value)
				}
				switch k {
				case "message", "Detail":
					add(v.Value, "error", false)
				case "Text":
					add(v.Value, "notice", false)
				}
			case *ast.AssignStmt:
				for i, lhs := range v.Lhs {
					if i >= len(v.Rhs) {
						continue
					}
					name := ""
					switch l := lhs.(type) {
					case *ast.Ident:
						name = l.Name
					case *ast.SelectorExpr:
						name = l.Sel.Name
					}
					if name == "detail" || name == "d" || name == "Error" {
						add(v.Rhs[i], "status", false)
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := []record{}
	for key, kind := range records {
		out = append(out, record{key, kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	json.NewEncoder(os.Stdout).Encode(out)
}
