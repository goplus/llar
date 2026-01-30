// Copyright 2024 The llar Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cmp

import "github.com/goplus/llar/mod/module"

const GopPackage = true

type CmpApp struct {
	fCompareVer func(a, b module.Version) int
}

// The provided function fn will be used to compare version strings
// when resolving dependencies for those versions which are not in Debian-style.
func (f *CmpApp) CompareVer(fn func(a, b module.Version) int) {
	f.fCompareVer = fn
}

// Gopt_CmpApp_Main is main entry of this classfile.
func Gopt_CmpApp_Main(this interface{ MainEntry() }) {
	this.MainEntry()
}
