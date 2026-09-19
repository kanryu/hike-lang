package testutil

import "os/exec"

// BuildHikec builds a fresh Hike compiler for one Go test package.
func BuildHikec(root, output string) ([]byte, error) {
	cmd := exec.Command("go", "build", "-o", output, root+"/cmd/hikec")
	cmd.Dir = root
	return cmd.CombinedOutput()
}
