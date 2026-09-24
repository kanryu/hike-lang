package e2e_test

import (
	"runtime"
	"testing"
)

// //go:build式とGOOS/GOARCH形式のファイル名選択を検証する。
func TestE2EBuildConstraints(t *testing.T) {
	t.Parallel()

	RunHikeCase(t, HikeTestCase{
		Source: `
package main

func printf(format string, ...) int

func main() int {
    printf("SELECTED=%d,CONSTRAINED=%d\n", selectedPlatform(), constrainedPlatform())
    return 0
}
`,
		Files: map[string]string{
			"platform_windows_amd64.hike": `package main

func selectedPlatform() int { return 64 }
`,
			"platform_linux_amd64.hike": `package main

func selectedPlatform() int { return 99 }
`,
			"constraint.hike": `//go:build (windows && amd64) || (darwin && !cgo)
package main

func constrainedPlatform() int { return 1 }
`,
			"constraint_linux.hike": `//hike:build linux && amd64
package main

func constrainedPlatform() int { return 2 }
`,
		},
		ExpectedOut: func() string {
			if runtime.GOOS == "windows" {
				return "SELECTED=64,CONSTRAINED=1"
			}
			return "SELECTED=99,CONSTRAINED=2"
		}(),
		ExpectedExit: 0,
	})
}
