package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderStreamFixtures(t *testing.T) {
	root := os.Getenv("HARNAS_SPEC")
	if root == "" {
		sibling := filepath.Clean(filepath.Join("..", "harnas"))
		if _, err := os.Stat(filepath.Join(sibling, "conformance", "provider-streams")); err == nil {
			root = sibling
		}
	}
	if root == "" {
		t.Fatal("HARNAS_SPEC is required to locate provider-stream conformance corpus")
	}
	if _, err := os.Stat(filepath.Join(root, "conformance", "provider-streams", "corpus.json")); err != nil {
		t.Skip("provider-stream corpus is not present in this spec version")
	}
	report, err := RunProviderStreamCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Cases == 0 || report.Profiles < report.Cases {
		t.Fatalf("invalid provider-stream report: %#v", report)
	}
	t.Logf("%d/%d provider-wire cases; %d chunked executions passed", report.Cases, report.Cases, report.Profiles)
}
