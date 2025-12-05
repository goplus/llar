package cmp

import "github.com/goplus/llar/pkgs/mod/module"

const GopPackage = true

type CmpApp struct {
	fCompareVer module.VersionComparator
}

func (f *CmpApp) CompareVer(fn module.VersionComparator) {
	f.fCompareVer = fn
}

func Gopt_CmpApp_Main(this interface{ MainEntry() }) {
	this.MainEntry()
}
