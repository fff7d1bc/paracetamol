package project

import (
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func TestDocumentationLocalLinksAndAnchors(t *testing.T) {
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"AGENTS.md", "CONTRIBUTING.md", "README.md", "SECURITY.md",
		"THIRD_PARTY_NOTICES.md", "catalog/README.md", "catalog/benchmarks/README.md",
	}
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".md" {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			paths = append(paths, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, relative := range paths {
		relative := relative
		t.Run(filepath.ToSlash(relative), func(t *testing.T) {
			contents, readErr := os.ReadFile(filepath.Join(root, relative))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, match := range markdownLink.FindAllStringSubmatch(string(contents), -1) {
				destination := strings.TrimSpace(match[1])
				if strings.HasPrefix(destination, "<") && strings.HasSuffix(destination, ">") {
					destination = destination[1 : len(destination)-1]
				}
				parsed, parseErr := url.Parse(destination)
				if parseErr != nil {
					t.Errorf("invalid link %q: %v", destination, parseErr)
					continue
				}
				if parsed.IsAbs() || parsed.Host != "" {
					continue
				}
				decoded, decodeErr := url.PathUnescape(parsed.Path)
				if decodeErr != nil {
					t.Errorf("invalid link path %q: %v", destination, decodeErr)
					continue
				}
				target := filepath.Clean(filepath.Join(root, filepath.Dir(relative), filepath.FromSlash(decoded)))
				if decoded == "" {
					target = filepath.Join(root, relative)
				}
				status, statErr := os.Stat(target)
				if statErr != nil {
					t.Errorf("link %q targets an unavailable path: %v", destination, statErr)
					continue
				}
				if parsed.Fragment != "" && !status.IsDir() && filepath.Ext(target) == ".md" {
					if !markdownAnchors(t, target)[strings.ToLower(parsed.Fragment)] {
						t.Errorf("link %q targets a missing heading", destination)
					}
				}
			}
		})
	}
}

func markdownAnchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchors := make(map[string]bool)
	counts := make(map[string]int)
	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.HasPrefix(line, "#") {
			continue
		}
		heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if heading == "" {
			continue
		}
		var rendered strings.Builder
		for _, character := range strings.ToLower(heading) {
			switch {
			case unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '-':
				rendered.WriteRune(character)
			case unicode.IsSpace(character):
				rendered.WriteByte('-')
			}
		}
		anchor := rendered.String()
		if count := counts[anchor]; count > 0 {
			anchor += "-" + string(rune('0'+count))
		}
		counts[rendered.String()]++
		anchors[anchor] = true
	}
	return anchors
}
