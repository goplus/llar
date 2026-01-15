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
	xgobuild.RegisterProject(&modfile.Project{
		Ext:   "_llar.gox",
		Class: "ModuleF",
		PkgPaths: []string{
			"github.com/goplus/llar/formula",
		},
		Import: []*modfile.Import{
			{
				Name: "cmake",
				Path: "github.com/goplus/llar/pkgs/buildsys/cmake",
			},
			{
				Name: "autotools",
				Path: "github.com/goplus/llar/pkgs/buildsys/autotools",
			},
		},
	})
}
