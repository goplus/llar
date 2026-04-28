// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package formula

import (
	"reflect"
	"testing"
)

// testFormula embeds ModuleF and provides a MainEntry that exercises every
// setter on the classfile DSL surface. This mirrors the struct layout the
// xgo interpreter generates for a real .gox formula.
type testFormula struct {
	ModuleF
	mainCalled bool
}

func (f *testFormula) MainEntry() {
	f.mainCalled = true
	f.Id("foo/bar")
	f.FromVer("1.0.0")
	f.Matrix(Matrix{Require: map[string][]string{"os": {"linux"}}})
	f.OnRequire(func(*Project, *ModuleDeps) {})
	f.OnBuild(func(*Context, *Project, *BuildResult) {})
	f.OnTest(func(*Context, *Project, *TestResult) {})
}

// TestGopt_ModuleF_Main exercises the classfile entry point together with
// every DSL setter on ModuleF. Going through Gopt_ModuleF_Main also covers
// the unexported app() helper it dispatches through.
func TestGopt_ModuleF_Main(t *testing.T) {
	f := &testFormula{}
	Gopt_ModuleF_Main(f)

	if !f.mainCalled {
		t.Fatal("MainEntry was not invoked by Gopt_ModuleF_Main")
	}
	if f.modPath != "foo/bar" {
		t.Errorf("Id: modPath = %q, want %q", f.modPath, "foo/bar")
	}
	if f.modFromVer != "1.0.0" {
		t.Errorf("FromVer: modFromVer = %q, want %q", f.modFromVer, "1.0.0")
	}
	wantMatrix := Matrix{Require: map[string][]string{"os": {"linux"}}}
	if !reflect.DeepEqual(f.matrix, wantMatrix) {
		t.Errorf("Matrix: matrix = %+v, want %+v", f.matrix, wantMatrix)
	}
	if f.fOnRequire == nil {
		t.Error("OnRequire: fOnRequire is nil")
	}
	if f.fOnBuild == nil {
		t.Error("OnBuild: fOnBuild is nil")
	}
	if f.fOnTest == nil {
		t.Error("OnTest: fOnTest is nil")
	}
}

// TestModuleF_app verifies that app() returns the pointer to the embedded
// gsh.App, which is the contract Gopt_ModuleF_Main relies on.
func TestModuleF_app(t *testing.T) {
	m := &ModuleF{}
	if got := m.app(); got != &m.App {
		t.Errorf("app() = %p, want %p (embedded App)", got, &m.App)
	}
}
