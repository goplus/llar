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
