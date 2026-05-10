package harnas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSkillsIndex(t *testing.T) {
	dir := t.TempDir()
	body := "---\nname: git_workflow\ndescription: Branching, commit, and PR description conventions\ntriggers: [pr, commit]\ncategory: coding\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, "git_workflow.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := BuildSkillsIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	expected := "## Skills\n\n" +
		"You have access to local skills. The skill index below is enough to answer what skills are available. Do not call `load_skill` just to list skills. Call `load_skill` only when a user request matches a skill and you need its full instructions.\n\n" +
		"- `git_workflow`: Branching, commit, and PR description conventions Category: coding. Triggers: pr, commit."
	if index != expected {
		t.Fatalf("unexpected index:\n%s", index)
	}
}

func TestBuildSkillsIndexEmptyDirectory(t *testing.T) {
	index, err := BuildSkillsIndex(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if index != "" {
		t.Fatalf("expected empty index, got %q", index)
	}
}
