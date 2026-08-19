# Architecture and invariants

## Mental model

ROCmplete has three layers:

```text
host launcher
  ├── builds locally pinned application images
  ├── selects an exact render-node set and constructs constrained Podman commands
  └── installs verified content into one persistent data directory

application images
  ├── shared pinned Ubuntu + minimal ROCm runtime
  ├── shared ROCm/PyTorch base where applicable
  ├── native build toolchains without PyTorch where required
  ├── separate application dependency/source layers
  └── entrypoints that confine paths and validate the selected profile

persistent storage
  ├── application-owned writable state under apps/<application>/
  ├── verified managed files under content/
  └── removable download scratch space under staging/
```

The host Python launcher does not import ROCm or application dependencies. It
only needs Python 3.12+, Podman, and access to the device files.

Data-path resolution has two explicit modes. Status, inspection, and dry-run
commands resolve an absent path without creating it. Real installs, shells,
and application execution prepare only the required partition. Each runtime
receives its application directory as writable `/data`; managed content is
mounted separately and read-only below `/content`.

## Command flow

`rocmplete` is a small Python entry script. It adds the repository-local
`src/` directory to the import path and calls `rocmplete.cli.main()`. Keeping
the import package below `src/` leaves `rocmplete` as the only matching path at
the repository root for shell completion. `src/rocmplete/project.py` owns
repository-root discovery for build context and catalog resources.

`src/rocmplete/ui.py` owns semantic terminal styling. It emits ANSI sequences
only to a TTY, honors `NO_COLOR`, and leaves redirected output plain. CLI and
content modules select roles such as heading, command, success, warning, or
error; they do not embed raw escape sequences.

For a managed web application:

```text
CLI parser
  → resolve application/profile/port/data/render-node set
  → validate host paths and device access
  → runtime/ constructs the application Podman command
  → application entrypoint validates the GPU architecture inside the image
  → application binds inside a private rootless network namespace
  → Podman publishes exactly one TCP port on the requested host IP
```

Profile precedence is:

```text
--profile → ROCMLETE_PROFILE → auto
```

With one render node, the host selects it. With zero or multiple nodes, a GPU
run fails until the caller supplies an exact set by repeating `--render-node`.
Applications declare whether one workload may receive more than one node.
The profile does not select a physical device. A multi-GPU workload must
resolve to one supported architecture.

Attached application runs keep Podman in the terminal process group for normal
interactive input and signal delivery. `podman.run_managed_foreground` owns
the named-container cleanup race: if Ctrl-C interrupts both the launcher and
Podman client before `--rm` completes, it force-removes that one exact managed
container and reaps the client before propagating the interruption. A nonzero
attached container status becomes a `LauncherError` that retains the exact
child status for the human diagnostic. Detached runs deliberately retain the
ordinary Podman lifecycle.

The llama.cpp entrypoint writes a closed, versioned runtime snapshot below the
container's private `/tmp` after resolving the hardware profile and backend
device names. `status llama-cpp` reads that bounded file and PID 1's
NUL-delimited command through exact `podman exec ... cat` calls. It combines
those live data with structured container environment and image inspection.
The snapshot is never persisted below `/data`, so a stopped container cannot
leave a stale runtime claim. Router model membership comes from the exact
read-only generated INI mounted into the running container. The report prints
authentication presence
but never reads or exposes API-key values or host secret paths.

Every ROCmplete-created runtime container carries Podman metadata labels for
ownership, role, and application where applicable. Scoped cleanup discovers
these labels instead of assuming that all owned containers are the
long-running application names. A small exact-name set covers known transient
containers, while generated downloader names retain their separately
constrained prefix.
Explicit application stop allows a brief graceful interval before forced
removal. Interruption and confirmed cleanup use a zero-second forced removal
and verify absence under a signal-masked critical section, so repeated Ctrl-C
cannot strand the removal client after Podman enters `Stopping`.

For content installation:

```text
catalog loader
  → resolve an application/category, family, aggregate, or exact bundle
  → show sizes, destinations, license state, and agreements
  → optionally find same-name bytes in a non-overlapping local mirror
  → download through the locally built image's pinned HF or HTTPS downloader
  → verify size and SHA-256
  → atomically install direct files
  → prune staging only after successful verification and installation
  → render and install a pinned workflow when the bundle has one
```

Artifact downloads execute in the dedicated content-tools image. Curated
workflow sources are package resources of the managed ComfyUI
image and must be read from that image instead; a content-tools image override
must never become a workflow-source override. Managed ComfyUI benchmarks also
default to the ComfyUI application image.

Downloader containers use unique `rocmplete-download-*` names. Repeated
Ctrl-C is ignored only while the launcher sends an immediate kill, reaps the
isolated Podman client, and waits for that exact child name to disappear. The
absence check is authoritative because Podman may return a transient failure
after the container has already entered `Stopping`. A new downloader refuses
to start while another managed downloader name remains. On enforcing SELinux
hosts, the reusable storage bind uses the shared `:z` label: private `:Z` MCS
categories would make an interrupted container's resumable partials
inaccessible to its successor even though both processes use the same keep-id
UID. Host umask and file ownership remain unchanged. Host-side progress
measures the current request's planned payloads and changed retry partials,
not the apparent size of the whole staging tree, because Hugging Face may
preserve superseded `.incomplete` files after interruption.

`content import URL` is a constrained front end to that same path:

```text
allowlisted Civitai or Hugging Face URL
  → provider metadata and immutable file identity
  → explicit choice when version, file, or destination is ambiguous
  → schema-valid ignored local content pack
  → ordinary content-install planning, approval, download, and verification
```

`src/rocmplete/remote_import.py` owns provider URL parsing, metadata requests,
file/type inference, stable generated identifiers, and atomic pack writes.
It does not own another downloader. Tokens are attached only to the initial
provider request and are not forwarded across redirects. Generated imports
remain `NOASSERTION` even when provider metadata names an upstream license.

`src/rocmplete/recipes.py` owns the small application-first recipes shown by
the guided installer and application guides. A recipe resolves to exact
catalog bundles, validates application ownership, and provides the copyable
next command printed after installation. It is not a broad model family.

For a benchmark:

```text
pinned API-format graph
  → deterministic seed/input/output rewriting
  → private ComfyUI instance on 127.0.0.1
  → fresh-process request then warm requests
  → persistent or isolated generated compiler caches
  → result JSON under apps/comfyui/benchmarks
  → benchmark container and isolated cache always removed in finally
```

For a native llama.cpp benchmark:

```text
exact local GGUF or installed catalog preset
  → one network-isolated llama-bench container
  → entrypoint enforces CPU or detected GPU profile policy
  → fixed explicit depth, pp/tg, batch, KV-cache, FA, and repetition parameters
  → parse llama-bench JSON stdout
  → add image, profile, device, model, and parameter identity
  → atomically create result JSON under apps/llama-cpp/benchmarks
  → named benchmark container removed in finally
```

Backend comparison invokes that same path once for ROCm and once for Vulkan.
Each native result remains independently useful. A small atomic comparison
manifest records both paths, failures, extracted pp/tg rates, and the derived
time for that exact token ratio. `LauncherError` from one backend is recorded
before the other runs; `KeyboardInterrupt` still escapes immediately after
the current benchmark's normal container cleanup.

For a speculative llama.cpp depth screen:

```text
installed speculative preset plus reviewed agent request policy
  → calibrate deterministic chat-template prompts with server tokenizer APIs
  → fingerprint source, image, model, devices, sampler, reasoning, and plan
  → start one retained, single-slot benchmark server per trial
  → issue one seeded Chat Completions request with no reusable prompt cache
  → capture structured response timings and accepted/drafted token counts
  → checkpoint atomically before and after every trial
  → remove the exact benchmark container in finally
  → aggregate server-timed generation work over total generation time by draft depth
```

`src/rocmplete/llama_speculative_benchmark.py` owns prompt generation,
calibration, result compatibility, server/request execution, checkpointing,
resume, and screening summaries. Native `llama-bench` remains a separate
non-speculative measurement and the screen does not mutate catalog policy.

For bounded target-hardware smoke acceptance:

```text
detected profile + selected render node
  → exact GPU exposure and CPU device-absence probes
  → profile-specific case plan and exact image/content prerequisites
  → one constrained real workload per applicable application
  → checkpoint after every attempt and require human review for media
  → fingerprinted JSON plus Markdown below apps/acceptance/results
```

The acceptance fingerprint binds the actual source identity, image IDs, catalog
artifact/tree/workflow hashes, detected architecture/profile, render node,
the complete immutable case descriptions and review criteria, and runtime
policies. Resume validates the suite ID, root status, attempts, artifacts, and
case metadata before those values can form output paths or be checkpointed.
Clean source uses the Git revision;
tracked changes and untracked runtime/build inputs add a content digest.
Resume skips accepted cases, retries failed or interrupted cases, and fails
closed when any bound input changes.
The bounded suite is an operational smoke layer, not a replacement for the
complete model-family and performance matrix.

This fingerprint is a resume-compatibility check over the run definition. It
does not authenticate the case outcomes and is not a signed attestation.

## Image layout

`Containerfile` defines a small `content-tools` target containing pinned
Hugging Face download dependencies and the resumable direct-HTTPS helper.
Every application build ensures and tags this prerequisite as
`localhost/rocmplete:content-ubuntu26.04-huggingface1.27-r1`; `content install`
uses it without building anything itself. This keeps downloads independent of
ComfyUI and makes a llama.cpp-only initial setup complete.

`Containerfile` defines a minimal `rocm-runtime` target, which the launcher
builds and tags as the managed local prerequisite
`localhost/rocmplete:runtime-ubuntu26.04-rocm7.14-r2`. It owns the pinned
Ubuntu runtime, Python environment, and AMD's modular ROCm core, libraries,
and exact `gfx1150`, `gfx1151`, `gfx1200`, and `gfx1201` device wheels. It
does not contain PyTorch or a compiler toolchain.

The `rocm-base` target starts from `ROCM_RUNTIME_IMAGE`, adds PyTorch,
torchvision, torchaudio, and the common Python-application build tools, and is
tagged as
`localhost/rocmplete:base-ubuntu26.04-rocm7.14-torch2.11-r5`. Each final
PyTorch application target starts from the `ROCM_BASE_IMAGE` build argument:

- `comfyui`

Application stages must not install conflicting frameworks into one
all-purpose image.
Derived builds use `--pull=never` for both managed base references, so a
missing local prerequisite fails rather than pulling an image with the same
name. `command_build` always passes required local prerequisites through
Podman's layer cache before building a dependent image. An existing tag alone
is not treated as proof that build-context inputs still match the checkout. A
fully cold `--no-cache` build deliberately refreshes the complete prerequisite
closure. The public `build base` target builds `rocm-runtime` followed by the
ROCm/PyTorch base for GPU diagnostics or prerequisite validation; it does not
also build content tools or an application. The symmetric
`build content-tools` target refreshes only the verified download-tools
prerequisite.

`llama-cpp` deliberately does not inherit the PyTorch base. Its SDK stage
starts from `ROCM_RUNTIME_IMAGE`, adds only AMD's modular development payload,
and compiles pinned upstream llama.cpp with both HIP and Vulkan backends for
all four supported architectures. The development payload exists only in the
builder. The final image also starts from `ROCM_RUNTIME_IMAGE`, adds the Mesa
RADV and small native runtime dependencies, and copies only the server, CLI,
and benchmark binaries. RPC and remote UI assets are disabled.
`ApplicationSpec.shared_pytorch_base` keeps the choice of the higher base
explicit in build and image-archive planning; every GPU application still
shares the lower runtime.

`dwarfstar` follows the same native split without inheriting llama.cpp's
Vulkan or HTTP dependencies. A shared `native-rocm-sdk` stage adds AMD's
modular development payload to `ROCM_RUNTIME_IMAGE`. The DwarfStar builder
fetches one full source commit, uses the upstream ROCm target while compiling
HIP code for `gfx1150`, `gfx1151`, `gfx1200`, and `gfx1201`, and embeds RPATHs
to the modular ROCm runtime libraries. Clang's per-architecture offload jobs
share the bounded GNU Make jobserver, so the four device targets compile in
parallel without creating a second, unbounded worker pool. The final image
starts again from `ROCM_RUNTIME_IMAGE` and retains only `ds4`, `ds4-server`,
`ds4-bench`, the license, and ROCmplete's constrained entrypoint. Upstream
setup scripts and runtime binaries are not build inputs.

All GPU applications follow the global `ROCM_VERSION`. The native llama.cpp
builder changes the development dependency shape, not release policy. A ROCm
update is therefore incomplete until the shared runtime, PyTorch base, and
native build pass their applicable hardware acceptance.

For `build all --no-cache`, the launcher builds `content-tools`,
`rocm-runtime`, and `rocm-base` once each with no cache, then invokes each
application target with no cache. PyTorch applications use the freshly tagged
PyTorch base; native applications use the freshly tagged lower runtime. This
keeps cache-free application semantics without reinstalling the common ROCm
payload for each dependency branch.

Podman's intermediate layers and downloaded Python packages are separate cache
classes. Normal builds reuse both while still asking Podman to validate every
required prerequisite target. `--no-layer-cache` passes Podman's `--no-cache`
only to the selected target; prerequisite targets retain their layer cache,
and all targets retain the host pip cache. `--no-cache` bypasses both cache
classes and cold-builds the prerequisite closure.

The host pip cache lives below
`${XDG_CACHE_HOME:-$HOME/.cache}/rocmplete/build/pip` and is mounted only for
build `RUN` instructions at `/var/cache/rocmplete/pip`. The launcher supplies
the mount and pip build arguments together; direct Containerfile builds
default to `PIP_NO_CACHE_DIR=true`. The cache is therefore neither copied into
an image nor mixed with persistent application data. `cleanup build-cache`
removes only ROCmplete's build-cache directory and never prunes Podman state.
Like every cleanup scope, it prints a validated non-empty plan and crosses the
shared confirmation boundary before mutation.

`src/rocmplete/image_archive.py` owns offline build-output transfer. A managed
export is one Docker-format archive containing the content-tools prerequisite,
the shared ROCm runtime, the ROCm/PyTorch base when required, and selected
application images.
It is written to a same-directory partial path,
validated against the pre-export image IDs, and atomically promoted without
overwriting an existing destination.

Import parses only bounded `manifest.json` and image-config members without
extracting the archive. It rejects duplicate or unmanaged tags, missing base
closure, unsupported OS/architecture, malformed configs, and current tags
whose local image ID differs. Podman performs layer loading and its own digest
checks only after this validation. Successful loading is followed by exact tag
and config-digest verification. Archive transfer never includes persistent
application or content data.

The `.containerignore` file is an allowlist. Any new build input must be
explicitly added there or Podman will report that the source was filtered out.

Application source repositories are initialized and fetched at exact commits.
The build checks `git rev-parse HEAD` before removing `.git`. Upstream license
files remain with their source trees; application stages may also copy them to
documented paths below `/usr/share/licenses/rocmplete`.

Application-specific build inputs live below `applications/<application>/`.
ComfyUI Manager policy uses a strict build-time patch program. It matches
expected upstream text exactly and refuses zero or multiple matches. An
upstream upgrade causing a patch failure is a review request, not something
to bypass with a looser replacement.

The pinned DwarfStar source receives the fail-closed
`applications/dwarfstar/multiarch-wmma-fallback.patch`. Upstream's direct
256-bit WMMA prefill kernel is specific to the `gfx11` targets it was written
for and does not compile for RDNA 4. The patch retains that optimized dispatch
on `gfx1150` and `gfx1151`, while RDNA 4 uses the existing generic batched Q8
path. Remove it only when a reviewed upstream pin selects a correct kernel by
device capability itself.

The pinned llama.cpp source receives the fail-closed
`applications/llama-cpp/hip-apu-host-buffer.patch`, taken from upstream PR
25863. It preserves integrated-device detection and pinned host allocation
while preventing direct computation on unsafe `ROCm_Host` buffers. Remove the
patch when a reviewed future pin contains the upstream fix.

The fail-closed `applications/llama-cpp/quantized-kv-flash-attention.patch`
contains the reviewed Vulkan q8_0 and HIP q8_0/q4_0 dequantize-on-load changes
from Nathan Wilson's `strix-halo-fa-fixes` branch at commits `4edaca09`,
`4355d03e`, and `2a24abc6`. It removes repeated KV dequantization at long
context while leaving f16 paths unchanged. The patch is retained only while
the pinned upstream source lacks those changes and must be requalified on all
four ROCmplete architectures whenever llama.cpp, ROCm, or Mesa moves.

The separate fail-closed
`applications/llama-cpp/vulkan-f16-kv-contiguize.patch` carries the small,
environment-gated part of commit `b1a10f981` that copies strided f16 KV data
into contiguous scratch before Vulkan Flash Attention prefill. ROCmplete
enables it only for the Vulkan backend on `gfx1151`; every other backend and
architecture retains pinned-upstream behavior. This deliberately excludes the
fork's wider collection of experimental kernels and tuning flags.
`applications/llama-cpp/entrypoint.sh` exposes a constrained server/CLI policy
around the resulting binaries. It checks the architecture reported by
`rocminfo`, then maps the exact selected render-node count to explicit
`ROCmN` or `VulkanN` llama.cpp device names. Building two backends into one
binary must not make both available devices part of one workload
accidentally. This native check intentionally does not import or depend on
PyTorch.

`applications/dwarfstar/entrypoint.sh` is intentionally smaller. It accepts
only server or CLI mode, verifies that exactly one supported architecture is
visible, resolves or checks the matching hardware profile, and constructs the
reviewed model, context, output-limit, thinking, and bind arguments. It does
not expose upstream arguments generically. One explicit `--dspark` path
selects the exact managed 0731 target and support GGUF pair and applies the
reviewed DSpark/MTP engine flags; normal launches remain unchanged. Arbitrary
MTP files, distributed execution, SSD streaming, multi-GPU, benchmark,
evaluation, and agent surfaces remain unavailable.

## Runtime isolation

The common web runtime in `src/rocmplete/runtime/web.py` applies:

- `--rm` and a deterministic container name;
- a private rootless network namespace and one explicitly published TCP port;
- a read-only root filesystem;
- all capabilities dropped;
- `no-new-privileges`;
- `keep-id` user namespacing and the host umask for persistent-data ownership
  and modes;
- a zero soft and hard core-dump limit;
- PID and shared-memory limits;
- an explicit writable `/tmp` tmpfs;
- one application-specific writable `/data` bind mount;
- only required managed-content partitions below read-only `/content`;
- `/dev/kfd` and only the selected render-node set for non-CPU profiles.

On enforcing SELinux hosts, the launcher requires the standard
`container_use_devices` policy boolean before a GPU run. Podman still mounts
only `/dev/kfd` and the selected render nodes. ROCmplete refuses a disabled
policy with a copyable host command instead of disabling SELinux labeling for
the container; ROCr otherwise reports device enumeration successfully and can
segfault only when it first maps `/dev/kfd`.

Doctor also reads
`/proc/sys/kernel/apparmor_restrict_unprivileged_userns` when that optional
kernel interface exists. A nonzero value can block bubblewrap installations
without a matching AppArmor profile. Doctor offers a persistent sysctl opt-out
and labels its system-wide security scope; it does not mutate the host or
pretend that disabling the restriction is equivalent to adding a narrow
executable profile.

Files copied from the host into immutable image paths are normalized to
readable image modes during the build; they must not inherit a restrictive
developer umask such as `0077`. Writable scratch directories baked below a
tmpfs mount likewise need an explicit mode suitable for the keep-id process.
Persistent application data continues to honor the invoking user's umask.
Managed content is writable only to the installer, not application runtimes.

Web applications bind to their address-family wildcard only inside their
private network namespace. Podman publishes the one application TCP port on
`127.0.0.1` by default. `--listen 0.0.0.0` publishes it on every IPv4 host
interface and explicitly puts pasta in IPv4-only mode so it does not accept
and reset IPv6 connections that should fall back to IPv4. An IPv6 host address
uses an internal `::` bind. One exact host IP limits publication to that
address. Non-loopback publication makes the launcher print an
unauthenticated-exposure warning.

`--unconfined` only disables the seccomp filter. It does not make the
filesystem writable or restore capabilities.

## Profile validation

Profiles exist on both sides of the container boundary:

- `src/rocmplete/config.py` controls accepted CLI values.
- `containers/common/profile.py` maps PyTorch architecture names to resolved
  profiles.

This duplication is intentional: the host validates user input, while the
container validates what ROCm actually sees. Both lists must change together.

`auto` resolves both `gfx1200` and `gfx1201` to `rdna4`, `gfx1151` to
`strix-halo`, and `gfx1150` to `strix-point`. A forced profile must match.
`cpu` does not query a GPU and the host exposes no GPU devices.

ComfyUI's entrypoint applies application-specific policy after resolution,
including `--disable-mmap` for both RDNA 3.5 APU profiles and conservative
memory flags.
ComfyUI Manager is installed at the version required by the pinned ComfyUI
source but is enabled only by forwarding `--enable-manager` after the
ROCmplete `--` separator. Its security policy receives
`ROCMLETE_HOST_LISTEN`, because ComfyUI itself must bind to a wildcard inside
the private container namespace even when Podman publishes only on host
loopback. Manager installation remains denied for a non-loopback host
publication.

The entrypoint creates a persistent child virtual environment below
`/data/custom-node-python`. A `.pth` file exposes the immutable image
site-packages as its base, while standard Manager and node `pip` subprocesses
write only to the child environment. ROCmplete's validated Manager patch uses
`pip` instead of Manager's optional `uv` path here because `pip` includes the
image packages exposed through `.pth` when resolving and listing the combined
environment. The image-owned Python environment stays immutable; custom-node
source and dependency changes are confined to application data.

ComfyUI-GGUF and rgthree-comfy are immutable, source-pinned image extensions.
At startup the entrypoint creates a temporary custom-node view containing
symlinks to the bundled copies. If `/data/custom_nodes` already contains the
same directory name, the persistent copy is omitted from that view and takes
precedence. This collision rule prevents duplicate registration without
modifying persistent user data. `--disable-bundled-extensions` omits the
complete temporary view while leaving persistent nodes alone.

llama.cpp performs its native architecture check with `rocminfo`. On Strix
Halo and Strix Point it enables `GGML_CUDA_ENABLE_UNIFIED_MEMORY` and
`--load-mode none`; on RDNA4 it retains upstream automatic GPU-layer and fit
behavior.

## Persistent data

Without configuration, the default host directory is:

```text
${XDG_DATA_HOME:-$HOME/.local/share}/rocmplete
```

`[storage].data_dir` in
`${XDG_CONFIG_HOME:-$HOME/.config}/rocmplete/config.toml` provides the durable
host-level override. An explicit `--data-dir` wins over `ROCMLETE_DATA_DIR`,
which wins over TOML configuration, which wins over the XDG data default.
Configuration lookup and dry-run resolution do not create either directory.
The TOML surface is intentionally limited and unknown sections or keys fail
closed.

The storage boundary is:

```text
apps/
  comfyui/
    user/
      default/workflows/
        curated/                rendered and validated workflows
        imported/               exact third-party workflow artifacts
    input/ output/ custom_nodes/
    custom-node-python/         persistent custom-node Python environment
    home/ cache/ benchmarks/
  llama-cpp/
    home/ cache/ models.ini
  dwarfstar/
    home/
  acceptance/
    results/                         checkpointed JSON and Markdown summaries
  agent-evaluation/
    sources/                         pinned public bare mirrors
    cache/                           prepared toolchain caches, currently Go
    results/                         machine-specific JSON and Markdown summaries
    runs/                            fixtures, transcripts, patches, and grading logs
content/
  comfyui/models/               direct ComfyUI model files
  llama-cpp/models/             managed GGUF model files
  dwarfstar/models/             managed DeepSeek V4 Flash GGUF
staging/
  comfyui/ llama-cpp/ dwarfstar/
  .cache/ .home/                downloader-only transient state
```

Managed `content/` and generated caches are reproducible from the catalog.
Application `custom-loras`, `state`, `user`, `input`, `output`, and benchmark
results may be irreplaceable and deserve backup before destructive
maintenance.

Persistent benchmarks reuse the normal generated caches but always start a
fresh ComfyUI process. Isolated benchmarks override HOME, XDG cache,
Hugging Face, Torch, and Triton cache paths with a fresh subtree under
`apps/comfyui/benchmarks/.cache/`. Cache mode is part of result metadata and
suite signatures.

Direct managed artifacts are regular files. A symlink, directory, device, or
other unexpected type at an artifact destination is treated as a user
collision rather than followed or replaced.

Temporary work is partitioned by consumer under `staging/`. Completed staging
is removed after its bytes reach their final verified location; failed or
interrupted staging remains resumable. A complete file that fails SHA-256
verification is atomically renamed with an `.invalid-*` suffix: the rejected
bytes remain available for investigation while the expected path becomes
retryable.

A local migration mirror is indexed recursively without following symlinks.
Candidates are shortlisted by basename but accepted only after exact size and
SHA-256 verification. Copy mode preserves the source. Move mode transfers the
in-mirror regular file and may break old managed symlinks; mirror and
destination roots are therefore required not to overlap.

## Catalog and workflow trust chain

`catalog/catalog.json` is schema version 23. The loader in
`src/rocmplete/catalog.py` rejects malformed identifiers, unsafe paths,
non-full revisions, invalid hashes, duplicate destinations, unknown
references, and incomplete bundle/benchmark relationships.

The catalog supports direct artifacts and bounded archive collections:

- A direct artifact is one remote file installed into its declared managed
  partition. ComfyUI artifacts use `content/comfyui/models`; managed GGUF
  artifacts use `content/llama-cpp/models`; DwarfStar GGUFs use the separate
  `content/dwarfstar/models` partition.
- An archive collection expands into direct artifacts that share one bounded
  archive transport while retaining independently verified member identities
  and destinations.

Every ComfyUI bundle with a workflow must also have a benchmark resource. This
is enforced while loading the catalog.

Each `llama_presets` entry must reference a llama.cpp bundle and a `.gguf`
artifact in that bundle's `llama-models` partition. Bundle and preset IDs name
the concrete architecture, variant, and quantization; task-oriented labels
belong to the recipe layer. `default_context` is the reviewed launcher
starting point. Agent presets may expose their native context, while bounded
or memory-constrained workloads may deliberately start smaller. A preset
can select one of the allowlisted `draft-mtp` or `draft-dflash` strategies
with a strategy-specific bounded token count and an optional closed
ROCm/Vulkan override map. A different draft GGUF must belong to that same
bundle. MTP may instead use tensors embedded in the target GGUF, while DFlash
always requires the separate draft artifact. A preset may
name exact GGUF architecture prefixes whose `context_length` metadata is
overridden to that launch's selected context. Presence of this narrow policy
also disables llama.cpp automatic fitting; it does not expose general metadata
or argument overrides. Presets can additionally enable Jinja, select a
project-bundled chat template from a closed allowlist, and select `on`, `off`,
or `auto` Flash Attention behavior for a concrete GPU profile. A preset can
also select a symmetric target K/V cache from the closed `f16`, `q8_0`, and
`q4_0` set for an accepted profile. Quantized cache policy requires Flash
Attention explicitly on and does not alter the speculative draft cache. Managed
templates are copied into the image and never read from host content. The
launcher renders only installed presets into a private atomic INI, rejects
partial managed content, and mounts both that exact file and the model
partition read-only. Single-model startup passes the same catalog-owned policy
through narrow environment fields that the entrypoint validates before
constructing llama.cpp arguments. The entrypoint derives a tmpfs router copy
that injects offline and resolved-profile policy into each model section
without modifying the mounted source. The router is upstream llama-server;
ROCmplete does not implement a routing daemon.

`agent_tools` is a reviewed compatibility claim, not an inference from model
size or Jinja alone. It requires Jinja and at least a 16384-token managed
context. One maintained client must complete an end-to-end function-tool
acceptance test on target hardware before promotion, and every generated
client configuration remains covered by schema and serialization tests.
The narrower `reasoning_control` and `reasoning_preserve` policies both require
`agent_tools`, but remain independent. The first records the model's native
toggle, effort, or strength surface; the second maps direct and router startup
to llama.cpp's reasoning-history preservation. A model can require one
without supporting the other.
Other clients may then expose the shared OpenAI-compatible contract
provisionally, but each still needs its own live loop before unattended write
access is considered accepted. Sibling presets with the byte-identical target
and template may share this protocol evidence, while their distinct context
or speculative-decoding policies retain separate runtime acceptance.

`reasoning_control` is the narrower model claim behind agent-client reasoning
selectors. `reasoning_levels`, `reasoning_default`, and `reasoning_off` define
the exact UI contract. Qwen3.6 declares a toggle, Qwen3.8 declares native
effort, and Muse declares native strength. llama.cpp forwards recognized
OpenAI-compatible labels through its template capability layer as both
`reasoning_effort` and `reasoning_strength`; each template consumes only its
own variable. ROCmplete does not infer a native label from a client's numeric
reasoning budget. Muse declares no off choice because its model always
reasons; the reviewed template maps clients' unavoidable generic off value to
Muse low without changing other model families.

`bin/rocmplete` is a PATH-friendly delegate to the root checkout launcher and
resolves symlinks before locating it. The public `agent` command groups coding
frontends below one command; their short PATH launchers retain the upstream
command names. The generated model maps contain only the reviewed agent set,
advertise each preset's managed starting context, and prefer Qwen3.8 27B MTP
Q8 at native medium effort when it is installed. A launcher otherwise selects
the first installed agent-capable preset and refuses a normal session when no
maintained model is available.

`agent-clients/pi/package.json` and its npm lockfile own the exact Pi release
and transitive runtime dependency graph. `src/rocmplete/pi_runtime.py` requires
distribution-provided Node.js and npm, stages `npm ci --ignore-scripts` below
`StorageLayout.application("pi") / "runtime"`, verifies the staged Pi version,
and only then atomically activates the content-addressed installation. A
failed install cannot look current, and normal launches never perform network
installation. System Node.js remains outside the managed tree so distribution
security updates do not require a separate ROCmplete runtime manager.
Runtime installation does not own provider addresses or confinement policy.
Those remain explicit inputs to configuration rendering and launch planning,
so a later remote or non-bubblewrap client path can reuse the reviewed model
profile without pretending that it is a local Linux sandbox.

`src/rocmplete/pi_agent.py` renders the reviewed model set into Pi's
`models.json` schema with the `openai-completions` API. `bin/pi` delegates to
the host launcher, which resolves only the runtime matching the current lock,
mounts that complete tree read-only, and atomically refreshes the file below
`StorageLayout.application("pi") / "sandbox"` and points
`PI_CODING_AGENT_DIR` at the same private state. Pi's ordinary user config is
never modified. The launcher disables startup network checks, telemetry,
and project `.pi` trust while leaving normal `AGENTS.md` context discovery
enabled. It supplies the recommended installed model and its catalog default
thinking level as
command-line defaults before forwarded Pi session arguments, so an explicit
later `--provider`, `--model`, or `--thinking` remains authoritative.

`src/rocmplete/agent_models.py` owns reviewed coding-task sampling metadata for
every maintained llama.cpp agent preset. Pi receives static fields as
`samplingParams` where the policy does not vary with thinking; explicit client
request settings remain higher-precedence caller policy. Qwen3.6 and Qwen3.8
are deliberately different because their official samplers depend on
reasoning mode, and sparse Qwen3.6 has a distinct thinking policy. The catalog
owns validated, reusable thinking and non-thinking policies and each
applicable preset references one. Direct startup and router rendering pass the
resolved data through a dedicated llama.cpp option that remains separate from
Jinja. The patched server resolves thinking and fills only omitted or null
sampling fields. Pi, Maki, and direct Chat Completions therefore share one
mode-aware policy without separate model processes or duplicated client
configuration. Evaluation metadata reads the same catalog policy when
recording the resolved tuple.

Pi recognizes package and configuration commands only when the command is its
first argument. The launcher classifies `install`, `remove`, `uninstall`,
`update`, `list`, `config`, and `auth` before adding session defaults, runs
them online when requested, and points them at the same private state used by
managed sessions. User-installed package resources therefore load on later
launches without exposing Pi's ordinary host state. Informational `--help` and
`--version` pass directly to the managed executable and require neither
private state nor an installed model. Since bare `pi update` means self-update
upstream, the bare, `self`, `pi`, `--self`, and `--all` forms are refused with
the repository-managed update command; package-only update forms use the
private state.

`src/rocmplete/maki_agent.py` publishes the same reviewed model catalog as two
executable dynamic providers below Maki's private XDG configuration. Both use
Maki's native `llama-cpp` provider as their protocol base, so the exact
loopback `/v1` URL comes from the generated provider while Chat Completions,
tool calls, and session reasoning state remain owned by Maki. Each model entry
maps Maki's generic selector onto direct native JSON request fragments. A
generated `init.lua` selects the recommended installed model, adaptive
reasoning, and one concurrent task subagent. Adaptive mode resolves through
the selected model entry, so switching families also switches to that
family's reviewed default. The initial tier file assigns that model to every
subagent tier. An unchanged seed follows a later default change, while any
user-edited tier assignment is preserved. Managed configuration and provider
scripts are refreshed atomically and reject links or multiply linked files.
Maki update, rollback, migration, and informational commands pass through to
the real executable. Other management commands use the private state without
requiring an installed model.
Maki 0.4.8 at commit `a9495e1` added this dynamic-provider `thinking_fields`
contract. Named modes send only the selected fragment: Qwen3.6 uses nested
`chat_template_kwargs.enable_thinking`, Qwen3.8 sends `reasoning_effort`, and
Muse sends `reasoning_strength`. Maki snaps unsupported effort names downward
to a declared level; models marked `requires_thinking` clamp off upward before
the fragment is selected. Explicit numeric budgets remain separate sampler
ceilings and are not decoded into model-native labels. The schema does not
carry per-model sampling parameters; those retain ROCmplete's server-side
mode-aware defaults and normal per-request override precedence.

DwarfStar is a separate provider at its own loopback endpoint, with the one
reviewed `deepseek-v4-flash-0731-q2-imatrix` model advertising the same
131072-token runtime allocation and 16000-token output ceiling as the managed
server. This public model ID follows the exact managed release and bundle
identity. It deliberately does not copy the pinned server's generic
`deepseek-v4-flash` discovery alias, which omits both the 0731 release and the
reviewed Q2 imatrix selection; the server accepts and echoes the exact managed
ID in Chat Completions requests. The clients expose only direct
(`reasoning_effort: none`) and normal thinking (`reasoning_effort: high`). At
this context DwarfStar maps low, medium, and
high to the same normal thinking mode; Think Max needs a substantially larger
context and is not advertised. Pi maps the same behavior to `off` and `high`
while hiding unsupported intermediate levels. Maki maps `/thinking off` and
`/thinking high` directly to the same
`reasoning_effort` values, with adaptive selecting high. A generated provider
does not imply that the DwarfStar server or model is installed or running.

`src/rocmplete/agent_sandbox.py` owns the common client boundary. Both
launchers use bubblewrap by default and refuse to fall back silently when
`bwrap` is unavailable. They unshare user, PID, IPC, UTS, cgroup, and other
available namespaces while deliberately restoring host networking for the
loopback model endpoints. They drop capabilities, start a new session, and use
parent-death cleanup. `/usr`, `/etc`, and the resolved client installation are
read-only. If `/etc/resolv.conf` points to a dynamic regular file below `/run`,
that exact file is rebound read-only after the private `/run` tmpfs is created;
DNS remains usable without exposing another runtime tree. The exact resolved
working directory is the only general persistent writable mount and keeps its
host path. This preserves project identity,
session directories, and absolute paths while exposing no siblings from the
host filesystem. When the
host uses Fedora's `/home -> var/home` link, the same link is recreated in the
otherwise empty sandbox root so login-home and canonical paths keep resolving
to the one mounted project. A minimal `/dev`, private `/tmp` and `/run`, and no
GPU devices are provided. The
host home and its ancestors are refused as working directories;
broader project parents remain an explicit user-selected scope and are printed
before launch.

The child environment starts empty. Terminal and locale values, generated
client settings, a narrow executable path, and sanitized Git author and
committer identity are added explicitly. Tokens, proxy settings, SSH and
desktop sockets, and unrelated `ROCMLETE_*` values are not inherited. The real
home is absent; config, data, state, cache, sessions, logs, and tool output
persist only below the client's `StorageLayout.application(CLIENT) /
"sandbox"`, whose owned directories are forced to mode `0700`. Sandbox state
and the writable working directory must not overlap. Project `AGENTS.md`
discovery remains available from the mounted project.

`src/rocmplete/agent_evaluation.py` builds a benchmark-specific layer on top
of the Pi boundary. Every pinned source commit is exported into a new
repository with one synthetic commit and no remote, while hidden graders and
protected inputs remain outside the mounted worktree. Fixed repository-owned
toolchain adapters select baseline, grading, and build commands. Go tasks add a
prepared module cache as an explicit read-only mount and receive an offline
proxy policy plus a temporary build cache. The standard-library Python task
receives no dependency environment. Task definitions cannot supply arbitrary
commands. The Pi process still needs shared host
networking for its loopback model endpoint, so structured tool transcripts are
audited for ordinary network commands and this boundary is not claimed as
adversarial network containment. Grading happens outside the client sandbox
against a copied worktree with dependency pins restored.

A client executable inside Linuxbrew causes its complete
`/home/linuxbrew/.linuxbrew` prefix to be mounted read-only. Other executables
outside `/usr` are mounted as the exact resolved file. Pi instead supplies its
complete content-addressed runtime as an explicit read-only mount while
executing distribution Node.js from `/usr`. `--no-sandbox` is an
explicit troubleshooting escape hatch and restores ordinary host filesystem
access while retaining generated provider settings and private client state.
Neither mode starts or supervises a model server.

Maki prefers a legacy `~/.maki` directory over XDG paths. Sandboxed runs have
an empty synthetic home and cannot see one. An unsandboxed run refuses to
start while the legacy directory exists and directs the user to upstream
`maki migrate xdg`; silently using ordinary host state would violate the
private-state contract.

Agent clients reserve the advertised per-turn output limit when deciding when
to compact a session. ROCmplete caps that allowance at 16384 tokens so a 256K
agent preset retains roughly 240K tokens for prompts, history, tools, and
retained context. Compaction remains lossy and the sandbox remains the hard
filesystem boundary when a local model misreads generated context.

This protocol choice is deliberate. llama.cpp maps ordinary function tools
from Chat Completions, which covers both clients' host-side tools. A newly
maintained preset still needs a complete tool-call and tool-result acceptance
test in each client before unattended use.

Most benchmark resources are used unchanged. A small allowlisted renderer may
derive a closely related task-specific prompt from the same pinned upstream
graph; both source and rendered SHA-256 values are then cataloged. Unknown
renderers or structural drift are fatal.

A benchmark suite orchestrates the same per-bundle runner. Its signature
covers the ordered selection, pinned workflow and benchmark hashes, image
reference, GPU profile, render node, seeds, run count, cache mode, and runtime
policies. Resume is rejected when a signed input changes. State is written
after every entry transition, and an entry is skipped only while its
referenced completed result still exists.

Workflow reproducibility has two hashes:

1. `source_sha256` proves the resource extracted from the pinned workflow
   template package.
2. `rendered_sha256` proves the deterministic ROCmplete transformation.

Installed workflows contain source, modification, license, and hash
provenance under `extra.rocmplete`. Existing differing workflows are treated
as user modifications and are not replaced without `--force`.

ROCmplete owns two workflow subdirectories. `curated/` contains deterministic
renderings of pinned official templates. `imported/` contains exact downloaded
workflow artifacts that may have unmet external dependencies. Other workflow
paths are user-owned and are not content installation targets.

## Fail-closed behavior to preserve

Many apparent inconveniences are deliberate:

- A changed upstream patch target breaks the build.
- A changed official workflow shape breaks its renderer.
- An unexpected destination file blocks installation.
- A catalog hash mismatch blocks download or benchmark use.
- Managed content without a current verification receipt blocks runtime use;
  `content install` hashes existing bytes and refreshes the receipt.
- An unsupported GPU architecture blocks startup.
- Multiple render nodes require an explicit complete selection.
- Missing or ambiguous license information requires explicit acknowledgment.

Fix the underlying pin, transform, metadata, or policy. Do not turn these into
warnings merely to make an upgrade pass.
