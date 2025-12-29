package modules

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"
	_ "github.com/goplus/llar/internal/ixgo"
	"github.com/goplus/llar/pkgs/gnu"
	"github.com/goplus/llar/pkgs/mod/module"
)

// loadComparator loads a version comparator for a module.
// If a custom comparator (*_cmp.gox) exists, it loads that;
// otherwise returns a default GNU version comparator.
//
// The returned comparator compares two version strings and returns:
//   - a negative value if v1 < v2
//   - zero if v1 == v2
//   - a positive value if v1 > v2
func loadComparator(path string) (comparator func(v1, v2 module.Version) int, err error) {
	if path == "" {
		return func(v1, v2 module.Version) int {
			return gnu.Compare(v1.Version, v2.Version)
		}, nil
	}
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

	val.Interface().(interface{ Main() }).Main()

	return valueOf(class, "fCompareVer").(func(v1, v2 module.Version) int), nil
}

// unexportValueOf creates a reflect.Value that allows access to unexported fields.
// It uses unsafe operations to bypass Go's exported field restrictions.
func unexportValueOf(field reflect.Value) reflect.Value {
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

// valueOf retrieves the value of a field by name from a struct element.
// It handles both exported and unexported fields, and both pointer and non-pointer types.
// For pointer fields, it returns the dereferenced value; for non-pointer fields, it returns the value directly.
func valueOf(elem reflect.Value, name string) any {
	if ast.IsExported(name) {
		field := elem.FieldByName(name)
		if field.Kind() == reflect.Ptr {
			return field.Elem().Interface()
		}
		return field.Interface()
	}
	return unexportValueOf(elem.FieldByName(name)).Interface()
}
