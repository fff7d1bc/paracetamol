package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/atomicfile"
	"paracetamol/internal/project"
)

const gufoCommit = "2c97b72d8671ddd0c74ae7d517b8cef4ebb78871"

//go:embed Containerfile.fragment
var gufoFragment []byte

type prepareManifest struct {
	SchemaVersion                 int                `json:"schema_version"`
	GufoCommit                    string             `json:"gufo_commit"`
	ProductionContainerfileSHA256 string             `json:"production_containerfile_sha256"`
	FragmentSHA256                string             `json:"fragment_sha256"`
	GeneratedContainerfileSHA256  string             `json:"generated_containerfile_sha256"`
	ProductionDefaults            productionDefaults `json:"production_defaults"`
}

// These are observations of the current production input, not another set of
// independently maintained runtime pins or claims about a locally built image.
type productionDefaults struct {
	ROCmVersion      string `json:"rocm_version"`
	ROCmRuntimeImage string `json:"rocm_runtime_image"`
}

func runPrepare(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("gufo-recheck prepare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "required fresh private output directory below the checkout build directory")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: gufo-recheck prepare --out build/gufo-recheck-UNIQUE")
		fmt.Fprintln(stderr, "Generate an isolated recipe and manifest only; no downloads, builds, or workloads.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *out == "" {
		return errors.New("prepare requires --out and accepts no positional arguments")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := project.Root()
	if err != nil {
		return err
	}
	production, err := os.ReadFile(filepath.Join(root, "Containerfile"))
	if err != nil {
		return fmt.Errorf("read current production Containerfile: %w", err)
	}
	generated, manifest, err := composeGufoRecipe(production, gufoFragment)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	output, err := newOutputDir(root, *out)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	recipePath := filepath.Join(output, "Containerfile")
	if err := atomicfile.Write(recipePath, generated, 0o600, atomicfile.Create); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := saveJSON(filepath.Join(output, "prepare.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Generated %s and %s\n", recipePath, filepath.Join(output, "prepare.json"))
	fmt.Fprintln(stdout, "Review both files before building. This is an unmanaged gfx1151 experiment, not GPU acceptance.")
	fmt.Fprintf(stdout, "Build the current production minimal ROCm runtime first with ./paracetamol build runtime.\nThen build manually from the checkout root (no GPU devices):\n  podman build --pull=never --target gufo-recheck -f '%s' -t localhost/paracetamol-experiment:gufo-recheck .\n", strings.ReplaceAll(recipePath, "'", "'\"'\"'"))
	return nil
}

type containerInstruction struct {
	name  string
	value string
}

// Parse logical instructions, not incidental text inside comments or RUNs.
// Unsupported stage syntax fails closed rather than guessing inheritance.
func containerInstructions(contents []byte) ([]containerInstruction, error) {
	var result []containerInstruction
	var pending string
	for _, physical := range strings.Split(string(contents), "\n") {
		line := strings.TrimSpace(physical)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		continued := strings.HasSuffix(line, "\\")
		line = strings.TrimSuffix(line, "\\")
		pending += line + " "
		if continued {
			continue
		}
		fields := strings.Fields(pending)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid Containerfile instruction: %s", pending)
		}
		result = append(result, containerInstruction{strings.ToUpper(fields[0]), strings.TrimSpace(pending[len(fields[0]):])})
		pending = ""
	}
	if pending != "" {
		return nil, errors.New("unterminated Containerfile continuation")
	}
	return result, nil
}

func composeGufoRecipe(production, fragment []byte) ([]byte, prepareManifest, error) {
	var manifest prepareManifest
	productionInstructions, err := containerInstructions(production)
	if err != nil {
		return nil, manifest, err
	}
	fragmentInstructions, err := containerInstructions(fragment)
	if err != nil {
		return nil, manifest, err
	}
	globalDefaults := make(map[string]string)
	stages := make(map[string]string)
	beforeStage := true
	for _, instruction := range productionInstructions {
		switch instruction.name {
		case "ARG":
			name, value, hasDefault := strings.Cut(instruction.value, "=")
			if name == "GUFO_COMMIT" {
				return nil, manifest, errors.New("production Containerfile already declares GUFO_COMMIT")
			}
			if beforeStage && hasDefault {
				if _, exists := globalDefaults[name]; exists {
					return nil, manifest, fmt.Errorf("duplicate production default %s", name)
				}
				globalDefaults[name] = value
			}
		case "FROM":
			beforeStage = false
			base, stage, err := namedStage(instruction.value)
			if err != nil {
				return nil, manifest, err
			}
			if _, exists := stages[stage]; exists {
				return nil, manifest, fmt.Errorf("duplicate production stage %s", stage)
			}
			stages[stage] = base
		}
	}
	if stages["native-rocm-sdk"] != "${ROCM_RUNTIME_IMAGE}" {
		return nil, manifest, errors.New("production must contain exactly one native-rocm-sdk stage derived from ${ROCM_RUNTIME_IMAGE}")
	}
	for _, name := range []string{"ROCM_VERSION", "ROCM_RUNTIME_IMAGE"} {
		if value := globalDefaults[name]; value == "" || strings.ContainsAny(value, " \t$\"'") {
			return nil, manifest, fmt.Errorf("missing or nonliteral production default %s", name)
		}
	}
	expectedStages := map[string]string{
		"gufo-recheck-sdk":     "native-rocm-sdk",
		"gufo-recheck-builder": "gufo-recheck-sdk",
		"gufo-recheck":         "${ROCM_RUNTIME_IMAGE}",
	}
	seenStages := make(map[string]bool)
	pins := 0
	for _, instruction := range fragmentInstructions {
		switch instruction.name {
		case "ARG":
			name, value, hasDefault := strings.Cut(instruction.value, "=")
			if name == "GUFO_COMMIT" {
				if value != gufoCommit {
					return nil, manifest, errors.New("fragment must use the reviewed full GUFO_COMMIT pin consistently")
				}
				pins++
				continue
			}
			if _, isGlobal := globalDefaults[name]; isGlobal && hasDefault {
				return nil, manifest, fmt.Errorf("fragment must not override production default %s", name)
			}
			if name != "DEBIAN_FRONTEND" && name != "GUFO_BUILD_JOBS" && !(name == "ROCM_VERSION" && !hasDefault) {
				return nil, manifest, fmt.Errorf("unexpected fragment ARG %s", name)
			}
		case "ENV":
			for _, assignment := range strings.Fields(instruction.value) {
				name, _, _ := strings.Cut(assignment, "=")
				if _, isGlobal := globalDefaults[name]; isGlobal || strings.HasPrefix(name, "ROCM_") {
					return nil, manifest, fmt.Errorf("fragment must not shadow production default %s with ENV", name)
				}
			}
		case "FROM":
			base, stage, err := namedStage(instruction.value)
			if err != nil {
				return nil, manifest, err
			}
			if _, collision := stages[stage]; collision {
				return nil, manifest, fmt.Errorf("production stage collides with fragment: %s", stage)
			}
			if expectedStages[stage] != base || seenStages[stage] {
				return nil, manifest, fmt.Errorf("unexpected or duplicate fragment stage %s", stage)
			}
			seenStages[stage] = true
		}
	}
	if pins != 2 || len(seenStages) != len(expectedStages) {
		return nil, manifest, errors.New("fragment is missing the reviewed stages or two full GUFO_COMMIT declarations")
	}
	if !strings.Contains(string(fragment), `test "$(git rev-parse HEAD)" = "${GUFO_COMMIT}"`) {
		return nil, manifest, errors.New("fragment is missing exact fetched source verification")
	}
	generated := append(append(append([]byte(nil), production...), '\n'), fragment...)
	manifest = prepareManifest{
		SchemaVersion: 1, GufoCommit: gufoCommit,
		ProductionContainerfileSHA256: hashRecipeBytes(production),
		FragmentSHA256:                hashRecipeBytes(fragment),
		GeneratedContainerfileSHA256:  hashRecipeBytes(generated),
		ProductionDefaults:            productionDefaults{globalDefaults["ROCM_VERSION"], globalDefaults["ROCM_RUNTIME_IMAGE"]},
	}
	return generated, manifest, nil
}

func namedStage(value string) (string, string, error) {
	fields := strings.Fields(value)
	if len(fields) != 3 || !strings.EqualFold(fields[1], "AS") {
		return "", "", fmt.Errorf("expected explicit FROM base AS stage, got %s", value)
	}
	return fields[0], strings.ToLower(fields[2]), nil
}

func hashRecipeBytes(contents []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(contents))
}
