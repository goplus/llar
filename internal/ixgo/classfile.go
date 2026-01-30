// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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
			"github.com/goplus/llar/formula",
		},
		Import: []*modfile.Import{
			{
				Name: "semver",
				Path: "golang.org/x/mod/semver",
			},
			{
				Name: "gnu",
				Path: "github.com/goplus/llar/x/gnu",
			},
		},
	})
	xgobuild.RegisterProject(&modfile.Project{
		Ext:   "_llar.gox",
		Class: "ModuleF",
		PkgPaths: []string{
			"github.com/goplus/llar/formula",
		},
	})
}
