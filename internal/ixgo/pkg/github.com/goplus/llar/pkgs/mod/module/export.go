// export by github.com/goplus/ixgo/cmd/qexp

package module

import (
	q "github.com/goplus/llar/pkgs/mod/module"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name:       "module",
		Path:       "github.com/goplus/llar/pkgs/mod/module",
		Deps:       map[string]string{},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{
			"Version":           reflect.TypeOf((*q.Version)(nil)).Elem(),
			"VersionComparator": reflect.TypeOf((*q.VersionComparator)(nil)).Elem(),
		},
		AliasTypes:    map[string]reflect.Type{},
		Vars:          map[string]reflect.Value{},
		Funcs:         map[string]reflect.Value{},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
