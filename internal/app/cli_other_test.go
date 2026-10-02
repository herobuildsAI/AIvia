//go:build !darwin && !linux

package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestCLIUnsupportedPlatformFailsClosed(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, err := runCLI(context.Background(), executable, []string{"--version"}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires macOS or Linux") || len(output) != 0 {
		t.Fatalf("unsupported platform did not reject CLI execution: %v", err)
	}
	status := inspectCLI(context.Background(), "claude-cli", executable)
	if status.Available || !strings.Contains(status.Detail, "requires macOS or Linux") {
		t.Fatalf("unsupported CLI reported as available: %+v", status)
	}
}
