// Package module provides types for representing module versions and comparison.
package module

// Version represents a specific version of a module.
type Version struct {
	ID      string // ModuleID
	Version string // Version string (e.g., "v1.0.0")
}

// VersionComparator is a function type that compares two versions.
// It returns:
//   - negative value if v1 < v2
//   - zero if v1 == v2
//   - positive value if v1 > v2
type VersionComparator func(v1, v2 string) int
