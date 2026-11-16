# Architecture and invariants

This reference describes durable ownership and trust boundaries. Source and
tests own current pins, image tags, schemas and defaults. Use
[upgrading](upgrading.md) for dependency and patch contracts,
[content catalog maintenance](content-catalog.md) for content policy, and
[testing](testing-and-release.md) for validation.

## Mental model

Paracetamol has three layers:

```text
host control plane
  ├── builds locally pinned images
  ├── validates devices and constructs constrained Podman commands
  ├── installs verified content
  └── serves the foreground inference gateway

application images
  ├── shared minimal ROCm runtime
  ├── ROCm/PyTorch prerequisite only where needed
  ├── separate pinned application sources and dependencies
  └── entrypoints enforcing paths, profiles and application policy

persistent storage
  ├── apps/<application>/       writable application state
  ├── content/                  verified managed files
  └── staging/                  resumable installer scratch
```

The host control plane is a Linux Go binary. Building it normally requires
Go, GNU Make, a C compiler and glibc development headers, not Podman or GPU
access. It does not import ROCm, PyTorch or application dependencies.
Managed execution needs rootless Podman and, for GPU workloads, access to
the selected devices. Python belongs to containers and focused container-policy
or archived evaluation fixtures, not host command dispatch.

## Command flow

The checkout launcher asks Make for the current platform binary below
`build/<goos>-<goarch>/bin/` and executes it. Make tracks Go source, module
files, the Makefile and embedded gateway assets. Parent-directory timestamps
also invalidate builds after input files or entire source subtrees are removed.
Make target names are checkout-relative so a checkout path containing spaces
does not split the dependency graph. Go state and the launcher result remain
absolute paths below that checkout.
Normal builds use CGO and glibc so networking follows host NSS policy, including
installed mDNS modules.
They fail when CGO is disabled. `make static` is the explicit pure-Go
alternative. All Go build, cache, module, temporary and telemetry state stays
below the anchored ignored `build/` tree.

`internal/project/` discovers the checkout containing the catalog,
Containerfile and workflow resources. A copied bare binary is not an
installation contract. `bin/paracetamol` is the maintained PATH-friendly
delegate to the checkout launcher.

`internal/identity/` and the Makefile own product and namespace identity.
A custom product build selects another command, environment prefix, image and
state namespace; it does not migrate data. Entrypoint environment keys and
result schemas are separate protocols requiring deliberate source-level
changes during a complete rename.

`internal/cli/` owns the command tree, leaf parsing, validation and
orchestration. Flags may occur before or after positionals; `--` is the
upstream boundary. Only ComfyUI currently accepts passthrough arguments,
under its reviewed allow/deny policy.

`internal/ui/` owns semantic output, context-cancellable prompts, TTY
progress and bounded redirected progress. `NO_COLOR`, `TERM=dumb` and
redirected streams preserve useful plain text. Human-oriented stdout is not
an incidental machine-readable API.

Read-only inspection and dry runs resolve absent data paths without creating
them. Mutating commands complete argument and selection validation plus
required approvals before preparing partitions. Host and image availability
checks still run at the execution boundary. Dry runs use real planning and
validation, but start no workload, download no content, delete nothing and do
not create persistent data.

The [documentation index](README.md#source-ownership) maps authoritative
packages and files. Keep policy at its owning boundary instead of adding
competing checks at callers.

## Image layout

Images are built locally from immutable base, source and dependency pins.
Paracetamol does not publish or depend on prebuilt application images.
Current tags and prerequisites come from `internal/application/registry.go`;
external pins come from `Containerfile` and application requirements or
constraints.

```text
pinned Ubuntu + modular ROCm
  └── rocm-runtime
        ├── rocm-base + PyTorch
        │     └── comfyui
        └── llama-cpp native builder and final runtime

pinned download dependencies
  └── content-tools
```

The minimal runtime owns shared ROCm and supported architecture payloads, not
PyTorch or a compiler toolchain. The higher base adds PyTorch for applications
that need it. Native llama.cpp starts its builder and final stage from the
lower runtime; development tools stay in the builder. Its final payload
contains the reviewed HIP/Vulkan CLI, server and benchmark binaries, not
unrelated PyTorch, RPC or remote UI assets.

Every managed GPU application follows the single project `ROCM_VERSION`.
A native development stage cannot select another release. Upgrade the shared
runtime, PyTorch base and native toolchain together, then validate the tuple
on applicable hardware.

`internal/buildplan/` validates dependency closure before execution and
builds prerequisites first. Managed base references use local tags with
`--pull=never`. An existing tag does not prove its inputs match the checkout.
Selected application builds do not build the independent content-tools image;
content installation requires that image and builds nothing itself.

Normal builds reuse layers and pip downloads. `--no-layer-cache` bypasses
only the selected target's layers. `--no-cache` bypasses both cache classes
for the required closure. An aggregate cold build constructs each shared
prerequisite once before its dependents.

The host pip cache is below
`${XDG_CACHE_HOME:-$HOME/.cache}/paracetamol/build/pip`, separate from
application data. It is mounted only during builds and never copied into
images. Direct Containerfile builds default to disabling pip cache.
Confirmed `cleanup build-cache` removes only that owned directory.

`internal/imagearchive/` owns offline image transfer, not persistent data.
Export validates image identities before create-once publication. Import
inspects bounded manifest/config members without extracting the archive,
rejects unmanaged or duplicate tags, incomplete prerequisites, unsupported
platforms and conflicting current IDs before Podman loads layers. Verify exact
tags/config IDs again after loading. Archive success is not GPU acceptance.
Image inspection accepts full bare or `sha256:`-prefixed SHA-256 config IDs and
normalizes them before comparison with archive digests. Malformed or genuinely
different IDs remain errors at export, import preflight and post-load checks.

The `.containerignore` allowlist must include new build inputs. Source is
fetched at full commits and checked against `HEAD`; licenses remain after
Git metadata is removed.

### Upstream base and broken-out patches

Keep an immutable upstream revision plus focused, attributed local changes.
llama.cpp patches live under `applications/llama-cpp/` in Containerfile
application order; other applications may use strict build-time transforms.
Each change needs a purpose, provenance, dependencies, protected behavior,
validation and a removal condition.

This is deliberate, much like a broken-out kernel patchset. It lets the
integration follow upstream without importing a fork's unrelated policies.
Regression work can compare pristine upstream, the minimum workload support
and the complete stack, then bisect the base and reduce local changes
separately. The Flash-Next repeated-answer investigation used those controls
to narrow an interaction without claiming that upstream arithmetic was wrong.
A fork can provide the same controls. Here their ownership and review stay
with the integration checkout rather than another project's rebase schedule.

Patch application and transformations fail closed on unexpected source.
Clean application does not prove inference correctness or performance.
Diagnostic subsets must be dependency-valid and carry exact source/image
identities; they are not additional supported configurations.

[Upgrading](upgrading.md#current-patch-contracts) owns current patch contracts.
[llama.cpp maintenance](llama-cpp-maintenance.md) routes the accepted baseline,
regression checks and evidence. Dated experiments support those contracts
without becoming a second current policy ledger.

## Runtime isolation

Managed runtimes are rootless, read-only and capability-free, with
`no-new-privileges`, explicit tmpfs mounts, PID/shared-memory limits and a
zero core-dump limit. Persistent writes honor the host umask and keep-id
ownership. Each application receives only its own writable `/data`;
required managed partitions are separately read-only below `/content`.

GPU runs expose `/dev/kfd` and only the selected render nodes. CPU exposes
neither. One detected node can be selected automatically; zero or multiple
nodes require an explicit valid selection. A profile does not choose a
physical device. Multiple nodes require an application's `MultiGPU`
capability and one supported architecture.

Canonical profiles and host validation live in `internal/platform/`.
Container Python validates what PyTorch sees; native llama.cpp independently
checks `rocminfo` and maps only selected devices to explicit backend names.
Forced profiles are assertions, not compatibility overrides. Host,
Containerfile targets and container mappings must agree.

Web applications bind to a wildcard only inside a private rootless network
namespace. Podman publishes exactly one TCP port, on loopback by default.
Non-loopback publication remains visibly warned about. IPv4 wildcard
publication uses IPv4-only pasta; IPv6 publication uses an internal IPv6 bind.
Direct applications remain unauthenticated unless a reviewed mode provides
otherwise. `--unconfined` relaxes only seccomp, not mounts or capabilities.

External GGUFs use exact read-only file mounts, including validated shards.
Never mount or recursively SELinux-relabel their parent. Managed presets use
the receipt-backed model partition.

Enforcing SELinux hosts require `container_use_devices` for GPU runs;
the launcher reports a remedy instead of disabling labels. Shared read-only
content and resumable downloader storage use shared `z` labels; private
application state uses `Z`. Label content before hashing so runtime access
does not invalidate receipts through ctime changes. Doctor reports device and
memory-policy remediation without host mutation; system-wide security
relaxations remain labelled as such. AppArmor's unprivileged-user-namespace
restriction is informational: its sysctl value alone does not establish a
managed-workload failure or justify a system-wide opt-out. Investigate actual
AppArmor denials and the applicable host policy before choosing remediation.

Copied image helpers need explicit readable modes even under a restrictive
developer umask. Tmpfs scratch must be writable to keep-id users. Persistent
data continues to honor the invoking user's mask.

### Exact lifecycle ownership

Containers carry managed ownership, role and application labels. Direct
application runs and the `benchmark` command family attach an opaque
launch-instance label. Direct runs, shells and managed benchmark workloads
prepare their writable data and create a stopped container under the data-root
mutation reservation. They verify its full ID and launch identity before
releasing that reservation, then start that exact ID. Destructive data cleanup
therefore sees either a startup reservation or a registered container. The
reservation does not span inference or an interactive shell, so ordinary
content installation remains possible while an application runs.

Attached runs retain terminal input/signals and child exit status. Failure
cleanup requires that exact launch instance and a captured full container ID, so a
name-conflict loser cannot remove another invocation's successful container.
Detached runs retain ordinary Podman lifecycle.

Benchmark command trials use a fresh launch instance for each backend start,
including repeated speculative trials at the same depth. The nonce belongs
to lifecycle control, not workload fingerprints or resume identities.

Stop allows a brief graceful interval. Confirmed cleanup uses forced exact
removal and verifies absence after an error. Discovery uses labels, never a
canonical application name or legacy transient name alone. Direct-application
stop and confirmed container cleanup validate their complete plans before
removal, then revalidate ownership and act on captured immutable IDs. A renamed
original can be removed, but a new occupant of its former name cannot redirect
cleanup. Downloader cleanup runs after every outcome, including cancellation,
while failed staging remains resumable. It checks the downloader's fresh launch
nonce before capturing and revalidating a full ID for removal. A name-conflict
winner is not evidence that the failed launch created a container. Never add
general Podman or system pruning.

Directory cleanup captures open target and parent handles before approval,
then revalidates every planned identity before the first deletion. Recursive
removal uses the captured target handle, not its absolute pathname. The final
empty directory removal uses the captured parent after another identity check
and a directory-only syscall that cannot unlink a replacement file or symlink.
Changes detected during removal can leave a partially completed cleanup, but
must not redirect recursive deletion into a replacement tree.

## Persistent data and publication

The default root is `${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol`.
Resolution is `--data-dir` → `PARACETAMOL_DATA_DIR` → `[storage].data_dir`
→ the XDG default. Global `-c`/`--config` selects one required file;
`--no-config` skips the optional XDG configuration. Selectors stop at `--`.

The typed configuration schema is closed and optional. Unknown keys,
duplicates and unsupported TOML forms fail. Flags override corresponding
environment variables, then configuration, then defaults; lists replace
rather than merge. `config init` creates once without replacing a file.
[Gateway configuration](gateway.md#configuration) and `config.example.toml`
describe supported fields and forms.

```text
apps/comfyui/       workflows, input/output, custom nodes and child Python env,
                    home, caches and benchmarks
apps/llama-cpp/     home, caches, generated router INI and benchmarks
apps/gateway/      private daily statistics
apps/acceptance/   checkpointed results and review artifacts
content/comfyui/   managed model files
content/llama-cpp/ managed GGUF files
content/.paracetamol/ verification receipts
staging/           per-consumer downloads and downloader-only scratch
```

Application inputs, outputs, workflows, custom models, state and benchmark
results can be irreplaceable. Managed bytes and generated caches are
reconstructible but may be costly or unavailable to fetch again. Retirement
does not delete old data, images, client state or credentials. Follow
[operations](guides/operations.md#persistent-data) before backup or cleanup.

Artifact destinations must be regular files, not links or unexpected types.
Installation verifies size and SHA-256 before atomic promotion. Existing files
without current receipts are hashed by the installer, not silently trusted.
Runtime readiness checks receipt filesystem identity without hashing large
models. Read-only `content status --verify` audits bytes without refreshing
receipts.

Failed/interrupted staging remains resumable. Complete bytes with a bad hash
are quarantined with an `.invalid-*` suffix. Local mirror traversal never follows
symlinks. Mirror and data roots cannot overlap, and candidates are reused only
after size/hash verification. Explicit move mode can remove files from the
old library. Interruption does not restore them.

Mirror selection retains the verified file identity through transfer. A
changed candidate is refused rather than treated as the earlier hash match.
Cross-filesystem moves validate the copied size and hash before publication,
then recheck the original identity before unlinking the source. Failed copies
retain scratch and leave the source in place.

Installer-owned data mutation, runtime startup admission and destructive data,
download and runtime-cache cleanup share a nonblocking directory-inode lock
owned by `internal/storage/`.
`internal/content/` retains one reservation from artifact installation through
curated workflow publication and the final readiness report. Standalone workflow
installation uses the same mutation boundary without requiring a downloader.
Cleanup holds its reservation across planning, confirmation and removal,
including when an offline install has no downloader container. This protects
moved mirror files while they are staged or verified. Directory aliases share
the same lock. Process exit releases it without a persistent lock file. The
gateway's lifecycle reservation is separate and remains required for cleanup.

This exclusion does not roll back completed work. If workflow publication fails
after model verification, verified models and any completed workflows remain in
place for inspection and retry. Failure must not print the final ready message.

`internal/atomicfile/` has two publication policies. Final results, archives
and immutable resources are create-once. Exact owned checkpoints, reports,
router files or explicitly forced workflows may replace only a singly linked
regular file. Both policies sync data and the parent directory; links,
directories and multiply linked destinations fail closed.

## Content trust chain

The built-in catalog owns immutable identities, licenses, agreements,
bundles, serving presets, sampling policies and workflow/benchmark provenance.
Direct artifacts and bounded archive collections produce independently
verified files. Unsafe definitions and inconsistent relationships fail loading.
Missing license evidence stays `NOASSERTION`; a base model's license is not
automatically a repack's license.

Remote import resolves one allowlisted provider file into a schema-valid
ignored local pack and uses the normal content installer. It is not an
arbitrary-URL downloader. Tokens are scoped to initial provider requests,
not forwarded across redirects. Local packs cannot add serving presets,
executable code or arbitrary workflow renderers.

Curated workflows retain pinned upstream provenance and reviewed JSON under
`catalog/workflows/`. Bundle installation preflights every selected workflow
before approval and model installation, and rechecks before mutation and again
when publishing workflows. Dry runs use that same validation without creating
data. Installation checks the rendered hash and copies exact
bytes; it does not extract or transform an image template. Source and rendered
hashes identify different provenance stages. The
[authoring procedure](content-catalog.md#add-or-update-a-curated-workflow)
must preserve transformation evidence. Differing installed workflows remain
user modifications unless replacement is forced. Imported workflow artifacts
occupy a separate directory and do not imply runnable graphs or installed
dependencies.

## Gateway and client boundary

The foreground gateway freezes receipt-verified model capabilities at startup.
Without explicit application selection it discovers built applications with
compatible verified models; an explicit list is strict. Content or policy
changes require restart to change the snapshot. Inventory is not residency.

No backend is eagerly loaded. One allocation is resident, using a constrained
llama.cpp router on an ephemeral loopback port. Public requests use gateway
listeners; managed local clients retain IPv4 loopback. Complete listener
validation and binding are all-or-nothing. Before backend checks, listener
binding or persistent preparation, startup binds a Linux abstract Unix control
socket and retains it through backend shutdown. Its bounded name derives from
the product namespace, current UID and cleaned absolute HOME, not the length
of a filesystem path. Different ports, data roots and `XDG_RUNTIME_DIR` values
do not create independent gateways within the same account HOME. The kernel
owns the reservation, so data cleanup cannot unlink it and process exit leaves
no stale socket or lock file. Both ends verify the peer UID before exchanging
control messages. This is not a cross-HOME or hostile same-user boundary.
Availability checks never remove containers; existing backend, direct,
benchmark and foreign collisions fail.

Direct llama.cpp launches, mutating managed benchmarks, hardware acceptance
and cleanup affecting gateway resources acquire the same reservation across
their checks and mutations. Benchmark and acceptance controllers retain it
across inter-trial gaps, host checkpoint/report publication and final cleanup,
when no workload container may exist. These maintenance runs are exclusive
with the gateway and with one another within the same HOME. Ordinary direct
ComfyUI runs and shells do not take this lifetime reservation and can coexist
with the gateway.

An idle gateway blocks exclusive operations even without a container.
Direct router publication also waits for image and container preflight, so a
rejected launch cannot change a running
service's generated configuration. Detached launches release the reservation
after startup, with subsequent ownership checks relying on the container.
Read-only dry runs do not reserve resources.

The serving process generates one opaque instance nonce and passes it to its
container lifecycle and HTTP server. Backend containers carry that nonce in
their gateway-instance label; status and request reports expose the same
process identity. Acceptance binds the inspected container to that identity
before sending requests and rechecks it at completion. Matching model
inventories or container names alone do not establish which image answered.

Each backend start also has a fresh launch nonce. Its verified full container
ID governs log following, port lookup, readiness and removal, so renaming the
original or reusing its name cannot redirect lifecycle operations. If a failed
Podman client returns no usable ID, recovery discovers full IDs filtered by
that exact instance and launch, then revalidates complete ownership. Failed
cleanup retains its target or pending launch identity for shutdown retry.
Gateway lifecycle cleanup requires the current process's label. A canonical
nonce does not establish that its owner has exited, so startup and allocation
never reclaim another process's backend. Operators must verify orphan ownership
and remove that exact container explicitly. This is local correlation, not
authentication against a malicious process running as the same user.

The scheduler, proxy, cancellation and failed-cleanup policy belong to
`internal/gateway/`. Backend slots, child eviction, KV allocation and
model-load policy are separate concepts. Catalog review selects tuples; the
native entrypoint validates its supported combinations. The
[gateway guide](gateway.md) owns current operational behavior.

Optional Bearer authentication applies consistently to API routes, including
loopback. Anonymous embedded assets contain no private inventory. Authentication
does not encrypt HTTP or protect private backend ports from untrusted local
processes. Public authorization is stripped before proxying. A same-user Unix
control socket requests graceful shutdown without a public shutdown API or
discovered-PID signalling.

Process-local observations retain bounded allowlisted metadata, not prompts,
answers, tools or credentials. Durable SQLite statistics retain daily
aggregates, not requests/sessions; accounting failure must not block inference.
The [gateway guide](gateway.md#durable-usage-statistics) owns retention,
privacy and failure semantics.

`internal/clientconfig/` renders portable client examples from frozen
capabilities without credentials, private paths, fixed session IDs or sampler
overrides. Native clients own installation, execution, permissions, credentials
and state. The controller is not a client sandbox.

## Benchmarks and acceptance

Runners preserve exact image/content/runtime identity, deterministic inputs,
cache policy and cancellation-independent cleanup. ComfyUI uses pinned API
graphs and a fresh process for its first request. Native throughput, backend
comparisons and speculative depth screens measure distinct things; none is
interchangeable with quality evidence.

Detached ComfyUI and speculative benchmark launches retain the full container
ID and ownership labels captured at creation. Readiness and ComfyUI history
polling use bounded metadata probes against that ID, not its reusable name.
Missing, exited or identity-mismatched backends fail the run. Live workloads
remain cancellable without an overall loading or inference deadline. Cleanup
revalidates the same captured identity.

Acceptance and speculative-benchmark checkpoint fingerprints bind run
definitions, inputs, images, runtime policy and host source identity. Clean
source uses the Git revision. Tracked or untracked changes add a content digest.
Tracked changes use Git's binary diff with external diff helpers, text
conversion and color disabled, so those renderers cannot hide changed bytes.
ComfyUI suite signatures bind bundle, artifact and workflow identities plus
image and runtime configuration, but not host source identity. Resume rejects
incompatible checkpoints and validates paths/state before trusting progress.
These checks establish compatibility, not authenticity of outcomes.

Acceptance and ComfyUI suites checkpoint an attempt number before starting each
workload. That identity selects new child result and media paths on every
retry. Child results remain create-only, including when a crash leaves a
published result without the parent's completion checkpoint. Resume retries
that inconclusive attempt rather than treating the orphan as a pass or
overwriting its evidence. Parent JSON retains earlier attempt status, errors
and recorded evidence. A failed cleanup remains a failure and never authorizes
automatic reclamation of another workload's container.

Persisted acceptance and ComfyUI suite diagnostics escape controls and line
breaks without dropping error causes. This keeps combined workload, evidence
and cleanup failures compatible with strict checkpoint validation. Returned
errors retain their original text and identity. Previously invalid checkpoints
are not repaired on read.

[Testing](testing-and-release.md) defines checks and
[hardware acceptance](hardware-acceptance.md) records applicable evidence.
A build, CPU startup, allocation or tiny retrieval does not establish GPU
inference or model-quality acceptance. Keep unavailable hardware and external
services explicit rather than converting them to successes.
