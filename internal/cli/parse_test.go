package cli

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestParseFlagsAcceptsInterspersedOptions(t *testing.T) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	verbose := set.Bool("verbose", false, "")
	output := set.String("output", "", "")
	if err := parseFlags(set, []string{"first", "--verbose", "second", "--output", "result"}); err != nil {
		t.Fatal(err)
	}
	if !*verbose || *output != "result" || !reflect.DeepEqual(set.Args(), []string{"first", "second"}) {
		t.Fatalf("verbose=%v output=%q args=%q", *verbose, *output, set.Args())
	}
}

func TestParseFlagsPreservesExplicitPassThrough(t *testing.T) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.Bool("verbose", false, "")
	if err := parseFlags(set, []string{"--verbose", "target", "--", "--upstream", "value"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"target", "--upstream", "value"}
	if !reflect.DeepEqual(set.Args(), want) {
		t.Fatalf("args=%q, want %q", set.Args(), want)
	}
}

func TestParseFlagsWithPassthroughSeparatesUpstreamArguments(t *testing.T) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	verbose := set.Bool("verbose", false, "")
	passthrough, err := parseFlagsWithPassthrough(set, []string{"--verbose", "unexpected", "--", "--upstream", "value"})
	if err != nil {
		t.Fatal(err)
	}
	if !*verbose || !reflect.DeepEqual(set.Args(), []string{"unexpected"}) {
		t.Fatalf("verbose=%v args=%q", *verbose, set.Args())
	}
	if !reflect.DeepEqual(passthrough, []string{"--upstream", "value"}) {
		t.Fatalf("passthrough=%q", passthrough)
	}
}
