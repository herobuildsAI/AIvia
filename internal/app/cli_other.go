//go:build !darwin && !linux

package app

import (
	"errors"
	"os/exec"
)

func configureCLIProcess(cmd *exec.Cmd) error {
	return errors.New("CLI integration currently requires macOS or Linux process-group cancellation.")
}
