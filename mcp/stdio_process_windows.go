//go:build windows

package mcp

import "os/exec"

func configureStdioCommand(_ *exec.Cmd) {}

func terminateStdioProcess(cmd *exec.Cmd, _ bool) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
