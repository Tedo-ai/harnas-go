//go:build unix

package harnas

import (
	"os/exec"
	"syscall"
)

func resolveBashSessionShell(config map[string]any) (string, string) {
	shell := stringValue(config["shell"])
	if shell == "" || shell == "auto" {
		shell = "bash"
	}
	if _, err := exec.LookPath(shell); err != nil && shell == "bash" {
		shell = "sh"
	}
	shellType := stringValue(config["shell_type"])
	if shellType == "" || shellType == "auto" {
		shellType = "posix"
	}
	return shell, shellType
}

func defaultBashSessionShellType() string {
	return "posix"
}

func configureBashSessionCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (s *bashSession) terminateProcess(force bool) error {
	if s.cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
	if err != nil {
		return err
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return syscall.Kill(-pgid, signal)
}
