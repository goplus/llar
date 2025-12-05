package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormulaDir(t *testing.T) {
	// Get the formula directory
	formulaDir, err := FormulaDir()
	if err != nil {
		t.Fatalf("FormulaDir() returned error: %v", err)
	}

	// Check that the directory path is not empty
	if formulaDir == "" {
		t.Fatal("FormulaDir() returned empty path")
	}

	// Get the expected cache directory
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir() returned error: %v", err)
	}
	expectedDir := filepath.Join(userCacheDir, ".llar", "formulas")

	// Verify the returned path matches expected path
	if formulaDir != expectedDir {
		t.Errorf("FormulaDir() = %q, want %q", formulaDir, expectedDir)
	}

	// Verify the directory was created
	info, err := os.Stat(formulaDir)
	if err != nil {
		t.Fatalf("Directory was not created: %v", err)
	}

	// Verify it's a directory
	if !info.IsDir() {
		t.Error("FormulaDir() created a file instead of a directory")
	}

	// Verify permissions (0700)
	mode := info.Mode().Perm()
	expectedMode := os.FileMode(0700)
	if mode != expectedMode {
		t.Errorf("Directory has permissions %v, want %v", mode, expectedMode)
	}
}

// TestFormulaDirIdempotent tests the idempotency of the FormulaDir function.
// It verifies that multiple calls return the same result without side effects.
func TestFormulaDirIdempotent(t *testing.T) {
	// Call FormulaDir multiple times
	dir1, err := FormulaDir()
	if err != nil {
		t.Fatalf("First FormulaDir() call failed: %v", err)
	}

	dir2, err := FormulaDir()
	if err != nil {
		t.Fatalf("Second FormulaDir() call failed: %v", err)
	}

	// Should return the same directory
	if dir1 != dir2 {
		t.Errorf("FormulaDir() not idempotent: first call = %q, second call = %q", dir1, dir2)
	}

	// Directory should still exist
	if _, err := os.Stat(dir1); err != nil {
		t.Errorf("Directory no longer exists after second call: %v", err)
	}
}

// TestFormulaDirWithCustomCache tests the behavior with a custom cache directory.
// It verifies that the function works correctly under different cache directory configurations.
func TestFormulaDirWithCustomCache(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Save original cache dir environment variable
	originalCacheDir := os.Getenv("XDG_CACHE_HOME")
	t.Cleanup(func() {
		if originalCacheDir != "" {
			os.Setenv("XDG_CACHE_HOME", originalCacheDir)
		} else {
			os.Unsetenv("XDG_CACHE_HOME")
		}
	})

	// Set custom cache directory (works on Linux/Unix systems)
	os.Setenv("XDG_CACHE_HOME", tempDir)

	// On macOS/Windows, os.UserCacheDir() might not respect XDG_CACHE_HOME
	// So we just verify that FormulaDir() succeeds
	formulaDir, err := FormulaDir()
	if err != nil {
		t.Fatalf("FormulaDir() failed with custom cache dir: %v", err)
	}

	// Verify directory was created and is accessible
	if _, err := os.Stat(formulaDir); err != nil {
		t.Errorf("Formula directory not accessible: %v", err)
	}
}
