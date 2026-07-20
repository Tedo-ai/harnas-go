package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tedo-ai/harnas-go/conformance"
)

func main() {
	spec := flag.String("spec", os.Getenv("HARNAS_SPEC"), "Harnas spec checkout")
	flag.Parse()
	if *spec == "" {
		*spec = filepath.Clean(filepath.Join("..", "harnas"))
	}
	report, err := conformance.RunProviderStreamCorpus(*spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "provider-wire conformance failed:", err)
		os.Exit(1)
	}
	fmt.Printf("%d/%d provider-wire cases; %d chunked executions passed\n",
		report.Cases, report.Cases, report.Profiles)
}
