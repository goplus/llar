package modules

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"unsafe"

	"github.com/goplus/ixgo"
	"github.com/goplus/ixgo/xgobuild"
	_ "github.com/goplus/llar/internal/ixgo"
	"github.com/goplus/llar/mod/module"
)

// loadComparator loads a version comparator from a .gox file at the given path.
// Returns an error if the file cannot be loaded or parsed.
//
// The returned comparator compares two module versions and returns:
//   - a negative value if v1 < v2
//   - zero if v1 == v2
//   - a positive value if v1 > v2
func loadComparator(path string) (comparator func(v1, v2 module.Version) int, err error) {
	ctx := ixgo.NewContext(0)

	source, err := xgobuild.BuildFile(ctx, path, nil)
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
		return nil, fmt.Errorf("failed to load: file name is not valid: %s", path)
	}
	typ, ok := interp.GetType(structName)
	if !ok {
		return nil, fmt.Errorf("failed to load: struct name not found: %s", structName)
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

func valueOf(elem reflect.Value, name string) any {
	return unexportValueOf(elem.FieldByName(name)).Interface()
}
