# Operations

[Documentation index](../README.md)

This is the guide for everything around a normal run: checking a new machine,
moving built images, finding persistent data, stopping services, and cleaning
up. Paracetamol only touches resources it owns and never performs a general
Podman or system prune.

## Target-hardware smoke acceptance

After onboarding a GPU host or updating software, dry-run the acceptance suite
and then let it work:

```bash
./paracetamol acceptance --dry-run
./paracetamol acceptance
```

The default suite first checks a real PyTorch GPU operation, exact render-node
exposure, and CPU-mode device absence. It then runs deliberately small
workloads across the maintained application paths:

- ComfyUI Qwen Image FP8 Lightning at 768×768;
- ComfyUI Wan FP8 Lightning T2V at 832×480 and five frames; and
- llama.cpp Qwen3 0.6B with `pp32`, `tg16`, and one repetition.

All automated workloads finish before the visual review pass begins, so you
can start the suite and walk away. When it is done, each generated image or
video comes with a small functional checklist. This is a smoke test, not an
art contest. Reviews can be passed, failed, or deferred independently.

Limit a diagnostic run with a repeatable `--application`:

```bash
./paracetamol acceptance --application llama-cpp
```

If images or exact content bundles are missing, inspect the dry-run plan first.
`--prepare` is the explicit authorization to build and install those exact
prerequisites; unattended use also disables visual prompts:

```bash
./paracetamol acceptance --prepare --non-interactive \
  --accept-license
```

Noninteractive visual cases remain `BLOCKED`, not falsely `PASS`. Inspect the
generated paths and criteria in the Markdown report, then resume in a terminal
to record the visual decision:

```bash
./paracetamol acceptance --resume RESULT.json
```

Each run is checkpointed below
`apps/acceptance/results/` in the configured data directory and receives a
neighboring Markdown summary. Resume retries failed or interrupted cases,
skips passed cases, and rejects changes to the source identity, image IDs,
catalog pins, hardware, selection, or runtime policies. Completed case
evidence is recorded with its size and SHA-256; resume also rejects evidence
that disappeared or changed. Exit status is 0 for `PASS`, 1 for `FAIL`, and 2
for `BLOCKED`.

Acceptance result schemas are deliberately not migrated in place. A result
from an older schema remains available as a historical JSON/Markdown record,
but a new Paracetamol acceptance policy requires a new run instead of resuming
that file.

An explicit `--output` must be a new `.json` path whose neighboring `.md`
path is also unused. Paracetamol validates output and resume files before GPU
probing, building, or downloading, then creates a new checkpoint without
replacing a file that appeared in the meantime.

The source identity is the Git commit for a clean checkout. In a dirty
checkout it also hashes staged and unstaged tracked changes plus every
nonignored untracked file. A resume therefore cannot
silently mix smoke cases run from two different local code states.

In practical terms, `PASS` means the automated work and visual review
completed. `FAIL` means something actually failed or the output was broken.
`BLOCKED` usually means the generated media still needs your review.

This smoke suite answers “does each principal application path work on this
machine now?” It does not cover every model family, precision, edit/I2V path,
or performance comparison in the maintainer acceptance matrix.

## Coding-agent evaluation

The Pi-based `benchmark agent` command is retired. Frozen task definitions and
hidden fixtures remain under `evaluations/coding/`. Existing raw results under
`apps/agent-evaluation/results/` and `apps/agent-evaluation/runs/` are untouched.
See the [archived protocol](../research/coding-agent-evaluation.md) when interpreting or
reproducing an older result.

The former Pi and Maki runtime and sandbox data under `apps/pi/` and
`apps/maki/` is also left in place. Native Maki uses its ordinary user
configuration and sessions, not these private trees. Back them up before any
manual cleanup. There is no automatic migration or deletion.

## Builds and local caches

Build one application during ordinary work, or all managed applications after
an update:

```bash
./paracetamol build comfyui
./paracetamol build all
```

Normal builds use Podman's image-layer cache and retain downloaded Python
packages below `${XDG_CACHE_HOME:-$HOME/.cache}/paracetamol/build/pip`.
Paracetamol still passes prerequisite targets through the build so changed
source inputs are noticed after `git pull`. Current explicit image tags remain
the boundary for reusing an unchanged local prerequisite.

Build the shared prerequisites directly when you need to inspect or refresh
one without building an application:

```bash
./paracetamol build pytorch-base
./paracetamol build content-tools
```

`pytorch-base` builds the minimal ROCm runtime and the ROCm/PyTorch diagnostic
base. `content-tools` builds the verified downloader image. Native applications
share the lower ROCm runtime without inheriting PyTorch.

Use `--no-layer-cache` to rerun the selected application image while retaining
downloaded packages and allowing prerequisite layers to use their normal
cache:

```bash
./paracetamol build comfyui --no-layer-cache
```

Use `--no-cache` for a genuinely cold build. It bypasses Podman's layers and
Paracetamol's package-download cache for the selected build closure:

```bash
./paracetamol build all --no-cache
```

Remove retained package downloads separately. The command prints the exact
path and size and asks for confirmation:

```bash
./paracetamol cleanup build-cache
```

## Source and image identity

A full Git commit selects the checkout's controller source, software pins and
catalog. Use the [upgrade procedure](upgrade-notes.md#update-an-existing-checkout)
to follow `master` or choose a particular commit. Before changing a working
setup or reporting a problem, record:

```bash
git rev-parse HEAD
git status --short
./paracetamol --version
./paracetamol status
```

Keep the commit together with the dirty/clean state. Do not publish a local
diff or untracked files without checking for secrets and personal paths.
For a running text model, also capture
`./paracetamol status llama-cpp --model MODEL_ID` or
`./paracetamol status gateway`, as appropriate. The direct llama.cpp report
includes the running image ID. General `status` lists the checkout's expected
image tags, while gateway status describes its inventory and allocation, not
an immutable image ID. For another known running container, record its image ID with
`podman inspect --format '{{.Image}}' CONTAINER_NAME` using the exact container
being investigated, not a guessed gateway backend name.

Source revision, built image identity and the live service are different facts.
A changed checkout does not replace a running container, and a rebuilt tag
need not identify the same image bytes as an earlier local build.

Immutable pins make the intended inputs reviewable. They do not promise
byte-identical rebuilds, permanent upstream availability, or GPU inference on
an untested host. Retain an image archive when you need the exact built
output. Back up persistent data separately.

## Image backups and transfer

Built the images on one machine and do not want to rebuild them on another?
Export the complete managed set to one archive:

```bash
./paracetamol images export all \
  --output /path/to/backup/paracetamol-images.tar

./paracetamol images import \
  /path/to/backup/paracetamol-images.tar --dry-run
./paracetamol images import \
  /path/to/backup/paracetamol-images.tar
```

`export all` writes content tools, the minimal ROCm runtime, the managed
ROCm/PyTorch base, and application images to one Docker-format archive while
retaining shared layers once. A selected PyTorch application includes both
base layers. A selected llama.cpp export includes only the lower ROCm runtime
and application image, not content tools or the unrelated PyTorch base.
Export `content-tools` separately if the destination also needs the downloader.

```bash
./paracetamol images export comfyui \
  --output /path/to/backup/paracetamol-comfyui.tar
./paracetamol images export content-tools \
  --output /path/to/backup/paracetamol-content-tools.tar
```

The output must be new. Export writes a same-directory partial file and
exposes the final name only after Podman succeeds and archive tags, platform,
and image IDs verify.

Import accepts only current managed tags for the host architecture and
validates the complete plan before calling Podman. Identical images make
import idempotent. A current tag pointing to different bytes is refused;
remove that exact tag deliberately before retrying:

```bash
./paracetamol cleanup images --image-tag IMAGE_TAG
```

Image archives contain build outputs only. Models, workflows, inputs, outputs,
and application state need a separate backup. The archive is an uncompressed
portable tar, so allow substantial space and copy time.

## Persistent data

Images are disposable. This directory is not. It contains the models,
workflows, inputs, outputs, and application state you probably care about.

Without configuration, the default host data directory is:

```text
${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol
```

For a durable location on another filesystem, create
`${XDG_CONFIG_HOME:-$HOME/.config}/paracetamol/config.toml`:

```toml
[storage]
data_dir = "/mnt/ai/paracetamol"
```

The path must be absolute. Generate a complete runnable configuration using
the current XDG data default with:

```bash
./paracetamol config init
```

Initialization publishes the file with mode `0600` and refuses to replace an
existing path. Every setting is optional; see the [gateway
configuration](../gateway.md#configuration) for the additional typed sections.
Paracetamol never moves existing data automatically. A malformed file,
misspelled section, or unknown setting is rejected instead of silently falling
back somewhere else. Select a different file with `-c PATH` or `--config PATH`,
or ignore the XDG file with `--no-config`.
Resolution order is:

1. `--data-dir`
2. `PARACETAMOL_DATA_DIR`
3. `[storage].data_dir` in the selected `config.toml`
4. the XDG data default above

Major partitions are:

```text
apps/       writable application state, input, and output
content/    installed models and their verification receipts
staging/    resumable downloads and reproducible caches
```

Curated and imported workflows are below
`apps/comfyui/user/default/workflows/`, not `content/`. Include the ComfyUI user
tree in a backup even when the installed models can be downloaded again.

Container root filesystems are read-only. Persistent mounts use Podman's
`keep-id` user namespace, so private host files remain accessible without
making them world-readable and container-created files belong to the invoking
host user. Paracetamol passes the launcher's umask into managed containers.

## Status, logs, and stop

`status` is a read-only human dashboard:

```bash
./paracetamol status
```

For the foreground inference gateway, query its versioned HTTP status rather
than looking for a fixed backend container:

```bash
./paracetamol status gateway
./paracetamol status gateway --gateway-url http://gateway.example:7455/v1
./paracetamol status gateway --requests 10
```

This reports the frozen inventory fingerprint, exposed applications and
models, current lazy allocation, lifecycle state, and active/queued request
counts. Model rows use the private backend's exact residency when available;
an unavailable probe is shown as `unknown`, not guessed from allocation state.
Each row includes its configured context, while a loaded llama.cpp row adds
effective context, training context, parameter count, byte size, and
quantization when reported by the private router.
The opt-in `--requests N` view adds up to 64 newest process-local records with
correlation IDs, timing, reported token counts, and explicit client
reasoning/sampler controls. llama.cpp timing metadata also produces weighted
prompt-processing and token-generation rates and speculative-token acceptance.
It never retains prompts, tools, response content, authorization, or complete
request/response bodies. Backend startup detail and
correlated `gateway | request` lines remain on the foreground gateway stderr.

For a running llama.cpp server, select the application scope to print a
shareable runtime report instead of the general dashboard:

```bash
./paracetamol status llama-cpp

# Required for model-specific details when the router serves many presets:
./paracetamol status llama-cpp \
  --model unsloth-qwen3.8-27b-ud-q8-k-xl
```

The report reads the live container's private runtime snapshot, image labels,
exact PID 1 command, and generated router selection. It identifies the
resolved profile and GPU rather than repeating a requested `auto`, then joins
that state to the managed model's source, template, reasoning, sampling,
context, cache, Flash Attention, and speculative-decoding policy. It also
prints a copyable Paracetamol command reproducing the effective launch. API-key
values and host secret paths are redacted. A router report without `--model`
lists its configured preset identifiers and shows how to select one.

Direct ComfyUI and llama.cpp servers can run detached:

```bash
./paracetamol run comfyui --detach
./paracetamol run llama-cpp server \
  --preset unsloth-qwen3.8-27b-ud-q8-k-xl --detach
```

These are separate services, not backends owned by a running gateway. The
gateway itself runs in the foreground and has no `--detach` mode.
Inspect and stop detached applications by their application names:

```bash
./paracetamol logs comfyui --follow
./paracetamol logs llama-cpp --follow

./paracetamol stop comfyui
./paracetamol stop llama-cpp
./paracetamol stop gateway
./paracetamol stop all
```

Logs show the newest 200 lines by default. Use `--tail N`, `--follow`, or
`--all` for another range. `stop gateway` asks the local host process to drain
and exit. `stop all` does that before stopping application containers. `stop`
is idempotent and never touches persistent data. It gives an application
container two seconds for an ordinary shutdown, then Podman force-removes the
exact container instead of leaving it indefinitely in `Stopping`.

An attached application or batch process that exits unsuccessfully is reported
as a Paracetamol error containing the container's exact exit status. The launcher
itself exits with status 1 after printing that diagnostic.

## Troubleshooting a first run

Use these checks for common first-run failures:

| Symptom | Next check |
| --- | --- |
| GPU permission, queue or memory-capacity error | Run `doctor` and follow [host GPU access](tuning.md#host-gpu-access) or [APU memory guidance](tuning.md#rdna-35-apus-gfx1150-and-gfx1151). A successful build is not a GPU check. |
| Required managed image is missing after changing source | Build the selected application from this checkout. Use [source and image identity](#source-and-image-identity) to distinguish its current tag from old local images. |
| Managed model exists on disk but is marked unverified | Inspect `content list models --details`, then rerun its normal installer to refresh missing or stale [verification receipts](content.md#terms-and-verification). Do not delete matching model bytes to force a download. |
| Gateway reports no verified schedulable model | Inspect `content list models --details` and the startup diagnostics. A model needs its required verified companions and a compatible backend/profile. Check [model selection](applications.md#choosing-a-managed-qwen-preset) before changing the runtime. |
| Newly installed model or projector does not appear in Maki | Restart the gateway to refresh its frozen inventory, then [download and merge client configuration again](remote-maki.md#refreshes-and-alternative-addresses). |
| Interrupted download cannot resume because a downloader container remains | Follow the exact stale-container recovery in [download handling](content.md#what-happens-during-a-download). Preserve staging and remove only the confirmed stale named container. |

If the failure remains, collect the [bug-report details](../../CONTRIBUTING.md#bug-reports)
and relevant logs. Review them for private paths and credentials before sharing.

## Scoped cleanup

Choose what you mean to remove. There is no hidden system-wide prune behind
any of these commands:

```bash
./paracetamol cleanup containers
./paracetamol cleanup containers comfyui
./paracetamol cleanup build-cache
./paracetamol cleanup images
./paracetamol cleanup images comfyui
./paracetamol cleanup caches
./paracetamol cleanup downloads
./paracetamol cleanup data
```

`cleanup containers` discovers containers by their reviewed Paracetamol
ownership labels, including application, benchmark, shell, diagnostic and
downloader containers. A familiar application or old benchmark name alone is
not proof of ownership. Unlabelled containers are not automatic cleanup
candidates. Stop commands refuse an application name occupied by a container
whose ownership labels do not match.
This scope uses immediate forced removal because it is explicitly for
abandoned or unwanted containers; persistent application and content data
are not removed.

Container plans retain full immutable IDs and recheck ownership before
removal. If a container disappears and another takes its name while you review
the plan, cleanup does not switch to the replacement. Old unlabelled containers
require manual ownership verification and deliberate removal outside this
automatic cleanup path.

Every cleanup scope resolves and validates its exact non-empty plan, prints
the resources it will remove, and asks once for confirmation on a terminal.
Directory plans keep open handles to the selected directories and their parents.
Cleanup revalidates the complete plan after confirmation and refuses changed
paths before deleting anything. Recursive removal stays inside the captured
directories, even if an ancestor is renamed during removal. If a directory
changes, review and run a new plan instead of retrying the old approval.
An already-empty scope reports the absent resources and exits without a
prompt. Scripts must make both intent and noninteractive execution explicit:

```bash
./paracetamol cleanup downloads --yes --non-interactive
```

The same `--yes` and `--non-interactive` flags apply to `containers`, `images`,
`build-cache`, `caches`, `downloads`, and `data`. Without `--yes`, a non-TTY
invocation fails before mutation. Persistent-data cleanup also refuses to
proceed while any managed container remains. Stop the gateway first, even if
it has no loaded backend. Data, cache and download cleanup, plus container or
image cleanup covering llama.cpp, refuse to run while the gateway owns its
local reservation. They retain that reservation across confirmation and
removal so a gateway cannot start halfway through cleanup. Build-cache and
ComfyUI-only container or image cleanup do not require this reservation.

Data, cache and download cleanup also reserves the selected data directory
against content installation before checks, planning and confirmation. This
protects offline mirror moves and existing-file verification even when no
downloader container exists. If installation or another cleanup is using that
directory, cleanup fails without prompting or deleting anything; retry after
it finishes. Installation cannot start mutating the same directory while you
review a cleanup plan. The directory lock is scoped to one real data root.
The gateway reservation and managed-container checks described above still
apply across data directories.
This reservation creates no lock file and is released on failure, cancellation
or process exit. Cleanup of an absent root creates no state. Build-cache
cleanup remains separate from this persistent-data reservation.

Completed installs remove their owned staging. A complete file that fails
SHA-256 verification is preserved with an `.invalid-*` suffix while the
expected staging path is freed for a verified retry. Reproducible caches and
resumable downloads can be removed independently of installed content.

`cleanup build-cache` removes reusable Python packages downloaded during image
builds from `${XDG_CACHE_HOME:-$HOME/.cache}/paracetamol/build`. This cache is
separate from persistent application/content data and from Podman's image
layers. Cleanup therefore does not require stopping managed containers and
never invokes a general Podman prune. Its plan includes the exact cache path
and current size before confirmation.

`cleanup data` is the broad persistent-data operation. Review its plan and
confirmation carefully: application inputs, outputs, state, custom LoRAs, user
workflows, and benchmark results are not reproducible content.
