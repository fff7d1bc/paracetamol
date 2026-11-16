# Testing, source-revision acceptance, and housekeeping

Validation is layered so ordinary development on a host without suitable
target GPU capacity remains useful without pretending it replaces
target-hardware acceptance.

## Development and deployment

The checkout launcher keeps binaries, module and build caches, temporary
files and telemetry state below the ignored `build/` directory. `make build`
is the normal incremental path. Force the ordinary binary through publication
with `make -B build` before finishing Go or Makefile changes. `make clean`
removes only the repository's anchored `build/` tree.
It refuses a symlink or non-directory at that path before changing permissions
or deleting anything. Read-only Go cache files inside a real build directory
are made owner-writable before removal; an absent build directory is a no-op.

Normal builds use CGO, glibc and the host NSS resolver. They require a C
compiler and glibc development headers. `make static` explicitly builds the
optional pure-Go binary. It supports DNS and `/etc/hosts`, but not NSS-only
name sources such as mDNS. The optional native-Linux `make race` also uses CGO.

Normal commands use `GOTOOLCHAIN=local`, so the installed Go toolchain must
satisfy `go.mod`. Use `make GOTOOLCHAIN=auto -B build` only when automatic
toolchain selection is intentional. Its Go state stays below `build/`.

Go commands and `gofmt` use `build/go-config` as their configuration directory,
including the launcher's platform probes. This confines Go telemetry using its
default local-only mode rather than relying on ineffective `GOTELEMETRY`
environment overrides. It does not change the user's global Go configuration.
The application and test binaries retain the caller's original
`XDG_CONFIG_HOME`. Tests run fresh while reusing compiler caches, because the
Go driver's isolated environment is not the test process's config environment.
A clean-only invocation skips Go probes and does not recreate build state.

### Transfer a source checkout

For ordinary image builds and application execution, a Git-free source copy
is supported. To copy one without transferring repository-local binaries or
Go caches, use:

```bash
rsync -a -n -v --progress --delete \
  --exclude=/.git --exclude=/build/ --exclude=/local-content/ \
  --exclude='__pycache__/' --exclude='*.py[cod]' \
  /absolute/source-checkout/ gateway.example:src/paracetamol/
```

`gateway.example` and the paths are examples. This is a dry run for a
dedicated deployment directory, not a contribution or user-update workflow.
Review every deletion and transfer, then remove `-n` only when that directory
contains no unrelated work. Excluded Git metadata, build state and private
local packs remain at the destination; do not add `--delete-excluded`.
Check other untracked/private files before transfer. Record the source commit
and dirty state separately because this copy deliberately excludes Git.

After **every actual transfer**, including the first deployment, repeated
updates and rollback, rebuild on the destination before running the launcher:

```bash
cd /absolute/destination-checkout
make -B build
./paracetamol --help
```

`rsync -a` preserves source timestamps while the excluded destination `build/`
survives. A newer existing binary can therefore look up to date to incremental
Make even when transferred source or resources changed. `make -B build`
forces the controller to match the transferred source and republishes its
normal product identity. Keep the old deployment recoverable until the new
build and relevant checks pass.

Maintainer `acceptance` and `benchmark llama-cpp speculative` runs, including
their dry runs, instead require a real Git checkout rooted at the project
directory. Their checkpoints bind the Git revision and tracked/untracked
source changes; a separately recorded revision is not a substitute. Use a
dedicated Git clone or worktree on the test host at the reviewed revision,
inspect `git status --short` there, and reproduce any intended source changes
explicitly before `make -B build`. Do not run these workflows from the Git-free
copy, place it inside an unrelated repository, or trust Git metadata retained
from an earlier destination. Source-identity validation fails before device
probes, image preparation, downloads or workloads. Ordinary CPU/application
dry runs remain available in Git-free execution copies.

### Build a custom product identity

The pre-release product identity has one definition shared by the Go packages.
For example:

```bash
make -B PRODUCT_ID=new-name DISPLAY_NAME='New Name' build
```

This changes the built command, host-configuration environment prefix
(`NEW_NAME_*`), persistent namespace, local image namespace, container names
and ownership-label namespace together. Set `ENV_PREFIX` explicitly only when
a renamed command needs another spelling. Changing identity selects new state
and image ownership. It does not migrate an existing Paracetamol data
directory. See [identity ownership and protocol boundaries](architecture.md#command-flow)
before making a source-level product rename.

## Validation tiers

### Tier 1: static and unit checks

Run for every change:

```bash
make check
make test
make static
PYTHONPYCACHEPREFIX=/tmp/paracetamol-pycache python3 -m compileall -q applications containers evaluations tests
bash -n applications/comfyui/entrypoint.sh \
  applications/llama-cpp/entrypoint.sh
find catalog -type f -name '*.json' -print0 | \
  xargs -0 -n1 python3 -m json.tool >/dev/null
git diff --check
```

If a shell entrypoint or JSON manifest is added, include it explicitly.

Package-local Go tests cover catalog invariants, download command confinement,
workflow-resource identity, benchmark preparation, CLI resolution, profile detection,
Podman command construction, persistent verification, and client-config exports.
Add tests for behavior and failure paths, not merely new lines or parser
choices.

Content-mutation tests use tiny local fixtures and fake Podman responses to
exercise the complete install/cleanup boundary. Cover artifact transfer,
curated workflow publication, forced replacement and the final readiness
report under one reservation. Test both contention orders, cancellation and
failure release. Standalone workflow installation must remain usable without
Podman. Run the content, CLI and storage race tests after changing this
boundary. No model download or inference is needed for these checks.

Downloader cleanup tests must include a name-conflict loser that created no
container. Preserve that foreign occupant, require the matching launch nonce
and captured full ID before removal, and retain staging and diagnostics after
workload failure, cancellation or failed cleanup.

Mirror tests replace or edit a candidate after preflight, including same-size
edits. Both copy and move must refuse it before transfer. Exercise real
same-filesystem moves plus deterministic cross-device copy, failed publication,
interruption and source replacement during copying or publication. No changed
source may be unlinked, no unverified payload published, and failed-copy
scratch must remain available.

Runtime startup tests must cover both contention orders with destructive data
cleanup, including cleanup paused at confirmation and startup paused during
preparation or container creation. A stopped, ownership-verified container must
be visible before admission is released. Check exact-ID start and failure
cleanup, cancellation identity, attached exit status and shell input/TTY
composition. The data reservation must be released before workload execution.
Managed benchmark and acceptance controllers must retain their separate local
lifecycle reservation through host result/checkpoint writes and inter-trial
gaps. Dry runs remain unreserved and non-mutating. Use fake subprocesses for
contention tests and a local CPU-only container for real create/start,
confinement and terminal lifecycle checks. This does not qualify GPU inference.

Detached benchmark polling tests must distinguish a live, slow-loading backend
from an exited or auto-removed one, including death during ComfyUI history
polling. Cover renamed originals, reused names, changed ownership, bounded
metadata probes and cancellation. Failures must release the maintenance
reservation and isolated cache, retain both workload and cleanup errors, and
never reclaim a replacement container.

Checkpoint recovery tests must interrupt the parent after a child result is
published and retry from the last durable checkpoint. The next attempt must
use distinct result/media/cache paths and leave the first result unchanged.
Also cover a published ComfyUI result followed by failed container cleanup,
failed initial checkpoint publication starting no workload, exhausted or
inconsistent attempt metadata, and intact passed entries remaining skipped.
Use fake Podman and fixture HTTP responses, not GPU workloads. Never make
resume work by replacing append-only evidence or accepting an orphan as a pass.

Acceptance path tests must cover both `--output` and `--resume`. Reject non-JSON
checkpoint names before source/device probes or data preparation. Keep the
neighboring Markdown report distinct during publication and resume, and refuse
symlink or hard-link aliases without changing either file.

For both acceptance and ComfyUI suites, round-trip a failed attempt through
save, read, validation and retry. Include joined workload/cleanup errors,
multiline subprocess stderr, terminal controls and long diagnostics. Preserve
every cause and prior attempt while keeping persisted text printable, and
retain the original returned error. Do not weaken metadata validation to make
a checkpoint resumable.

Source-identity checks use a real temporary Git checkout. Test configured
external diffs and text conversion that return empty output, plus forced diff
color. They must neither hide staged/unstaged changes nor alter the fingerprint
for the same bytes. A clean checkout must still identify its exact revision.

Gateway statistics tests use temporary SQLite files and fixture HTTP responses.
They cover UTC boundaries, retention, restart and shutdown draining, streaming
and JSON usage, missing observations, queue overflow, corrupt stores, file
permissions, and private-data sentinels in the database and WAL. Run the gateway
race tests when changing the writer or shutdown lifecycle. Check both normal
and static builds after updating the SQLite driver. No inference is needed to
validate accounting.

CI explicitly uses umask `0022`. Statistics fixtures must create a dedicated
private database directory rather than assume `t.TempDir()` itself is mode
`0700`. Negative path tests must start with a valid private parent so they
exercise the intended file or schema rejection instead of failing early on
directory permissions. Streaming HTTP tests must synchronize with handler
completion before asserting final request accounting, not just receipt of
the last flushed response bytes.

The small `tests/` Python suite exercises retained container-side policy at
the source boundary: profile validation, resumable downloader confinement,
and ComfyUI Manager patch fail-closed behavior. `make test` runs it after the
Go suite.

Gateway ownership checks need no Podman service or GPU. Temporary abstract
Unix sockets and fake subprocesses exercise duplicate startup, direct router
publication, cleanup during an unloaded gateway, long HOME paths, graceful
stop and crash release. Keep these checks at the shared ownership boundary
when changing startup, cleanup or control transport.

Gateway backend lifecycle fixtures must rename the original and reuse its name
before port lookup, readiness and shutdown. Only the captured full ID may be
used. Cover missing or malformed launch output, cancellation after creation,
renamed failed launches and changed ownership after recovery discovery.
Failed cleanup must retain its exact target or pending launch for retry, and
an unstarted lifecycle must never adopt an incumbent container.

`.github/workflows/checks.yml` runs the Go 1.26 checks, static build, the
container-owned Python and shell checks, catalog validation, and representative
CLI dry-runs without Podman.
Hosted CI does not replace local image builds, CPU startup, provider-network
checks, or target-hardware acceptance.

Image builds also run native source fixtures. Their
[regression gates](#native-build-regression-gates) are separate from the
host-only checks here.

Native Maki compatibility has an optional installed-client check. The normal
suite uses renderer and HTTP fixtures and needs no installed client.

```bash
PARACETAMOL_TEST_MAKI=/absolute/path/to/maki make test
```

It downloads generated TOML and Lua into temporary XDG directories, checks the
model listing, and performs a simulated tool round trip with native Maki.
It checks reasoning, authenticated and keyless access, replay and the absence
of sampler overrides without a GPU or real credentials. It does not touch
normal client state or prove macOS execution.

Embedded gateway page checks cover exact anonymous routes, protected data,
read-only behavior, empty inventory and separate HTTP/client reasoning defaults.
Real-browser acceptance is optional and uses an installed Chromium and
Puppeteer module. Neither is a build or runtime dependency. For example, with
browser tooling already available locally,

```bash
PARACETAMOL_TEST_BROWSER=/absolute/path/to/chrome \
PARACETAMOL_TEST_PUPPETEER=/absolute/path/to/puppeteer-core/lib/puppeteer/puppeteer-core.js \
PARACETAMOL_TEST_SCREENSHOTS="$PWD/build/web-acceptance" \
make test GO_PACKAGES=./internal/gateway
```

This starts fixture HTTP servers, never inference. It checks mobile layout,
configuration previews/downloads, model filtering, HTML-safe rendering,
clipboard fallback, failed refresh and the instructions-only auth state.
The Statistics tab also covers lazy reads, date-range races, historical model
IDs, incomplete counts, missing versus zero observations, and disabled or
unavailable storage.
Screenshots and downloaded fixtures stay under `build/`. Manually inspect
the images after layout changes. Browser automation is not a substitute for
checking native client configuration or target GPU inference.

For a Go or Makefile change, also force the ordinary checkout binary through
its atomic publication path rather than relying on an older incremental
artifact:

```bash
make -B build
```

The project tests exercise both build recipes with a fake compiler and failed
file operations, and apply the same publication checks to both developer
helpers. Source, embedded-asset and helper-fixture deletions, including whole
subtrees, must invalidate the existing executable. A successful rebuild must
return to an unchanged incremental no-op without republishing the binary.
Run these fixtures in checkout paths containing spaces. Make's `launcher-binary`
target must print only the absolute binary path, and clean must retain its anchored-path and
symlink refusal checks there too. Exercise the real Go test wrapper and custom
static output in that path, not only fake compilation.
The tests also retain older transferred source timestamps to
check that a forced deployment build replaces an otherwise up-to-date binary.
A failure or interruption must return a nonzero status,
preserve the previous executable and remove the temporary output. Only a
successful, non-empty build may replace the executable.
The actual clean recipe is also exercised in isolated scratch checkouts,
including read-only cache trees, top-level and nested symlinks, missing build
directories, and attempts to override the anchored cleanup path. Refused paths
must retain their contents and permissions.

CI uses Go 1.26.9. The `go 1.26.0` directive in `go.mod` is the language
compatibility floor, not a recommendation to run the original 1.26.0
toolchain. Keep host toolchains on a supported security patch release through
the host's package or configuration management. Normal Make targets use the
installed toolchain and do not download an update automatically.

The optional `make gufo-recheck` target builds a developer-only experiment
tool, never an application image. Its `prepare` operation generates a recipe
from the current Containerfile without a build or download. Its `proxy` and
`check` operations require `--experimental`; ordinary package tests substitute
HTTP fixtures and never load a model. See the
[Gufo recheck procedure](research/qwen3.8-flash-next-gufo-optimization-screen.md#developer-only-recheck-kit)
for the explicit source build, operator-owned backend, regression gate and
acceptance limits. It is not part of `build all` or production discovery.

The optional native-Linux race pass is appropriate after changes to process,
signal, cleanup, checkpointing, HTTP, or other concurrent boundaries:

```bash
make race
```

### Tier 2: CLI and dry-run checks

Exercise help, catalog planning and CPU command composition without installed
models, managed images, rootless Podman or GPU devices:

```bash
./paracetamol --help
./paracetamol guide comfyui
./paracetamol run --help
./paracetamol doctor --help
./paracetamol content install all --dry-run
./paracetamol content install llama-cpp qwen3.8 --dry-run
./paracetamol run comfyui --profile cpu \
  --listen 127.0.0.1 --dry-run
```

For the external-model path, replace the placeholder with an existing regular
GGUF file. Dry-run still validates the model path:

```bash
./paracetamol run llama-cpp server --model /path/to/model.gguf \
  --profile cpu --listen 127.0.0.1 --dry-run
```

With the tiny `llama-qwen-qwen3-0.6b-q8-0` bundle installed through the normal
installer and carrying current verification receipts, check managed-model CPU
composition. The router requires at least one compatible verified preset:

```bash
./paracetamol run llama-cpp server --router \
  --profile cpu --listen 127.0.0.1 --dry-run
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --profile cpu --dry-run
```

The following benchmark plans also require supported GPU device access and
unambiguous render-node selection. Use an explicit `--render-node` when needed:

```bash
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --compare-backends --dry-run
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --context-depth 32768 \
  --cache-type-k q8_0 --cache-type-v q8_0 --flash-attn on --dry-run
```

Hardware acceptance planning requires exactly one selected GPU render node.
It reports missing images and content as preparation requirements rather than
requiring them to be ready:

```bash
./paracetamol acceptance --dry-run
```

Image export planning requires working rootless Podman and every selected
managed image already built locally. The output must not exist and its parent
must be an existing directory:

```bash
./paracetamol images export all --output /tmp/paracetamol-images.tar --dry-run
```

These plans start no workload. Record unavailable files, receipts, images,
Podman or hardware as check prerequisites, not code regressions. Do not weaken
validation to make a plan pass on an unprepared host.

For remote-import changes, also exercise one exact Hugging Face LFS file and
one exact Civitai version with `content import ... --dry-run`. Confirm that
the plan shows a full Hugging Face commit or exact Civitai version/file,
provider size and SHA-256, the intended destination, `NOASSERTION`, and the
ignored local-pack path. The dry run must not create that pack or the selected
data directory. Treat provider/network/authentication failure as an external
acceptance limitation, not a unit-test replacement.

Inspect resolved commands for:

- correct image and container name;
- a private network namespace and exactly one TCP publication on the selected
  host IP for web applications;
- read-only root;
- dropped capabilities and `no-new-privileges`;
- `--userns keep-id` and an explicit host-derived `--umask` for commands with
  a persistent `/data` bind mount;
- only intended bind mounts;
- no GPU devices in CPU mode;
- exactly `/dev/kfd` plus the complete selected render-node set in GPU mode;
- offline/network-none behavior where promised.

Direct llama.cpp publication must warn for non-loopback and wildcard addresses
with or without an API-key file. Cover IPv4 and IPv6, keep loopback quiet, and
never print key contents. The authenticated warning must distinguish access
control from transport encryption.

For image-archive changes, additionally create a tiny disposable local image,
save it as a Docker archive, inspect it through `images import --dry-run`, remove and
load it, compare the image config ID, and remove it again. Do not use a full
Paracetamol export merely to exercise archive plumbing on every development
pass.

### Tier 3: image builds

Build every affected target. During ordinary iteration, force the affected
image instructions to run while retaining locally downloaded Python packages:

```bash
./paracetamol build comfyui --no-layer-cache
```

Before accepting shared base/dependency changes, perform a genuinely cold
build of all affected targets; for a shared runtime change, build all:

```bash
./paracetamol build all --no-cache
```

The output must show one `content` tools build, one minimal `runtime` build,
one shared PyTorch `base` build, and all application builds. A cache-free
aggregate build must not reinstall a prerequisite once per application.
PyTorch application builds reference the newly tagged PyTorch base with
`--pull=never`; native applications reference the lower runtime with
`--pull=never` and must not reference the managed PyTorch base.

`--no-cache` bypasses both Podman's image-layer cache and Paracetamol's host pip
download cache. `--no-layer-cache` bypasses only the selected application's
image layers; prerequisite targets still pass through their normal layer
cache. It is suitable for repeated local build testing where downloading the
same multi-gigabyte AMD wheels adds no coverage.

Check installed dependency consistency. Read the current image references from
`internal/application/registry.go`; for every Python application image run:

```bash
podman run --rm --entrypoint /opt/venv/bin/python CURRENT_COMFY_IMAGE \
  -m pip check
```

For llama.cpp, inspect the exact labels and native runtime closure:

```bash
podman image inspect CURRENT_LLAMA_IMAGE
podman run --rm --network none --read-only --cap-drop all \
  --security-opt no-new-privileges \
  --entrypoint /usr/local/bin/llama-cli CURRENT_LLAMA_IMAGE --version
```

The image must contain `llama-cli`, `llama-server`, and `llama-bench`, have no
unresolved `ldd` entries, link both `libggml-hip` and `libggml-vulkan`, and
retain rocBLAS/hipBLASLt payloads only for `gfx1150`, `gfx1151`, `gfx1200`,
and `gfx1201`. CPU startup is not GPU inference acceptance.

For the late DFlash multimodal-position correction, retain source and payload
identity evidence separately from inference acceptance. Its patch is pinned by
SHA-256 and applied with `git apply --check` after the existing native build.
Rebuild common, server, CLI and bench through normal CMake/Ninja and installation
rules; do not use header shims, restored timestamps, copied build-control files
or configure bypasses. Every Git subprocess in the late step must inherit the
private index. The installed native-policy and serializer fixtures rerun after
installation. An isolated comparison must keep target-numerical shared
libraries and HIP/Vulkan/libllama objects byte-identical, then exercise actual
images, identical-image history reuse, changed-image rewinds and text after
images. Count MTMD tokens separately from M-RoPE positions when checking native
cache accounting. Missing or failed target evidence is not a successful image
acceptance merely because the build and CPU checks pass.

Inspect image metadata and history when pins or licenses changed:

```bash
podman image inspect CURRENT_IMAGE
podman history CURRENT_IMAGE
```

### Tier 4: CPU-only startup

CPU mode is a startup and HTTP smoke test, not an inference test:

```bash
./paracetamol run comfyui --profile cpu \
  --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8188/ >/dev/null
./paracetamol logs comfyui
./paracetamol stop comfyui

./paracetamol run llama-cpp server --router --models-max 1 \
  --profile cpu --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/v1/models
./paracetamol stop llama-cpp
```

Use different ports if they are occupied. Confirm startup banners show `cpu`
and that containers disappear after stopping. Run this check from a shell with
`umask 0077` as well: immutable profile/downloader helpers must remain readable
to the keep-id process, `/tmp/comfy` must be writable, and persistent files
must retain the private host mask.

The llama.cpp router check requires `content install llama-cpp qwen3.8`,
but does not load a model. Confirm `unsloth-qwen3.8-27b-ud-q8-k-xl` is present
and unloaded. Retired dense Qwen MTP and Muse IDs must not be advertised.
The pinned upstream router may also advertise its reserved empty `default`
preset; that is not managed content and should not be selected during the
smoke test.

For gateway changes, first run the unit and race tests for `internal/gateway`,
then start `run gateway --profile cpu`. Confirm omitted application selection
discovers only built applications with a compatible verified model, startup
loads no backend, `/v1/models` contains only the receipt-verified frozen
snapshot, `status gateway` reports `unloaded`, and Ctrl-C prints a clean stop
and exits zero without creating a backend. With an exact non-loopback `--listen`, confirm both
`127.0.0.1:PORT` and the selected address serve the same frozen inventory and
that shutdown closes both; an occupied required address must close any listener
opened earlier and fail startup. Confirm `-a llama-cpp` and
`--application llama-cpp` produce the same strict inventory and the default
llama.cpp loaded-model limit is one.
Also exercise `stop gateway` and `stop all` through the private local control
socket while unloaded. Both commands must
wait for gateway cleanup, leave no gateway listener or private backend, and
remain idempotent. `stop gateway` must work without invoking Podman; `stop all`
must drain the gateway before removing normal application containers.

### Tier 5: GPU diagnostics

On each target host, use the finite
[target-hardware acceptance matrix](hardware-acceptance.md):

```bash
./paracetamol doctor --render-node /dev/dri/renderD128
./paracetamol run comfyui --profile EXPECTED_PROFILE \
  --render-node /dev/dri/renderD128 --listen 127.0.0.1
```

Verify:

- RX 9060 family cards report `gfx1200` and resolve `rdna4`;
- Radeon AI PRO R9700 and RX 9070 family cards report `gfx1201` and resolve
  `rdna4`;
- Strix Halo reports `gfx1151` and resolves `strix-halo`;
- Strix Point reports `gfx1150` and resolves `strix-point`;
- forcing either other profile fails;
- `auto` succeeds;
- multiple render nodes require an explicit complete selection;
- Doctor performs a tensor operation on every explicitly selected GPU and
  rejects a mixed-architecture set;
- one llama.cpp server selected on two matching cards reports managed layer
  splitting and runs a model that exercises both cards;
- one ComfyUI process selected on two matching cards completes a graph using
  built-in component-placement or multi-GPU nodes;
- device permissions work without changing host groups or broadening
  container privileges.
- enforcing SELinux hosts report `container_use_devices`, fail clearly while
  it is off, and pass a real GPU tensor operation after it is enabled without
  disabling container labels;
- hosts exposing AppArmor's unprivileged-user-namespace sysctl report whether
  it is active, and unreadable or invalid values remain explicit; this is
  informational and prints no global opt-out or host-policy apply command;
- Strix Halo and Strix Point diagnostics report system RAM, the active TTM
  module and ceiling, and effective GTT; undersized GTT prints the expected
  RAM-aware host recipe without changing the host. The additional KFD kernel
  warning remains specific to Strix Halo.
- an OSTree boot with rpm-ostree available receives a transactional
  `rpm-ostree kargs` TTM recipe; a conventional GRUB host receives an owned
  `/etc/default/grub.d` drop-in and `update-grub`; a conventional Fedora host
  with `grubby` receives an all-kernel boot-entry update; other conventional
  hosts retain the active-module `modprobe.d` and detected initramfs-tool
  fallback.

### Tier 6: representative inference

Run this only on suitable target hardware. For changes affecting a family, begin with the bounded checkpointed smoke:

```bash
./paracetamol acceptance
```

Keep its JSON and Markdown result. A `BLOCKED` result means generated media
still needs human review; resume the same result rather than rerunning passed
cases. Then exercise the additional matrix rows coupled to the change:

- each changed llama.cpp speculative preset against a non-speculative control,
  recording prompt processing, generation speed, peak memory, acceptance
  behavior, and output sanity on `gfx1150`, `gfx1151`, `gfx1200`, and
  `gfx1201` where the preset is practical;

- one base and one accelerated bundle;
- every affected precision;
- T2V and I2V/edit paths when both exist;
- native application and ComfyUI paths if both consume related content;
- balanced memory policy;
- conservative policy on either RDNA 3.5 APU or another constrained-memory
  case.

Record the image tag, Git commit, profile, render-node set, model bundle,
policy, and result. Managed benchmark JSON records much of this for ComfyUI.

For llama.cpp, run the same catalog preset and explicit pp/tg/repetition
tuple with both ROCm and Vulkan on all target hardware classes. Preserve the
native result JSON below `apps/llama-cpp/benchmarks`; it records the selected
backend. Do not compare a catalog-pinned result with an unhashed local-model
result.

### Service and gateway inference acceptance

For llama.cpp concurrency changes, verify the running backend's slot count,
KV layout and total token pool. Use explicit worker counts, distinct request
markers and repeated turns to catch state leakage. Confirm overlapping
generation rather than merely successful requests queued through one slot.
Measure aggregate generated tokens divided by total wall time and report
per-request latency separately. Include an overflow request and long-context
retrieval without mistaking configured capacity for populated-context coverage.

These checks run real requests on suitable target hardware. They are not
CPU-only startup checks. Select the cases coupled to the change and keep
source, image, model, runtime and client identity with the results.

Repeat `stop gateway` and `stop all` with a resident backend. Both must drain
requests, wait for exact backend cleanup and leave no listener or private
backend. Also confirm clean Ctrl-C shutdown joins the backend log follower.

On suitable target hardware, cover lazy llama.cpp start, prefixed backend
output, a streamed request, client cancellation, FIFO draining, and a
router model switch. Confirm `status gateway` derives llama.cpp child residency
from a read-only private `/models` probe without `reload`, filters foreign
models and router arguments, and reports bounded fixed diagnostics on malformed
or unavailable responses. Send native Maki and generic OpenAI-compatible
reasoning and sampler payloads, then use `status gateway --requests N` to verify that client
and gateway-default values, selected policy/mode, wait/upstream/total timing,
lifetime/model aggregates, validated
session grouping, and available input/output/cached/reasoning tokens are
credible while prompts, tools, authorization, response content, invalid
session values, and forwarded peer headers are absent. Confirm the private
Paracetamol session header is not forwarded upstream. Exercise normal JSON,
fragmented SSE, client disconnect,
and concurrent backend/controller logs; correlation IDs must match start and
finish/reject lines and every physical line must retain one complete
`gateway |` or `llama-cpp |` prefix.

Request-report checks must use the ordinary inference path. Obtain the opaque
`X-Paracetamol-Request-ID`, then fetch its authenticated
`/paracetamol/v1/requests/REQUEST_ID` report. Verify receiving/waiting/upstream
metadata where observable, atomic completion, restart-safe identities, bounded
active/completed retention and explicit not-retained errors. Report reads must
not query the scheduler, start a backend, probe residency or sample resources.
Check sampler provenance alongside forwarded values, terminal `stop`/`length`
or tool finishes, SSE `[DONE]`, missing metadata, malformed/oversized events,
multiple choices and cancellation. HTTP success and gateway cancellation are
not native completion or slot-idle oracles. Preserve proxied bytes, flushing
and credential stripping, and check privacy sentinels in reports and status.
Run the gateway race tests for active/completed lookup and observer changes.

For sampling-policy changes, test every catalog-backed Qwen mode with missing,
null, partial, and explicit-zero sampler fields. Invalid or conflicting
thinking controls must return HTTP 400 before allocation. Unknown fields,
tools, messages, and large integer values must survive normalization. Models
without a policy must remain byte-preserving. Inspect llama.cpp's verbose
generation settings on real requests, not only gateway logs. Direct benchmarks
must send and record the same catalog tuple now that engine startup carries
no sampling patch or option.

Keep production-path acceptance separate from controlled engine measurements.
An external HTTP probe should use the ordinary gateway, omit sampler fields
to exercise its defaults, then send explicit overrides and inspect the
resolved policy and field provenance in the request ledger. No special gateway
test mode should bypass normalization, scheduling or backend ownership.
Direct engine probes remain useful for isolating native changes, but must
record the complete sampler tuple and any deliberate departures, such as
temperature zero. A fixed seed does not make those tests equivalent to normal
client sampling. Native `llama-bench` throughput tests do not sample language
responses and belong to the engine layer.

For llama.cpp cancellation, cover both streaming and non-streaming requests
through the native router and through the gateway. Wait until a child slot
has actually generated tokens, disconnect that client, and observe the slot
returning to idle through the private native `/slots?model=MODEL_ID` endpoint.
Keep the request's output limit high enough that natural completion cannot
masquerade as successful cancellation. Record the disconnect-to-idle time and
then check a normal follow-up request. A successful follow-up alone is not
proof that the abandoned work stopped, especially with several slots. Do not
expose this diagnostic route on the public gateway or publish the child port
outside loopback for the test.

First-output timing must ignore role-only and usage-only SSE chunks and remain
unavailable for non-streaming responses. Check text, reasoning and tool-call
streams, plus fragmented, malformed and oversized events. Cache-reuse totals
must use paired input/cache observations, not all input counts divided by a
partially reported cache count. Verify status reads only selected GPU sensors,
preserves unavailable values, reports host RAM rather than estimated model
memory, and labels APU SoC power as including the CPU. No sensor sampling may
start a backend or run in the inference proxy path.

With optional gateway authentication, test every route with missing, wrong,
duplicate and correct Bearer headers on both loopback and publication
listeners. Rejected requests must not read bodies, sample resources, query
model residency or start a backend. Confirm credentials do not reach backend
requests, logs, status or process arguments. Metadata clients must refuse
redirects, distinguish HTTP 401 from a wrong service, and bound response reads.
Check server/client key-file precedence independently, private file modes,
symlink/FIFO/empty/malformed rejection and key rotation by restart. Run native
Maki against an authenticated endpoint using temporary XDG state and
verify actual inference headers, not only generated configuration. Anonymous
operation must remain unchanged when no key is configured.

For a backend-restricted llama.cpp preset, verify direct startup automatically
selects its declared backend only when `--backend` was omitted. An explicit
incompatible choice and a managed benchmark must fail before container start.
Confirm an incompatible router and gateway omit the preset, while the declared
backend advertises and serves it. Hardware acceptance must include coherent
multi-turn output and a complete function-tool round trip. Treat a successful
load, synthetic `llama-bench` result, or short retrieval key as capacity or
performance evidence only.

For profile-scoped model allocation, also test missing/ambiguous host topology
and the independent container-side profile check. A mixed router must retain
its ordinary parent allocation policy while parsing the exceptional preset.
Load an ordinary model, switch to the exceptional model, then back again and
inspect actual child options, output and speculation. Run the build-time
`test-native-policy.cpp` checks and a non-root CPU startup through the real
entrypoint. Do not bypass bundled helper permissions by testing only the
native binary. A populated near-limit request, live-prefix reuse, cancellation
and reload are separate gates from reserving a large context at startup.

For host-configuration changes, load the complete generated example and
partial files with every table omitted in turn. Confirm unknown keys,
duplicates, oversized files, non-regular paths, invalid scalar types, and an
explicitly selected missing file fail closed. Exercise `-c` before and after a
command, preserve arguments after `--`, confirm CLI and environment precedence,
and verify `config init` creates mode `0600` once without replacing an existing
file. `--help` must remain available without loading configuration.

For native Maki integration, cover both configuration export routes. Check the
frozen inventory, tool-eligibility filtering, endpoint derivation, explicit
base-URL override, no-store responses and authentication. Requests must not
start a backend, fetch a supplied URL or export credentials. Forwarded headers
must not change the exported authority.

Use separately installed Maki 0.6.3 or newer for the optional transport fixture
above. Qwen3.8 supports off, low, medium and xhigh. Unsupported high snaps to
medium. The exported startup snippet explicitly selects xhigh, including for
Huihui. Test that default without rewriting the snippet, separately from
explicit adaptive, off and effort selections. Adaptive maps regular Flash-Next
to xhigh and Huihui or dense Qwen to medium. HTTP calls without reasoning
retain the catalog medium fallback.
Use Maki's native `always_thinking` Lua setting in print-mode tests. Its hidden
`--thinking` flag is accepted but ignored in the tested release.

On an available inference host, download the export to temporary native client
state and complete a real file-read and shell-tool round trip over the LAN
hostname. Check off and xhigh on regular Flash-Next and one other model family.
Require reasoning history to survive the tool-result request. Inspect actual
gateway request metadata and model output.
Run a native macOS smoke separately when macOS is available. Linux fixtures
do not establish DNS, permissions, keychain or runtime behavior on a Mac.

The former Pi coding-task runner is archived. Do not alter its frozen inputs
or relabel its old results as native Maki measurements. See the
[historical evaluation record](research/coding-agent-evaluation.md).

Speculative-decoding catalog changes additionally require one single-model
dry run and router INI inspection for every affected strategy. Cover an
embedded MTP draft, a separate MTP draft, and a separate DFlash draft when
those paths change. Confirm the target and draft are both hash-verified before
startup, the draft path is mounted through the existing read-only model
partition, and no speculative arguments appear for ordinary presets. CPU
startup validates argument shape only; it is not a correctness or performance
result for speculative decoding.

Changes to the llama.cpp share report require direct and router tests. Confirm
that `status llama-cpp` reports the running image ID, source labels, resolved
profile, exact PID 1 command, and redacted authentication state. In router
mode, confirm that no model selection lists the mounted preset identifiers and
that `--model PRESET` reports its context, reasoning and sampling ownership,
profile-specific cache and Flash Attention choices, and backend-specific draft
depth. Start with `auto` on target hardware so the report proves it shows the
resolved profile rather than merely echoing the request.

A context-metadata override additionally requires single-model and router
inspection for the exact `override-kv` values and disabled fitting. Hardware
acceptance must exercise prompts beyond the GGUF-declared window and compare
retrieval, output quality, memory, and speculative acceptance with the normal
metadata-backed preset. Successful allocation alone is not acceptance.

### llama.cpp source-update acceptance

Use this finite sequence after moving `LLAMA_CPP_COMMIT` or rebasing a native
backend patch. It turns the general validation tiers into a repeatable update
gate without assuming that one contributor owns every hardware class.

1. **Host-independent checks:** run Tier 1, perform the final no-cache
   `llama-cpp` build, inspect image labels and history, check the native
   runtime closure with `ldd`, and exercise CPU CLI and router startup. Confirm
   that all four GPU targets remain in the image and that no source, model, or
   web asset was fetched at runtime. Build the tracked
   [maintainer acceptance kit](../tools/llama-acceptance/README.md) and run its
   `self-test --source-only`; neither requires Podman or inference. Its
   `prepare-native` operation materializes the matching-source diagnostic
   recipe; building the recipe and executing native gates remain explicit.
2. **Primary target acceptance:** on one capable machine, run `doctor`, the
   bounded acceptance smoke, real CLI and router generation, both API
   endpoints, reasoning controls, speculative and non-speculative generation,
   and interruption cleanup. Run the kit's complete required production-path
   cases for regular Flash-Next plain and greedy DFlash2, its real CPU grader
   self-test, and matched baseline/candidate comparisons with a predeclared
   prefill tolerance. Keep matching-source numerical/state/operator fixtures
   and the benchmark matrix below for every affected backend or patch path.
3. **Cross-architecture spot checks:** on each remaining applicable hardware
   class, run `doctor`, a tiny-model GPU smoke, and the smallest representative
   benchmark that exercises the changed backend path. A profile-specific
   change does not require unrelated large-model testing, but generic HIP,
   Vulkan, device-selection, or model-loading changes do.
4. **Handoff:** record unavailable hardware as deferred rather than silently
   treating the primary machine as universal acceptance. Use the handoff
   format in [hardware-acceptance.md](hardware-acceptance.md#deferred-acceptance-handoff)
   and provide commands with every default made explicit.

Use catalog-managed models and keep the image, model, profile, render nodes,
prompt sizes, generation sizes, repetitions, batch sizes, cache types, Flash
Attention policy, and context depth identical between comparisons. The
minimum matrix is:

| Case | Purpose | Minimum comparison |
| --- | --- | --- |
| Tiny smoke | Detects basic load, offload, and generated-output failures cheaply. | ROCm and Vulkan, 32 prompt tokens, 16 generated tokens, one measured repetition. |
| Representative f16 KV | Detects ordinary dense or sparse long-context regressions. | Both backends at shallow depth and at least 32K context, 512 prompt tokens, 128 generated tokens, three repetitions. |
| Representative q8_0 KV | Exercises quantized-KV Flash Attention and its memory/performance tradeoff. | Repeat the long-context case with both K and V explicitly set to `q8_0` and Flash Attention explicitly enabled. |
| Speculative control | Separates model/runtime changes from speculative-decoding behavior. | The same managed family, prompt, and backend with MTP or DFlash enabled and disabled; record acceptance rate as well as aggregate timing. |
| Service behavior | Detects integration regressions outside `llama-bench`. | Router model selection, Chat Completions, Responses, reasoning levels, one-shot CLI exit, and clean stop or interruption. |

The exact representative model may change with the catalog. State why the
selected dense or sparse model covers the changed code, and do not substitute
a faster unrelated architecture merely because it is convenient.

Current source refreshes use regular Unsloth Flash-Next UD-Q4_K_XL as the
primary large-model acceptance case, with the managed greedy DFlash2 preset
and a non-speculative control. Cover populated long context, prefix reuse,
history edits, reasoning, tools and cancellation through the ordinary gateway.
Retained dense Qwen3.8 27B needs a short completion and tool round trip, not
a duplicate long-context matrix, unless a changed dense-specific path or a
failed smoke requires deeper investigation. This does not replace numerical
backend tests or the hardware-specific patch removal gates.
The kit's `short`, `lifecycle`, `coding`, `history` and `prefill` suites permit
targeted investigation, not a reduced full source-update gate. Missing required native,
production-path, performance or hardware evidence remains BLOCKED/deferred;
neither an image build nor a short PASS supplies it. Use the
[bounded coding-quality screen](#bounded-coding-quality-screen) to interpret
practical code generation separately from protocol/numerical acceptance. This
does not introduce a supported `benchmark agent` command.

Do not treat one successful tiny workflow as acceptance of every model family.

For the quantized on-device state-copy correction, run upstream's
`test-llama-context` with `--test '^(baseline|cp_d|cp_d_s|ps)$'` on a small managed
model. Repeat with both cache types set to `f16`, `q8_0` and `q4_0`. Require
four passed cases and zero skipped cases for each run. The `ps` case checks
that partial checkpoint size does not grow with tokens for a non-SWA cache.
Also check real MTP checkpoint reuse because a plain tiny-model cache does
not exercise the target/draft combination. The `baseline` case
is a prerequisite, not an optional control. Filtering it out can produce a
successful exit with the device-copy test skipped. This optional-cache test
does not change the managed presets' F16 policy.

Compile `applications/llama-cpp/test-topk-moe-lifetime.cpp` in the matching
builder when changing the HIP top-k MoE boundary patch. Run it on the selected
GPU with the same confinement as the state fixture below. It needs no model
and checks allocation-lifetime callbacks for eight terminal or partial-suffix
graphs. Zero callbacks is a failure, not a passing skipped fusion. Follow it
with upstream's `test-backend-ops -b ROCm0 -o TOPK_MOE` numerical cases.

#### Native build regression gates

The llama.cpp build runs `build-mmq-activation-sum-test.sh` against the exact
patched source before compiling GPU kernels. It extracts the shared MMQ
quantization/sum body and checks HIP and non-HIP preprocessing across all
three layouts, including independent scale and quantized-byte expectations.
This protects the temporary HIP compatibility policy, not GPU arithmetic or
model quality. To check an already patched source tree locally:

```bash
bash applications/llama-cpp/build-mmq-activation-sum-test.sh \
  SOURCE_DIR build/mmq-source-check
```

Keep the numerical, routing, state, gateway and populated-context gates.
The compatibility patch applies to every HIP device, so Strix Halo results
do not replace checks on the other supported architectures. Upstream
ownership of the former quantized-KV copy fix does not remove its F16,
Q8_0 and Q4_0 device-copy regression checks.

The llama.cpp build also runs `build-conv-rollback-test.sh` in CPU-only mode.
The fixture follows the pinned upstream overlapping-window/`SET_ROWS` graph
and checks all rollback slots, including cells that must remain untouched.
It deliberately fails on a different upstream pin until reviewed. Hardware
acceptance runs its forty cases and 160 rounds against the actual final
runtime libraries, checking postallocation ROCm matcher eligibility and
exact agreement with an unmarked reference. Keep graph capture enabled.
Matcher eligibility and numerical agreement alone do not prove optimized
kernel activation, which needs a normal model trace. Follow the tracked
[Qwen4Exp dispatch/span protocol](../tools/llama-acceptance/trace.md) for its
source-guarded diagnostic recipe, profiler qualification, expected launch
names, physical-span interpretation and rejected `qsa-host-span` control.
Host graph-construction logs are not graph-replay execution evidence;
profiling and source logging cannot supply normal-image timing acceptance.
The mechanically extracted routing fixture and maintained `QWEN_MOE_TILES` operator cases
remain separate gates.

#### Host pool and reset regressions

The late llama.cpp host-patch group runs three CPU-only build gates without
loading model weights or discovering GPU backends:

- `build-kpool-prefix-test.sh` mechanically extracts the exact production
  layout struct, scanner and complete update. Its separate integer oracle
  checks 28,032 updates, four deliberately broken variants, the production
  full-rebuild verifier and ASan/UBSan. Source boundaries and full region
  hashes fail closed. The small mock cache does not prove actual invalidation
  propagation, serialization, GPU writes or model quality.
- `build-active-pool-test.sh` compiles the actual-API fixture against matching
  installed libraries. Fifty-six cases cover idle/active/shared sequences,
  masks, tails, image positions and padded writes. Four child processes must
  hit their exact production assertion guards. The unchanged baseline must
  pass its old-policy oracle and fail the candidate contract. This is
  metadata/input coverage, not GPU attention arithmetic.
- `build-recurrent-state-clear-test.sh` compiles a six-graph actual-helper
  regression. It checks finite and poisoned reset rows, empty clears, real
  extra-state copies, unowned recurrent words and nonzero attention/indexer
  payload. The old helper must reproduce persistent nonfinite values and be
  rejected as the candidate. The corrected helper must clear them. This does
  not diagnose the first source of NaN or prove full-model inference.

The baseline binaries run before mutation of the private hybrid class layout.
Recompile candidate fixtures after the joint source rebuild and installation.
Use the exact matching installed libraries, not normalized build-tree hashes.
Native fixtures stay in the builder and are not part of the runtime payload.
For a reviewed matching checkout and installed closure, the compile commands
are:

```bash
bash applications/llama-cpp/build-kpool-prefix-test.sh SOURCE build/prefix-check
bash applications/llama-cpp/build-active-pool-test.sh SOURCE INSTALLED_LIBDIR build/active-check
bash applications/llama-cpp/build-recurrent-state-clear-test.sh candidate SOURCE INSTALLED_LIBDIR build/reset-check
```

The prefix command executes its integer tests. The other helpers compile only;
their callers must run the binaries explicitly with the matching library path.
The container build performs both the old and new CPU checks. Exact pin,
owning-file and source-tree guards require deliberate review on refresh, not
automatic hash regeneration. Host packaging tests only assert this wiring.

Keep independent device and model gates. In particular, a long resident idle
slot must remain reusable after work in another slot. Actual GPU-backed reset
tests must preserve finite attention/indexer state and reject CPU fallback.
The tracked recurrent-reset fixture currently executes CPU graphs only.
It does not implement that GPU gate, which remains BLOCKED until a GPU
fixture and rejected old-helper control are qualified. The full-model state
screen below checks different behavior and cannot substitute for poisoning.
Populated context, prefix reuse, edits, speculation, concurrency, cancellation
and changed-media histories remain necessary where applicable. Never time a
layout-verifier image as if it were the normal candidate.

#### Fast Flash-Next state screen

Before a large llama.cpp refresh matrix, run
`applications/llama-cpp/test-qwen4exp-state.cpp` with `--state-smoke`.
It prefills 4096 deterministic tokens, decodes two more, saves sequence state
and compares the next token's full logits after ordinary restore and three
clear-and-restore cycles. It uses no sampler, speculative decoder, harness,
HTTP service or causal-mode transition. The default without `--state-smoke`
retains the causal-transition regression test.

Compile the fixture in the matching llama.cpp builder, against that build's
headers and libraries. Do not reuse a fixture executable across different
patch stacks. `common_params` has no stable ABI. Keep the resulting executable
below the checkout's ignored `build/` tree and bind it read-only into the
corresponding runtime image. Use the normal rootless, read-only, network-none
GPU confinement and only the selected render node.

Inside that runtime, the direct-reader invocation is

```sh
env -u GGML_CUDA_ENABLE_UNIFIED_MEMORY /opt/probe/test-qwen4exp-state \
  --state-smoke --model /models/FIRST_FLASH_NEXT_SHARD.gguf \
  --device ROCm0 --gpu-layers 999 --flash-attn on \
  --ctx-size 8192 --batch-size 2048 --ubatch-size 2048 \
  --cache-type-k f16 --cache-type-v f16 --load-mode none \
  --lazy-mode on-direct --override-tensor per_layer_token_embd.weight=CPU \
  --no-host --fit off
```

Set `PARACETAMOL_QWEN4EXP_QSA_KERNEL=1` and
`PARACETAMOL_QWEN4EXP_QSA_GRAPH=1` when testing the accepted compact-graph
stack. Leave them unset for upstream attention. The fixture prints those
switches and the graph-disable overrides rather than requiring downstream
features, so it can also test unpatched upstream.

For an unpatched control, replace `--lazy-mode on-direct` with
`--lazy-mode off` and check available memory first. This makes the embedding
table resident and is not the managed long-context policy. On the October 6
Strix Halo screen it reached about 109 GiB of used host memory at 8K context.
Do not substitute mmap for this control without a separate residency check.

Any nonfinite logit, top-1 mismatch or normalized squared error above `1e-6`
fails the screen. Load time and test time are printed separately. A passing
screen is permission to continue acceptance, not evidence of long-context
quality, speculative correctness or a production-ready source update.

For a separate cross-build logit comparison, repeat the unchanged baseline
in independent processes first. A fixed sampler seed does not establish
bit-identical inference across processes. Keep the same teacher-forced
tokens and compare full-vocabulary vectors, including a real-source input
rather than only repeated synthetic tokens. If the baseline itself varies,
record those controls before attributing a mismatch to a candidate. Do not
relax the within-process save/restore gate above. Isolated operator tests
against the unfused GPU path and CPU reference remain necessary, and a
passing retrieval answer alone is not a numerical-equivalence test.

#### Bounded coding-quality screen

Protocol/operator checks and coding quality answer different questions.
Use the tracked [llama.cpp maintainer kit](../tools/llama-acceptance/README.md)
for original public Go/Python toy tasks, independent value/ownership oracles,
explicit CPU sandbox preparation and trusted/known-bad self-tests. Its
predeclared first attempt, one repair when available and fresh seeds 1234/2026
after a miss retain separate results. Coding misses and coding output ceilings
remain REVIEW, not a deterministic engine defect or an erased first attempt. Sandbox
or prerequisite failures are BLOCKED. Public tests are withheld from model
requests, not secret. The kit is not a general coding benchmark, Pi runner or
Maki client; `tools/gufo-recheck/` remains its separate small literal/tool gate.

Coding REVIEW is not an automatic source-update rejection or a requirement
for perfect one-shot code generation. Review the complete, unchanged
baseline/candidate attempt matrix, distinguishing an isolated miss recovered
by the predeclared repair or fresh attempts from persistent failure to
produce usable code. A maintainer may accept a documented inherited coding
limitation when matched evidence shows no candidate deterioration. Preserve
REVIEW and record that decision separately, never relabel the campaign PASS.
Persistent candidate failure against a usable baseline requires investigation
before acceptance. Persistent baseline failure makes this screen
non-discriminating, not evidence that the candidate is healthy. Do not change
budgets, seeds or oracles after observing results. This judgment cannot waive
protocol, sampling-policy, numerical, isolation, performance or hardware gates.

The [historical bounded screen](research/coding-quality-screen.md) preserves
its full task, attempt, oracle, toolchain and sandbox protocol. Its ignored
private kit is not available in a fresh clone. Exact reproduction requires the
complete hash-verified fixtures and toolchain manifests; otherwise declare a
new fixture identity rather than reconstructing historical bytes from prose.
The new public kit is not a recovery of that lost/private fixture identity.
Never weaken oracles, leak answers into prompts, retry after observing a
miss without a predeclared protocol, or replace an earlier miss with a later
pass. Generated-code execution needs the recorded CPU-only no-network
sandbox and exact-owned cleanup. Infrastructure failures are not model-quality
results, and a single task miss does not establish an engine defect.

## Source-revision acceptance checklist

Users build a source checkout locally; this checklist accepts the relevant
source/pin/runtime tuple, not a versioned release package. Apply checks to the
change's actual scope and record unavailable hardware or services explicitly.
Do not add tags, release branches or stable-interface obligations merely to
use this checklist.

### Source and documentation

- [ ] Working tree contains only intended changes.
- [ ] All new upstreams use full commits or image digests.
- [ ] Default image tags match the pins.
- [ ] Current guidance matches the checkout; historical versions/commands are
      explicitly labelled evidence rather than runnable current instructions.
- [ ] `THIRD_PARTY_NOTICES.md` matches built and downloaded components.
- [ ] `catalog/README.md` describes new content/license classes.
- [ ] User README quick-start and focused `docs/guides/` examples are current.
- [ ] Maintainer docs still point to real files and commands.
- [ ] Local Markdown links and section anchors pass the unit-test check.

Useful searches:

```bash
rg -n 'TODO|FIXME|WIP|latest|main' \
  Containerfile applications catalog internal docs README.md
rg -n 'localhost/paracetamol:|_COMMIT|_VERSION' .
```

`latest` or `main` may appear in explanatory prose, but must not be a stored
source revision or image base.

### Catalog and provenance

- [ ] Catalog, workflow and benchmark resources parse as JSON.
- [ ] All model revisions are full 40-character commits.
- [ ] Sizes and SHA-256 values come from pinned metadata or verified bytes.
- [ ] License URLs point to the exact relevant source/revision where possible.
- [ ] `NOASSERTION` is used for unresolved repacks/conversions.
- [ ] Required agreements are attached.
- [ ] Every bundle is in `all` and intended selector groups.
- [ ] Every workflow bundle has a pinned benchmark.
- [ ] `content install all --dry-run` counts and disk totals look intentional.

### Automated and container checks

- [ ] Static checks and full unit suite pass.
- [ ] All images build from cache.
- [ ] All images build without cache after base/dependency changes.
- [ ] `pip check` passes in each affected Python environment.
- [ ] CPU HTTP smoke passes for web applications.
- [ ] Batch dry-runs are confined and reference installed paths.
- [ ] No managed test container remains:

  ```bash
  podman ps -a --filter name=paracetamol
  ```

### Hardware acceptance

- [ ] The finite [target-hardware acceptance matrix](hardware-acceptance.md)
      records current results or explicit `N/P` reasons.
- [ ] `acceptance` passes, or its checkpointed result records reviewed
      failures and explicit profile-level `N/P` cases.
- [ ] RX 9060 family diagnostics and forced/auto profile tests pass.
- [ ] R9700 or RX 9070 family diagnostics and forced/auto profile tests pass.
- [ ] Strix Halo diagnostics and forced/auto profile tests pass.
- [ ] Strix Point diagnostics and forced/auto profile tests pass.
- [ ] Representative inference passes for affected applications/families.
- [ ] Memory and experimental kernel policies are tested if changed.
- [ ] Relevant managed benchmark completes and records expected metadata.
- [ ] A relevant `benchmark comfyui suite --dry-run` resolves the intended ordered
      bundle set without starting a container.
- [ ] Suite resume skips intact completed entries and rejects changed
      catalog/runtime inputs, rebuilt image IDs, and mismatched result files.

## Routine housekeeping

### Before working

```bash
git status --short
make test
podman ps -a --filter name=paracetamol
```

This distinguishes pre-existing local changes and containers from work created
in the current session.

### Periodically

- Review upstream releases and security notices for Ubuntu, ROCm/PyTorch,
  ComfyUI, ComfyUI-GGUF, rgthree-comfy, and llama.cpp.
- Review model cards and licenses at pinned revisions for later changes or
  takedowns.
- Check whether gated-model access behavior changed.
- Build without cache to expose transitive dependency drift.
- Run `pip check` in all final images.
- Run `content install all --dry-run` and compare counts/size with user
  documentation.
- Verify a rotating sample of installed bundles by full SHA-256.
- Inspect disk use:

  ```bash
  podman system df
  du -sh "${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol" 2>/dev/null
  ```

- Back up user-owned persistent subtrees.

Do not automate dependency or model upgrades directly into the main branch.
Discovery may be automated; pin selection, license review, and compatibility
acceptance require maintainer judgment.

## Backup and recovery

The [persistent-data](guides/operations.md#persistent-data) and
[scoped-cleanup](guides/operations.md#scoped-cleanup) sections define what is
irreplaceable and what each cleanup scope owns. Review those paths and back up
user input, output, state, custom models, workflows, and benchmark results
before any destructive source-update housekeeping. Catalog content and generated
caches are reproducible, although replacing them may be expensive.

For a safe rebuild:

1. Stop applications.
2. Back up user-owned data.
3. Build new image tags.
4. Start in CPU mode.
5. Run GPU diagnostics.
6. Test existing content without forcing workflow replacement.
7. Only then remove obsolete images.

## Failure guide

### Build-time patch no longer matches

The upstream source changed. Diff the old and new commits, understand the new
behavior, and rewrite the strict patch. Do not weaken `replace_once` or skip
the patch.

### `pip check` fails

The selected pins are not a consistent environment. Fix requirements or
constraints; do not add `--no-deps` merely to complete the build.

### Catalog refuses to load

Read the exact loader error. Common causes are a short revision, invalid
SHA-256, unsafe path, duplicate destination, unknown selector, missing
agreement, or a workflow bundle without a benchmark.

### Setup reports size or hash mismatch

Paracetamol will not overwrite an installed file. Determine whether it is:

- user content at a managed destination;
- an old pinned model revision;
- an incomplete manual download;
- upstream content that changed unexpectedly despite the same revision.

Move the file aside explicitly, verify provenance again, and rerun
`content install TARGET`. A remote mutation at an immutable-looking revision
is a security/provenance event and should not be normalized by updating the
hash without investigation.

An exact-size staging file that fails SHA-256 is different: Paracetamol preserves
it beside the expected path with an `.invalid-*` suffix. Retrying can then
download or reuse a verified replacement without deleting other resumable
staging. Inspect the quarantined file if the mismatch may indicate remote or
storage corruption.

### Hugging Face returns 401 or 403

Check repository visibility, gating, account acceptance, and `HF_TOKEN`.
License acceptance in Paracetamol does not grant upstream account access, and an
HF token does not replace `--accept-license`.

### Curated workflow resource/hash mismatch

Installation reads `catalog/workflows/<ID>.json` from the checkout and
compares its exact hash with `rendered_sha256`. Check the resource, catalog
and source checkout together. Runtime installation does not extract a
workflow-template package from an image.

For an authoring change, independently check pinned upstream source bytes,
license and transformation provenance before updating the rendered hash.
A hash change alone is not evidence that the graph still has the intended
model references and behavior. See
[workflow maintenance](content-catalog.md#add-or-update-a-curated-workflow).

### Container already exists

Use:

```bash
./paracetamol logs APPLICATION
./paracetamol stop APPLICATION
```

If normal stop cannot recover an abandoned managed container, inspect it with
Podman before using `./paracetamol cleanup containers` and confirming its plan.
The cleanup plan includes containers with reviewed managed ownership labels,
including benchmark, diagnostic, shell and downloader roles as well as
applications. A canonical application name, exact legacy transient name or
generated downloader prefix is not an ownership exception. Unlabelled
containers require manual verification and explicit operator removal.
Cleanup never removes persistent data unless the explicit `data` scope is
selected and its plan is confirmed.

### Profile mismatch or unsupported architecture

Confirm the selected render-node set with `doctor`. A forced profile is
an assertion, not a compatibility override. Do not map an untested
architecture to the nearest known profile merely to let startup continue.
