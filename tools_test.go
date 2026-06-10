package harnas

import (
	"fmt"
	"strings"
	"testing"
)

func TestRunnerErrorsOnUnresolvedHandler(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Tool{Name: "clock", Handler: "conformance.get_current_time"}); err != nil {
		t.Fatal(err)
	}
	log := NewLog()
	toolUse := log.Append(EventToolUse, map[string]any{
		"id":        "toolu_1",
		"name":      "clock",
		"arguments": map[string]any{},
	})

	(&Runner{Registry: registry}).Run(toolUse, log)

	result := log.Events()[1]
	if result.Payload["error"] == nil || result.Payload["output"] != nil {
		t.Fatalf("expected unresolved handler error, got %#v", result.Payload)
	}
	if strings.Contains(fmt.Sprint(result.Payload["error"]), "conformance stub") {
		t.Fatalf("unresolved handler leaked conformance stub: %#v", result.Payload)
	}
}
