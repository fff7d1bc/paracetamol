# Architecture and invariants

## Mental model

Paracetamol has three layers:

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

The host control plane is a Linux Go binary. It does not import ROCm, PyTorch,
or application dependencies. Building it normally needs Go 1.26+, GNU Make, a
C compiler, glibc development headers, rootless Podman, and access to the
selected device files. Platform-qualified build directories separate local
artifacts and caches. They do not define a portable non-Linux command surface.
Python remains an implementation detail of Python application containers,
their focused policy tests, and frozen evaluation fixtures. No host command
dispatches through Python.

Data-path resolution has two explicit modes. Status, inspection, and dry-run
commands resolve an absent path without creating it. Real installs, shells,
and application execution prepare only the required partition. Each runtime
receives its application directory as writable `/data`; managed content is
mounted separately and read-only below `/content`.

## Command flow

`paracetamol` is a small POSIX shell bootstrap. It asks the `Makefile` for the
current platform binary below `build/<goos>-<goarch>/bin/` and executes it.
Make rebuilds when its Go module, source, or Makefile inputs are newer. Normal
builds use the installed toolchain's native CGO support and link glibc. This
lets Go network clients follow the host NSS policy, including mDNS modules.
The build fails instead of silently producing a pure-Go binary when CGO is
disabled. `make static` is the explicit exception. It builds a stripped
pure-Go artifact that cannot consult NSS-only name sources. All targets keep
compiler caches, module state, temporary files, telemetry state, and binaries
below the anchored ignored `build/` tree. `internal/project/` owns
repository-root discovery for build context, catalog, and immutable workflow
resources. Consequently a copied bare binary without its source checkout is
not a supported installation.

`internal/identity/` and the `Makefile` own the product identity. `PRODUCT_ID`
derives the command name, host-configuration environment prefix, persistent
namespace, image namespace, container prefix, and ownership labels;
`DISPLAY_NAME` is presentation only. Container-entrypoint environment keys and
result schema identifiers are fixed, versioned internal protocols rather than
build-time aliases. A complete source-level product rename must update those
protocols deliberately; a custom `PRODUCT_ID` build does not. Durable
compatibility with another state namespace remains an explicit migration
decision rather than a partial string replacement.

The Go packages under `internal/` are the host control plane. `internal/cli/`
owns a declarative public command tree, explicit leaf parsers, and
orchestration. `internal/application/` owns the closed application registry,
capabilities, guide actions, and prerequisite build DAG. Filesystem,
subprocess, HTTP, device, catalog, and runtime boundaries remain in focused
packages so tests can replace them without invoking GPU inference.

The command parser accepts launcher flags before or after positionals while
preserving `--` as an explicit upstream boundary. A mode only defines flags it
can honor: for example, llama.cpp and DwarfStar CLI mode do not accept server
publication or detach controls. Only ComfyUI currently accepts upstream
arguments after `--`.

`internal/ui/` owns semantic terminal presentation. Interactive output uses
color to distinguish headings, commands, states, warnings, and success while
`NO_COLOR`, `TERM=dumb`, and redirected streams retain stable plain text.
Leaf help includes its option descriptions and copyable examples. Interactive
questions are separated from the preceding plan and wait on both input and the
application context, so Ctrl-C exits promptly instead of leaving a blocked
read behind. Exact SHA-256 progress and approximate downloader progress rewrite
one TTY line; redirected logs receive bounded line-delimited milestones.

`internal/hostdoctor/` owns read-only host inspection and pure remediation
planning for kernel, device-access, security-policy, and RDNA 3.5 shared-memory
diagnostics. `internal/modelinventory/` joins catalog presets, exact artifacts,
verification receipts, and explicitly scanned local GGUF files without
creating the data root or treating an unverified file as runnable content.

The Go migration preserved persistent application state, managed content paths
and receipts, staging, image references, and Podman labels within its original
identity. The later pre-release Paracetamol identity cutover deliberately
selected a new default state and ownership namespace and does not silently
adopt differently named state. Benchmark and acceptance checkpoint schemas are
a typed controller boundary; earlier result files remain inspectable
historical evidence but are not accepted for resume.

Controller-owned output uses two explicit publication policies from
`internal/atomicfile/`. Final results, immutable derived resources, and
archives are create-once and never replace another writer. Generated router
configuration, a known checkpoint, and its derived report may
replace only a singly linked regular file at the exact owned path. Both paths
sync file data and the containing directory; links, directories, and multiply
linked files fail closed.

The typed controller schemas introduced at that boundary are hardware
acceptance v2, ComfyUI benchmark/result-suite v2, llama.cpp benchmark/backend
comparison v2 and speculative-depth sweep v3. The coding-agent evaluation v5
schema is archived with its retired runner.
Active checkpoints use unknown-field rejection and validate their bound
definitions and managed paths before trusting progress. Older files remain
readable records, not resumable state.

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
--profile → PARACETAMOL_PROFILE → auto
```

With one render node, the host selects it. With zero or multiple nodes, a GPU
run fails until the caller supplies an exact set by repeating `--render-node`.
Applications declare whether one workload may receive more than one node.
The profile does not select a physical device. A multi-GPU workload must
resolve to one supported architecture.

Attached application runs keep Podman in the terminal process group for normal
interactive input and signal delivery. A nonzero attached container status
becomes a controlled error that retains the child status. If an attached run
returns before Podman's `--rm` cleanup completes, the launcher removes that one
exact named container through a context-independent typed Podman operation.
Detached runs deliberately retain the ordinary Podman lifecycle.

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

Every Paracetamol-created runtime container carries Podman metadata labels for
ownership, role, and application where applicable. Scoped cleanup discovers
these labels instead of assuming that all owned containers are the
long-running application names. A small exact-name set covers known transient
containers, while generated downloader names retain their separately
constrained prefix.
Explicit application stop allows a brief graceful interval before forced
removal. Confirmed cleanup uses a zero-second forced removal and verifies
absence when Podman reports an error. Runtime, benchmark, and downloader
cleanup each target one exact managed container rather than using a general
Podman prune.

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
workflows are immutable, hash-verified resources below `catalog/workflows/`;
their catalog metadata retains the exact upstream package provenance. Managed
ComfyUI benchmarks also
default to the ComfyUI application image.

Downloader containers use unique `paracetamol-download-*` names and ownership
labels. After every downloader process outcome—including cancellation—the
launcher issues an exact, context-independent `podman rm --force --ignore`
before returning. A separately confirmed `cleanup containers` also discovers
any interrupted downloader by its ownership labels. On enforcing SELinux
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

`internal/remoteimport/` owns provider URL parsing, metadata requests,
file/type inference, stable generated identifiers, and atomic pack writes.
It does not own another downloader. Tokens are attached only to the initial
provider request and are not forwarded across redirects. Generated imports
remain `NOASSERTION` even when provider metadata names an upstream license.

`internal/recipes/` owns the small application-first recipes shown by
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
time for that exact token ratio. A controlled failure from one backend is
recorded before the other runs; interruption still escapes immediately after
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

`internal/benchmark/` owns prompt generation,
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
`localhost/paracetamol:content-ubuntu26.04-huggingface1.27-r1`; `content install`
uses it without building anything itself. This keeps downloads independent of
ComfyUI and makes a llama.cpp-only initial setup complete.

`Containerfile` defines a minimal `rocm-runtime` target, which the launcher
builds and tags as the managed local prerequisite
`localhost/paracetamol:runtime-ubuntu26.04-rocm10.1-r4`. It owns the pinned
Ubuntu runtime, Python environment, and AMD's modular ROCm core, libraries,
and exact `gfx1150`, `gfx1151`, `gfx1200`, and `gfx1201` device wheels. It
does not contain PyTorch or a compiler toolchain.

The `rocm-base` target starts from `ROCM_RUNTIME_IMAGE`, adds PyTorch,
torchvision, torchaudio, and the common Python-application build tools, and is
tagged as
`localhost/paracetamol:base-ubuntu26.04-rocm10.1-torch2.13-r7`. Each final
PyTorch application target starts from the `ROCM_BASE_IMAGE` build argument:

- `comfyui`

Application stages must not install conflicting frameworks into one
all-purpose image.
Derived builds use `--pull=never` for both managed base references, so a
missing local prerequisite fails rather than pulling an image with the same
name. `internal/buildplan/` resolves and validates the selected build-unit
closure before invoking Podman, then builds prerequisites before dependents.
An existing tag alone
is not treated as proof that build-context inputs still match the checkout. A
fully cold `--no-cache` build deliberately refreshes the complete prerequisite
closure. The public `build pytorch-base` target builds `rocm-runtime` followed by the
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
The application registry's `PyTorchBase` capability keeps the choice of the
higher base explicit in build and image-archive planning; every GPU application still
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
`ds4-bench`, the license, and Paracetamol's constrained entrypoint. Upstream
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
`${XDG_CACHE_HOME:-$HOME/.cache}/paracetamol/build/pip` and is mounted only for
build `RUN` instructions at `/var/cache/paracetamol/pip`. The launcher supplies
the mount and pip build arguments together; direct Containerfile builds
default to `PIP_NO_CACHE_DIR=true`. The cache is therefore neither copied into
an image nor mixed with persistent application data. `cleanup build-cache`
removes only Paracetamol's build-cache directory and never prunes Podman state.
Like every cleanup scope, it prints a validated non-empty plan and crosses the
shared confirmation boundary before mutation.

`internal/imagearchive/` owns offline build-output transfer. A managed
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
documented paths below `/usr/share/licenses/paracetamol`.

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

`applications/dwarfstar/glm-tool-argument-types.patch` carries upstream PR
1016 at `9db96f0e96928e2245664b83e0498145e86a4c25`. GLM tool syntax lacks
DeepSeek's explicit string flag. The parser uses the request's property types
to preserve valid JSON literals, keeps strings and ambiguous schemas as text,
and retains raw generated tool text for replay. This is type recovery, not
schema validation. The image build runs model-free CPU server tests before
building ROCm binaries. Remove the patch only after a reviewed upstream pin
owns the correction and passes the same typed, streaming, and DeepSeek
regressions. This patch does not add a managed GLM model or gateway policy.

The pinned llama.cpp source receives the fail-closed
`applications/llama-cpp/hip-apu-host-buffer.patch`, taken from upstream PR
25863. It preserves integrated-device detection and pinned host allocation
while preventing direct computation on unsafe `ROCm_Host` buffers. Remove the
patch when a reviewed future pin contains the upstream fix.

The fail-closed `applications/llama-cpp/quantized-kv-flash-attention.patch`
contains the reviewed HIP q8_0/q4_0 tile dequantize-on-load changes from
Nathan Wilson's commit `2a24abc6`. It removes repeated KV dequantization at
long context while leaving f16 paths unchanged. Upstream llama.cpp owns the
Vulkan q8_0 dequantize-on-load path as of commit `dc72703fc`, so Paracetamol
no longer patches that backend. The remaining HIP patch must be requalified
on every applicable Paracetamol architecture whenever llama.cpp or ROCm
moves.

The fail-closed `applications/llama-cpp/hip-strix-halo-tiled-gdn.patch`
adds a tiled F32 gated-delta-net prefill kernel from Piotr Wilkin's commit
`964c6f2f0`. Dispatch requires exactly `gfx1151`, 48 value heads, a 128-element
state dimension, one sequence and 16 through 32768 tokens in the operation.
Other shapes and architectures keep the existing kernel. It changes neither
allocation policy nor KV precision, and does not accelerate ordinary
single-token decode. The patch also adds CPU-reference operator cases for
tile boundaries, permuted layouts, grouped heads and recurrent snapshots.
The [isolated Strix Halo comparison](qwen3.8-tiled-gdn-strix-halo-feasibility.md)
records its scope and removal criteria.

Upstream llama.cpp owns Vulkan KV contiguization through PR 27703. It copies
eligible strided F16 or BF16 KV data into contiguous scratch without changing
the stored precision. Upstream selects this path on AMD except RDNA 4, with
at least 256 query tokens and additional layout and device checks.
Paracetamol no longer carries a dedicated shader or exports
`GGML_VK_FA_KV_CONTIG`. The runtime status report does not claim that a local
Vulkan fix is enabled. GPU execution, rather than the hardware profile alone,
determines whether an operation uses the upstream path.

`applications/llama-cpp/entrypoint.sh` exposes a constrained server/CLI policy
around the resulting binaries. It checks the architecture reported by
`rocminfo`, then maps the exact selected render-node count to explicit
`ROCmN` or `VulkanN` llama.cpp device names. Building two backends into one
binary must not make both available devices part of one workload
accidentally. This native check intentionally does not import or depend on
PyTorch.

Managed llama.cpp presets normally force resident loading on Strix Halo and
Strix Point. The catalog may opt one preset into the closed
`mmap-lazy-token-embedding` policy for one of those profiles. The entrypoint
maps it to mmap, lazy reads, and CPU placement of only
`per_layer_token_embd.weight`; direct launches and generated router sections
use the same mapping. This keeps an exceptional capacity policy visible and
reviewable without exposing arbitrary tensor overrides. It does not alter
benchmark mode, which remains resident for comparable measurements.

The Strix Halo `stream-token-embedding` policy keeps that Vulkan mapping but
uses explicit CPU embedding reads for ROCm. Its ROCm recipe disables inherited
unified-memory allocation and host weight buffers, retains resident loading
for the other tensors, disables automatic fitting and the RAM prompt archive,
and uses a 2048 batch/microbatch. Server concurrency defaults to four request
slots per loaded model, independently of the router's model count. Multiple
slots use a unified KV token pool of the preset's configured size. The
single-slot override preserves the accepted non-unified cache for this ROCm
policy. Do not multiply the context allocation by the slot count or partition
the advertised context into smaller per-slot ceilings. Live-prefix
reuse remains available. The shell helper `model-load-policy.sh` owns this
one tuple for direct arguments and router INI output. F16 KV and Flash Attention
are required by catalog validation. Native `llama-bench` refuses the managed
ROCm preset because its independent resident benchmark path does not implement
this serving policy.

`qwen4exp-direct-reader.patch` provides the opt-in reader without changing
Gemma4. It fails when an explicitly requested reader cannot open the model file.
`process-allocation-policy.patch` adds `--no-unified-memory`. Parsing or
rendering a model preset only records that option. Only actual native process
startup removes `GGML_CUDA_ENABLE_UNIFIED_MEMORY`, before model allocation.
The router parent and other children retain their original environment.

A llama.cpp preset can additionally declare a closed backend compatibility
list. Omission means both built backends are supported. Direct startup selects
the first declared backend only when the caller did not choose one and rejects
an explicit mismatch. Router rendering, gateway inventory construction, and
managed benchmarks filter or reject the same mismatch before starting a
container. Backend compatibility remains separate from hardware profile,
model-load, cache, and speculative-decoding policy.

Optional `backend_profiles` further restricts a backend to named hardware
profiles. The host reads KFD topology for the exact selected render nodes when
an automatic profile needs resolving for inventory. Missing or ambiguous
topology cannot opt into a restricted model. The direct entrypoint and router
renderer also check the restriction against the profile detected inside the
container. This is a safety gate, not a new hardware profile or backend.

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

The common web runtime in `internal/runtime/web.go` applies:

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
only `/dev/kfd` and the selected render nodes. Paracetamol refuses a disabled
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

- `internal/config/config.go` controls accepted CLI values.
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
Paracetamol `--` separator. Its security policy receives
`PARACETAMOL_HOST_LISTEN`, because ComfyUI itself must bind to a wildcard inside
the private container namespace even when Podman publishes only on host
loopback. Manager installation remains denied for a non-loopback host
publication.

The entrypoint creates a persistent child virtual environment below
`/data/custom-node-python`. A `.pth` file exposes the immutable image
site-packages as its base, while standard Manager and node `pip` subprocesses
write only to the child environment. Paracetamol's validated Manager patch uses
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

The model-scoped streaming policy above is the explicit Strix Halo exception.
It does not disable unified-memory allocation globally for other presets.

## Persistent data

Without configuration, the default host directory is:

```text
${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol
```

`[storage].data_dir` in
`${XDG_CONFIG_HOME:-$HOME/.config}/paracetamol/config.toml` provides the durable
host-level override. An explicit `--data-dir` wins over `PARACETAMOL_DATA_DIR`,
which wins over TOML configuration, which wins over the XDG data default.
The global `-c`/`--config` selector may appear before or after a command and
selects one required file; `--no-config` bypasses the optional XDG file. Both
selectors stop at the explicit `--` passthrough boundary. `config init`
publishes one complete default file through the create-once atomic-file policy.
Configuration lookup and dry-run resolution do not create either directory.
The typed TOML surface is intentionally closed, all fields are optional, and
unknown sections or keys fail closed. A schema-specific standard-library
parser accepts the one-line strings, booleans, decimal integers, string arrays,
comments and table headers emitted by `config init`. It does not implement
unrelated TOML features. Command flags win over corresponding environment variables,
which win over configuration, which wins over built-in defaults. Lists replace
lower-precedence lists rather than merging them.

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
  gateway/
    statistics/                  private SQLite daily usage totals
  acceptance/
    results/                         checkpointed JSON and Markdown summaries
content/
  comfyui/models/               direct ComfyUI model files
  llama-cpp/models/             managed GGUF model files
  dwarfstar/models/             managed DeepSeek V4 Flash GGUF
staging/
  comfyui/ llama-cpp/ dwarfstar/
  .cache/ .home/                downloader-only transient state
```

Former `apps/pi/`, `apps/maki/` and `apps/agent-evaluation/` trees may still
exist. They are preserved user data, not active runtime partitions.

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

`catalog/catalog.json` is schema version 29. The loader in
`internal/catalog/` rejects malformed identifiers, unsafe paths,
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
Paracetamol does not implement a routing daemon.

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
the exact UI contract. Qwen3.8 declares native
effort, and Muse declares native strength. llama.cpp forwards recognized
OpenAI-compatible labels through its template capability layer as both
`reasoning_effort` and `reasoning_strength`; each template consumes only its
own variable. Paracetamol does not infer a native label from a client's numeric
reasoning budget. Muse declares no off choice because its model always
reasons; the reviewed template maps clients' unavoidable generic off value to
Muse low without changing other model families.

`internal/textmodel/` derives backend-neutral model capabilities from the
application-specific llama.cpp and DwarfStar preset collections. The catalog
therefore remains the only owner of public model IDs, starting context, output
limits, agent-tool eligibility, and reasoning choices.

`internal/gateway/` owns a frozen receipt-backed model registry, the bounded
FIFO allocation scheduler, the OpenAI-compatible proxy, and exact backend
lifecycle. The foreground host process always retains an IPv4 loopback
listener. A concrete non-loopback publication adds one exact listener on the
same port; an IPv4 wildcard listener already includes loopback. IPv6 listeners
use an explicit `tcp6` socket beside IPv4 loopback so platform dual-stack
policy cannot break managed local clients. A lazily started llama.cpp router
or DwarfStar server remains in its normal constrained container and publishes
only one ephemeral loopback port. With no application selection, the CLI
discovers built applications that contribute at least one compatible
receipt-verified model. Explicit selections replace discovery and retain
strict missing-image and missing-content failures. The foreground lifecycle
follows the active backend's prefixed logs and joins that follower when the
allocation stops. The gateway owns no Podman socket and keeps only one
application allocation resident. It reclaims only an exact stale container
with all gateway ownership labels and refuses direct, benchmark, or foreign
collisions. A fixed process-local ledger retains only allowlisted request
metadata and bounded session/model aggregates; request and response bodies,
tools, authorization, and backend model arguments never enter it. Session
correlation accepts only explicit UUID-shaped headers and never derives an
identifier from conversation content. Exact llama.cpp child residency comes
from a bounded read-only probe of the router's private `/models` endpoint, not
from allocation state or a model-management request. Streaming first-output
latency is observed from generated SSE deltas without retaining their content.
Cache-reuse aggregates keep matched input/cache counts. `internal/hostdoctor/`
owns the on-demand `/proc` and selected-device sysfs resource snapshot. It does
not estimate model memory from unified-memory GPU counters or alter power
policy. See `docs/gateway.md` for
the public surface and current limits.

Durable statistics are separate from the in-memory request ledger. A bounded
background writer in `internal/gateway/` receives only UTC completion dates,
verified model/application IDs and numeric observations. SQLite stores daily
aggregates, not individual requests or sessions. The `modernc.org/sqlite`
driver needs neither a system SQLite installation nor CGO. The private database
uses WAL on a local filesystem and a single-owner lock. Retention defaults to
90 days. Storage failure or queue overflow never blocks inference, and the
statistics surface reports unavailable or incomplete accounting. HTTP drains
before the writer closes, so normal shutdown preserves completed work.

The gateway also owns one private Unix control socket per host user. Its
lock serializes startup and stale-socket recovery. Local `stop gateway` and
`stop all` use that socket to request the same graceful drain as Ctrl-C;
neither sends a signal to a discovered PID nor adds a shutdown route to the
published HTTP API. The control socket is independent of model storage and
the selected gateway port.

Optional gateway Bearer authentication is enforced before every API route on
every listener. Only GET/HEAD for the generic root document and two exact
embedded asset paths bypass it. No inventory or host data is embedded in those
public resources. A private configured server key is loaded once and reduced to a
digest for constant-time comparison. Client key selection is independent of
server key selection. Bounded inventory and status probes share the gateway
identity check and never follow redirects. Public authorization headers are
stripped before backend proxying. Native clients manage their own credentials.
The Maki export references an environment variable and never includes the
server key. A client's own tools may be able to read its credentials.
Authentication changes neither backend loopback publication nor the trusted-local-process boundary, and does not
provide transport encryption.

`internal/gateway/assets/` contains the embedded browser page. Make tracks
these files as build inputs. The protected `/paracetamol/v1/ui` projection
reuses the frozen registry, client preference policy and read-only residency
probe. It does not sample host resources or inspect the request ledger.
The separate protected `/paracetamol/v1/statistics` endpoint reads durable
aggregates without consulting a backend or writing a request-ledger entry.
The browser uses text nodes for inventory and export previews, performs only
explicit reads, and never accepts credentials. It has no third-party assets,
polling, lifecycle controls or browser storage. Browser-origin export URLs
use the existing validated `base_url` override rather than trusting forwarded
headers at the server boundary.

`internal/clientconfig/` renders portable Maki example configuration from
reviewed model capabilities. Gateway exports consume only the frozen registry,
not the full catalog or a fresh disk scan. They carry no secrets, private
paths, fixed session identifiers or sampler overrides. Client preferences
select regular Flash-Next at xhigh without changing the HTTP reasoning
fallback. The native provider uses Maki's TOML reasoning mappings and a small
separate startup-preferences snippet, not a provider plugin or remote runner.

`bin/paracetamol` is a PATH-friendly delegate to the checkout launcher and
resolves symlinks before locating it. It is the only maintained PATH wrapper.
Native Maki uses its ordinary configuration and permissions on macOS or Linux.
The controller does not execute clients, own their state, install runtimes or
provide a client sandbox.

The export prefers regular Flash-Next at xhigh, then dense Qwen3.8 MTP Q8,
then the first available coding-capable model. It records context, output
limits and native reasoning mappings. Adaptive reasoning follows each model's
reviewed preference. It does not rewrite saved tiers or global model policies.
Refreshing is an explicit download and merge, not a startup action.

The catalog owns reusable thinking and non-thinking sampling policies. The
gateway freezes each model's policy, resolves reasoning before scheduling and
fills only omitted or null sampler fields. Explicit values win field by field.
Ambiguous reasoning controls fail before allocation. Templates and native
reasoning remain backend-owned. Direct engine endpoints retain native
defaults, while direct benchmarks send and record the catalog tuple.

DwarfStar is exported only when the gateway advertises a coding-capable preset.
Its 128K model supports off and high, not the larger-context Think Max mode.
A frozen registry proves receipt validity at startup, not backend residency.
A new model still needs a complete function-tool round trip, not only a plain
chat response, before being marked coding-capable.

The former Pi runtime, extensions, client launchers, sandbox and coding-task
runner are retired. Frozen task definitions and hidden fixtures remain in
`evaluations/coding/` as archival evidence. No retirement step deletes or
migrates installed runtimes, sessions, credentials or raw evaluation results.

Most benchmark resources are used unchanged. A small allowlisted renderer may
derive a closely related task-specific prompt from the same pinned upstream
graph; both source and rendered SHA-256 values are then cataloged. Unknown
renderers or structural drift are fatal.

A benchmark suite orchestrates the same per-bundle runner. Its fingerprint
covers the ordered selection, pinned artifact, workflow, and benchmark hashes,
the exact local image reference and ID, GPU profile, render node, seeds, run
count, cache mode, and runtime policies. Resume is rejected when a
fingerprinted input changes. State is written
after every entry transition, and an entry is skipped only while its
referenced completed result still exists.

Workflow reproducibility has two hashes:

1. `source_sha256` proves the resource extracted from the pinned workflow
   template package.
2. `rendered_sha256` proves the deterministic Paracetamol transformation.

Installed workflows contain source, modification, license, and hash
provenance under `extra.paracetamol`. Existing differing workflows are treated
as user modifications and are not replaced without `--force`.

Paracetamol owns two workflow subdirectories. `curated/` contains deterministic
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
