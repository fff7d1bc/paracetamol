package catalog

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestArtifactDestinationsRejectAncestorFilesInEitherOrder(t *testing.T) {
	for _, child := range []string{"foo.gguf/bar.gguf", "foo.gguf/nested/bar.gguf"} {
		for _, reverse := range []bool{false, true} {
			for _, target := range []string{"llama-models", "models"} {
				t.Run(fmt.Sprintf("%s/reverse=%t/target=%s", child, reverse, target), func(t *testing.T) {
					artifacts := []Artifact{
						{ID: "parent", Target: "llama-models", Destination: "foo.gguf"},
						{ID: "sibling", Target: "llama-models", Destination: "foo.gguf-other"},
						{ID: "child", Target: target, Destination: child},
					}
					if reverse {
						slices.Reverse(artifacts)
					}
					err := ValidateArtifactDestinations(artifacts)
					if target == "llama-models" {
						if err == nil || !strings.Contains(err.Error(), "overlapping destinations") {
							t.Fatalf("accepted ancestor file collision: %v", err)
						}
					} else if err != nil {
						t.Fatalf("rejected independent target namespace: %v", err)
					}
				})
			}
		}
	}
}

func TestCatalogDestinationOwnership(t *testing.T) {
	for _, target := range []string{"llama-models", "models", "workflows"} {
		t.Run(target, func(t *testing.T) {
			managed := Catalog{Artifacts: map[string]Artifact{
				"first":  {ID: "first", Target: "llama-models", Destination: "fixture.json", SHA256: "same", Size: 1},
				"second": {ID: "second", Target: target, Destination: "fixture.json", SHA256: "same", Size: 1},
			}}
			err := managed.validateRelationships()
			if target == "llama-models" {
				if err == nil || !strings.Contains(err.Error(), "share destination") {
					t.Fatalf("catalog accepted identical-byte destination collision: %v", err)
				}
			} else if err != nil {
				t.Fatalf("catalog rejected independent target namespace: %v", err)
			}
		})
	}
}
