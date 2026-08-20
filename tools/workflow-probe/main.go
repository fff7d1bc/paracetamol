package main

import (
	"encoding/json"
	"fmt"
	"os"

	"paracetamol/internal/probe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/workflow-probe WORKFLOW.json [...]")
		os.Exit(2)
	}
	results := make([]probe.WorkflowSummary, 0, len(os.Args)-1)
	for _, path := range os.Args[1:] {
		result, err := probe.InspectWorkflow(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		results = append(results, result)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(struct {
		Workflows []probe.WorkflowSummary `json:"workflows"`
	}{Workflows: results}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
