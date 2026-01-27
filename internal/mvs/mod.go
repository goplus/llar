package mvs

// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

import (
	"sort"
	"strings"

	"github.com/goplus/llar/mod/module"
)

// modules and their version ordering.
func sortWith(cmp func(p string, v1, v2 string) int, list []module.Version) {
	sort.Slice(list, func(i, j int) bool {
		mi := list[i]
		mj := list[j]
		if mi.Path != mj.Path {
			return mi.Path < mj.Path
		}
		vi := mi.Version
		vj := mj.Version
		var fi, fj string
		if k := strings.Index(vi, "/"); k >= 0 {
			vi, fi = vi[:k], vi[k:]
		}
		if k := strings.Index(vj, "/"); k >= 0 {
			vj, fj = vj[:k], vj[k:]
		}
		if vi != vj {
			return cmp(mi.Path, mi.Version, mj.Version) < 0
		}
		return fi < fj
	})
}
