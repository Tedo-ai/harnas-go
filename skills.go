package harnas

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var skillNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

const skillsIndexHeader = "## Skills"
const skillsIndexGuard = "You have access to local skills. The skill index below is enough to answer what skills are available. Do not call `load_skill` just to list skills. Call `load_skill` only when a user request matches a skill and you need its full instructions."

type SkillEntry struct {
	Name        string
	Description string
	Category    string
	Triggers    []string
}

func ValidSkillName(name string) bool {
	return skillNamePattern.MatchString(name)
}

func BuildSkillsIndex(skillsDir string) (string, error) {
	entries, err := SkillEntries(skillsDir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	lines := []string{skillsIndexHeader, "", skillsIndexGuard, ""}
	for _, entry := range entries {
		lines = append(lines, formatSkillEntry(entry))
	}
	return strings.Join(lines, "\n"), nil
}

func SkillEntries(skillsDir string) ([]SkillEntry, error) {
	matches, err := filepath.Glob(filepath.Join(skillsDir, "*.md"))
	if err != nil {
		return nil, err
	}
	entries := []SkillEntry{}
	for _, path := range matches {
		frontmatter, _, err := ParseSkillFile(path)
		if err != nil {
			return nil, err
		}
		name := stringValue(frontmatter["name"])
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), ".md")
		}
		if !ValidSkillName(name) || name != strings.TrimSuffix(filepath.Base(path), ".md") {
			continue
		}
		description := stringValue(frontmatter["description"])
		if description == "" {
			continue
		}
		entries = append(entries, SkillEntry{
			Name:        name,
			Description: description,
			Category:    stringValue(frontmatter["category"]),
			Triggers:    skillStringSlice(frontmatter["triggers"]),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

func ParseSkillFile(path string) (map[string]any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return map[string]any{}, content, nil
	}
	lines := strings.SplitAfter(content, "\n")
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return map[string]any{}, content, nil
	}
	frontmatter := strings.Join(lines[1:closing], "")
	body := strings.Join(lines[closing+1:], "")
	return ParseSkillFrontmatter(frontmatter), body, nil
}

func ParseSkillFrontmatter(raw string) map[string]any {
	fields := map[string]any{}
	currentListKey := ""
	for _, line := range strings.Split(raw, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if currentListKey != "" && strings.HasPrefix(stripped, "- ") {
			fields[currentListKey] = append(skillStringSlice(fields[currentListKey]), strings.TrimSpace(strings.TrimPrefix(stripped, "- ")))
			continue
		}
		key, value, ok := strings.Cut(stripped, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			fields[key] = []string{}
			currentListKey = key
		} else if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			fields[key] = splitInlineList(value[1 : len(value)-1])
			currentListKey = ""
		} else {
			fields[key] = value
			currentListKey = ""
		}
	}
	return fields
}

func formatSkillEntry(entry SkillEntry) string {
	line := "- `" + entry.Name + "`: " + entry.Description
	if entry.Category != "" {
		line += " Category: " + entry.Category + "."
	}
	triggers := []string{}
	for _, trigger := range entry.Triggers {
		if trigger != "" {
			triggers = append(triggers, trigger)
		}
	}
	if len(triggers) > 0 {
		line += " Triggers: " + strings.Join(triggers, ", ") + "."
	}
	return line
}

func splitInlineList(value string) []string {
	parts := strings.Split(value, ",")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func skillStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, stringValue(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}
