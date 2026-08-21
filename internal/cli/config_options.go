package cli

import (
	"strings"

	"paracetamol/internal/config"
	"paracetamol/internal/controlerr"
)

var globalConfigurationHelp = [][2]string{
	{"-c, --config PATH", "select this configuration file"},
	{"--no-config", "ignore the optional XDG configuration"},
}

// extractConfigurationSelection reserves the two configuration selectors as
// global options wherever they occur before --. This keeps agent arguments
// after the passthrough boundary untouched.
func extractConfigurationSelection(arguments []string) ([]string, config.Selection, error) {
	remaining := make([]string, 0, len(arguments))
	selection := config.Selection{}
	selected := false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" {
			remaining = append(remaining, arguments[index:]...)
			break
		}
		name, inline, hasInline := strings.Cut(argument, "=")
		switch name {
		case "-c", "--config":
			if selected || selection.Disabled {
				return nil, config.Selection{}, controlerr.Usage("--config and --no-config may be specified only once and are mutually exclusive")
			}
			value := inline
			if !hasInline {
				if index+1 >= len(arguments) {
					return nil, config.Selection{}, controlerr.Usage("%s requires a path", name)
				}
				index++
				value = arguments[index]
			}
			if strings.TrimSpace(value) == "" {
				return nil, config.Selection{}, controlerr.Usage("%s requires a non-empty path", name)
			}
			selection.Path = value
			selected = true
		case "--no-config":
			if hasInline {
				return nil, config.Selection{}, controlerr.Usage("--no-config does not accept a value")
			}
			if selected || selection.Disabled {
				return nil, config.Selection{}, controlerr.Usage("--config and --no-config may be specified only once and are mutually exclusive")
			}
			selection.Disabled = true
		default:
			remaining = append(remaining, argument)
		}
	}
	return remaining, selection, nil
}
