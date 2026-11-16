package podman

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"paracetamol/internal/process"
)

func TestInspectImageIDCanonicalizesOnlyFullSHA256(t *testing.T) {
	digest := strings.Repeat("0123456789abcdef", 4)
	for _, test := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"bare", digest, true},
		{"prefixed", "sha256:" + digest, true},
		{"podman newline", digest + "\n", true},
		{"empty", "", false},
		{"short", digest[:12], false},
		{"long", digest + "00", false},
		{"nonhex", strings.Repeat("g", 64), false},
		{"uppercase", strings.ToUpper(digest), false},
		{"wrong algorithm", "sha512:" + digest, false},
		{"duplicate prefix", "sha256:sha256:" + digest, false},
		{"uppercase algorithm", "SHA256:" + digest, false},
		{"multiple IDs", digest + "\n" + digest, false},
		{"embedded space", digest[:32] + " " + digest[32:], false},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{result: process.Result{Stdout: []byte(test.raw)}}
			got, err := (Client{Runner: runner}).InspectImageID(context.Background(), "localhost/example:fixture")
			if test.valid {
				if err != nil || got != "sha256:"+digest {
					t.Fatalf("ID=%q err=%v", got, err)
				}
			} else if err == nil || got != "" {
				t.Fatalf("accepted invalid ID: ID=%q err=%v", got, err)
			}
			want := []string{"image", "inspect", "--format", "{{.Id}}", "localhost/example:fixture"}
			if !reflect.DeepEqual(runner.commands[0].Args, want) {
				t.Fatalf("command=%v want=%v", runner.commands[0].Args, want)
			}
		})
	}
}

func TestInspectImageIDFailsOnInspectionError(t *testing.T) {
	runner := &fakeRunner{result: process.Result{Status: 125, Stderr: []byte("inspection failed")}}
	if _, err := (Client{Runner: runner}).InspectImageID(context.Background(), "fixture"); err == nil || !strings.Contains(err.Error(), "inspection failed") {
		t.Fatalf("error=%v", err)
	}
}
