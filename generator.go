package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	initTemplate = `
func init%s() {
	env.Packages["%s"] = map[string]reflect.Value{
		// constants
%s
		// variables
%s
		// functions
%s	}
	env.PackageTypes["%s"] = map[string]reflect.Type{
%s	}
}
`

	tabs = "\t\t"

	// "Compare": reflect.ValueOf(bytes.Compare),
	valFormat = tabs + `"%s": reflect.ValueOf(%s.%s),` + "\n"

	// "Conn": reflect.TypeOf(&conn).Elem(),
	typeFormat = tabs + `"%s": reflect.TypeOf((*%s.%s)(nil)).Elem(),` + "\n"
)

func exportDeclaration(root, path, dir, init string) (string, error) {
	packages, err := parseDir(filepath.Join(root, dir))
	if err != nil {
		return "", err
	}
	name := getPackageName(packages)
	pak := packages[name]
	if pak == nil {
		return "", nil
	}
	constants := make(map[string]struct{})
	variables := make(map[string]struct{})
	types := make(map[string]struct{})
	functions := make(map[string]struct{})
	for _, file := range pak.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				switch decl.Tok {
				case token.CONST:
					exportValues(decl, constants)
				case token.VAR:
					exportValues(decl, variables)
				case token.TYPE:
					exportTypes(decl, types)
				}
			case *ast.FuncDecl:
				exportFunction(decl, functions)
			}
		}
	}
	if len(constants) == 0 && len(variables) == 0 && len(types) == 0 && len(functions) == 0 {
		return "", nil
	}
	cs := sortStringMap(constants)
	vs := sortStringMap(variables)
	ts := sortStringMap(types)
	fs := sortStringMap(functions)
	return generateCode(path, name, init, cs, vs, ts, fs), nil
}

func isGoFile(info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	name := info.Name()
	if name == "fuzz.go" {
		return false
	}
	if strings.HasSuffix(name, "_test.go") {
		return false
	}
	if strings.HasPrefix(name, "example_") {
		return false
	}
	return true
}

func parseDir(dir string) (map[string]*ast.Package, error) {
	return parser.ParseDir(token.NewFileSet(), dir, isGoFile, parser.ParseComments)
}

func getPackageName(packages map[string]*ast.Package) string {
	var pkgName string
loop:
	for pn := range packages {
		switch {
		case pn == "main":
		case strings.HasSuffix(pn, "_test"):
		default:
			pkgName = pn
			break loop
		}
	}
	return pkgName
}

func isDeprecated(text string) bool {
	for _, item := range [...]string{
		"Deprecated:",
		"Deprecated.",
	} {
		if strings.Contains(text, item) {
			return true
		}
	}
	return false
}

func exportValues(decl *ast.GenDecl, m map[string]struct{}) {
	if isDeprecated(decl.Doc.Text()) {
		return
	}
	for _, spec := range decl.Specs {
		vs := spec.(*ast.ValueSpec)
		if isDeprecated(vs.Doc.Text()) {
			continue
		}
		for _, name := range vs.Names {
			// skip some special variables
			if name.Name == "ErrTrailingComma" {
				continue
			}
			if name.IsExported() {
				m[name.Name] = struct{}{}
			}
		}
	}
}

func exportTypes(decl *ast.GenDecl, m map[string]struct{}) {
	if isDeprecated(decl.Doc.Text()) {
		return
	}
	for _, spec := range decl.Specs {
		ts := spec.(*ast.TypeSpec)
		if isDeprecated(ts.Doc.Text()) {
			continue
		}
		if ts.Name.IsExported() {
			m[ts.Name.Name] = struct{}{}
		}
	}
}

func exportFunction(decl *ast.FuncDecl, m map[string]struct{}) {
	if isDeprecated(decl.Doc.Text()) {
		return
	}
	if decl.Recv != nil {
		return
	}
	if decl.Name.IsExported() {
		m[decl.Name.Name] = struct{}{}
	}
}

func sortStringMap(m map[string]struct{}) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}

func generateCode(path, name, init string, constants, vars, types, fns []string) string {
	// constants
	buf := new(bytes.Buffer)
	for _, c := range constants {
		fmt.Fprintf(buf, valFormat, c, name, c)
	}
	cs := buf.String()

	// variables
	buf.Reset()
	for _, v := range vars {
		fmt.Fprintf(buf, valFormat, v, name, v)
	}
	vs := buf.String()

	// functions
	buf.Reset()
	for _, fn := range fns {
		fmt.Fprintf(buf, valFormat, fn, name, fn)
	}
	fs := buf.String()

	// prepare var buffer for struct and interface
	buf.Reset()
	for _, typ := range types {
		fmt.Fprintf(buf, typeFormat, typ, name, typ)
	}
	ts := buf.String()
	return fmt.Sprintf(initTemplate, init, path, cs, vs, fs, path, ts)
}
