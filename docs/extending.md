# Extending applications, modes, and profiles

Use this guide for executable behavior. For model content and ComfyUI workflow
variants, use [content-catalog.md](content-catalog.md).

## Decide what kind of extension it is

Before adding a command, classify the change:

| Need | Extension point |
| --- | --- |
| Different GPU architecture or CPU behavior | execution profile |
| New isolated web UI/service | application image plus `run` target |
| New foreground generator or converter | application image plus `run` application/mode |
| T2V/I2V or another operation within one tool | application command mode |
| FP8/BF16, base/accelerated model choice | catalog bundle variant |
| Different deterministic Comfy graph | workflow renderer |
| New client configuration example | pure renderer in `internal/clientconfig/` plus a reviewed gateway export |

Do not create top-level application commands or commands such as
`run-new-gpu`. Keep application, profile, and mode orthogonal below `run`.
Keep clients independent of the controller. Configuration exports consume the
frozen gateway inventory and carry no credentials, fixed session IDs or
sampler overrides. Do not add client installers, launchers or state managers.

## Extend human-facing output

Keep output compact, plain-text friendly, and useful when copied into an issue.
Preserve established stream placement and do not make color the only way to
communicate state. Cover new output behavior in the owning package's Go test.

## Add a web application

### 1. Review the upstream project

Before implementation, record:

- canonical repository and exact commit;
- license at that commit;
- Python and system dependencies;
- how model loading can be forced offline and pointed at read-only
  `/content`;
- all paths the application writes;
- listen address and port controls;
- whether it runs on all supported architectures;
- any telemetry, update checks, model downloads, or GPU monitoring to remove.

Prefer an upstream that can run without patching. If policy changes are
needed, make them deterministic and reviewable.

### 2. Add a final image target

Add a target based on the managed local base in `Containerfile`:

```dockerfile
FROM ${ROCM_BASE_IMAGE} AS new-application
```

- fetch the source at a full commit and verify `HEAD`;
- preserve its license in the image;
- pin every direct dependency and preferably constrain the full transitive
  environment;
- apply a fail-closed patch if necessary;
- copy an entrypoint;
- create required `/data` directories;
- add OCI labels, including the source revision and GPU targets;
- set `WORKDIR`, `EXPOSE`, `STOPSIGNAL`, and `ENTRYPOINT`.

Add every copied file to the `.containerignore` allowlist.

Register the application and its build unit in `internal/application/registry.go`.
The build planner follows declared prerequisites and passes managed local base
references with pulling disabled. Do not add a separate remote or application-specific ROCm
base.

Keep the target late-copy pattern: copy frequently changed Paracetamol
entrypoints and patches after expensive dependency layers where possible so
normal edits retain build cache.

#### Standalone native applications

Use the shared PyTorch base for Python applications that depend on PyTorch. A
native application that does not use PyTorch should start its builder and
final image from `ROCM_RUNTIME_IMAGE`, as llama.cpp does, rather than carrying
the higher PyTorch base. In that case:

- reuse the project runtime's pinned OS and native ROCm package tuple;
- leave `PyTorchBase` false;
- pass `ROCM_RUNTIME_IMAGE` as an exact local prerequisite with pulling
  disabled;
- build upstream source at a full immutable commit for every supported GPU
  target;
- prevent unpinned build-time asset downloads;
- retain upstream licenses and label the native ROCm version;
- select only the supported architecture payloads, or prune unrelated payloads
  only after verifying their naming and lookup behavior;
- implement a container-side architecture check that does not assume PyTorch;
- test that image export includes the shared ROCm runtime and excludes the
  unrelated PyTorch base.

This is not permission to pull a prebuilt application image. The final image
still comes from locally built, pinned base, source, and dependency inputs.

### 3. Implement the entrypoint contract

A managed web entrypoint receives the common profile, address, port, and
kernel inputs:

```text
PARACETAMOL_PROFILE
PARACETAMOL_LISTEN
PARACETAMOL_HOST_LISTEN
PARACETAMOL_PORT
PARACETAMOL_KERNEL_POLICY
```

Pass `PARACETAMOL_MEMORY_POLICY` only to an application whose entrypoint
implements a selectable memory behavior. A shared parser or runtime helper is
not enough to make the policy real.

For a PyTorch application, use `/opt/paracetamol/container_profile.py` to
resolve and enforce profiles; native applications need an independent detector
as described above. Create writable paths before importing PyTorch. Force model libraries offline
where supported, and route state, inputs, outputs, caches, and custom content
below `/data`.

A GPU-only batch application still consumes `PARACETAMOL_PROFILE` and validates
the architecture inside its container before the workload. Host-side parsing
cannot replace that check.

`PARACETAMOL_LISTEN` is the internal container bind address; managed web runs use
`0.0.0.0` for IPv4 host publications and `::` for IPv6 host publications
inside a private namespace. `PARACETAMOL_HOST_LISTEN` records the exact host IP
on which Podman publishes the one application port. Print a short startup
banner containing the resolved profile, selected device/architecture, relevant
runtime versions, data path, container bind and host publication. Never put secrets
in it.

### 4. Register the application on the host

Add one `application.Spec` to the closed registry in
`internal/application/registry.go`. Declare its image, container, build unit,
optional web port, supported shell/log capabilities, and lifecycle guidance.
The application-name and capability tuples are derived from this registry.

Add its writable host partition and any managed-content partitions to
`internal/storage/`. Update the owning module in
`internal/runtime/` so `/data` mounts
only that application's directory and `/content` exposes only the managed
content it needs. Shared SELinux labels must remain shared (`z`) for
read-only content; application state keeps a private label (`Z`).

Update parsing and dispatch in `internal/cli/`:

- an application branch below `run`, reusing the common web flags when the
  generic web contract applies;
- any genuinely application-specific arguments or dispatch.

Build, shell, logs, stop, cleanup, status, and lifecycle hints derive their
generic choices from registry capabilities. Their tests still need updated
expectations for a new application.

Add a focused entry to `internal/cli/`. Keep it to the
working setup, run modes, network behavior, and routine operations that a new
user needs. Guide commands are parser-tested, but the application-specific
explanations and any new mode still need direct output assertions.

The generic `runtime.WebOptions` and command builder should remain usable. If the
application needs a genuinely different isolation model, add a dedicated
options type and command builder rather than accumulating application-specific
conditionals in the generic path.

Only ComfyUI may receive trailing arguments after `--` today. If forwarding is
needed for another application, define an allow/deny policy first; do not
allow callers to override launcher-owned networking, paths, or security.

### 5. Add content

Managed content is direct files or bounded archive members with independent
file identities, bundles and application-owned destinations. There is no
native repository-tree manifest contract. Follow
[content catalog maintenance](content-catalog.md) for artifact, license,
recipe, workflow and benchmark changes.

llama.cpp GGUFs use the `llama-models` target and built-in serving presets.
The managed router is pinned upstream llama-server, not a separate daemon.
Keep its generated INI catalog-only, atomic and mounted as one read-only file;
do not accept arbitrary host preset files.

Each new optimized policy needs an explicit schema, review criteria, direct
startup mapping, router rendering and tests. Keep backend compatibility,
hardware profile, speculative decoding, context, cache precision, model-load,
template and client capability separate. Do not replace those fields with a
generic upstream-arguments collection.

The [preset field reference](content-catalog.md#add-a-direct-artifact) owns
their current contract and enforcement boundaries. In particular:

- Backend restrictions need coherent multi-turn output and a function-tool
  exchange on every declared tuple, not only startup or throughput. Record
  retest/removal conditions.
- Speculative decoding needs a verified target/draft tuple, explicit greedy
  DFlash proposals, bounded strategy-specific depth and direct/router parity.
  Streamed allocation has a narrower reviewed exception; general DFlash
  support does not extend it.
- Context metadata overrides need a normal metadata-backed control and real
  retrieval, quality, memory and speculative evidence beyond the declared
  window before becoming a default.
- Non-default Flash Attention, K/V-cache and model-load policies require
  applicable hardware acceptance. Do not transfer Strix Halo evidence to
  Strix Point or RDNA 4. Preserve cold/warm, long-context, output-coherence,
  memory-headroom and kernel-journal checks.
- Named chat templates need immutable source/license provenance, exact-byte
  tests and synchronized image/entrypoint/router mappings. A host template
  path is not a managed extension point.
- Agent/reasoning/vision claims require the complete client protocol and
  companion-file acceptance described in the content guide. Catalog loading
  alone does not prove those review conditions.

### 6. Test it

At minimum add tests for:

- parser choices and defaults;
- image tag, port, and container name resolution;
- complete Podman security arguments;
- CPU mode exposing no devices;
- forced profile validation in the entrypoint helper;
- application-specific path and network confinement;
- build-all, stop-all, and cleanup coverage.

Then build the target and perform a CPU-only loopback HTTP smoke test. GPU
acceptance belongs on all target hardware classes.

## Add a foreground or batch application

A managed foreground or batch application needs:

- dedicated immutable options struct in the owning
  `internal/runtime/` module;
- dedicated Podman command builder;
- dedicated application/mode parser subtree below `run` and command handler;
- application-specific input validation before Podman;
- exact read-only input mounts;
- `--network none`;
- output restricted to a known `/data/output` subtree;
- installed bundle readiness checked before execution;
- `--dry-run` available without loading a model.

For a new command mode:

1. Add the parser choice and validate all mode-specific argument combinations.
2. Map the mode to one exact catalog bundle and content destination.
3. Keep paths and upstream task names in one command-builder branch.
4. Reject unsupported upstream modes explicitly.
5. Add command-shape and confinement tests.
6. Add a dry-run example to the user README.

Do not expose every upstream command automatically. The managed surface is an
allowlist with known persistence, network, and model behavior.

## Add a hardware profile

A new profile means Paracetamol intentionally supports another architecture. It
is more than a CLI label.

Update all of the following:

1. The profile registry in `internal/platform/`. Add an
   architecture to an existing profile tuple when it needs the same runtime
   policy, or add a profile only when the policy itself differs.
2. AMD wheel extras in `Containerfile`, if the architecture needs another
   device package.
3. llama.cpp CMake targets and OCI `gpu.targets` labels.
4. Shell entrypoint policy for architecture-specific workarounds.
5. `doctor` compatibility guidance where kernel or memory configuration
   differs.
6. README hardware requirements and examples.
7. Configuration, detector, runtime, CLI, and entrypoint tests.

Decide explicitly whether the new architecture is:

- auto-detected;
- allowed only when forced;
- supported by every image or only a subset.

The container detector inspects the devices exposed through the exact selected
render-node set and requires one supported architecture; PyTorch and native
applications use their corresponding detector. An application must
opt into the registry's `MultiGPU` capability before the launcher will expose more than
one node to a workload. That capability means only that its runtime has a
deliberate way to use the devices; document the application-specific
placement or splitting behavior.

Hardware acceptance must verify:

- `doctor` reports the expected architecture;
- `auto` resolves correctly;
- the matching forced profile starts;
- a mismatching forced profile fails;
- representative inference works;
- both balanced and conservative policies behave as documented;
- a multi-render-node host still requires an explicit complete selection;
- homogeneous multi-GPU selection succeeds only for an opted-in application;
- mixed architectures and multi-GPU selection for other applications fail
  closed.

## Add a runtime policy

Memory and kernel policies are independent of hardware profiles. Add a policy
only when it represents a reusable behavior callers may select.

Update:

- accepted values and defaults in `internal/config/`;
- parser choices and environment fallback in `internal/cli/`;
- environment propagation in the owning `internal/runtime/` package;
- application entrypoints that implement it;
- benchmark result metadata if it affects performance;
- documentation and tests.

Avoid a cross-product such as `strix-halo-conservative`. It should remain
`--profile strix-halo --memory-policy conservative`.

## Build-time patch discipline

The narrowly scoped `applications/llama-cpp/hip-apu-host-buffer.patch` is an
executable specification of an intentional deviation from upstream. Python
transformations are appropriate for application policy rewrites; a reviewed
upstream source diff may remain a literal patch when preserving that diff
exactly is the clearer contract.

When adding or updating a patch:

- match enough surrounding text to be unique;
- require exactly one match;
- keep replacement text readable;
- validate the resulting source with the appropriate language/compiler checks
  (`compileall` for Python, matching native fixtures for C++);
- test the security/policy outcome, not only the text replacement;
- describe the behavioral change in `THIRD_PARTY_NOTICES.md`;
- preserve upstream copyright and license material.

Implement policy against the canonical licensed upstream and record that
upstream directly.
