package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tedo-ai/harnas-go/conformance"
)

func main() {
	fixture := flag.String("fixture", "minimal-chat", "fixture name to run")
	flag.Parse()

	fixtureDir := filepath.Join(specRoot(), "conformance", "agents", *fixture)
	result, err := conformance.Run(fixtureDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if !result.Passed {
		fmt.Printf("  ✗  %s  FAIL\n", result.Fixture)
		os.Exit(1)
	}
	fmt.Printf("  ✓  %s  ok (%d events)\n", result.Fixture, len(result.Actual))
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
