package config

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The host configuration deliberately supports only the TOML forms emitted
// by DefaultContents. Keeping the parser coupled to the closed schema makes a
// new setting an explicit code and test change, and avoids granting a general
// configuration decoder a larger input surface than Paracetamol needs.
func parseConfiguration(contents []byte) (Configuration, error) {
	if !utf8.Valid(contents) {
		return Configuration{}, fmt.Errorf("configuration is not valid UTF-8")
	}

	var configuration Configuration
	section := ""
	seenSections := make(map[string]bool)
	seenSettings := make(map[string]bool)
	for index, rawLine := range strings.Split(string(contents), "\n") {
		lineNumber := index + 1
		line, err := stripConfigurationComment(rawLine)
		if err != nil {
			return Configuration{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			section, err = parseConfigurationSection(line)
			if err != nil {
				return Configuration{}, fmt.Errorf("line %d: %w", lineNumber, err)
			}
			if seenSections[section] {
				return Configuration{}, fmt.Errorf("line %d: duplicate configuration section [%s]", lineNumber, section)
			}
			seenSections[section] = true
			continue
		}

		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		rawValue = strings.TrimSpace(rawValue)
		if !found || key == "" || rawValue == "" {
			return Configuration{}, fmt.Errorf("line %d: expected key = value", lineNumber)
		}
		if section == "" {
			return Configuration{}, fmt.Errorf("line %d: setting %q is outside a configuration section", lineNumber, key)
		}
		if strings.ContainsRune(key, '.') {
			return Configuration{}, fmt.Errorf("line %d: unknown configuration setting %s.%s", lineNumber, section, key)
		}
		setting := section + "." + key
		if seenSettings[setting] {
			return Configuration{}, fmt.Errorf("line %d: duplicate configuration setting %s", lineNumber, setting)
		}

		if err := applyConfigurationSetting(&configuration, section, key, rawValue); err != nil {
			return Configuration{}, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		seenSettings[setting] = true
	}
	return configuration, nil
}

func parseConfigurationSection(line string) (string, error) {
	if !strings.HasSuffix(line, "]") || strings.HasPrefix(line, "[[") || strings.HasSuffix(line, "]]") {
		return "", fmt.Errorf("malformed configuration section %q", line)
	}
	section := strings.TrimSpace(line[1 : len(line)-1])
	switch section {
	case "storage", "gateway", "gateway.llama-cpp":
		return section, nil
	default:
		return "", fmt.Errorf("unknown configuration section [%s]", section)
	}
}

func applyConfigurationSetting(configuration *Configuration, section, key, rawValue string) error {
	setting := section + "." + key
	switch setting {
	case "storage.data_dir":
		value, err := parseConfigurationString(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Storage.DataDir = &value
	case "gateway.applications":
		value, err := parseConfigurationStringArray(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.Applications = value
	case "gateway.profile":
		value, err := parseConfigurationString(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.Profile = &value
	case "gateway.render_nodes":
		value, err := parseConfigurationStringArray(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.RenderNodes = value
	case "gateway.listen":
		value, err := parseConfigurationString(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.Listen = &value
	case "gateway.port":
		value, err := parseConfigurationInteger(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.Port = &value
	case "gateway.startup_timeout":
		value, err := parseConfigurationString(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.StartupTimeout = &value
	case "gateway.llama-cpp.backend":
		value, err := parseConfigurationString(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.LlamaCPP.Backend = &value
	case "gateway.llama-cpp.models_max":
		value, err := parseConfigurationInteger(rawValue)
		if err != nil {
			return invalidConfigurationValue(setting, err)
		}
		configuration.Gateway.LlamaCPP.ModelsMax = &value
	default:
		return fmt.Errorf("unknown configuration setting %s", setting)
	}
	return nil
}

func invalidConfigurationValue(setting string, err error) error {
	return fmt.Errorf("invalid value for %s: %w", setting, err)
}

func parseConfigurationString(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return "", fmt.Errorf("expected a quoted string")
	}
	switch raw[0] {
	case '\'':
		if raw[len(raw)-1] != '\'' || strings.ContainsRune(raw[1:len(raw)-1], '\'') {
			return "", fmt.Errorf("malformed literal string")
		}
		value := raw[1 : len(raw)-1]
		if hasDisallowedStringControl(value) {
			return "", fmt.Errorf("string contains a disallowed control character")
		}
		return value, nil
	case '"':
		if raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated basic string")
		}
		if err := validateConfigurationBasicString(raw); err != nil {
			return "", err
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("malformed basic string: %w", err)
		}
		return value, nil
	default:
		return "", fmt.Errorf("expected a quoted string")
	}
}

func validateConfigurationBasicString(raw string) error {
	closingQuote := len(raw) - 1
	for index := 1; index < closingQuote; {
		character := raw[index]
		if character == '"' {
			return fmt.Errorf("basic string contains an unescaped quote")
		}
		if character != '\\' {
			if character < 0x20 && character != '\t' || character == 0x7f {
				return fmt.Errorf("string contains a disallowed control character")
			}
			index++
			continue
		}
		if index+1 >= closingQuote {
			return fmt.Errorf("unterminated escape sequence")
		}
		escape := raw[index+1]
		switch escape {
		case 'b', 't', 'n', 'f', 'r', '"', '\\':
			index += 2
		case 'u', 'U':
			digits := 4
			if escape == 'U' {
				digits = 8
			}
			end := index + 2 + digits
			if end > closingQuote || !isHexadecimal(raw[index+2:end]) {
				return fmt.Errorf("invalid Unicode escape sequence")
			}
			index = end
		default:
			return fmt.Errorf("unsupported escape sequence \\%c", escape)
		}
	}
	return nil
}

func hasDisallowedStringControl(value string) bool {
	return strings.IndexFunc(value, func(character rune) bool {
		return character < 0x20 && character != '\t' || character == 0x7f
	}) >= 0
}

func isHexadecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			lower := character | 0x20
			if lower < 'a' || lower > 'f' {
				return false
			}
		}
	}
	return true
}

func parseConfigurationStringArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil, fmt.Errorf("expected a one-line string array")
	}
	body := raw[1 : len(raw)-1]
	values := make([]string, 0)
	for offset := 0; ; {
		for offset < len(body) && (body[offset] == ' ' || body[offset] == '\t' || body[offset] == '\r') {
			offset++
		}
		if offset == len(body) {
			return values, nil
		}
		if body[offset] != '"' && body[offset] != '\'' {
			return nil, fmt.Errorf("array element must be a quoted string")
		}

		start := offset
		quote := body[offset]
		offset++
		for offset < len(body) {
			if quote == '"' && body[offset] == '\\' {
				offset += 2
				continue
			}
			if body[offset] == quote {
				offset++
				break
			}
			offset++
		}
		if offset > len(body) || body[offset-1] != quote {
			return nil, fmt.Errorf("unterminated string array element")
		}
		value, err := parseConfigurationString(body[start:offset])
		if err != nil {
			return nil, err
		}
		values = append(values, value)

		for offset < len(body) && (body[offset] == ' ' || body[offset] == '\t' || body[offset] == '\r') {
			offset++
		}
		if offset == len(body) {
			return values, nil
		}
		if body[offset] != ',' {
			return nil, fmt.Errorf("expected a comma between array elements")
		}
		offset++
	}
}

func parseConfigurationInteger(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("expected a decimal integer")
	}
	digits := raw
	if digits[0] == '+' || digits[0] == '-' {
		digits = digits[1:]
	}
	if digits == "" {
		return 0, fmt.Errorf("expected a decimal integer")
	}
	for index, character := range digits {
		if character >= '0' && character <= '9' {
			continue
		}
		if character != '_' || index == 0 || index == len(digits)-1 || digits[index-1] == '_' || digits[index+1] == '_' {
			return 0, fmt.Errorf("expected a decimal integer")
		}
	}
	normalized := strings.ReplaceAll(raw, "_", "")
	unsigned := strings.TrimPrefix(strings.TrimPrefix(normalized, "+"), "-")
	if len(unsigned) > 1 && unsigned[0] == '0' {
		return 0, fmt.Errorf("decimal integer has a leading zero")
	}
	parsed, err := strconv.ParseInt(normalized, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("expected a decimal integer")
	}
	value := int(parsed)
	if int64(value) != parsed {
		return 0, fmt.Errorf("decimal integer is outside the supported range")
	}
	return value, nil
}

func stripConfigurationComment(line string) (string, error) {
	var quote byte
	escaped := false
	for index := 0; index < len(line); index++ {
		character := line[index]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '"' || character == '\'' {
			quote = character
			continue
		}
		if character == '#' {
			return line[:index], nil
		}
	}
	if quote != 0 || escaped {
		return "", fmt.Errorf("unterminated quoted value")
	}
	return line, nil
}
