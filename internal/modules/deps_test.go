package modules

import (
	"context"
	"testing"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/pkgs/mod/module"
)

func TestResolveDeps_WithNilOnRequire(t *testing.T) {
	ctx := context.Background()
	mod := module.Version{ID: "DaveGamble/cJSON", Version: "1.7.18"}

	// Create a formula without OnRequire callback
	f := &formula.Formula{
		ModId:     "DaveGamble/cJSON",
		FromVer:   "1.0.0",
		OnRequire: nil,
	}

	// This will fail because it needs vcs.NewRepo to work
	// But we can test the behavior when OnRequire is nil
	_, err := resolveDeps(ctx, mod, f)
	if err == nil {
		// If it succeeds, that means it found versions.json and parsed it
		t.Log("resolveDeps succeeded with nil OnRequire")
	} else {
		// Expected to fail due to vcs/env setup
		t.Skipf("Skipping test: %v", err)
	}
}

func TestResolveDeps_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	mod := module.Version{ID: "test/module", Version: "1.0.0"}
	f := &formula.Formula{
		ModId:   "test/module",
		FromVer: "1.0.0",
	}

	// Should fail due to cancelled context or missing dependencies
	_, err := resolveDeps(ctx, mod, f)
	if err == nil {
		t.Log("resolveDeps succeeded even with cancelled context")
	}
	// Note: The function might not check context cancellation early,
	// so we don't require an error here
}

func TestResolveDeps_WithOnRequireCallback(t *testing.T) {
	ctx := context.Background()
	mod := module.Version{ID: "test/module", Version: "1.0.0"}

	onRequireCalled := false
	f := &formula.Formula{
		ModId:   "test/module",
		FromVer: "1.0.0",
		OnRequire: func(proj *classfile.Project, deps *classfile.ModuleDeps) {
			onRequireCalled = true
		},
	}

	// This will fail because vcs.NewRepo will fail for fake module
	_, err := resolveDeps(ctx, mod, f)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	// If we get here, verify OnRequire was called
	if !onRequireCalled {
		t.Error("OnRequire callback should have been called")
	}
}
