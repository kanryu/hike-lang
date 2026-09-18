package wabt_test

import "testing"

// WABT counterpart of tests/e2e/e2e_build_constraints_test.go.
func TestWabtBuildConstraints(t *testing.T) {
	wasm := buildWabtProject(t, `
package main

func main() int {
    return selectedPlatform() + constrainedPlatform()
}
`, map[string]string{
		"platform_wasip1_wasm32.hike": `package main

func selectedPlatform() int { return 32 }
`,
		"platform_linux_amd64.hike": `package main

func selectedPlatform() int { return 99 }
`,
		"constraint.hike": `//go:build (wasip1 && wasm32) || (darwin && !cgo)
package main

func constrainedPlatform() int { return 1 }
`,
		"constraint_linux.hike": `//hike:build linux && amd64
package main

func constrainedPlatform() int { return 2 }
`,
	})

	if got, want := runWabt(t, wasm), "WABT_RESULT=33\n"; got != want {
		t.Fatalf("Wabt build-constraints output = %q, want %q", got, want)
	}
}
