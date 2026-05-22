//go:build windows

package harnas

import (
	"os/exec"
	"strings"
)

func resolveBashSessionShell(config map[string]any) (string, string) {
	shell := stringValue(config["shell"])
	if shell == "" || shell == "auto" {
		for _, candidate := range []string{"pwsh", "powershell.exe", "cmd.exe"} {
			if _, err := exec.LookPath(candidate); err == nil {
				shell = candidate
				break
			}
		}
		if shell == "" {
			shell = "cmd.exe"
		}
	}
	shellType := stringValue(config["shell_type"])
	if shellType == "" || shellType == "auto" {
		shellType = detectWindowsShellType(shell)
	}
	return shell, shellType
}

func detectWindowsShellType(shell string) string {
	lower := strings.ToLower(shell)
	if strings.Contains(lower, "powershell") || strings.Contains(lower, "pwsh") {
		return "powershell"
	}
	return "cmd"
}

func defaultBashSessionShellType() string {
	shell, shellType := resolveBashSessionShell(map[string]any{"shell": "auto", "shell_type": "auto"})
	_ = shell
	return shellType
}

func configureBashSessionCommand(_ *exec.Cmd) {}

func (s *bashSession) terminateProcess(_ bool) error {
	if s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Kill()
}
