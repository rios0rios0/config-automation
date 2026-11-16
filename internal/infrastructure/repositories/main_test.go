package repositories_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(writeFakesAndRun(m))
}

// writeFakesAndRun writes the fake CLIs before any test runs. Writing an
// executable while a parallel test forks lets the child inherit the open
// file, and executing it then fails with "text file busy".
func writeFakesAndRun(m *testing.M) int {
	directory := filepath.Dir(fakeClaudePath())
	// The fake must be executable to stand in for the CLI.
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.MkdirAll(directory, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(directory)
	// nosemgrep: go.lang.correctness.permissions.file_permission.incorrect-default-permission
	if err := os.WriteFile(fakeClaudePath(), []byte(fakeClaude), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// fakeClaudePath is where TestMain writes the fake Claude CLI.
func fakeClaudePath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("config-automation-fakes-%d", os.Getpid()), "claude")
}
