// Package identity owns product names and persistent namespace identities.
package identity

import (
	"strings"
	"unicode"
)

// Product is the one deliberate rename boundary. Build-time selection of a
// persistent namespace intentionally selects separate state and ownership;
// it does not silently migrate an earlier namespace.
type Product struct {
	CommandName    string
	DisplayName    string
	StateNamespace string
	EnvPrefix      string
	ImageNamespace string
	LabelNamespace string
	Version        string
}

var (
	CommandName    = "paracetamol"
	DisplayName    = "Paracetamol"
	StateNamespace = "paracetamol"
	// An empty EnvPrefix derives it from CommandName. Builds can still set an
	// explicit prefix when a rename needs a spelling other than the normalized
	// command name.
	EnvPrefix      = ""
	ImageNamespace = "localhost/paracetamol"
	LabelNamespace = "io.github.fff7d1bc.paracetamol"
)

const Version = "0.1.0-dev"

func Current() Product {
	return Product{CommandName: CommandName, DisplayName: DisplayName, StateNamespace: StateNamespace, EnvPrefix: EnvironmentPrefix(), ImageNamespace: ImageNamespace, LabelNamespace: LabelNamespace, Version: Version}
}

func EnvironmentPrefix() string {
	if EnvPrefix != "" {
		return EnvPrefix
	}
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToUpper(character)
		}
		return '_'
	}, CommandName)
}

func Command(arguments ...string) string {
	parts := append([]string{"./" + CommandName}, arguments...)
	return strings.Join(parts, " ")
}

func Image(tag string) string { return ImageNamespace + ":" + tag }

func Container(suffix string) string { return StateNamespace + "-" + suffix }
