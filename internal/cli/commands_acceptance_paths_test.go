package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/benchmark"
	"paracetamol/internal/process"
)

func TestAcceptanceRejectsNonJSONPathsBeforeProbesOrMutation(t *testing.T) {
	for _, flag := range []string{"--output", "--resume"} {
		for _, name := range []string{"acceptance.md", "acceptance", "acceptance.txt"} {
			t.Run(flag+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				path, data := filepath.Join(root, name), filepath.Join(root, "absent-data")
				if flag == "--resume" {
					if err := os.WriteFile(path, []byte(`{"preserve":"checkpoint"}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				runner := &commandRunner{run: func(command process.Command) process.Result {
					t.Fatalf("invalid path reached a source/device/runtime probe: %v", command)
					return process.Result{}
				}}
				app, _, _ := testApp(t, runner)
				if err := app.commandAcceptance([]string{flag, path, "--data-dir", data}); err == nil || !strings.Contains(err.Error(), ".json extension") {
					t.Fatalf("invalid checkpoint extension accepted: %v", err)
				}
				if _, err := os.Stat(data); !os.IsNotExist(err) {
					t.Fatalf("invalid path prepared data: %v", err)
				}
				if flag == "--resume" {
					if content, err := os.ReadFile(path); err != nil || string(content) != `{"preserve":"checkpoint"}` {
						t.Fatalf("existing checkpoint changed: %q %v", content, err)
					}
				} else if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalid output was created: %v", err)
				}
			})
		}
	}
}

func TestAcceptanceJSONAndReportRemainDistinctOnResume(t *testing.T) {
	for _, suffix := range []string{".json", ".JSON"} {
		t.Run(suffix, func(t *testing.T) {
			path, report, err := resolveAcceptancePaths(filepath.Join(t.TempDir(), "acceptance"+suffix), atomicfile.Create)
			if err != nil || path == report || filepath.Ext(report) != ".md" {
				t.Fatalf("checkpoint=%q report=%q err=%v", path, report, err)
			}
			if err := benchmark.WriteNewCheckpoint(path, map[string]int{"attempt": 1}); err != nil {
				t.Fatal(err)
			}
			if err := atomicfile.Write(report, []byte("# First report\n"), 0o644, atomicfile.Create); err != nil {
				t.Fatal(err)
			}
			resumePath, resumeReport, err := resolveAcceptancePaths(path, atomicfile.ReplaceRegular)
			if err != nil || resumePath != path || resumeReport != report {
				t.Fatalf("resume destinations changed: %q %q %v", resumePath, resumeReport, err)
			}
			if err := benchmark.WriteCheckpoint(resumePath, map[string]int{"attempt": 2}); err != nil {
				t.Fatal(err)
			}
			if err := atomicfile.Write(resumeReport, []byte("# Updated report\n"), 0o644, atomicfile.ReplaceRegular); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			var checkpoint map[string]int
			if err != nil || json.Unmarshal(content, &checkpoint) != nil || checkpoint["attempt"] != 2 {
				t.Fatalf("report overwrote checkpoint: %q %v", content, err)
			}
		})
	}
}

func TestAcceptanceResumeRejectsCheckpointReportAliases(t *testing.T) {
	for _, kind := range []string{"report-symlink", "checkpoint-symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path, report := filepath.Join(root, "acceptance.json"), filepath.Join(root, "acceptance.md")
			original, link := path, report
			if kind == "checkpoint-symlink" {
				original, link = report, path
			}
			if err := os.WriteFile(original, []byte("preserve evidence"), 0o600); err != nil {
				t.Fatal(err)
			}
			createLink := os.Symlink
			if kind == "hardlink" {
				createLink = os.Link
			}
			if err := createLink(original, link); err != nil {
				t.Fatal(err)
			}
			if _, _, err := resolveAcceptancePaths(path, atomicfile.ReplaceRegular); err == nil {
				t.Fatal("checkpoint/report alias accepted")
			}
			if content, err := os.ReadFile(original); err != nil || string(content) != "preserve evidence" {
				t.Fatalf("alias validation changed evidence: %q %v", content, err)
			}
		})
	}
}
