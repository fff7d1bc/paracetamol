// Package evaluation owns the frozen coding-agent suite and disposable fixtures.
package evaluation

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rocmplete/internal/process"
)

const ResultSchema = "rocmplete.coding-agent-evaluation.v3"

type HiddenTest struct {
	Resource    string `json:"resource"`
	SHA256      string `json:"sha256"`
	Destination string `json:"destination"`
}

type Task struct {
	Identifier      string      `json:"identifier"`
	Kind            string      `json:"kind"`
	Toolchain       string      `json:"toolchain"`
	Repository      string      `json:"repository"`
	Remote          string      `json:"remote"`
	BaseCommit      string      `json:"base_commit"`
	BaseTree        string      `json:"base_tree"`
	ReferenceCommit string      `json:"reference_commit"`
	Difficulty      string      `json:"difficulty"`
	SafetyCritical  bool        `json:"safety_critical"`
	Prompt          string      `json:"prompt"`
	Hidden          *HiddenTest `json:"hidden"`
	Answer          string      `json:"answer"`
}

type Suite struct {
	Schema              int    `json:"schema"`
	Identifier          string `json:"identifier"`
	Description         string `json:"description"`
	FixtureInstructions string `json:"fixture_instructions"`
	Tasks               []Task `json:"tasks"`
	Fingerprint         string `json:"-"`
}

type Attempt struct {
	Root    string
	Fixture string
	Task    Task
}

func Load(path string) (Suite, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return Suite{}, fmt.Errorf("decode coding evaluation: %w", err)
	}
	if suite.Schema != 2 || suite.Identifier == "" || suite.Description == "" || suite.FixtureInstructions == "" || len(suite.Tasks) == 0 {
		return Suite{}, fmt.Errorf("coding evaluation definition is incomplete or unsupported")
	}
	seen := map[string]bool{}
	for index := range suite.Tasks {
		task := &suite.Tasks[index]
		if task.Identifier == "" || seen[task.Identifier] || task.Remote == "" || len(task.BaseCommit) != 40 || len(task.BaseTree) != 40 || task.Prompt == "" {
			return Suite{}, fmt.Errorf("coding evaluation task %d is invalid", index)
		}
		if task.Kind != "implementation" && task.Kind != "review" || task.Toolchain != "go" && task.Toolchain != "python-stdlib" {
			return Suite{}, fmt.Errorf("coding evaluation task %s has unsupported kind or toolchain", task.Identifier)
		}
		seen[task.Identifier] = true
	}
	digest := sha256.Sum256(contents)
	suite.Fingerprint = hex.EncodeToString(digest[:])
	return suite, nil
}

func Select(suite Suite, identifiers []string) ([]Task, error) {
	if len(identifiers) == 0 {
		return append([]Task(nil), suite.Tasks...), nil
	}
	wanted := map[string]bool{}
	for _, identifier := range identifiers {
		wanted[identifier] = true
	}
	var result []Task
	for _, task := range suite.Tasks {
		if wanted[task.Identifier] {
			result = append(result, task)
			delete(wanted, task.Identifier)
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for identifier := range wanted {
			unknown = append(unknown, identifier)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown coding evaluation tasks: %s", strings.Join(unknown, ", "))
	}
	return result, nil
}

func Prepare(ctx context.Context, runner process.Runner, suite Suite, task Task, repetition int, evaluationRoot, runRoot string, environment []string) (Attempt, error) {
	attemptRoot := filepath.Join(runRoot, "tasks", task.Identifier, fmt.Sprintf("attempt-%02d", repetition))
	fixture := filepath.Join(attemptRoot, "fixture")
	if err := os.MkdirAll(attemptRoot, 0o755); err != nil {
		return Attempt{}, err
	}
	mirror := filepath.Join(evaluationRoot, "sources", task.Repository+".git")
	if status, err := os.Stat(mirror); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
			return Attempt{}, err
		}
		if _, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"clone", "--mirror", "--", task.Remote, mirror}}, "clone coding evaluation source"); err != nil {
			return Attempt{}, err
		}
	} else if err != nil || !status.IsDir() {
		return Attempt{}, fmt.Errorf("coding evaluation mirror is invalid: %s", mirror)
	}
	tree, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"-C", mirror, "rev-parse", task.BaseCommit + "^{tree}"}}, "verify coding evaluation base")
	if err != nil {
		return Attempt{}, err
	}
	if strings.TrimSpace(string(tree.Stdout)) != task.BaseTree {
		return Attempt{}, fmt.Errorf("coding evaluation base tree changed for %s", task.Identifier)
	}
	archive, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"-C", mirror, "archive", "--format=tar", task.BaseCommit}}, "archive coding evaluation base")
	if err != nil {
		return Attempt{}, err
	}
	if err := extractArchive(archive.Stdout, fixture); err != nil {
		return Attempt{}, err
	}
	if err := os.WriteFile(filepath.Join(fixture, "AGENTS.md"), []byte(suite.FixtureInstructions), 0o644); err != nil {
		return Attempt{}, err
	}
	for _, arguments := range [][]string{{"init", "--quiet"}, {"config", "user.name", "ROCmplete Evaluation"}, {"config", "user.email", "evaluation@invalid.local"}, {"add", "--all"}, {"commit", "--quiet", "-m", "evaluation base"}} {
		if _, err := checked(ctx, runner, process.Command{Name: "git", Args: arguments, Dir: fixture, Env: environment}, "prepare coding evaluation fixture"); err != nil {
			return Attempt{}, err
		}
	}
	if task.Toolchain == "go" {
		if _, err := checked(ctx, runner, process.Command{Name: "go", Args: []string{"mod", "download"}, Dir: fixture, Env: environment}, "prepare coding evaluation modules"); err != nil {
			return Attempt{}, err
		}
	}
	log, err := Test(ctx, runner, task, fixture, environment)
	if writeErr := os.WriteFile(filepath.Join(attemptRoot, "baseline.log"), log, 0o644); writeErr != nil {
		return Attempt{}, writeErr
	}
	if err != nil {
		return Attempt{}, fmt.Errorf("baseline tests fail for %s: %w", task.Identifier, err)
	}
	return Attempt{Root: attemptRoot, Fixture: fixture, Task: task}, nil
}

func Test(ctx context.Context, runner process.Runner, task Task, directory string, environment []string) ([]byte, error) {
	name, args := "go", []string{"test", "-count=1", "./..."}
	if task.Toolchain == "python-stdlib" {
		name, args = "python3", []string{"-m", "unittest", "discover", "-s", "tests"}
	}
	result, err := runner.Run(ctx, process.Command{Name: name, Args: args, Dir: directory, Env: environment})
	log := append(append([]byte("stdout:\n"), result.Stdout...), append([]byte("\nstderr:\n"), result.Stderr...)...)
	if err != nil {
		return log, err
	}
	if result.Status != 0 {
		return log, fmt.Errorf("exit status %d", result.Status)
	}
	return log, nil
}

func Grade(ctx context.Context, runner process.Runner, root string, attempt Attempt, environment []string) (map[string]any, error) {
	status, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"status", "--porcelain=v1", "--untracked-files=all"}, Dir: attempt.Fixture, Env: environment}, "inspect evaluation changes")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(status.Stdout)), "\n") {
		if len(line) >= 4 && strings.HasPrefix(line, "??") {
			if _, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"add", "--intent-to-add", "--", line[3:]}, Dir: attempt.Fixture, Env: environment}, "include untracked evaluation change"); err != nil {
				return nil, err
			}
		}
	}
	patch, err := checked(ctx, runner, process.Command{Name: "git", Args: []string{"diff", "--binary", "--no-ext-diff", "HEAD", "--"}, Dir: attempt.Fixture, Env: environment}, "capture evaluation patch")
	if err != nil {
		return nil, err
	}
	if len(patch.Stdout) > 20*1024*1024 {
		return nil, fmt.Errorf("evaluation patch exceeds 20 MiB")
	}
	if err := os.WriteFile(filepath.Join(attempt.Root, "agent.patch"), patch.Stdout, 0o644); err != nil {
		return nil, err
	}
	if attempt.Task.Kind == "review" {
		contents, err := os.ReadFile(filepath.Join(attempt.Fixture, attempt.Task.Answer))
		words := len(strings.Fields(string(contents)))
		outcome := "failed"
		if err == nil && words >= 200 && words <= 2000 {
			outcome = "answered"
		}
		return map[string]any{"outcome": outcome, "answer_words": words, "patch_sha256": digest(patch.Stdout)}, nil
	}
	if attempt.Task.Hidden == nil {
		return nil, fmt.Errorf("implementation task %s has no hidden test", attempt.Task.Identifier)
	}
	resource := filepath.Join(root, "evaluations", "coding", filepath.FromSlash(attempt.Task.Hidden.Resource))
	contents, err := os.ReadFile(resource)
	if err != nil {
		return nil, fmt.Errorf("read private hidden test for %s: %w", attempt.Task.Identifier, err)
	}
	if digest(contents) != attempt.Task.Hidden.SHA256 {
		return nil, fmt.Errorf("private hidden test does not match frozen suite for %s", attempt.Task.Identifier)
	}
	destination := filepath.Join(attempt.Fixture, filepath.FromSlash(attempt.Task.Hidden.Destination))
	relative, err := filepath.Rel(attempt.Fixture, destination)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("hidden test destination escapes fixture")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return nil, err
	}
	handle, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := handle.Write(contents); err != nil {
		handle.Close()
		return nil, err
	}
	if err := handle.Close(); err != nil {
		return nil, err
	}
	log, testErr := Test(ctx, runner, attempt.Task, attempt.Fixture, environment)
	_ = os.WriteFile(filepath.Join(attempt.Root, "grade.log"), log, 0o644)
	outcome := "solved"
	if testErr != nil {
		outcome = "failed"
	}
	return map[string]any{"outcome": outcome, "hidden_test_passed": testErr == nil, "patch_sha256": digest(patch.Stdout)}, nil
}

func extractArchive(contents []byte, destination string) error {
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	reader := tar.NewReader(bytes.NewReader(contents))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe evaluation archive member %q", header.Name)
		}
		path := filepath.Join(destination, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(header.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > 20*1024*1024 {
				return fmt.Errorf("evaluation archive member has unsafe size: %s", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(header.Mode)&0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(handle, reader, header.Size)
			closeErr := handle.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported evaluation archive member type: %s", header.Name)
		}
	}
}

func checked(ctx context.Context, runner process.Runner, command process.Command, description string) (process.Result, error) {
	result, err := runner.Run(ctx, command)
	if err != nil {
		return result, fmt.Errorf("%s: %w", description, err)
	}
	if result.Status != 0 {
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = fmt.Sprintf("exit status %d", result.Status)
		}
		return result, fmt.Errorf("%s: %s", description, detail)
	}
	return result, nil
}

func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return hex.EncodeToString(value[:])
}

func Identifier() string {
	value := sha256.Sum256([]byte(fmt.Sprint(os.Getpid(), "-", time.Now().UnixNano())))
	return fmt.Sprintf("%x", value[:8])
}
