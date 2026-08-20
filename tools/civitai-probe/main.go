package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"rocmplete/internal/probe"
)

func main() {
	set := flag.NewFlagSet("civitai-probe", flag.ExitOnError)
	raw := set.Bool("raw", false, "print the complete API object")
	set.Parse(os.Args[1:])
	if len(set.Args()) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/civitai-probe [--raw] model|version|search|hash IDENTIFIER")
		os.Exit(2)
	}
	resource, identifier := set.Args()[0], set.Args()[1]
	var result any
	var err error
	if *raw {
		result, err = probe.InspectCivitaiRaw(context.Background(), nil, "", resource, identifier, os.Getenv("CIVITAI_TOKEN"))
	} else {
		result, err = probe.InspectCivitai(context.Background(), nil, "", resource, identifier, os.Getenv("CIVITAI_TOKEN"))
	}
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
