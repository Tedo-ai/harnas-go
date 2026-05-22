package harnas

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultBashSessionMaxOutputBytes = 64 * 1024

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[mGKHF]|\r`)

type BashSessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*bashSession
}

func NewBashSessionRegistry() *BashSessionRegistry {
	return &BashSessionRegistry{sessions: map[string]*bashSession{}}
}

func (r *BashSessionRegistry) Handle(args map[string]any, config map[string]any) (string, error) {
	action := stringValue(args["action"])
	command := stringValue(args["command"])
	if action == "" && command != "" {
		action = "run"
	}
	if action == "" {
		action = "status"
	}

	sessionID := stringValue(args["session_id"])
	switch action {
	case "run":
		if command == "" {
			return "", fmt.Errorf("missing required argument: command")
		}
		session, err := r.session(sessionID, config, true)
		if err != nil {
			return "", err
		}
		timeout := durationMillis(args["timeout_ms"])
		commandEnv, err := parseCommandEnv(args["env"])
		if err != nil {
			return "", err
		}
		return marshalBashSessionResult(session.run(command, commandEnv, timeout))
	case "status":
		session, err := r.session(sessionID, config, false)
		if err != nil {
			return "", err
		}
		return marshalBashSessionResult(session.status())
	case "kill":
		session, err := r.session(sessionID, config, false)
		if err != nil {
			return "", err
		}
		return marshalBashSessionResult(session.kill())
	default:
		return "", fmt.Errorf("unknown bash_session action: %s", action)
	}
}

func (r *BashSessionRegistry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, session := range r.sessions {
		_ = session.close()
	}
	r.sessions = map[string]*bashSession{}
}

func (r *BashSessionRegistry) session(id string, config map[string]any, create bool) (*bashSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != "" {
		if session := r.sessions[id]; session != nil {
			return session, nil
		}
		if !create {
			return nil, fmt.Errorf("unknown bash_session session_id: %s", id)
		}
	} else if !create {
		return nil, fmt.Errorf("missing required argument: session_id")
	}

	if id == "" {
		id = "sh_" + newID()
	}
	session, err := startBashSession(id, config)
	if err != nil {
		return nil, err
	}
	r.sessions[id] = session
	return session, nil
}

type bashSession struct {
	id        string
	shell     string
	shellType string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *ringBuffer
	stderr    *ringBuffer

	mu      sync.Mutex
	current *bashCommand
	closed  bool
}

type bashCommand struct {
	token       string
	done        chan struct{}
	exitCode    *int
	stdoutStart int64
	stderrStart int64
	stdoutDone  bool
	stderrDone  bool
}

type bashSessionResult struct {
	SessionID     string `json:"session_id"`
	Status        string `json:"status"`
	ExitCode      *int   `json:"exit_code"`
	Stdout        string `json:"stdout"`
	Stderr        string `json:"stderr"`
	CommandStdout string `json:"command_stdout"`
	CommandStderr string `json:"command_stderr"`
	Truncated     bool   `json:"truncated"`
}

func startBashSession(id string, config map[string]any) (*bashSession, error) {
	shell, shellType := resolveBashSessionShell(config)
	maxBytes := intValue(config["max_output_bytes"])
	if maxBytes <= 0 {
		maxBytes = DefaultBashSessionMaxOutputBytes
	}
	cwd := stringValue(config["cwd"])
	if cwd == "" {
		cwd = "."
	}
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(shell)
	cmd.Dir = absCWD
	cmd.Stdin = nil
	configureBashSessionCommand(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	session := &bashSession{
		id:        id,
		shell:     shell,
		shellType: shellType,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    newRingBuffer(maxBytes),
		stderr:    newRingBuffer(maxBytes),
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go session.readStdout(stdoutPipe)
	go session.readStderr(stderrPipe)
	go session.waitShell()
	return session, nil
}

func effectiveBashSessionConfig(config map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range config {
		out[key] = value
	}
	_, shellType := resolveBashSessionShell(out)
	out["shell_type"] = shellType
	if stringValue(out["shell"]) == "" {
		out["shell"] = "auto"
	}
	return out
}

func (s *bashSession) run(command string, commandEnv map[string]string, timeout time.Duration) bashSessionResult {
	s.mu.Lock()
	for s.current != nil {
		current := s.current
		s.mu.Unlock()
		<-current.done
		s.mu.Lock()
	}
	if s.closed {
		s.mu.Unlock()
		return s.snapshot("killed", nil, nil)
	}

	run := &bashCommand{
		token:       newID(),
		done:        make(chan struct{}),
		stdoutStart: s.stdout.Offset(),
		stderrStart: s.stderr.Offset(),
	}
	s.current = run
	executable := command
	if len(commandEnv) > 0 {
		executable = wrapCommandEnv(s.shell, s.shellType, command, commandEnv)
	}
	framed := frameCommand(executable, run.token, s.shellType)
	if _, err := io.WriteString(s.stdin, framed); err != nil {
		s.current = nil
		close(run.done)
		s.mu.Unlock()
		return s.snapshot("killed", nil, run)
	}
	s.mu.Unlock()

	if timeout > 0 {
		select {
		case <-run.done:
			return s.snapshot("completed", run.exitCode, run)
		case <-time.After(timeout):
			return s.snapshot("running", nil, run)
		}
	}
	<-run.done
	return s.snapshot("completed", run.exitCode, run)
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func parseCommandEnv(value any) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bash_session env must be an object")
	}
	out := map[string]string{}
	for key, item := range raw {
		if !envNamePattern.MatchString(key) {
			return nil, fmt.Errorf("invalid bash_session env key: %s", key)
		}
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("bash_session env value for %s must be a string", key)
		}
		out[key] = text
	}
	return out, nil
}

func wrapCommandEnv(shell, shellType, command string, env map[string]string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if shellType == "powershell" {
		assignments := make([]string, 0, len(keys))
		restores := make([]string, 0, len(keys))
		for _, key := range keys {
			assignments = append(assignments, fmt.Sprintf("$__harnas_old_%s=$env:%s; $env:%s=%s", key, key, key, powershellQuote(env[key])))
			restores = append(restores, fmt.Sprintf("$env:%s=$__harnas_old_%s", key, key))
		}
		return strings.Join(assignments, "; ") + "; try { " + command + " } finally { " + strings.Join(restores, "; ") + " }"
	}
	if shellType == "cmd" {
		parts := []string{"setlocal"}
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("set %q", key+"="+env[key]))
		}
		parts = append(parts, command, "endlocal")
		return strings.Join(parts, " & ")
	}
	parts := []string{"env"}
	for _, key := range keys {
		parts = append(parts, key+"="+shellQuote(env[key]))
	}
	parts = append(parts, shellQuote(shell), "-c", shellQuote(command))
	return strings.Join(parts, " ")
}

func frameCommand(command, token, shellType string) string {
	switch shellType {
	case "powershell":
		return fmt.Sprintf("\n& { %s }; $__harnas_status=$LASTEXITCODE; if ($null -eq $__harnas_status) { $__harnas_status=0 }; [Console]::Error.WriteLine('__HARNAS_ERR_DONE_%s'); [Console]::Out.WriteLine('__HARNAS_DONE_%s:' + $__harnas_status)\n", command, token, token)
	case "cmd":
		return fmt.Sprintf("\r\n%s\r\nset __harnas_status=%%ERRORLEVEL%%\r\necho __HARNAS_ERR_DONE_%s 1>&2\r\necho __HARNAS_DONE_%s:%%__harnas_status%%\r\n", command, token, token)
	default:
		return fmt.Sprintf("\n{ %s\n} </dev/null; __harnas_status=$?; printf '__HARNAS_ERR_DONE_%s\\n' >&2; printf '__HARNAS_DONE_%s:%%s\\n' \"$__harnas_status\"\n", command, token, token)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (s *bashSession) status() bashSessionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		return s.snapshot("running", nil, s.current)
	}
	if s.closed {
		return s.snapshot("killed", nil, nil)
	}
	return s.snapshot("completed", nil, nil)
}

func (s *bashSession) kill() bashSessionResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return s.snapshot("completed", nil, nil)
	}
	_ = s.terminateProcess(false)
	current := s.current
	s.mu.Unlock()
	select {
	case <-current.done:
	case <-time.After(3 * time.Second):
		_ = s.terminateProcess(true)
		<-current.done
	}
	s.mu.Lock()
	s.closed = true
	return s.snapshot("killed", nil, current)
}

func (s *bashSession) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	_ = s.terminateProcess(true)
	return nil
}

func (s *bashSession) snapshot(status string, exitCode *int, command *bashCommand) bashSessionResult {
	commandStdout := ""
	commandStderr := ""
	if command != nil {
		commandStdout = s.stdout.StringFrom(command.stdoutStart)
		commandStderr = s.stderr.StringFrom(command.stderrStart)
	}
	return bashSessionResult{
		SessionID:     s.id,
		Status:        status,
		ExitCode:      exitCode,
		Stdout:        s.stdout.String(),
		Stderr:        s.stderr.String(),
		CommandStdout: commandStdout,
		CommandStderr: commandStderr,
		Truncated:     s.stdout.Truncated() || s.stderr.Truncated(),
	}
}

func (s *bashSession) readStdout(reader io.Reader) {
	buffered := bufio.NewReader(reader)
	for {
		line, err := buffered.ReadString('\n')
		if line != "" {
			if before, ok := s.handleSentinel(line); ok {
				if before != "" {
					s.stdout.WriteString(stripANSI(before))
				}
				if err != nil {
					return
				}
				continue
			}
			s.stdout.WriteString(stripANSI(line))
		}
		if err != nil {
			return
		}
	}
}

func (s *bashSession) handleSentinel(line string) (string, bool) {
	const prefix = "__HARNAS_DONE_"
	index := strings.Index(line, prefix)
	if index < 0 {
		return "", false
	}
	before := line[:index]
	trimmed := strings.TrimSpace(line[index:])
	parts := strings.SplitN(strings.TrimPrefix(trimmed, prefix), ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	if before != "" {
		s.stdout.WriteString(stripANSI(before))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.token != parts[0] {
		return "", false
	}
	if code, err := strconv.Atoi(parts[1]); err == nil {
		s.current.exitCode = &code
	}
	s.current.stdoutDone = true
	s.completeCurrentIfReadyLocked()
	return "", true
}

func (s *bashSession) readStderr(reader io.Reader) {
	buffered := bufio.NewReader(reader)
	for {
		line, err := buffered.ReadString('\n')
		if line != "" {
			if before, ok := s.handleStderrSentinel(line); ok {
				if before != "" {
					s.stderr.WriteString(stripANSI(before))
				}
				if err != nil {
					return
				}
				continue
			}
			s.stderr.WriteString(stripANSI(line))
		}
		if err != nil {
			return
		}
	}
}

func (s *bashSession) handleStderrSentinel(line string) (string, bool) {
	const prefix = "__HARNAS_ERR_DONE_"
	index := strings.Index(line, prefix)
	if index < 0 {
		return "", false
	}
	before := line[:index]
	token := strings.TrimSpace(strings.TrimPrefix(line[index:], prefix))
	if before != "" {
		s.stderr.WriteString(stripANSI(before))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.token != token {
		return "", false
	}
	s.current.stderrDone = true
	s.completeCurrentIfReadyLocked()
	return "", true
}

func (s *bashSession) completeCurrentIfReadyLocked() {
	if s.current == nil || !s.current.stdoutDone || !s.current.stderrDone {
		return
	}
	close(s.current.done)
	s.current = nil
}

func (s *bashSession) waitShell() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.current != nil {
		s.current.exitCode = &code
		close(s.current.done)
		s.current = nil
	}
}

type ringBuffer struct {
	mu        sync.Mutex
	max       int
	data      []byte
	total     int64
	truncated bool
}

func newRingBuffer(max int) *ringBuffer {
	return &ringBuffer{max: max}
}

func (b *ringBuffer) WriteString(value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	chunk := []byte(value)
	b.total += int64(len(chunk))
	b.data = append(b.data, chunk...)
	if b.max > 0 && len(b.data) > b.max {
		b.truncated = true
		b.data = append([]byte(nil), b.data[len(b.data)-b.max:]...)
	}
}

func (b *ringBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func (b *ringBuffer) Offset() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total
}

func (b *ringBuffer) StringFrom(offset int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	startOffset := b.total - int64(len(b.data))
	if offset < startOffset {
		offset = startOffset
	}
	if offset > b.total {
		offset = b.total
	}
	start := int(offset - startOffset)
	return string(b.data[start:])
}

func (b *ringBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func durationMillis(value any) time.Duration {
	ms := intValue(value)
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func marshalBashSessionResult(result bashSessionResult) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func BuiltinBashSession(args map[string]any, config map[string]any) (string, error) {
	registry := NewBashSessionRegistry()
	defer registry.Close()
	return registry.Handle(args, config)
}
