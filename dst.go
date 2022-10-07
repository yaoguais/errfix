package errfix

import (
	"go/token"
	"path"
	"regexp"
	"strconv"

	"github.com/dave/dst"
)

// isName returns true when the name of the identifier n is the same as the name entered.
func isName(n dst.Expr, name string) bool {
	id, ok := n.(*dst.Ident)
	return ok && id.String() == name
}

// isMatchName returns true when the name of the identifier n matches the regular expression.
func isMatchName(n dst.Expr, pattern string) bool {
	re := regexp.MustCompile(pattern)
	name := ""
	if id, ok := n.(*dst.Ident); ok {
		name = id.String()
	}
	return re.MatchString(name)
}

// getImports returns a list of all imported packages in this file.
func getImports(f *dst.File) []*dst.GenDecl {
	var imports []*dst.GenDecl
	dst.Inspect(f, func(n dst.Node) bool {
		switch n := n.(type) {
		case *dst.File:
			return true
		case *dst.GenDecl:
			if n.Tok == token.IMPORT {
				imports = append(imports, n)
			}
		}
		return false
	})
	return imports
}

// findImportByPath returns the same import structure as the specified package.
// It returns nil when the target is not found.
func findImportByPath(imports []*dst.GenDecl, ipath string) *dst.ImportSpec {
	for _, imp := range imports {
		for _, spec := range imp.Specs {
			importSpec := spec.(*dst.ImportSpec)
			if importPath(importSpec) == ipath {
				return importSpec
			}
		}
	}
	return nil
}

// addImport adds a specified package to the file. It does not detect whether there is a conflict in the package.
// When there is a conflict, it needs to be handled manually.
func addImport(f *dst.File, ipath, name string, imports []*dst.GenDecl) {
	var ident *dst.Ident
	if name != "" {
		ident = dst.NewIdent(name)
	}

	imp := &dst.ImportSpec{
		Name: ident,
		Path: &dst.BasicLit{
			Kind:  token.STRING,
			Value: strconv.Quote(ipath),
		},
	}

	if len(imports) > 0 {
		imports[0].Specs = append(imports[0].Specs, imp)
	} else {
		decl := &dst.GenDecl{Tok: token.IMPORT, Specs: []dst.Spec{imp}, Lparen: true, Rparen: true}
		decls := append([]dst.Decl{}, decl)
		decls = append(decls, f.Decls...)
		f.Decls = decls
	}
}

// importName returns the short name of a package.
func importName(s *dst.ImportSpec) string {
	if s.Name != nil {
		return s.Name.Name
	}
	_, name := path.Split(importPath(s))
	return name
}

// importPath returns the full name of a package.
func importPath(s *dst.ImportSpec) string {
	t, err := strconv.Unquote(s.Path.Value)
	if err == nil {
		return t
	}
	return ""
}

// isPkgSelector returns true when the input node matches the rule "pkg.name".
func isPkgSelector(t dst.Expr, pkg, name string) bool {
	sel, ok := t.(*dst.SelectorExpr)
	return ok && isTopName(sel.X, pkg) && sel.Sel.String() == name
}

// isTopName returns true when identifier n is a top-level identifier.
func isTopName(n dst.Expr, name string) bool {
	id, ok := n.(*dst.Ident)
	return ok && id.Name == name && id.Obj == nil
}
