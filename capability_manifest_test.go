package harnas

import "testing"

func TestCapabilityManifestRefIsStable(t *testing.T) {
	a := map[string]any{"tools": []any{"read_file"}, "provider": map[string]any{"kind": "mock"}}
	b := map[string]any{"provider": map[string]any{"kind": "mock"}, "tools": []any{"read_file"}}

	refA, err := CapabilityManifestRef(a)
	if err != nil {
		t.Fatal(err)
	}
	refB, err := CapabilityManifestRef(b)
	if err != nil {
		t.Fatal(err)
	}
	if refA != refB {
		t.Fatalf("refs differ: %s != %s", refA, refB)
	}
	if len(refA) != len("cap_sha256_")+64 {
		t.Fatalf("unexpected ref: %s", refA)
	}
}

func TestMemoryCapabilityManifestStore(t *testing.T) {
	store := NewMemoryCapabilityManifestStore()
	ref, err := store.Put(map[string]any{"tools": []any{"read_file"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(ref); !ok {
		t.Fatalf("missing manifest for %s", ref)
	}
}
