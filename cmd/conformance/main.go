package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tedo-ai/harnas-go/conformance"
)

func main() {
	fixture := flag.String("fixture", "", "fixture name to run")
	flag.Parse()

	root := filepath.Join(specRoot(), "conformance", "agents")
	fixtures, err := fixtureDirs(root, *fixture)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	failed := 0
	for _, fixtureDir := range fixtures {
		result, err := conformance.Run(fixtureDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if !result.Passed {
			fmt.Printf("  ✗  %s  FAIL\n", result.Fixture)
			fmt.Printf("     %s\n", result.Diff)
			failed++
			continue
		}
		fmt.Printf("  ✓  %s  ok (%d events)\n", result.Fixture, len(result.Actual))
	}
	fmt.Printf("\n%d fixtures · %d passed · %d failed\n", len(fixtures), len(fixtures)-failed, failed)
	if failed > 0 {
		os.Exit(1)
	}
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
		if stat, err := os.Stat(filepath.Join(candidate, "conformance", "agents")); err == nil && stat.IsDir() {
			return candidate
		}
	}
	return filepath.Join("..", "harnas")
}

func fixtureDirs(root, only string) ([]string, error) {
	if only != "" {
		return []string{filepath.Join(root, only)}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	return dirs, nil
}
