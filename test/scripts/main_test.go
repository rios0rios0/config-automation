// Package scripts_test runs the refresh's shell against local Git
// repositories, with bash doubles standing in for the remote CLIs.
package scripts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// binaryDirectoryVariable carries the directory of the refresh-docs
	// binary that TestMain builds to every test.
	binaryDirectoryVariable = "REFRESH_DOCS_TEST_BIN"
	// executableMode makes a test double runnable.
	executableMode = 0o755
)

func TestMain(m *testing.M) {
	os.Exit(buildAndRun(m))
}

// buildAndRun builds refresh-docs once, as the workflows do before a
// batch, and runs the tests with it on PATH.
func buildAndRun(m *testing.M) int {
	directory, err := os.MkdirTemp("", "refresh-docs-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(directory)
	build := exec.Command("go", "build", "-o", filepath.Join(directory, "refresh-docs"), "./cmd/refresh-docs")
	build.Dir = projectRoot()
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "building refresh-docs: %v\n%s", buildErr, output)
		return 1
	}
	if err = os.Setenv(binaryDirectoryVariable, directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// projectRoot is the repository this package tests.
func projectRoot() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

// environment returns this process's environment with overrides applied.
// CLAUDE_EFFORT set in the caller's shell never leaks into the defaults.
func environment(overrides map[string]string) []string {
	result := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[name]; !overridden && name != "CLAUDE_EFFORT" {
			result = append(result, entry)
		}
	}
	for name, value := range overrides {
		result = append(result, name+"="+value)
	}
	return result
}

// writeTree writes files below root, creating their directories.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		location := filepath.Join(root, filepath.FromSlash(path))
		// A directory needs its execute bit; the rule below is written for files.
		// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
		if err := os.MkdirAll(filepath.Dir(location), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(location, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// writeStub writes an executable bash double named name into directory.
func writeStub(t *testing.T, directory, name, body string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(directory, name),
		[]byte("#!/usr/bin/env bash\n"+body),
		executableMode,
	); err != nil {
		t.Fatal(err)
	}
}
