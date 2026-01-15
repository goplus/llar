// export by github.com/goplus/ixgo/cmd/qexp

package autotools

import (
	q "github.com/goplus/llar/pkgs/buildsys/autotools"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "autotools",
		Path: "github.com/goplus/llar/pkgs/buildsys/autotools",
		Deps: map[string]string{
			"fmt":                                    "fmt",
			"github.com/goplus/llar/formula":         "formula",
			"github.com/goplus/llar/pkgs/buildsys":   "buildsys",
			"github.com/goplus/llar/pkgs/mod/module": "module",
			"os":                                     "os",
			"os/exec":                                "exec",
			"path/filepath":                          "filepath",
			"runtime":                                "runtime",
			"sort":                                   "sort",
			"strings":                                "strings",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{
			"AutoTools": reflect.TypeOf((*q.AutoTools)(nil)).Elem(),
		},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"New": reflect.ValueOf(q.New),
		},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
