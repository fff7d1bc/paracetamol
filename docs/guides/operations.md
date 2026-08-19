# Operations

[Documentation index](../README.md)

This is the guide for everything around a normal run: checking a new machine,
moving built images, finding persistent data, stopping services, and cleaning
up. ROCmplete only touches resources it owns and never performs a general
Podman or system prune.

## Target-hardware smoke acceptance

After onboarding a GPU host or updating software, dry-run the acceptance suite
and then let it work:

```bash
./rocmplete acceptance run --dry-run
./rocmplete acceptance run
```

The default suite first checks a real PyTorch GPU operation, exact render-node
exposure, and CPU-mode device absence. It then runs deliberately small
workloads across the maintained application paths:

- ComfyUI Qwen Image FP8 Lightning at 768×768;
- ComfyUI Wan FP8 Lightning T2V at 832×480 and five frames; and
- llama.cpp Qwen3 0.6B with `pp32`, `tg16`, and one repetition; and
- DwarfStar DeepSeek V4 Flash with a 4096-token context and a bounded
  no-thinking reply. It runs by default on Strix Halo and is opt-in on other
  profiles because its model alone is about 80.76 GiB.

All automated workloads finish before the visual review pass begins, so you
can start the suite and walk away. When it is done, each generated image or
video comes with a small functional checklist. This is a smoke test, not an
art contest. Reviews can be passed, failed, or deferred independently.

Limit a diagnostic run with a repeatable `--application`:

```bash
./rocmplete acceptance run --application llama-cpp
./rocmplete acceptance run --application dwarfstar
```

If images or exact content bundles are missing, terminal use shows the
preparation plan and asks once before building or downloading. For unattended
preparation, authorize it explicitly:

```bash
./rocmplete acceptance run --prepare --non-interactive \
  --accept-license
```

Noninteractive visual cases remain `BLOCKED`, not falsely `PASS`. Inspect the
generated paths and criteria in the Markdown report, then resume in a terminal
to record the visual decision:

```bash
./rocmplete acceptance run --resume RESULT.json
```

Each run is checkpointed below
`apps/acceptance/results/` in the configured data directory and receives a
neighboring Markdown summary. Resume retries failed or interrupted cases,
skips passed cases, and rejects changes to the source identity, image IDs,
catalog pins, hardware, selection, or runtime policies. Exit status is 0 for
`PASS`, 1 for `FAIL`, and 2 for `BLOCKED`.

Acceptance result schemas are deliberately not migrated in place. A result
from an older schema remains available as a historical JSON/Markdown record,
but a new ROCmplete acceptance policy requires a new run instead of resuming
that file.

An explicit `--output` must be a new `.json` path whose neighboring `.md`
path is also unused. ROCmplete validates output and resume files before GPU
probing, building, or downloading, then creates a new checkpoint without
replacing a file that appeared in the meantime.

The source identity is the Git commit for a clean checkout. In a dirty
checkout it also includes staged and unstaged tracked changes, plus untracked
files below runtime and build-input directories. A resume therefore cannot
silently mix smoke cases run from two different local code states.

In practical terms, `PASS` means the automated work and visual review
completed. `FAIL` means something actually failed or the output was broken.
`BLOCKED` usually means the generated media still needs your review.

This smoke suite answers “does each principal application path work on this
machine now?” It does not cover every model family, precision, edit/I2V path,
or performance comparison in the maintainer acceptance matrix.

## Coding-agent evaluation

Use the frozen coding suite to compare how managed models investigate and
change real Go and Python repositories. It is deliberately separate from
target-hardware smoke acceptance and `llama-bench`: a model can generate
tokens quickly while still making an unsafe or incomplete patch.

Inspect the eleven tasks and preview one model without fetching sources,
creating data, loading a model, or starting Pi:

```bash
./rocmplete benchmark agent --list-tasks
./rocmplete benchmark agent \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium --dry-run
```

The frozen suite has nine implementation tasks and two read-only review tasks
drawn from `reencode`, `fzr`, `ssh-host-proxy`, `rocmplete`, and `nonet`. Run
the complete suite with one exact managed llama.cpp preset:

```bash
./rocmplete benchmark agent \
  --preset qwen3.6-27b-mtp-q8-0 --thinking high
```

Or run selected tasks while calibrating a model:

```bash
./rocmplete benchmark agent \
  --preset qwen3.8-27b-mtp-ud-q8-k-xl \
  --thinking medium \
  --task re-align --task re-cancel
```

`--repetitions 3` creates a fresh checkout and Pi session for every attempt.
`--thinking high` means native thinking on for Qwen3.6; it is not a graduated
high effort. Qwen3.8 accepts off, low, medium, or xhigh, while Muse accepts
low, medium, high, or xhigh and has no off mode. The runner rejects selectors
outside the chosen preset's native contract and records the native value in
the result. It deliberately provides no “normalized” three-model condition:
the same label would not mean the same control. Keep the Pi harness, hardware,
task selection, repetitions, context, and runtime policy fixed, and state
whether a run compares each model's operational default or performs a
within-model reasoning sweep. Do not mix Pi results with Maki or another
harness; harness comparisons are a separate experiment.

DwarfStar can run the same suite through its independently managed endpoint:

```bash
./rocmplete benchmark agent --dwarfstar
```

ROCmplete fetches each pinned public repository into a managed source mirror,
verifies the exact Git tree, and archives the base commit into a new repository
with one synthetic commit and no remote. Pi therefore cannot discover the
later reference fix through local Git history. Each attempt is sandboxed to
its fixture. Go tasks receive an offline module cache, while the Python task
uses its standard-library test environment without prepared dependencies. The
model is instructed not to use the network, and recognized network commands in
Pi's structured transcript invalidate the attempt. The host network remains
available to the Pi process because it must reach the loopback model server,
so this is an audited benchmark policy rather than syscall-level hostile-code
containment.

Implementation grading runs the resulting patch against the repository tests,
then adds a hash-verified hidden test and runs the fixed toolchain's build or
compilation check. Dependency files are restored to their pinned baseline
before grading. A result is `solved` only when Pi exits cleanly, ordinary and
hidden tests pass, the final check passes, and no dependency or audited-network
policy was violated. Review answers are preserved as `review-pending`; they are
intentionally not folded into the implementation solve rate and need factual
human review.

Raw results live below:

```text
apps/agent-evaluation/results/
apps/agent-evaluation/runs/
```

Each result JSON has a neighboring Markdown summary. The run directory keeps
the prompt-visible fixture, agent patch, Pi JSONL transcript, stderr, grading
logs, review answer, and per-attempt server log. An unsolved implementation is
a completed benchmark result, not a launcher failure. Infrastructure failure
or interruption remains a nonzero command result and the checkpoint records
where execution stopped.

Public historical fixes may have appeared in model training data. Recent,
small repositories reduce but do not eliminate that risk. Use the frozen
suite for reproducible calibration, then add an unpublished holdout task when
making a high-stakes final model choice.

## Builds and local caches

Build one application during ordinary work, or all managed applications after
an update:

```bash
./rocmplete build comfyui
./rocmplete build all
```

Normal builds use Podman's image-layer cache and retain downloaded Python
packages below `${XDG_CACHE_HOME:-$HOME/.cache}/rocmplete/build/pip`.
ROCmplete still passes prerequisite targets through the build so changed
source inputs are noticed after `git pull`. Current explicit image tags remain
the boundary for reusing an unchanged local prerequisite.

Build the shared prerequisites directly when you need to inspect or refresh
one without building an application:

```bash
./rocmplete build base
./rocmplete build content-tools
```

`base` builds the minimal ROCm runtime and the ROCm/PyTorch diagnostic base.
`content-tools` builds the verified downloader image. Native applications
share the lower ROCm runtime without inheriting PyTorch.

Use `--no-layer-cache` to rerun the selected application image while retaining
downloaded packages and allowing prerequisite layers to use their normal
cache:

```bash
./rocmplete build comfyui --no-layer-cache
```

Use `--no-cache` for a genuinely cold build. It bypasses Podman's layers and
ROCmplete's package-download cache for the selected build closure:

```bash
./rocmplete build all --no-cache
```

Remove retained package downloads separately. The command prints the exact
path and size and asks for confirmation:

```bash
./rocmplete cleanup build-cache
```

## Image backups and transfer

Built the images on one machine and do not want to rebuild them on another?
Export the complete managed set to one archive:

```bash
./rocmplete images export all \
  --output /path/to/backup/rocmplete-images.tar

./rocmplete images import \
  /path/to/backup/rocmplete-images.tar --dry-run
./rocmplete images import \
  /path/to/backup/rocmplete-images.tar
```

`export all` writes content tools, the minimal ROCm runtime, the managed
ROCm/PyTorch base, and application images to one Docker-format archive while
retaining shared layers once. A selected PyTorch application includes both
base layers. llama.cpp includes content tools and the lower ROCm runtime but
not the unrelated PyTorch base:

```bash
./rocmplete images export comfyui \
  --output /path/to/backup/rocmplete-comfyui.tar
```

The output must be new. Export writes a same-directory partial file and
exposes the final name only after Podman succeeds and archive tags, platform,
and image IDs verify.

Import accepts only current managed tags for the host architecture and
validates the complete plan before calling Podman. Identical images make
import idempotent. A current tag pointing to different bytes is refused;
remove that exact tag deliberately before retrying:

```bash
./rocmplete cleanup images --image-tag IMAGE_TAG
```

Image archives contain build outputs only. Models, workflows, inputs, outputs,
and application state need a separate backup. The archive is an uncompressed
portable tar, so allow substantial space and copy time.

## Persistent data

Images are disposable. This directory is not. It contains the models,
workflows, inputs, outputs, and application state you probably care about.

Without configuration, the default host data directory is:

```text
${XDG_DATA_HOME:-$HOME/.local/share}/rocmplete
```

For a durable location on another filesystem, create
`${XDG_CONFIG_HOME:-$HOME/.config}/rocmplete/config.toml`:

```toml
[storage]
data_dir = "/mnt/ai/rocmplete"
```

The path must be absolute. ROCmplete reads this file but never creates it or
moves existing data automatically. A malformed file, misspelled section, or
unknown setting is rejected instead of silently falling back somewhere else.
Resolution order is:

1. `--data-dir`
2. `ROCMLETE_DATA_DIR`
3. `[storage].data_dir` in `config.toml`
4. the XDG data default above

Major partitions are:

```text
apps/       writable application state, input, and output
content/    verified installed models and managed workflows
staging/    resumable downloads and reproducible caches
```

Container root filesystems are read-only. Persistent mounts use Podman's
`keep-id` user namespace, so private host files remain accessible without
making them world-readable and container-created files belong to the invoking
host user. ROCmplete passes the launcher's umask into managed containers.

## Status, logs, and stop

`status` is a read-only human dashboard:

```bash
./rocmplete status
```

For a running llama.cpp server, select the application scope to print a
shareable runtime report instead of the general dashboard:

```bash
./rocmplete status llama-cpp

# Required for model-specific details when the router serves many presets:
./rocmplete status llama-cpp \
  --model qwen3.8-27b-mtp-ud-q8-k-xl
```

The report reads the live container's private runtime snapshot, image labels,
exact PID 1 command, and generated router selection. It identifies the
resolved profile and GPU rather than repeating a requested `auto`, then joins
that state to the managed model's source, template, reasoning, sampling,
context, cache, Flash Attention, and speculative-decoding policy. It also
prints a copyable ROCmplete command reproducing the effective launch. API-key
values and host secret paths are redacted. A router report without `--model`
lists its configured preset identifiers and shows how to select one.

Detached application containers have independent names:

```bash
./rocmplete logs comfyui --follow
./rocmplete logs llama-cpp --follow

./rocmplete stop comfyui
./rocmplete stop llama-cpp
./rocmplete stop all
```

Logs show the newest 200 lines by default. Use `--tail N`, `--follow`, or
`--all` for another range. `stop` is idempotent and never touches persistent
data. It gives an application two seconds for an ordinary shutdown, then
Podman force-removes the exact container instead of leaving it indefinitely
in `Stopping`.

An attached application or batch process that exits unsuccessfully is reported
as a ROCmplete error containing the container's exact exit status. The launcher
itself exits with status 1 after printing that diagnostic.

## Scoped cleanup

Choose what you mean to remove. There is no hidden system-wide prune behind
any of these commands:

```bash
./rocmplete cleanup containers
./rocmplete cleanup containers comfyui
./rocmplete cleanup build-cache
./rocmplete cleanup images
./rocmplete cleanup images comfyui
./rocmplete cleanup caches
./rocmplete cleanup downloads
./rocmplete cleanup data
```

`cleanup containers` covers more than the application
names. It also discovers ROCmplete-labelled benchmark, acceptance, shell,
diagnostic, and downloader containers. Exact benchmark names and the generated
`rocmplete-download-*` namespace remain recognized so cleanup can recover
owned containers after an interruption.
This scope uses immediate forced removal because it is explicitly for
abandoned or unwanted containers; persistent application and content data
are not removed.

Every cleanup scope resolves and validates its exact non-empty plan, prints
the resources it will remove, and asks once for confirmation on a terminal.
An already-empty scope reports the absent resources and exits without a
prompt. Scripts must make both intent and noninteractive execution explicit:

```bash
./rocmplete cleanup downloads --yes --non-interactive
```

The same `--yes` and `--non-interactive` flags apply to `containers`, `images`,
`build-cache`, `caches`, `downloads`, and `data`. Without `--yes`, a non-TTY
invocation fails before mutation. Persistent-data cleanup also refuses to
proceed while any managed container remains.

Completed installs remove their owned staging. A complete file that fails
SHA-256 verification is preserved with an `.invalid-*` suffix while the
expected staging path is freed for a verified retry. Reproducible caches and
resumable downloads can be removed independently of installed content.

`cleanup build-cache` removes reusable Python packages downloaded during image
builds from `${XDG_CACHE_HOME:-$HOME/.cache}/rocmplete/build`. This cache is
separate from persistent application/content data and from Podman's image
layers. Cleanup therefore does not require stopping managed containers and
never invokes a general Podman prune. Its plan includes the exact cache path
and current size before confirmation.

`cleanup data` is the broad persistent-data operation. Review its plan and
confirmation carefully: application inputs, outputs, state, custom LoRAs, user
workflows, and benchmark results are not reproducible content.
