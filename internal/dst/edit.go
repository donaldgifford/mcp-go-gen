// Package dst performs the structural edit embed-mode generation makes to
// the user's main.go. The single responsibility is: locate the
// `// mcpgen:hook` comment inside func main, insert the generated
// package's Register call immediately after it, and add the necessary
// import — idempotently.
//
// See IMPL-0001 Phase 6. No other code path under internal/dst edits
// files; the Tidy / go-format pass happens in the caller.
package dst

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// HookMarker is the comment text the embed-mode DST edit keys off of.
// Users must add this line inside their `func main` to opt into mcpgen
// embed mode; the generator refuses to guess a location.
const HookMarker = "mcpgen:hook"

// EditResult is what Edit returns: the rewritten source and a flag for
// whether any change was actually made. Callers use the flag to decide
// whether to rewrite the file on disk — a no-op edit should not touch
// mtime.
type EditResult struct {
	Source  []byte
	Changed bool
}

// Edit applies the idempotent embed-mode edit to src.
//
// pkgPath is the import path of the generated mcpserver package (for
// example "example.com/svc/internal/mcpserver"); pkgAlias is the name
// used at the call site. Both are required — the caller knows the
// module path of the user's module, dst does not.
//
// Returns ErrNoHook when the file does not contain the marker inside a
// function literally named `main`. That case is intentionally loud:
// silent insertion into a random scope is how people lose hours of
// debugging time.
func Edit(src []byte, pkgPath, pkgAlias string) (*EditResult, error) {
	if pkgPath == "" {
		return nil, fmt.Errorf("pkgPath is required")
	}
	if pkgAlias == "" {
		return nil, fmt.Errorf("pkgAlias is required")
	}

	fset := token.NewFileSet()
	d := decorator.NewDecorator(fset)
	file, err := d.ParseFile("main.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	mainFn, err := findMainFunc(file)
	if err != nil {
		return nil, err
	}

	inserted := false
	if !hasRegisterCall(mainFn, pkgAlias) {
		insertAfterHook(mainFn, pkgAlias)
		inserted = true
	}

	importAdded := ensureImport(file, pkgPath, pkgAlias)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return nil, fmt.Errorf("fprint: %w", err)
	}
	return &EditResult{Source: buf.Bytes(), Changed: inserted || importAdded}, nil
}

// findMainFunc returns the first top-level function named `main`, or
// ErrNoHook with an actionable suggestion when either the function or
// the hook comment inside it is missing.
func findMainFunc(file *dst.File) (*dst.FuncDecl, error) {
	var mainFn *dst.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}
		mainFn = fn
		break
	}
	if mainFn == nil {
		return nil, fmt.Errorf(
			"no func main found in target file; add a `func main()` with a `// %s` marker inside",
			HookMarker)
	}
	if !funcContainsHook(mainFn) {
		return nil, fmt.Errorf(
			"no `// %s` marker inside func main; add the comment on its own line so mcpgen can attach the Register call deterministically",
			HookMarker)
	}
	return mainFn, nil
}

// funcContainsHook walks the decorator-attached comments on every
// statement inside the function body looking for the marker.
func funcContainsHook(fn *dst.FuncDecl) bool {
	if fn.Body == nil {
		return false
	}
	for _, stmt := range fn.Body.List {
		if commentsContainHook(stmt.Decorations().Start.All(), stmt.Decorations().End.All()) {
			return true
		}
	}
	return commentsContainHook(fn.Body.Decs.Lbrace.All())
}

func commentsContainHook(groups ...[]string) bool {
	for _, g := range groups {
		for _, c := range g {
			if strings.Contains(c, HookMarker) {
				return true
			}
		}
	}
	return false
}

// hasRegisterCall reports whether func main already contains a
// statement of the form `if err := <alias>.Register(...); err != nil
// { log.Fatalf(...) }`. Full structural match keeps idempotency strict;
// comparing alias + method name + call shape is enough in practice.
func hasRegisterCall(fn *dst.FuncDecl, alias string) bool {
	if fn.Body == nil {
		return false
	}
	for _, stmt := range fn.Body.List {
		ifStmt, ok := stmt.(*dst.IfStmt)
		if !ok {
			continue
		}
		if isRegisterIfStmt(ifStmt, alias) {
			return true
		}
	}
	return false
}

func isRegisterIfStmt(ifs *dst.IfStmt, alias string) bool {
	assign, ok := ifs.Init.(*dst.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return false
	}
	call, ok := assign.Rhs[0].(*dst.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*dst.SelectorExpr)
	if !ok {
		return false
	}
	pkgIdent, ok := sel.X.(*dst.Ident)
	if !ok {
		return false
	}
	return pkgIdent.Name == alias && sel.Sel.Name == "Register"
}

// insertAfterHook finds the statement whose leading comment contains
// the hook marker and inserts the Register call immediately after it.
// When the marker lives on the braces themselves (empty function body),
// the call is inserted as the first statement.
func insertAfterHook(fn *dst.FuncDecl, alias string) {
	call := newRegisterStmt(alias)

	if fn.Body == nil || len(fn.Body.List) == 0 {
		fn.Body.List = []dst.Stmt{call}
		return
	}

	for i, stmt := range fn.Body.List {
		if commentsContainHook(stmt.Decorations().Start.All(), stmt.Decorations().End.All()) {
			fn.Body.List = append(fn.Body.List[:i+1], append([]dst.Stmt{call}, fn.Body.List[i+1:]...)...)
			return
		}
	}
	// Marker was on the braces — prepend.
	fn.Body.List = append([]dst.Stmt{call}, fn.Body.List...)
}

// newRegisterStmt builds the `if err := <alias>.Register(ctx, app,
// cfg); err != nil { log.Fatalf(...) }` statement. Arg list follows
// the shape documented in DESIGN-0004; the generated Register function
// signature must match.
func newRegisterStmt(alias string) dst.Stmt {
	return &dst.IfStmt{
		Init: &dst.AssignStmt{
			Lhs: []dst.Expr{&dst.Ident{Name: "err"}},
			Tok: token.DEFINE,
			Rhs: []dst.Expr{
				&dst.CallExpr{
					Fun: &dst.SelectorExpr{
						X:   &dst.Ident{Name: alias},
						Sel: &dst.Ident{Name: "Register"},
					},
					Args: []dst.Expr{
						&dst.Ident{Name: "ctx"},
						&dst.Ident{Name: "app"},
						&dst.Ident{Name: "cfg"},
					},
				},
			},
		},
		Cond: &dst.BinaryExpr{
			X:  &dst.Ident{Name: "err"},
			Op: token.NEQ,
			Y:  &dst.Ident{Name: "nil"},
		},
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun: &dst.SelectorExpr{
							X:   &dst.Ident{Name: "log"},
							Sel: &dst.Ident{Name: "Fatalf"},
						},
						Args: []dst.Expr{
							&dst.BasicLit{Kind: token.STRING, Value: `"mcp register: %v"`},
							&dst.Ident{Name: "err"},
						},
					},
				},
			},
		},
	}
}

// ensureImport adds an import for pkgPath aliased as pkgAlias when it is
// not already present. Returns true when a new import was added. The
// alias is emitted explicitly so user-local name collisions with the
// package's basename resolve predictably.
func ensureImport(file *dst.File, pkgPath, pkgAlias string) bool {
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok {
				continue
			}
			if strings.Trim(is.Path.Value, `"`) == pkgPath {
				return false // already imported
			}
		}
	}

	newImport := &dst.ImportSpec{
		Name: &dst.Ident{Name: pkgAlias},
		Path: &dst.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", pkgPath)},
	}

	// Append to an existing import decl if present; otherwise synthesize one.
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, newImport)
		gd.Lparen = true // force parenthesized form so the formatter renders cleanly
		return true
	}

	// No import block yet — insert one at the top.
	file.Decls = append([]dst.Decl{
		&dst.GenDecl{
			Tok:    token.IMPORT,
			Lparen: true,
			Specs:  []dst.Spec{newImport},
			Rparen: true,
		},
	}, file.Decls...)
	return true
}
