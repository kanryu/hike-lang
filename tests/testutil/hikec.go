package testutil

import "os/exec"

import (
	"os"
	"strings"
)

// CompileForkEnabled enables the optional fork-mode test pass without
// changing the default behavior of the test suite.
func CompileForkEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HIKE_TEST_COMPILE_FORK")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// AddCompileFork appends the compiler option when explicitly requested by the
// test environment.
func AddCompileFork(args []string) []string {
	if !CompileForkEnabled() {
		return args
	}
	return append(args, "-compile-fork")
}

// BuildHikec builds a fresh Hike compiler for one Go test package.
func BuildHikec(root, output string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", output, root+"/cmd/hikec")
	cmd.Dir = root
	return cmd.CombinedOutput()
}
