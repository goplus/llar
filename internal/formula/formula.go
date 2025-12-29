package formula

import (
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/formula"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/parser/fsx"
	"github.com/goplus/xgo/token"

	_ "github.com/goplus/llar/internal/ixgo"
)

// Formula represents a loaded LLAR formula file with its metadata and callbacks.
// It contains module information and build/dependency handling functions.
type Formula struct {
	structElem reflect.Value

	ModId     string
	FromVer   string
	OnRequire func(proj *formula.Project, deps *formula.ModuleDeps)
	OnBuild   func(proj *formula.Project, out *formula.BuildResult) error
}

// FromVerOf extracts the FromVer value from a LLAR formula file by parsing its AST.
// It searches for the fromVer() call in the formula file and returns its argument.
func FromVerOf(formulaPath string) (fromVer string, err error) {
	fs := token.NewFileSet()
	astFile, err := parser.ParseFSEntry(fs, fsx.Local, formulaPath, nil, parser.Config{
		ClassKind: xgobuild.ClassKind,
	})
	if err != nil {
		return "", err
	}
	return fromVerFrom(astFile)
}

// Load loads and executes a LLAR formula file, returning a Formula instance.
// The formula file name must follow the pattern "StructName_*.gox".
// It compiles the XGo code, executes the Main() method, and extracts formula metadata.
func Load(path string) (*Formula, error) {
	ctx := ixgo.NewContext(0)

	// FIXME(MeteorsLiu): there's a bug with ixgo ParseFile, which cause the failure compiling classfile
	// we use BuildDir temporarily, in the future we will change it back.
	source, err := xgobuild.BuildDir(ctx, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	pkgs, err := ctx.LoadFile("main.go", source)
	if err != nil {
		return nil, err
	}
	interp, err := ctx.NewInterp(pkgs)
	if err != nil {
		return nil, err
	}
	defer interp.ResetIcall()

	if err = interp.RunInit(); err != nil {
		return nil, err
	}
	structName, _, ok := strings.Cut(filepath.Base(path), "_")
	if !ok {
		return nil, fmt.Errorf("failed to load formula: file name is not valid: %s", path)
	}
	typ, ok := interp.GetType(structName)
	if !ok {
		return nil, fmt.Errorf("failed to load formula: struct name not found: %s", structName)
	}
	val := reflect.New(typ)
	class := val.Elem()

	val.Interface().(classfileMain).Main()

	return &Formula{
		structElem: class,
		ModId:      valueOf(class, "modID").(string),
		FromVer:    valueOf(class, "modFromVer").(string),
		OnBuild:    valueOf(class, "fOnBuild").(func(*formula.Project, *formula.BuildResult) error),
		OnRequire:  valueOf(class, "fOnRequire").(func(*formula.Project, *formula.ModuleDeps)),
	}, nil
}

// SetStdout sets the stdout writer for the formula's gsh.App.
// This is used to control build output verbosity.
func (f *Formula) SetStdout(w io.Writer) {
	if f.structElem.IsValid() {
		setValue(f.structElem, "fout", w)
	}
}

// SetStderr sets the stderr writer for the formula's gsh.App.
func (f *Formula) SetStderr(w io.Writer) {
	if f.structElem.IsValid() {
		setValue(f.structElem, "ferr", w)
	}
}

// fromVerFrom extracts the FromVer value from a formula AST by finding the FromVer() call.
func fromVerFrom(formulaAST *ast.File) (fromVer string, err error) {
	ast.Inspect(formulaAST, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := c.Fun.(type) {
		case *ast.Ident:
			switch fn.Name {
			case "fromVer":
				fromVer, err = parseCallArg(c, fn.Name)

				return false
			}
		}
		return true
	})
	return
}

// parseCallArg extracts the first string argument from a function call expression.
func parseCallArg(c *ast.CallExpr, fnName string) (string, error) {
	if len(c.Args) == 0 {
		return "", fmt.Errorf("failed to parse %s from AST: no argument", fnName)
	}
	var argResult string
	switch arg := c.Args[0].(type) {
	case *ast.BasicLit:
		argResult = strings.Trim(strings.Trim(arg.Value, `"`), "`")
		if argResult == "" {
			return "", fmt.Errorf("failed to parse %s from AST: no argument", fnName)
		}
	}
	return argResult, nil
}
