package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"paracetamol/internal/probe"
)

type stringsFlag []string

func (values *stringsFlag) String() string { return fmt.Sprint([]string(*values)) }
func (values *stringsFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	set := flag.NewFlagSet("archive-probe", flag.ExitOnError)
	var members stringsFlag
	set.Var(&members, "member", "inspect one exact member; repeatable")
	set.Parse(os.Args[1:])
	if len(set.Args()) != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/archive-probe [--member NAME] ARCHIVE.zip")
		os.Exit(2)
	}
	result, err := probe.InspectZIP(set.Args()[0], members)
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
