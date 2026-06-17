package harnas

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestJCSV1OracleVectors(t *testing.T) {
	root := harnasSpecRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "conformance", "oracle-corpus", "event-content-hash", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Valid []struct {
			Name                string   `json:"name"`
			InputJSON           string   `json:"input_json"`
			ExpectedCanonical   string   `json:"expected_canonical"`
			ExpectedContentHash string   `json:"expected_content_hash"`
			ExcludeKeys         []string `json:"exclude_keys"`
		} `json:"valid"`
		Invalid []struct {
			Name          string `json:"name"`
			InputJSON     string `json:"input_json"`
			ExpectedError string `json:"expected_error"`
		} `json:"invalid"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, vector := range corpus.Valid {
		t.Run(vector.Name, func(t *testing.T) {
			canonical, err := CanonicalizeJCSV1JSON([]byte(vector.InputJSON), vector.ExcludeKeys...)
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != vector.ExpectedCanonical {
				t.Fatalf("canonical mismatch\n got: %s\nwant: %s", canonical, vector.ExpectedCanonical)
			}
			digest := sha256.Sum256(canonical)
			actualHash := hex.EncodeToString(digest[:])
			if actualHash != vector.ExpectedContentHash {
				t.Fatalf("hash mismatch: got %s want %s", actualHash, vector.ExpectedContentHash)
			}
		})
	}
	for _, vector := range corpus.Invalid {
		t.Run(vector.Name, func(t *testing.T) {
			_, err := CanonicalizeJCSV1JSON([]byte(vector.InputJSON))
			if err == nil {
				t.Fatalf("expected %s", vector.ExpectedError)
			}
			if err.Error() != vector.ExpectedError {
				t.Fatalf("got %q, want %q", err.Error(), vector.ExpectedError)
			}
		})
	}
}

func TestEventRowContentHashOracle(t *testing.T) {
	root := harnasSpecRoot(t)
	rowBytes, err := os.ReadFile(filepath.Join(root, "conformance", "oracle-corpus", "event-content-hash", "event-row-with-content-hash.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(root, "conformance", "oracle-corpus", "event-content-hash", "expected-content-hash.txt"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := ContentHashForEventRowJSON(rowBytes)
	if err != nil {
		t.Fatal(err)
	}
	if hash+"\n" != string(expected) {
		t.Fatalf("content hash mismatch: got %s want %s", hash, string(expected))
	}
}

func harnasSpecRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("HARNAS_SPEC"); root != "" {
		return root
	}
	return filepath.Join("..", "harnas")
}
