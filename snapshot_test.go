package harnas

import "testing"

func TestToolDescriptorsExportRegistryToolsWithConfig(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Tool{
		Name:        "load_skill",
		Handler:     "harnas.builtin.load_skill",
		Description: "Load a skill",
		InputSchema: map[string]any{"type": "object"},
		Config:      map[string]any{"skills_dir": "/tmp/skills"},
		Call:        func(map[string]any) (string, error) { return "ok", nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	descriptors := ToolDescriptors(registry)

	if len(descriptors) != 1 {
		t.Fatalf("expected one descriptor, got %#v", descriptors)
	}
	if descriptors[0].Handler != "harnas.builtin.load_skill" {
		t.Fatalf("unexpected handler: %#v", descriptors[0])
	}
	if descriptors[0].Config["skills_dir"] != "/tmp/skills" {
		t.Fatalf("unexpected config: %#v", descriptors[0])
	}
}
