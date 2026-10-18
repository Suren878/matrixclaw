package repomap

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"strings"
)

var generatedGo = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// goTags reads the top-level definitions of a Go file (functions, methods
// with their receiver, types, interface methods, constants and variables)
// and counts the identifiers it names as references, leaving out its locals,
// builtins, package names and names from the standard library. Generated files give nothing.
func goTags(src []byte) fileTags {
	if header, _, ok := bytes.Cut(src, []byte("\npackage ")); ok && generatedGo.Match(header) {
		return fileTags{}
	}
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "", src, 0)
	if file == nil {
		return fileTags{}
	}
	lines := fset.File(file.Pos())
	lineText := func(pos token.Pos) (int, string) {
		line := lines.Line(pos)
		start := lines.Offset(lines.LineStart(line))
		text, _, _ := bytes.Cut(src[start:], []byte("\n"))
		return line, string(bytes.TrimRight(text, " \t\r"))
	}
	var tags fileTags
	declared := map[*ast.Ident]bool{file.Name: true}
	define := func(name *ast.Ident, line int, text string) {
		declared[name] = true
		if name.Name != "_" {
			tags.defs = append(tags.defs, newDefinition(name.Name, line, text))
		}
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			end := decl.End()
			if decl.Body != nil {
				end = decl.Body.Lbrace
			}
			text := bytes.Join(bytes.Fields(src[lines.Offset(decl.Pos()):lines.Offset(end)]), []byte(" "))
			define(decl.Name, lines.Line(decl.Pos()), string(text))
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					line, text := lineText(spec.Name.Pos())
					define(spec.Name, line, text)
					if iface, ok := spec.Type.(*ast.InterfaceType); ok {
						for _, method := range iface.Methods.List {
							for _, name := range method.Names {
								line, text := lineText(name.Pos())
								define(name, line, text)
							}
						}
					}
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						line, text := lineText(name.Pos())
						define(name, line, text)
					}
				}
			}
		}
	}
	// imports maps each imported package's name to whether it is in the
	// standard library, whose names cannot be the repository's.
	imports := map[string]bool{}
	for _, spec := range file.Imports {
		importPath := strings.Trim(spec.Path.Value, `"`)
		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		first, _, _ := strings.Cut(importPath, "/")
		imports[name] = !strings.Contains(first, ".")
	}
	tags.refs = map[string]int{}
	selected := map[*ast.Ident]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			selected[node.Sel] = true
			if x, ok := node.X.(*ast.Ident); ok && x.Obj == nil {
				if std, imported := imports[x.Name]; imported {
					declared[x] = true
					declared[node.Sel] = std
				}
			}
		case *ast.Ident:
			// Locals and parameters resolve inside the file, builtins in the universe.
			builtin := node.Obj == nil && !selected[node] && types.Universe.Lookup(node.Name) != nil
			if !declared[node] && !builtin && node.Name != "_" && (node.Obj == nil || file.Scope.Lookup(node.Name) == node.Obj) {
				tags.refs[node.Name]++
			}
		}
		return true
	})
	return tags
}
