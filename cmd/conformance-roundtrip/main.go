package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	harnas "github.com/Tedo-ai/harnas-go"
	"github.com/Tedo-ai/harnas-go/conformance"
)

func main() {
	fixture := flag.String("fixture", "", "fixture name")
	phase := flag.Int("phase", 0, "phase number: 1 or 2")
	savePath := flag.String("save", "", "path to save phase 1 session JSONL")
	loadPath := flag.String("load", "", "path to load phase 2 session JSONL")
	checkPath := flag.String("check", "", "expected final JSONL")
	flag.Parse()

	if *fixture == "" || (*phase != 1 && *phase != 2) {
		fmt.Fprintln(os.Stderr, "--fixture and --phase 1|2 are required")
		os.Exit(1)
	}

	fixtureDir := filepath.Join(specRoot(), "conformance", "round-trips", *fixture)
	manifest, err := conformance.LoadManifest(fixtureDir)
	must(err)
	inputs := []any{}
	must(readJSON(filepath.Join(fixtureDir, fmt.Sprintf("phase-%d-inputs.json", *phase)), &inputs))
	scriptPath := filepath.Join(fixtureDir, fmt.Sprintf("phase-%d-provider-script.json", *phase))

	if *phase == 1 {
		if *savePath == "" {
			fmt.Fprintln(os.Stderr, "--save is required for phase 1")
			os.Exit(1)
		}
		session, err := conformance.RunSession(manifest, scriptPath, inputs, nil)
		must(err)
		must(session.Save(*savePath))
		fmt.Printf("saved %s (%d events)\n", *fixture, len(session.Log.Events()))
		return
	}

	if *loadPath == "" || *checkPath == "" {
		fmt.Fprintln(os.Stderr, "--load and --check are required for phase 2")
		os.Exit(1)
	}
	session, err := harnas.LoadSession(*loadPath)
	must(err)
	session, err = conformance.RunSession(manifest, scriptPath, inputs, session)
	must(err)
	expected, err := conformance.ReadExpected(*checkPath)
	must(err)
	diff := conformance.FirstDiff(session.Log.Events(), expected)
	if diff != "" {
		fmt.Fprintf(os.Stderr, "round-trip mismatch: %s\n", diff)
		os.Exit(1)
	}
	fmt.Printf("checked %s (%d events)\n", *fixture, len(session.Log.Events()))
}

func specRoot() string {
	if root := os.Getenv("HARNAS_SPEC"); root != "" {
		return root
	}
	candidates := []string{
		filepath.Join("..", "harnas"),
		filepath.Join("..", "..", "harnas"),
	}
	for _, candidate := range candidates {
		if stat, err := os.Stat(filepath.Join(candidate, "conformance", "round-trips")); err == nil && stat.IsDir() {
			return candidate
		}
	}
	return filepath.Join("..", "harnas")
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
