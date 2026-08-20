// Package config owns host defaults, applications, and configuration precedence.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"rocmplete/internal/application"
	"rocmplete/internal/controlerr"
	"rocmplete/internal/identity"
)

type Application = application.Spec

var (
	ROCmRuntimeImage  = mustBuildUnit(application.BuildRuntime).Image
	ROCmBaseImage     = mustBuildUnit(application.BuildPyTorchBase).Image
	ContentToolsImage = mustBuildUnit(application.BuildContentTools).Image
)

const DefaultListen = "127.0.0.1"

func ApplicationByID(identifier string) (Application, bool) { return application.ByID(identifier) }
func Applications() []Application                           { return application.All() }

func mustBuildUnit(identifier application.BuildID) application.BuildUnit {
	unit, ok := application.BuildUnitByID(string(identifier))
	if !ok {
		panic("application registry lacks build unit " + string(identifier))
	}
	return unit
}

func EnvironmentValue(environment map[string]string, name, fallback string) string {
	if value, ok := environment[identity.EnvironmentPrefix()+"_"+name]; ok {
		return value
	}
	return fallback
}

func ConfigFile(environment map[string]string) (string, bool, error) {
	root := environment["XDG_CONFIG_HOME"]
	if root == "" {
		home := environment["HOME"]
		if home == "" {
			return "", false, nil
		}
		root = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(root) {
		return "", false, controlerr.New("XDG_CONFIG_HOME must be an absolute path")
	}
	return filepath.Join(root, identity.StateNamespace, "config.toml"), true, nil
}

func DefaultDataDir(environment map[string]string) (string, error) {
	if configured, ok, err := configuredDataDir(environment); err != nil {
		return "", err
	} else if ok {
		return configured, nil
	}
	root := environment["XDG_DATA_HOME"]
	if root == "" {
		home := environment["HOME"]
		if home == "" {
			return "", controlerr.New("HOME is not set")
		}
		root = filepath.Join(home, ".local", "share")
	}
	if !filepath.IsAbs(root) {
		return "", controlerr.New("XDG_DATA_HOME must be an absolute path")
	}
	return filepath.Join(root, identity.StateNamespace), nil
}

func SelectDataDir(flagValue string, environment map[string]string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if value := EnvironmentValue(environment, "DATA_DIR", ""); value != "" {
		return value, nil
	}
	return DefaultDataDir(environment)
}

func configuredDataDir(environment map[string]string) (string, bool, error) {
	path, available, err := ConfigFile(environment)
	if err != nil || !available {
		return "", false, err
	}
	status, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, controlerr.New("cannot inspect configuration %s: %v", path, err)
	}
	if !status.Mode().IsRegular() {
		return "", false, controlerr.New("configuration is not a regular file: %s", path)
	}
	if status.Size() > 64*1024 {
		return "", false, controlerr.New("configuration is unexpectedly large: %s", path)
	}
	handle, err := os.Open(path)
	if err != nil {
		return "", false, controlerr.New("cannot read configuration %s: %v", path, err)
	}
	defer handle.Close()
	value, found, err := parseStorageDataDir(handle)
	if err != nil {
		return "", false, controlerr.New("cannot read configuration %s: %v", path, err)
	}
	if !found {
		return "", false, nil
	}
	if !filepath.IsAbs(value) {
		return "", false, controlerr.New("[storage].data_dir must be an absolute path: %s", path)
	}
	return value, true, nil
}

// parseStorageDataDir intentionally implements the project's closed TOML
// schema, not a partial general-purpose TOML parser.
func parseStorageDataDir(input io.Reader) (string, bool, error) {
	scanner := bufio.NewScanner(input)
	section := ""
	seenSection := false
	seenValue := false
	value := ""
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := stripTOMLComment(strings.TrimSpace(scanner.Text()))
		if err != nil {
			return "", false, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") || strings.Count(line, "[") != 1 || strings.Count(line, "]") != 1 {
				return "", false, fmt.Errorf("line %d: invalid table", lineNumber)
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section != "storage" {
				return "", false, fmt.Errorf("unknown configuration section %q", section)
			}
			if seenSection {
				return "", false, fmt.Errorf("line %d: duplicate [storage] table", lineNumber)
			}
			seenSection = true
			continue
		}
		key, raw, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) == "" {
			return "", false, fmt.Errorf("line %d: expected key = value", lineNumber)
		}
		if section != "storage" {
			return "", false, fmt.Errorf("line %d: settings must be below [storage]", lineNumber)
		}
		key = strings.TrimSpace(key)
		if key != "data_dir" {
			return "", false, fmt.Errorf("unknown [storage] setting %q", key)
		}
		if seenValue {
			return "", false, fmt.Errorf("line %d: duplicate data_dir", lineNumber)
		}
		parsed, err := parseTOMLString(strings.TrimSpace(raw))
		if err != nil {
			return "", false, fmt.Errorf("line %d: data_dir %w", lineNumber, err)
		}
		if parsed == "" {
			return "", false, errors.New("must be a non-empty string")
		}
		value = parsed
		seenValue = true
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	return value, seenValue, nil
}

func stripTOMLComment(line string) (string, error) {
	quote := rune(0)
	escaped := false
	for index, character := range line {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if quote == 0 && (character == '\'' || character == '"') {
			quote = character
			continue
		}
		if quote != 0 && character == quote {
			quote = 0
			continue
		}
		if quote == 0 && character == '#' {
			return line[:index], nil
		}
	}
	if quote != 0 || escaped {
		return "", errors.New("unterminated string")
	}
	return line, nil
}

func parseTOMLString(raw string) (string, error) {
	if len(raw) < 2 {
		return "", errors.New("must be a string")
	}
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		return raw[1 : len(raw)-1], nil
	}
	if raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", errors.New("must be a string")
	}
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", err
	}
	return value, nil
}

func ValidatePort(value string) (int, error) {
	if value == "" || strings.IndexFunc(value, func(character rune) bool {
		return character < '0' || character > '9'
	}) >= 0 {
		return 0, controlerr.Usage("port must be an integer")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, controlerr.Usage("port must be an integer")
	}
	if port < 1 || port > 65535 {
		return 0, controlerr.Usage("port must be between 1 and 65535")
	}
	return port, nil
}

func ValidateListenAddress(value string) error {
	if net.ParseIP(value) == nil {
		return controlerr.Usage("listen address must be an IPv4 or IPv6 address")
	}
	return nil
}
