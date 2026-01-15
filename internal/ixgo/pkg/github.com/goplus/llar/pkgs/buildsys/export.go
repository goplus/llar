// export by github.com/goplus/ixgo/cmd/qexp

package buildsys

import (
	q "github.com/goplus/llar/pkgs/buildsys"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "buildsys",
		Path: "github.com/goplus/llar/pkgs/buildsys",
		Deps: map[string]string{
			"github.com/goplus/llar/pkgs/mod/module": "module",
		},
		Interfaces: map[string]reflect.Type{
			"BuildSystem": reflect.TypeOf((*q.BuildSystem)(nil)).Elem(),
		},
		NamedTypes:    map[string]reflect.Type{},
		AliasTypes:    map[string]reflect.Type{},
		Vars:          map[string]reflect.Value{},
		Funcs:         map[string]reflect.Value{},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
