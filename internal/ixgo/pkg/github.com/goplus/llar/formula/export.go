// export by github.com/goplus/ixgo/cmd/qexp

package formula

import (
	q "github.com/goplus/llar/formula"

	"go/constant"
	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "formula",
		Path: "github.com/goplus/llar/formula",
		Deps: map[string]string{
			"github.com/goplus/llar/pkgs/mod/versions": "versions",
			"github.com/qiniu/x/gsh":                   "gsh",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{
			"BuildResult": reflect.TypeOf((*q.BuildResult)(nil)).Elem(),
			"ModuleDeps":  reflect.TypeOf((*q.ModuleDeps)(nil)).Elem(),
			"ModuleF":     reflect.TypeOf((*q.ModuleF)(nil)).Elem(),
			"Project":     reflect.TypeOf((*q.Project)(nil)).Elem(),
		},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"Gopt_ModuleF_Main": reflect.ValueOf(q.Gopt_ModuleF_Main),
		},
		TypedConsts: map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{
			"GopPackage": {"untyped bool", constant.MakeBool(bool(q.GopPackage))},
		},
	})
}
