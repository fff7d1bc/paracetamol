package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"paracetamol/internal/probe"
)

func main() {
	args := os.Args[1:]
	if len(args) < 2 || args[0] != "repository" && args[0] != "revision" && args[0] != "file" {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/huggingface-probe repository REPO | revision REPO REV | file REPO REV PATH")
		os.Exit(2)
	}
	repository, revision, file := args[1], "", ""
	if args[0] == "revision" && len(args) == 3 {
		revision = args[2]
	} else if args[0] == "file" && len(args) == 4 {
		revision, file = args[2], args[3]
	} else if args[0] != "repository" || len(args) != 2 {
		fmt.Fprintln(os.Stderr, "error: invalid arguments")
		os.Exit(2)
	}
	result, err := probe.InspectHuggingFace(context.Background(), nil, "", repository, revision, file, os.Getenv("HF_TOKEN"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
