package cli

import (
	"flag"
	"strings"
)

// parseFlags keeps the standard-library flag implementation while accepting
// options before or after positional arguments. An explicit -- ends launcher
// option parsing and preserves every following argument verbatim.
func parseFlags(set *flag.FlagSet, arguments []string) error {
	return parseInterspersed(set, arguments, false)
}

// parseFlagsWithPassthrough accepts launcher options around positional
// arguments, but keeps everything following -- out of the launcher FlagSet.
// It is intended for commands that explicitly forward arguments upstream.
func parseFlagsWithPassthrough(set *flag.FlagSet, arguments []string) ([]string, error) {
	separator := len(arguments)
	for index, argument := range arguments {
		if argument == "--" {
			separator = index
			break
		}
	}
	if err := parseInterspersed(set, arguments[:separator], true); err != nil {
		return nil, err
	}
	if separator == len(arguments) {
		return nil, nil
	}
	return append([]string(nil), arguments[separator+1:]...), nil
}

func parseInterspersed(set *flag.FlagSet, arguments []string, rejectSeparator bool) error {
	options := make([]string, 0, len(arguments))
	positionals := make([]string, 0, len(arguments))
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			if rejectSeparator {
				break
			}
			positionals = append(positionals, arguments[index+1:]...)
			break
		}
		if argument == "-" || !strings.HasPrefix(argument, "-") {
			positionals = append(positionals, argument)
			continue
		}
		name := strings.TrimLeft(argument, "-")
		name, _, hasInlineValue := strings.Cut(name, "=")
		option := set.Lookup(name)
		options = append(options, argument)
		if option == nil || hasInlineValue || isBooleanFlag(option.Value) {
			continue
		}
		if index+1 < len(arguments) {
			index++
			options = append(options, arguments[index])
		}
	}
	if len(positionals) > 0 {
		options = append(options, "--")
		options = append(options, positionals...)
	}
	return set.Parse(options)
}

func isBooleanFlag(value flag.Value) bool {
	boolean, ok := value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}
