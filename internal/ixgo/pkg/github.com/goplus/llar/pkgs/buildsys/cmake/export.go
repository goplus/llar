// export by github.com/goplus/ixgo/cmd/qexp

package cmake

import (
	q "github.com/goplus/llar/pkgs/buildsys/cmake"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "cmake",
		Path: "github.com/goplus/llar/pkgs/buildsys/cmake",
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
			"CMake": reflect.TypeOf((*q.CMake)(nil)).Elem(),
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
