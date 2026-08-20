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
| New supported coding-agent frontend | client below `agent` plus an optional PATH launcher |

Do not create top-level application commands or commands such as
`run-new-gpu`. Keep application, profile, and mode orthogonal below `run`.
Keep coding-agent frontends below `agent` rather than assigning each client a
top-level command.

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

Register the application target through `config.Application`. The common build
orchestrator will ensure the tagged base first and pass `ROCM_BASE_IMAGE` with
pulling disabled. Do not add a separate remote or application-specific ROCm
base.

Keep the target late-copy pattern: copy frequently changed ROCmplete
entrypoints and patches after expensive dependency layers where possible so
normal edits retain build cache.

#### Standalone native applications

Use the shared PyTorch base for Python applications that depend on PyTorch. A
native application that does not use PyTorch should start its builder and
final image from `ROCM_RUNTIME_IMAGE`, as llama.cpp does, rather than carrying
the higher PyTorch base. In that case:

- reuse the project runtime's pinned OS and native ROCm package tuple;
- leave `SharedPyTorchBase` false;
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
ROCMLETE_PROFILE
ROCMLETE_LISTEN
ROCMLETE_HOST_LISTEN
ROCMLETE_PORT
ROCMLETE_KERNEL_POLICY
```

Pass `ROCMLETE_MEMORY_POLICY` only to an application whose entrypoint
implements a selectable memory behavior. A shared parser or runtime helper is
not enough to make the policy real.

Use `/opt/rocmplete/container_profile.py` to resolve and enforce profiles.
Create writable paths before importing PyTorch. Force model libraries offline
where supported, and route state, inputs, outputs, caches, and custom content
below `/data`.

A GPU-only batch application still consumes `ROCMLETE_PROFILE` and invokes the
same detector before its workload. Do not rely on host-side profile parsing as
a substitute for checking the architecture PyTorch actually sees.

`ROCMLETE_LISTEN` is the internal container bind address; managed web runs use
`0.0.0.0` for IPv4 host publications and `::` for IPv6 host publications
inside a private namespace. `ROCMLETE_HOST_LISTEN` records the exact host IP
on which Podman publishes the one application port. Print a short startup
banner containing the resolved profile, device, architecture, PyTorch/ROCm
versions, data path, container bind, and host publication. Never put secrets
in it.

### 4. Register the application on the host

Add one `config.Application` to the application registry in
`internal/config/config.go`. Declare its image, container, build target,
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

If the application needs a native repository tree, add a tree manifest and
bundle as described in [content-catalog.md](content-catalog.md). Runtime model
paths must match the manifest destination exactly.

llama.cpp is the direct-artifact exception: GGUF files use the dedicated
`llama-models` target and an optional `llama_presets` entry. Its server router
is the pinned upstream implementation. Keep generated INI input catalog-only,
atomic, and mounted as one exact read-only file; do not expose arbitrary
user-supplied presets through the managed router.

For speculative decoding, use the preset schema's closed
`speculative_type` and bounded `draft_tokens` fields. `draft-mtp` accepts up
to eight draft tokens and may use embedded prediction heads;
`draft-dflash` accepts up to fifteen and requires `draft_artifact`. Include a
separate draft GGUF in the same bundle as the target. Do not add a generic
preset-arguments collection: every new optimized policy needs an explicit
schema, validation, runtime mapping, router rendering, and hardware
acceptance.

When controlled measurements establish different speculative depths for the
ROCm and Vulkan builds, `draft_tokens_by_backend` may override the preset's
fallback for either exact backend. Keep the common value in `draft_tokens`,
limit override keys to `rocm` and `vulkan`, and verify both direct and router
startup. Backend policy remains separate from hardware-profile policy.

When an upstream release explicitly advertises a window beyond the GGUF
target or draft metadata, `context_override_architectures` may name only the
affected GGUF architecture prefixes. Its presence maps the selected context to
those `context_length` keys and disables llama.cpp fitting. Preserve a normal
metadata-backed control preset, document the upstream ambiguity, and do not
promote the override until retrieval, quality, memory, and speculative
acceptance have been measured beyond the declared window.

For models that require Jinja or have a measured Flash Attention difference
between GPU classes, use the explicit `jinja` boolean and `flash_attention`
profile map. Only `rdna4`, `strix-halo`, and `strix-point` keys and `on`,
`off`, or `auto` values are accepted. Keep the single-model environment
mapping and generated router INI behavior equivalent, and add target-hardware
acceptance for every non-default profile policy.

A measured symmetric target K/V-cache policy belongs in the preset's closed
`kv_cache` profile map. Only `f16`, `q8_0`, and `q4_0` are accepted, and a
quantized cache requires Flash Attention explicitly on for the same profile.
Keep draft-cache behavior independent. Validate long-context retrieval and
generation on every listed architecture rather than carrying a Strix Halo
result to Strix Point or RDNA 4.

If the embedded GGUF template cannot express the managed API contract, add a
named `chat_template` only when one fixed, reviewable adapter is enough. Keep
the catalog allowlist, image file, entrypoint validation, single-model mapping,
and router preset in sync. For a copied upstream template, pin its immutable
revision, preserve its license notice, and hash the exact bundled bytes in a
test. Do not turn this into a host path or arbitrary template loader.

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

A new profile means ROCmplete intentionally supports another architecture. It
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

The detector inspects every PyTorch device visible through the selected
render-node set and requires one supported architecture. An application must
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
- validate the resulting source with `compileall`;
- test the security/policy outcome, not only the text replacement;
- describe the behavioral change in `THIRD_PARTY_NOTICES.md`;
- preserve upstream copyright and license material.

Implement policy against the canonical licensed upstream and record that
upstream directly.
