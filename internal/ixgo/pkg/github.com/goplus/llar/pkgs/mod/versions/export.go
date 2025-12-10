// export by github.com/goplus/ixgo/cmd/qexp

package versions

import (
	q "github.com/goplus/llar/pkgs/mod/versions"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "versions",
		Path: "github.com/goplus/llar/pkgs/mod/versions",
		Deps: map[string]string{
			"bytes":         "bytes",
			"encoding/json": "json",
			"io":            "io",
			"os":            "os",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{
			"Dependency": reflect.TypeOf((*q.Dependency)(nil)).Elem(),
			"Versions":   reflect.TypeOf((*q.Versions)(nil)).Elem(),
		},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"Parse": reflect.ValueOf(q.Parse),
		},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
