package ixgo

import (
	"github.com/goplus/ixgo/xgobuild"
	"github.com/goplus/mod/modfile"
)

func init() {
	xgobuild.RegisterProject(&modfile.Project{
		Ext:   "_cmp.gox",
		Class: "CmpApp",
		PkgPaths: []string{
			"github.com/goplus/llar/cmp",
		},
		Import: []*modfile.Import{
			{
				Name: "semver",
				Path: "golang.org/x/mod/semver",
			},
			{
				Name: "gnu",
				Path: "github.com/goplus/llar/pkgs/gnu",
			},
		},
	})
	xgobuild.RegisterClassFileType("_llar.gox", "ModuleF", nil, "github.com/goplus/llar/formula")
}
