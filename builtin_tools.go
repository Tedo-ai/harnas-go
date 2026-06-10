package harnas

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultShellTimeoutSeconds = 30
	GrepMaxMatches             = 200
	MaxFetchBytes              = 256 * 1024
)

var DefaultFetchURLTimeout = 60 * time.Second

func BuiltinHandlers() map[string]ToolHandler {
	return map[string]ToolHandler{
		"harnas.builtin.read_file":  BuiltinReadFile,
		"harnas.builtin.write_file": BuiltinWriteFile,
		"harnas.builtin.edit_file":  BuiltinEditFile,
		"harnas.builtin.list_dir":   BuiltinListDir,
		"harnas.builtin.glob":       BuiltinGlob,
		"harnas.builtin.grep":       BuiltinGrep,
		"harnas.builtin.run_shell":  BuiltinRunShell,
		"harnas.builtin.fetch_url":  BuiltinFetchURL,
		"harnas.builtin.spawn_agent": func(map[string]any) (string, error) {
			return "", fmt.Errorf("spawn_agent is handled by Runner")
		},
		"harnas.builtin.load_skill": func(args map[string]any) (string, error) {
			return BuiltinLoadSkill(args, nil)
		},
		"harnas.builtin.bash_session": func(args map[string]any) (string, error) {
			return BuiltinBashSession(args, nil)
		},
	}
}

func BuiltinConfiguredHandlers() map[string]ConfiguredToolHandler {
	bashSessions := NewBashSessionRegistry()
	return map[string]ConfiguredToolHandler{
		"harnas.builtin.load_skill":   BuiltinLoadSkill,
		"harnas.builtin.bash_session": bashSessions.Handle,
	}
}

func BuiltinDescriptors() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "read_file",
			Handler:     "harnas.builtin.read_file",
			Description: "Read a text file with cat -n style line numbers. Supports optional offset and limit.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":   map[string]any{"type": "string"},
					"offset": map[string]any{"type": "integer", "description": "Start at line N (0-indexed). Default 0."},
					"limit":  map[string]any{"type": "integer", "description": "Read at most N lines. Default 2000."},
				},
				"required": []any{"path"},
			},
		},
		{
			Name:        "write_file",
			Handler:     "harnas.builtin.write_file",
			Description: "Write text content to a file at the given path, overwriting any existing content.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []any{"path", "content"},
			},
		},
		{
			Name:        "edit_file",
			Handler:     "harnas.builtin.edit_file",
			Description: "Replace one occurrence of `old_string` with `new_string` in the file at the given path. Pass replace_all: true to replace every occurrence. Fails if old_string is not found or appears more than once when replace_all is false.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":        map[string]any{"type": "string"},
					"old_string":  map[string]any{"type": "string"},
					"new_string":  map[string]any{"type": "string"},
					"replace_all": map[string]any{"type": "boolean"},
				},
				"required": []any{"path", "old_string", "new_string"},
			},
		},
		{
			Name:        "list_dir",
			Handler:     "harnas.builtin.list_dir",
			Description: "List the entries (files and directories) of the directory at the given path.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"path": map[string]any{"type": "string"}},
				"required":   []any{"path"},
			},
		},
		{
			Name:        "glob",
			Handler:     "harnas.builtin.glob",
			Description: "Find files matching a glob pattern (e.g. \"**/*.rb\") under the optional `path` root. Returns a newline-separated list of paths, sorted.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern": map[string]any{"type": "string"},
					"path":    map[string]any{"type": "string"},
				},
				"required": []any{"pattern"},
			},
		},
		{
			Name:        "grep",
			Handler:     "harnas.builtin.grep",
			Description: "Search for a regular expression in file contents under the given path (file or directory). Optional `glob` filters files; optional `case_insensitive` toggles the /i flag. Returns path:lineno:content matches, capped at 200.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":          map[string]any{"type": "string"},
					"path":             map[string]any{"type": "string"},
					"glob":             map[string]any{"type": "string"},
					"case_insensitive": map[string]any{"type": "boolean"},
				},
				"required": []any{"pattern", "path"},
			},
		},
		{
			Name:        "run_shell",
			Handler:     "harnas.builtin.run_shell",
			Description: "Run a shell command and return its stdout, stderr, and exit status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":         map[string]any{"type": "string"},
					"timeout_seconds": map[string]any{"type": "integer", "minimum": float64(1)},
				},
				"required": []any{"command"},
			},
		},
		{
			Name:        "fetch_url",
			Handler:     "harnas.builtin.fetch_url",
			Description: "Fetch a URL via HTTP GET and return the response body as text.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":     map[string]any{"type": "string"},
					"headers": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				},
				"required": []any{"url"},
			},
		},
		{
			Name:        "load_skill",
			Handler:     "harnas.builtin.load_skill",
			Description: "Load the body of a named skill from the configured skills directory.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
		},
		{
			Name:        "bash_session",
			Handler:     "harnas.builtin.bash_session",
			Description: "Run a command in a persistent bash session. Sessions preserve working directory and environment variables across calls. stdin is /dev/null; interactive programs cannot receive input.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"session_id": map[string]any{"type": "string"},
					"command":    map[string]any{"type": "string"},
					"action":     map[string]any{"type": "string", "enum": []any{"run", "status", "kill"}},
					"timeout_ms": map[string]any{"type": "integer", "minimum": float64(1)},
					"env":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				},
			},
			Config: map[string]any{
				"shell":            "auto",
				"shell_type":       defaultBashSessionShellType(),
				"max_output_bytes": DefaultBashSessionMaxOutputBytes,
			},
		},
		{
			Name:        "spawn_agent",
			Handler:     "harnas.builtin.spawn_agent",
			Description: "Create a child agent Session receipt for a delegated task. Products run and join the child according to their supervisor policy.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task":       map[string]any{"type": "string"},
					"label":      map[string]any{"type": "string"},
					"role":       map[string]any{"type": "string"},
					"tools_deny": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []any{"task"},
			},
		},
	}
}

func BuiltinReadFile(args map[string]any) (string, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	checkLen := len(data)
	if checkLen > 8*1024 {
		checkLen = 8 * 1024
	}
	if bytes.Contains(data[:checkLen], []byte{0}) {
		return "", fmt.Errorf("Cannot read binary file %q. Use bash_session to inspect binary files.", path)
	}
	offset := intValue(args["offset"])
	if offset < 0 {
		offset = 0
	}
	limit := intValue(args["limit"])
	if limit <= 0 {
		limit = 2000
	}
	if limit > 10000 {
		limit = 10000
	}
	return formatNumberedFile(path, data, offset, limit), nil
}

func formatNumberedFile(_ string, data []byte, offset, limit int) string {
	if len(data) == 0 {
		return "... [file has 0 total lines; showing 0–0]\n"
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if offset >= total {
		return fmt.Sprintf("... [file has %d total lines; offset %d is past EOF]\n", total, offset)
	}
	end := offset + limit
	if end > total {
		end = total
	}
	var out strings.Builder
	for index := offset; index < end; index++ {
		fmt.Fprintf(&out, "%6d\t%s\n", index+1, lines[index])
	}
	if total > offset+limit {
		fmt.Fprintf(&out, "... [file has %d total lines; showing %d–%d]\n", total, offset, offset+limit)
	}
	return out.String()
}

func BuiltinWriteFile(args map[string]any) (string, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	content, err := requiredString(args, "content")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len([]byte(content)), path), nil
}

func BuiltinEditFile(args map[string]any) (string, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	oldString, ok := args["old_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing required argument: old_string")
	}
	newString, ok := args["new_string"].(string)
	if !ok {
		return "", fmt.Errorf("missing required argument: new_string")
	}
	if oldString == newString {
		return "", fmt.Errorf("old_string and new_string must differ")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	count := strings.Count(content, oldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", path)
	}
	replaceAll, _ := args["replace_all"].(bool)
	if count > 1 && !replaceAll {
		return "", fmt.Errorf("old_string appears %d times in %s; pass replace_all: true or add surrounding context", count, path)
	}
	updated := strings.Replace(content, oldString, newString, 1)
	if replaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", err
	}
	suffix := ""
	if count != 1 {
		suffix = "s"
	}
	return fmt.Sprintf("edited %s (%d replacement%s)", path, count, suffix), nil
}

func BuiltinListDir(args map[string]any) (string, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

func BuiltinGlob(args map[string]any) (string, error) {
	pattern, err := requiredString(args, "pattern")
	if err != nil {
		return "", err
	}
	root := stringValue(args["path"])
	if root == "" {
		root = "."
	}
	full := pattern
	if !filepath.IsAbs(pattern) {
		full = filepath.Join(root, pattern)
	}
	matches, err := glob(full)
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}

func BuiltinGrep(args map[string]any) (string, error) {
	pattern, err := requiredString(args, "pattern")
	if err != nil {
		return "", err
	}
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if ci, _ := args["case_insensitive"].(bool); ci {
		pattern = "(?i)" + pattern
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}
	files, err := grepFiles(path, stringValue(args["glob"]))
	if err != nil {
		return "", err
	}
	matches := []string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for lineno, line := range strings.Split(string(data), "\n") {
			if regex.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", file, lineno+1, line))
				if len(matches) >= GrepMaxMatches {
					return strings.Join(matches, "\n") + fmt.Sprintf("\n... (truncated at %d matches)", GrepMaxMatches), nil
				}
			}
		}
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	return strings.Join(matches, "\n"), nil
}

func BuiltinRunShell(args map[string]any) (string, error) {
	command, err := requiredString(args, "command")
	if err != nil {
		return "", err
	}
	timeout := DefaultShellTimeoutSeconds
	if raw, ok := args["timeout_seconds"]; ok {
		timeout = int(asFloat(raw))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("command timed out after %ds", timeout)
	}
	exitCode := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			exitCode = exit.ExitCode()
		} else {
			return "", err
		}
	}
	return formatShellResult(stdout.String(), stderr.String(), exitCode), nil
}

func BuiltinFetchURL(args map[string]any) (string, error) {
	url, err := requiredString(args, "url")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("only http(s) is supported")
	}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	for key, value := range asMap(args["headers"]) {
		request.Header.Set(key, stringValue(value))
	}
	client := &http.Client{Timeout: DefaultFetchURLTimeout}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxFetchBytes))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("HTTP %d\n%s", response.StatusCode, string(body)), nil
}

func BuiltinLoadSkill(args map[string]any, config map[string]any) (string, error) {
	name, err := requiredString(args, "name")
	if err != nil {
		return "", err
	}
	if !ValidSkillName(name) {
		return "", fmt.Errorf("RuntimeError: invalid skill name: %s", name)
	}
	skillsDir := stringValue(config["skills_dir"])
	if skillsDir == "" {
		return "", fmt.Errorf("RuntimeError: missing skills_dir config")
	}
	matches, err := filepath.Glob(filepath.Join(skillsDir, "*.md"))
	if err != nil {
		return "", err
	}
	allowed := false
	for _, path := range matches {
		if strings.TrimSuffix(filepath.Base(path), ".md") == name {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("RuntimeError: unknown skill: %s", name)
	}
	path := filepath.Join(skillsDir, name+".md")
	strip := true
	if raw, ok := config["strip_frontmatter"].(bool); ok {
		strip = raw
	}
	if !strip {
		data, err := os.ReadFile(path)
		return string(data), err
	}
	_, body, err := ParseSkillFile(path)
	return body, err
}

func requiredString(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	return value, nil
}

func glob(pattern string) ([]string, error) {
	if !strings.Contains(pattern, "**") {
		return filepath.Glob(pattern)
	}
	root, rest, _ := strings.Cut(pattern, "**")
	root = strings.TrimSuffix(root, string(os.PathSeparator))
	if root == "" {
		root = "."
	}
	suffix := strings.TrimPrefix(rest, string(os.PathSeparator))
	suffix = filepath.ToSlash(suffix)
	matches := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ok, err := filepath.Match(suffix, rel)
		if err == nil && !ok && !strings.Contains(suffix, "/") {
			ok, err = filepath.Match(suffix, filepath.Base(rel))
		}
		if err == nil && ok {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

func grepFiles(path, globPattern string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("path does not exist: %s", path)
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	if globPattern == "" {
		globPattern = "**/*"
	}
	result, err := glob(filepath.Join(path, globPattern))
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, candidate := range result {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			files = append(files, candidate)
		}
	}
	sort.Strings(files)
	return files, nil
}

func formatShellResult(stdout, stderr string, exitCode int) string {
	parts := []string{fmt.Sprintf("[exit %d]", exitCode)}
	if stdout != "" {
		parts = append(parts, "--- stdout ---\n"+stdout)
	}
	if stderr != "" {
		parts = append(parts, "--- stderr ---\n"+stderr)
	}
	return strings.Join(parts, "\n")
}
