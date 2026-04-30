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
	}
}

func BuiltinReadFile(args map[string]any) (string, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
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
	response, err := http.Get(url)
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
	matches := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		ok, err := filepath.Match(suffix, rel)
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
