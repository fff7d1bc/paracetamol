package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"paracetamol/internal/identity"
)

func SelectGatewayServerKey(flagPath string, environment map[string]string, configuration Configuration) (string, error) {
	return selectGatewayKey(flagPath, environment, "GATEWAY_API_KEY_FILE", configuration.Gateway.APIKeyFile)
}

func SelectGatewayClientKey(flagPath string, environment map[string]string, configuration Configuration) (string, error) {
	// Never inherit the local server credential. The client URL may address a
	// different host, and publishing a server is not permission to send its key.
	return selectGatewayKey(flagPath, environment, "GATEWAY_CLIENT_API_KEY_FILE", configuration.Gateway.Client.APIKeyFile)
}

func selectGatewayKey(flagPath string, environment map[string]string, name string, configured *string) (string, error) {
	path := flagPath
	if path == "" {
		if value, present := environment[identity.EnvironmentPrefix()+"_"+name]; present {
			if value == "" {
				return "", fmt.Errorf("%s_%s must name a private key file", identity.EnvironmentPrefix(), name)
			}
			path = value
		} else if configured != nil {
			if *configured == "" {
				return "", fmt.Errorf("configured gateway API-key file must not be empty")
			}
			path = *configured
		}
	}
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("gateway API-key file must be an absolute path")
	}
	// O_NONBLOCK avoids waiting on a substituted FIFO. O_NOFOLLOW rejects a
	// final symlink and fstat validates the file actually opened, not a prior
	// pathname check. Parent directories remain within the user's trust boundary.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("cannot open gateway API-key file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("gateway API-key file must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("gateway API-key file must be private (chmod 600)")
	}
	contents, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil || len(contents) > 1024 {
		return "", fmt.Errorf("gateway API-key file is unreadable or too large")
	}
	key := strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r")
	if err := ValidateGatewayKey(key); err != nil {
		return "", err
	}
	return key, nil
}

// ValidateGatewayKey also excludes command/config interpolation characters.
// Generate a random token, for example 32 random bytes encoded as hex.
func ValidateGatewayKey(key string) error {
	if len(key) < 32 || len(key) > 512 {
		return fmt.Errorf("gateway API key must contain 32 to 512 token characters")
	}
	for _, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+/=", c) {
			continue
		}
		return fmt.Errorf("gateway API key must use only letters, digits or -._~+/=")
	}
	return nil
}
