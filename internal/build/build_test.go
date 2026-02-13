package build

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/mod/module"
)

// mod creates a Module with the given path, version, and direct deps.
func mod(path, version string, deps ...*modules.Module) *modules.Module {
	return &modules.Module{
		Path:    path,
		Version: version,
		Deps:    deps,
	}
}

// paths returns the "Path@Version" strings for []*modules.Module.
func paths(mods []*modules.Module) string {
	var s []string
	for _, m := range mods {
		s = append(s, fmt.Sprintf("%s@%s", m.Path, m.Version))
	}
	return strings.Join(s, " ")
}

// versions returns the "Path@Version" strings for []module.Version.
func versions(vers []module.Version) string {
	var s []string
	for _, v := range vers {
		s = append(s, fmt.Sprintf("%s@%s", v.Path, v.Version))
	}
	return strings.Join(s, " ")
}

func TestConstructBuildList(t *testing.T) {
	b := &Builder{}

	t.Run("single module", func(t *testing.T) {
		A := mod("A", "1.0.0")
		got := b.constructBuildList([]*modules.Module{A})
		if want := "A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("linear chain", func(t *testing.T) {
		// A -> B -> C
		C := mod("C", "1.0.0")
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C) // main has all deps
		got := b.constructBuildList([]*modules.Module{A, B, C})
		if want := "C@1.0.0 B@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		// A -> B -> C, A -> D -> C
		C := mod("C", "1.2.0")
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D) // main has all deps
		got := b.constructBuildList([]*modules.Module{A, B, C, D})
		// C first (leaf), then B, then D, then A (root)
		if want := "C@1.2.0 B@1.2.0 D@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("deep chain", func(t *testing.T) {
		// A -> B -> C -> D -> E
		E := mod("E", "1.0.0")
		D := mod("D", "1.0.0", E)
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		got := b.constructBuildList([]*modules.Module{A, B, C, D, E})
		if want := "E@1.0.0 D@1.0.0 C@1.0.0 B@1.0.0 A@1.0.0"; paths(got) != want {
			t.Errorf("got %q, want %q", paths(got), want)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := b.constructBuildList(nil)
		if len(got) != 0 {
			t.Errorf("got %d modules, want 0", len(got))
		}
	})
}

func TestResolveModTransitiveDeps(t *testing.T) {
	b := &Builder{}

	t.Run("case1: simple", func(t *testing.T) {
		// C -> D
		D := mod("D", "1.0.0")
		C := mod("C", "1.2.0", D)
		B := mod("B", "1.2.0")
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, C)
		if want := "D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case2: diamond", func(t *testing.T) {
		// A -> B -> C, A -> D -> C  (MVS selects C@2.0.0)
		C := mod("C", "2.0.0")
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@2.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case3: diamond with transitive dep", func(t *testing.T) {
		// A -> B -> C, A -> D -> C -> E  (MVS selects C@2.0.0)
		E := mod("E", "1.0.0")
		C := mod("C", "2.0.0", E)
		B := mod("B", "1.2.0", C)
		D := mod("D", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "E@1.0.0 C@2.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case4: multiple direct deps", func(t *testing.T) {
		// B -> C, B -> D  (C and D are independent leaves)
		C := mod("C", "1.1.0")
		D := mod("D", "1.0.0")
		B := mod("B", "1.2.0", C, D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@1.1.0 D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("case5: dep ordering by topology", func(t *testing.T) {
		// B -> C -> D, B -> D
		D := mod("D", "1.2.0")
		C := mod("C", "1.1.0", D)
		B := mod("B", "1.2.0", C, D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		// D before C because C depends on D
		if want := "D@1.2.0 C@1.1.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("leaf module has no deps", func(t *testing.T) {
		D := mod("D", "1.0.0")
		A := mod("A", "1.0.0", D)
		targets := []*modules.Module{A, D}

		got := b.resolveModTransitiveDeps(targets, D)
		if len(got) != 0 {
			t.Errorf("got %q, want empty", versions(got))
		}
	})

	t.Run("deep transitive chain", func(t *testing.T) {
		// A -> B -> C -> D -> E
		E := mod("E", "1.0.0")
		D := mod("D", "1.0.0", E)
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "E@1.0.0 D@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("shared transitive dep", func(t *testing.T) {
		// A -> B -> D, A -> C -> D
		D := mod("D", "2.0.0")
		B := mod("B", "1.0.0", D)
		C := mod("C", "1.0.0", D)
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		// resolve for A: B and C both need D
		got := b.resolveModTransitiveDeps(targets, A)
		// D first (shared leaf), then B, then C
		if want := "D@2.0.0 B@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("w-shaped cross dependencies", func(t *testing.T) {
		// B -> C -> F, B -> C -> G
		// B -> D -> F, B -> D -> G
		F := mod("F", "1.0.0")
		G := mod("G", "1.0.0")
		C := mod("C", "1.0.0", F, G)
		D := mod("D", "1.0.0", F, G)
		B := mod("B", "1.0.0", C, D)
		A := mod("A", "1.0.0", B, C, D, F, G)
		targets := []*modules.Module{A, B, C, D, F, G}

		got := b.resolveModTransitiveDeps(targets, B)
		// F and G are leaves, then C and D (both depend on F,G), order follows DFS
		if want := "F@1.0.0 G@1.0.0 C@1.0.0 D@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("circular dependency", func(t *testing.T) {
		// B -> C -> D -> B (cycle)
		// visited breaks the cycle at B
		D := mod("D", "1.0.0")
		C := mod("C", "1.0.0", D)
		B := mod("B", "1.0.0", C)
		D.Deps = []*modules.Module{B} // close the cycle: D -> B
		A := mod("A", "1.0.0", B, C, D)
		targets := []*modules.Module{A, B, C, D}

		got := b.resolveModTransitiveDeps(targets, B)
		// B is excluded (mod itself), D -> B is a back-edge (B already visited)
		// so: visit(C) -> visit(D) -> visit(B) no-op -> append D -> append C
		if want := "D@1.0.0 C@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})

	t.Run("wide fan-out", func(t *testing.T) {
		// B -> C, B -> D, B -> E  (all leaves, no inter-deps)
		C := mod("C", "1.0.0")
		D := mod("D", "1.0.0")
		E := mod("E", "1.0.0")
		B := mod("B", "1.0.0", C, D, E)
		A := mod("A", "1.0.0", B, C, D, E)
		targets := []*modules.Module{A, B, C, D, E}

		got := b.resolveModTransitiveDeps(targets, B)
		if want := "C@1.0.0 D@1.0.0 E@1.0.0"; versions(got) != want {
			t.Errorf("got %q, want %q", versions(got), want)
		}
	})
}
