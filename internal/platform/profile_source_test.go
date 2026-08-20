package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"rocmplete/internal/project"
)

func TestContainerProfileIdentitiesMatchHostRegistry(t *testing.T) {
	root, err := project.Root()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "containers", "common", "profile.py"))
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`\("([a-z0-9-]+)", \(([^)]*)\)\)`) // reviewed Python tuple literals
	container := make(map[string][]string)
	for _, match := range pattern.FindAllStringSubmatch(string(contents), -1) {
		for _, quoted := range strings.Split(match[2], ",") {
			architecture := strings.Trim(strings.TrimSpace(quoted), `"'`)
			if architecture != "" {
				container[match[1]] = append(container[match[1]], architecture)
			}
		}
	}
	if len(container) != len(ProfileIDs()) {
		t.Fatalf("container profile count = %d, want %d", len(container), len(ProfileIDs()))
	}
	for _, identifier := range ProfileIDs() {
		host, _ := LookupProfile(identifier)
		got := append([]string(nil), container[identifier]...)
		sort.Strings(got)
		want := append([]string(nil), host.Architectures...)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("container profile %s = %v, want %v", identifier, got, want)
		}
	}
}
