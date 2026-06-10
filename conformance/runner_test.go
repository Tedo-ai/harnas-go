package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirstDiffRejectsExtraActualPayloadFields(t *testing.T) {
	dir := filepath.Join(specRoot(t), "conformance", "oracle-corpus", "strict-diff-extra-payload-field")
	actual, err := ReadExpected(filepath.Join(dir, "actual-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ReadExpected(filepath.Join(dir, "expected-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := FirstDiff(actual, expected); diff == "" {
		t.Fatal("expected strict diff to reject extra actual payload field")
	}
}

func specRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("HARNAS_SPEC"); root != "" {
		return root
	}
	root := filepath.Clean(filepath.Join("..", "harnas"))
	if _, err := os.Stat(filepath.Join(root, "conformance", "oracle-corpus")); err == nil {
		return root
	}
	t.Fatal("HARNAS_SPEC is required to locate conformance oracle corpus")
	return ""
}
