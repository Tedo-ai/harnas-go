package mcp

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFlattenContentSamples(t *testing.T) {
	data, err := os.ReadFile("testdata/mcp-content-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var samples []struct {
		Name     string           `json:"name"`
		Content  []map[string]any `json:"content"`
		Expected string           `json:"expected"`
	}
	if err := json.Unmarshal(data, &samples); err != nil {
		t.Fatal(err)
	}
	for _, sample := range samples {
		t.Run(sample.Name, func(t *testing.T) {
			if got := Flatten(sample.Content); got != sample.Expected {
				t.Fatalf("expected %q, got %q", sample.Expected, got)
			}
		})
	}
}
