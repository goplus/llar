package formula

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/llar/formula"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"

	_ "github.com/goplus/llar/internal/ixgo"
)

// Formula represents a loaded LLAR formula file with its metadata and callbacks.
// It contains module information and build/dependency handling functions.
type Formula struct {
	structElem reflect.Value

	// NOTE(MeteorsLiu): these signatures MUST match with
	// 	the method declaration of ModuleF in formula/classfile.go
	ModPath   string
	FromVer   string
	OnRequire func(proj *formula.Project, deps *formula.ModuleDeps)
	OnBuild   func(ctx *formula.Context, proj *formula.Project, out *formula.BuildResult)
}

// Dir returns the directory path where formulas are stored.
// It creates the directory with 0700 permissions if it doesn't exist.
// The directory is located at <UserCacheDir>/.llar/formulas.
//
// Returns:
//   - string: The absolute path to the formulas directory
//   - error: An error if the user cache directory cannot be determined or the directory cannot be created
func Dir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	formulaDir := filepath.Join(userCacheDir, ".llar", "formulas")

	if err := os.MkdirAll(formulaDir, 0700); err != nil {
		return "", err
	}
	return formulaDir, nil
}

// FromVerOf extracts the FromVer value from a LLAR formula file by parsing its AST.
// It searches for the fromVer() call in the formula file and returns its argument.
func FromVerOf(formulaPath string) (fromVer string, err error) {
	fs := token.NewFileSet()
	astFile, err := parser.ParseEntry(fs, formulaPath, nil, parser.Config{
		ClassKind: xgobuild.ClassKind,
	})
	if err != nil {
		return "", err
	}
	return fromVerFrom(astFile)
}

// loadFS is the internal implementation for loading a formula from a filesystem.
// It builds and interprets the formula file, then extracts the struct fields.
func loadFS(fs fs.ReadFileFS, path string) (*Formula, error) {
	ctx := ixgo.NewContext(0)

	content, err := fs.ReadFile(path)
	if err != nil {
		return nil, err
	}
	source, err := xgobuild.BuildFile(ctx, path, content)
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

	val.Interface().(interface{ Main() }).Main()

	return &Formula{
		structElem: class,
		ModPath:    valueOf(class, "modPath").(string),
		FromVer:    valueOf(class, "modFromVer").(string),
		OnBuild:    valueOf(class, "fOnBuild").(func(*formula.Context, *formula.Project, *formula.BuildResult)),
		OnRequire:  valueOf(class, "fOnRequire").(func(*formula.Project, *formula.ModuleDeps)),
	}, nil
}

// Load loads a formula from the local filesystem.
// The path must be within the formula directory (env.FormulaDir).
func Load(path string) (*Formula, error) {
	formulaDir, err := Dir()
	if err != nil {
		return nil, err
	}
	relPath, err := filepath.Rel(formulaDir, path)
	if err != nil || strings.HasPrefix(relPath, "..") {
		if err == nil {
			return nil, fmt.Errorf("failed to load formula: disallow non formula dir access")
		}
		return nil, err
	}
	return loadFS(os.DirFS(formulaDir).(fs.ReadFileFS), relPath)
}

// LoadFS loads a formula from a filesystem interface.
// This allows loading formulas from remote repositories or mock filesystems.
// The path should be relative to the filesystem root.
func LoadFS(fsys fs.ReadFileFS, path string) (*Formula, error) {
	return loadFS(fsys, path)
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
