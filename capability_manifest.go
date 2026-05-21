package harnas

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

func CapabilityManifestRef(manifest any) (string, error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return fmt.Sprintf("cap_sha256_%x", digest[:]), nil
}

type CapabilityManifestStore interface {
	Put(manifest any) (string, error)
	Get(ref string) (any, bool)
}

type MemoryCapabilityManifestStore struct {
	items map[string]any
}

func NewMemoryCapabilityManifestStore() *MemoryCapabilityManifestStore {
	return &MemoryCapabilityManifestStore{items: map[string]any{}}
}

func (s *MemoryCapabilityManifestStore) Put(manifest any) (string, error) {
	ref, err := CapabilityManifestRef(manifest)
	if err != nil {
		return "", err
	}
	s.items[ref] = manifest
	return ref, nil
}

func (s *MemoryCapabilityManifestStore) Get(ref string) (any, bool) {
	value, ok := s.items[ref]
	return value, ok
}
