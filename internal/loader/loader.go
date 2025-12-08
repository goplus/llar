package loader

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"

	// make ixgo happy
	_ "github.com/goplus/llar/internal/ixgo"
)

// classfileMain represents a Go+ class file that can be executed
type classfileMain interface {
	Main()
}

// StructElem wraps a reflected struct element loaded from a Go+ formula file.
// It provides methods to get and set field values by name.
type StructElem struct {
	elem reflect.Value
}

// newStructElem creates a new StructElem by looking up the struct type by name,
// instantiating it, and executing its Main method.
func newStructElem(interp *ixgo.Interp, structName string) (*StructElem, error) {
	typ, ok := interp.GetType(structName)
	if !ok {
		return nil, fmt.Errorf("failed to load formula: struct name not found: %s", structName)
	}
	val := reflect.New(typ)

	val.Interface().(classfileMain).Main()

	return &StructElem{elem: val.Elem()}, nil
}

// Value retrieves the value of a struct field by name.
// It supports both exported and unexported fields.
func (e *StructElem) Value(key string) any {
	return valueOf(e.elem, key)
}

// SetValue sets the value of a struct field by name.
// It supports both exported and unexported fields.
func (e *StructElem) SetValue(key string, value any) {
	setValue(e.elem, key, value)
}

// Loader defines the interface for loading Go+ formula files into StructElem instances.
type Loader interface {
	Load(path string) (*StructElem, error)
}

// FormulaLoader is an implementation of Loader that uses ixgo to load and execute Go+ formula files.
type FormulaLoader struct {
	ctx *ixgo.Context
}

// NewFormulaLoader creates a new FormulaLoader with the given ixgo context.
func NewFormulaLoader(ctx *ixgo.Context) Loader {
	return &FormulaLoader{ctx: ctx}
}

// Load loads a Go+ formula file from the specified path and returns a StructElem.
// The file name should follow the pattern "{StructName}_*.gox" where StructName
// is the name of the struct to be loaded.
func (f *FormulaLoader) Load(path string) (*StructElem, error) {
	lookupFn := f.ctx.Lookup
	defer func() {
		f.ctx.Lookup = lookupFn
	}()

	setupGoModResolver(f.ctx)

	interp, err := load(f.ctx, path)
	if err != nil {
		return nil, err
	}
	defer interp.ResetIcall()

	structName, _, ok := strings.Cut(filepath.Base(path), "_")
	if !ok {
		return nil, fmt.Errorf("failed to load formula: file name is not valid: %s", path)
	}

	structElem, err := newStructElem(interp, structName)
	if err != nil {
		return nil, err
	}

	return structElem, nil
}

// load builds and loads a Go+ directory, returning an initialized interpreter.
func load(ctx *ixgo.Context, path string) (*ixgo.Interp, error) {
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
	if err = interp.RunInit(); err != nil {
		return nil, err
	}
	return interp, nil
}

// unexportValueOf creates a reflect.Value that allows access to unexported fields.
func unexportValueOf(field reflect.Value) reflect.Value {
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

// valueOf retrieves the value of a field by name from a struct element.
// It handles both exported and unexported fields.
func valueOf(elem reflect.Value, name string) any {
	if ast.IsExported(name) {
		return elem.FieldByName(name).Elem().Interface()
	}
	return unexportValueOf(elem.FieldByName(name)).Interface()
}

// setValue sets the value of a field by name in a struct element.
// It handles both exported and unexported fields.
func setValue(elem reflect.Value, name string, value any) {
	if ast.IsExported(name) {
		elem.FieldByName(name).Elem().Set(reflect.ValueOf(value))
		return
	}
	unexportValueOf(elem.FieldByName(name)).Set(reflect.ValueOf(value))
}

// setupGoModResolver configures the ixgo context to use a custom resolver
// for Go module dependency lookup.
func setupGoModResolver(ctx *ixgo.Context) {
	resolver := newResolver()

	ctx.Lookup = func(_, path string) (dir string, found bool) {
		return resolver.Lookup(path, path)
	}
}
