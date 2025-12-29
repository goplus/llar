package modules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewResolver(t *testing.T) {
	r := newResolver()
	if r == nil {
		t.Fatal("newResolver returned nil")
	}
	if r.listDrvier == nil {
		t.Error("resolver.listDrvier is nil")
	}
}

func TestResolverLookup_AutoInitGoMod(t *testing.T) {
	// Create a temporary directory as test project root
	tmpDir := t.TempDir()

	// Ensure no go.mod exists in the directory
	goModPath := filepath.Join(tmpDir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		t.Fatal("go.mod already exists in temp directory")
	}

	r := newResolver()

	// Look up a standard library package (no download needed)
	// This should trigger go mod init
	dir, found := r.Lookup(tmpDir, "fmt")

	// Verify go.mod was created automatically
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		t.Error("go.mod was not created automatically")
	} else {
		// Read and verify go.mod content
		content, err := os.ReadFile(goModPath)
		if err != nil {
			t.Errorf("Failed to read go.mod: %v", err)
		}
		t.Logf("Created go.mod content:\n%s", string(content))
	}

	// fmt is stdlib, may not return found depending on environment
	t.Logf("Lookup result: dir=%s, found=%v", dir, found)
}

func TestResolverLookup_DownloadModule(t *testing.T) {
	// This test will actually download a Go module
	tmpDir := t.TempDir()

	r := newResolver()

	// Look up a real external module
	// Use a small, stable module for testing
	testModule := "golang.org/x/mod/semver"

	dir, found := r.Lookup(tmpDir, testModule)

	// Verify go.mod was created
	goModPath := filepath.Join(tmpDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		t.Error("go.mod was not created")
	}

	// Verify the module was found and directory was returned
	if !found {
		t.Error("Module was not found")
	}

	if dir == "" {
		t.Error("Module directory is empty")
	}

	// Verify the returned directory actually exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("Module directory does not exist: %s", dir)
	}

	t.Logf("Successfully downloaded module to: %s", dir)
}

func TestResolverLookup_WithExistingGoMod(t *testing.T) {
	// Test behavior when go.mod already exists
	tmpDir := t.TempDir()

	// Pre-create go.mod
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := []byte("module testmodule\n\ngo 1.21\n")
	if err := os.WriteFile(goModPath, goModContent, 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	r := newResolver()

	// Look up a module
	testModule := "golang.org/x/mod/semver"
	dir, found := r.Lookup(tmpDir, testModule)

	// Verify go.mod still exists (not overwritten)
	newContent, err := os.ReadFile(goModPath)
	if err != nil {
		t.Errorf("Failed to read go.mod: %v", err)
	}

	// go mod init won't reinitialize if go.mod exists
	// but content may change due to go get (adding dependencies)
	t.Logf("go.mod content after lookup:\n%s", string(newContent))

	if !found {
		t.Error("Module was not found")
	}

	if dir == "" {
		t.Error("Module directory is empty")
	}

	t.Logf("Module found at: %s", dir)
}

func TestExecCommand(t *testing.T) {
	t.Run("SuccessfulCommand", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Test a command that should succeed
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("execCommand panicked unexpectedly: %v", r)
			}
		}()

		output := execCommand(tmpDir, "go", "version")
		if len(output) == 0 {
			t.Error("execCommand returned empty output for 'go version'")
		}
		t.Logf("go version output: %s", string(output))
	})

	t.Run("FailingCommand", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Test that failing commands should panic
		defer func() {
			if r := recover(); r == nil {
				t.Error("execCommand should panic on failing command")
			} else {
				t.Logf("execCommand correctly panicked: %v", r)
			}
		}()

		execCommand(tmpDir, "go", "invalid-subcommand-xyz123")
	})
}
