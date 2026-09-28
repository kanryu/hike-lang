package e2e_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var lifetimeHikec string

type HikeTestCase struct {
	Source       string
	ExpectedOut  string
	ExpectedExit int
}

func lifetimeProjectRoot() string {
	dir, err := os.Getwd()
	if err == nil {
		for {
			if _, statErr := os.Stat(filepath.Join(dir, "std")); statErr == nil {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	return root
}

func TestMain(m *testing.M) {
	root := lifetimeProjectRoot()
	tmp, err := os.MkdirTemp("", "hike-lifetime-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	name := "hikec"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	lifetimeHikec = filepath.Join(tmp, name)
	build := exec.Command("go", "build", "-o", lifetimeHikec, filepath.Join(root, "cmd", "hikec"))
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "lifetime HikeC build failed: %v\n%s\n", buildErr, output)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func RunHikeCase(t *testing.T, tc HikeTestCase) {
	t.Helper()
	root := lifetimeProjectRoot()
	tmp, err := os.MkdirTemp("", "hike-lifetime-case-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	if err := os.WriteFile(filepath.Join(tmp, "main.hike"), []byte(tc.Source), 0644); err != nil {
		t.Fatal(err)
	}
	stdRel, err := filepath.Rel(tmp, filepath.Join(root, "std"))
	if err != nil {
		stdRel = filepath.Join(root, "std")
	}
	stdRel = filepath.ToSlash(stdRel)
	mod := "module lifetime-test\n\nhike 0.1.0\n\nreplace std => " + stdRel + "\n"
	if entries, readErr := os.ReadDir(filepath.Join(root, "std")); readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				mod += fmt.Sprintf("replace std/%s => %s/%s\n", entry.Name(), stdRel, entry.Name())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "hike.mod"), []byte(mod), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	run := exec.Command(lifetimeHikec, "run", filepath.Join(tmp, "main.hike"))
	run.Dir = tmp
	run.Stdout = &stdout
	run.Stderr = &stderr
	err = run.Run()
	actualExit := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			actualExit = exitErr.ExitCode()
		} else {
			t.Fatalf("lifetime case failed: %v\n%s", err, stderr.String())
		}
	}
	if actualExit != tc.ExpectedExit {
		t.Fatalf("exit=%d want=%d\nstderr=%s", actualExit, tc.ExpectedExit, stderr.String())
	}
	normalize := func(value string) string {
		value = strings.ReplaceAll(value, "\r\n", "\n")
		return strings.TrimSpace(value)
	}
	if got, want := normalize(stdout.String()), normalize(tc.ExpectedOut); got != want {
		t.Fatalf("output=%q want=%q\nstderr=%s", got, want, stderr.String())
	}
}
