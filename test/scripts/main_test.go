// Package scripts_test runs the release reconciliation's shell against local
// Git repositories, with bash doubles standing in for the remote CLIs.
package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// executableMode makes a test double runnable.
const executableMode = 0o755

// projectRoot is the repository this package tests.
func projectRoot() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}
	return root
}

// environment returns this process's environment with overrides applied.
func environment(overrides map[string]string) []string {
	result := []string{}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[name]; !overridden {
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
