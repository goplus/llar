package cmp

import "github.com/goplus/llar/pkgs/mod/module"

const GopPackage = true

type CmpApp struct {
	fCompareVer func(a, b module.Version) int
}

func (f *CmpApp) CompareVer(fn func(a, b module.Version) int) {
	f.fCompareVer = fn
}

func Gopt_CmpApp_Main(this interface{ MainEntry() }) {
	this.MainEntry()
}
