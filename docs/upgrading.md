# Upgrading dependencies and upstream applications

Use the [routine upgrade runbook](routine-upgrade-runbook.md) for a read-only
scan or end-to-end checklist, then select the relevant procedure here. Read
[architecture](architecture.md) before changing ownership or runtime policy and
[testing](testing-and-release.md) for the required checks. For llama.cpp,
start with [maintenance](llama-cpp-maintenance.md) to establish the accepted
baseline and open issues.

Source owns current pins and image identities. The [upgrade history](research/upgrade-history.md)
preserves dated decisions, superseded holds, rejected experiments and retired
templates without making them current instructions. Model and workflow changes
follow [content catalog maintenance](content-catalog.md).

## Pin inventory

Start every upgrade by locating the current values:

```bash
rg -n '^(ARG .*VERSION|ARG .*COMMIT|ARG .*UBUNTU_IMAGE)' Containerfile
rg -n 'BuildUnit|Spec{' internal/application/registry.go
rg -n 'source_version|source_revision' catalog/catalog.json
cat go.mod
sort -u containers/content_tools/requirements.txt \
  applications/comfyui/constraints.txt
```

The main pin classes are:

| Layer | Location | Pin type |
| --- | --- | --- |
| Ubuntu base | global `UBUNTU_IMAGE` in `Containerfile` | registry digest |
| ROCm runtime and PyTorch family | runtime/base-stage arguments | exact package versions |
| ComfyUI | ComfyUI stage | release label plus full commit |
| ComfyUI-GGUF | ComfyUI stage | full commit |
| rgthree-comfy | ComfyUI stage | full commit |
| llama.cpp native build | llama stages | full commit and exact build dependencies |
| Content download tools | `containers/content_tools/requirements.txt` | full exact dependency set |
| Comfy dependencies | `applications/comfyui/constraints.txt` | transitive constraints |
| Models and workflows | `catalog/` | full commits plus content hashes |
| Host SQLite driver and transitive Go modules | `go.mod`, `go.sum` | exact module versions and checksums |

AMD ROCm runtime packages belong to the lower tagged runtime; PyTorch, `pip`,
and `wheel` policy belong to the higher shared base.

## Standard upgrade loop

Upgrade one compatibility unit at a time. Combining unrelated foundation,
application and model changes obscures failures and weakens license/patch review.

For any layer:

1. Inspect and preserve existing work, then run the baseline tests before editing.
2. Read upstream release notes, compatibility notes, license changes, and
   security advisories.
3. Resolve a full immutable commit or digest.
4. Change only the selected layer and any directly coupled tags/metadata.
5. Build the affected target without image-layer cache at least once.
6. Inspect the resulting installed versions and licenses.
7. Run CPU-only startup checks.
8. Run GPU diagnostics and representative inference on all supported hardware
   classes when the change touches ROCm, PyTorch, kernels, models, or runtime
   policy.
9. Update notices and documentation in the same change.

Use `git diff --word-diff` for lock/constraint changes and normal `git diff`
for source patches. A large resolver churn deserves the same review as source
code.

Use `--no-layer-cache` during repeated local upgrade iterations: it re-runs
the selected target's build instructions while checking prerequisites through
their normal layer cache and retaining downloaded Python packages. Use
`--no-cache` for the final cold validation because it also cold-builds the
prerequisite closure and bypasses the host package-download cache.

## Upgrade the host SQLite driver

`modernc.org/sqlite` is used only for local gateway usage statistics. Review
both the driver release notes and the bundled SQLite security fixes. Keep the
driver's required `modernc.org/libc` version together with it, inspect all
transitive changes, and refresh `licenses/modernc-sqlite/` from the exact module
release plus `THIRD_PARTY_NOTICES.md`. The full upstream notice distinguishes
linked components from optional extensions and test/build dependencies.

Run normal and static builds, gateway persistence tests, the race detector and
browser acceptance. Verify reopen, retention and corruption behavior with
temporary databases. Do not use a user's history as an upgrade fixture or
silently recreate a database with an unknown schema. The driver is embedded in
the host binary, so this upgrade needs no application image rebuild or system
SQLite installation.

## Check native Maki compatibility

Paracetamol does not install or pin Maki. Its maintained example targets
Maki 0.6.3 or newer. When changing the renderer or reviewing an upstream Maki
schema change, run the native transport fixture with a separately installed
client.

```bash
PARACETAMOL_TEST_MAKI=/absolute/path/to/maki make test
```

This uses disposable native configuration and a fake HTTP backend. It checks
model selection, reasoning, credentials, a tool round trip and replay without
touching normal client state. Follow it with a live remote tool round trip on
an available inference host. Record the client version and distinguish native
macOS execution from Linux protocol checks.

## Upgrade content download tools

The dedicated `content-tools` image keeps content installation independent of
any application image. `containers/content_tools/requirements.txt` is a
complete exact dependency set, not merely a list of direct dependencies. When
upgrading `huggingface-hub`, resolve its full environment in a clean Python
virtual environment, review every transitive change, replace the complete pin
set, and update the content-tools image identity in
`internal/application/registry.go`.
Advance that image's revision when the bundled HTTPS helper changes too, so
an older locally built helper cannot silently satisfy the installer's image
check.

Build the independent content-tools target explicitly. A selected application
build does not include it in that application's prerequisite closure:

```bash
./paracetamol build content-tools --no-cache
podman run --rm --entrypoint /opt/venv/bin/python \
  CURRENT_CONTENT_TOOLS_IMAGE \
  -m pip check
```

Use the current tag from `internal/application/registry.go`. Then dry-run at
least one direct Hugging Face artifact and one authenticated Civitai artifact. A dry run
validates metadata but does not test transport; use small pinned fixtures when
transport behavior itself changed.

## Upgrade the Ubuntu base

The base is pinned by digest:

```Dockerfile
ARG UBUNTU_IMAGE=docker.io/library/ubuntu@sha256:...
```

Choose the intended Ubuntu release explicitly, resolve its current manifest
digest, record the corresponding dated Ubuntu snapshot tag in the comment
above it, and review whether package names in the `apt-get` layer changed. The
official Ubuntu image publishes dated tags such as `resolute-20261105.1` as well
as the moving `26.04` release tag. The dated tag keeps the selected snapshot
visible upstream; the digest proves its exact identity. Do not replace that
pair with a floating tag.

A digest cannot be recycled into different content. If the selected manifest
ever becomes unavailable, let the build fail and handle the current `26.04`
manifest as a normal reviewed Ubuntu-base upgrade. Do not silently fall back
to whatever the release tag resolves to, because that would make an old
checkout build from different root-filesystem bytes without a source change.

The image digest freezes the starting root filesystem, not the Ubuntu package
archive. A no-cache build resolves named `apt` packages from the archive as it
exists at build time. Review the installed package delta when exact distro
package identity matters for a reviewed source state.

Validate with:

```bash
./paracetamol build all --no-cache
podman run --rm --entrypoint /bin/bash \
  CURRENT_COMFY_IMAGE \
  -lc 'cat /etc/os-release; python --version'
```

Replace `CURRENT_COMFY_IMAGE` with the exact tag from
`internal/application/registry.go`.

Check that rootless startup, read-only filesystems, `ffmpeg`, Git certificate
validation, Python virtual environments, and the AMD wheels still work.

## Upgrade ROCm and PyTorch

Treat these as one compatibility tuple:

```text
ROCM_VERSION
TORCH_VERSION
TORCHVISION_VERSION
TORCHAUDIO_VERSION
device extras for gfx1150, gfx1151, gfx1200, and gfx1201
```

Consult AMD's current
[ROCm PyTorch installation documentation](https://rocm.docs.amd.com/projects/ai-ecosystem/en/latest/frameworks/pytorch/install.html)
and the exact package inventory at the release's documented aggregate index.
ROCm 10.0 and later use
`https://stable.repo.amd.com/rocm/whl-next/`; older TheRock releases remain at
the legacy multi-architecture index. Confirm that the selected release still
supplies all four device extras. Ordinary upstream PyPI wheels are not a
substitute for this multi-architecture image.

The selected ROCm 10.1.0 tuple is PyTorch 2.14.0,
torchvision 0.29.0a0, and torchaudio 2.11.0.3. The alpha suffix on torchvision
is intentional: AMD prescribes that exact build for this tuple, including all
four managed device extras. Do not substitute the stable 0.29.0 wheel built
for ROCm 10.0.0. Review the wheel metadata and upstream installation instructions
again on the next upgrade rather than inferring compatibility from version names.

Update:

- the four global ROCm/PyTorch version arguments;
- the shared runtime and llama.cpp SDK using the same `ROCM_VERSION`;
- device extras if AMD renamed them;
- every default image tag in `internal/application/registry.go`;
- ROCm/PyTorch OCI labels if their structure changes;
- README requirements or kernel notes;
- tests that assert image tags or command contents.

Build all targets without cache:

```bash
./paracetamol build all --no-cache
```

Inspect the installed tuple:

```bash
podman run --rm -i --entrypoint /opt/venv/bin/python CURRENT_IMAGE - <<'PY'
import torch
print("torch:", torch.__version__)
print("HIP:", torch.version.hip)
print("available:", torch.cuda.is_available())
PY
```

Then run:

```bash
./paracetamol doctor --render-node /dev/dri/renderD128
```

`doctor` imports PyTorch, reads device properties, and performs a small GPU
operation. Complete acceptance still requires a real representative workflow
on an RX 9060 family card, an R9700 or RX 9070 family card, Strix Halo, and
Strix Point.

## Upgrade llama.cpp

llama.cpp is independent of the PyTorch image, but not its ROCm release. Its
builder and final image both start from the shared minimal ROCm runtime.
Never hold it on an older ROCm version or upgrade it ahead of the other GPU
applications. Treat these pins as one reviewed compatibility tuple:

```text
UBUNTU_IMAGE
LLAMA_CPP_COMMIT
ROCM_VERSION
GLSLC_VERSION, SPIRV_HEADERS_VERSION
VULKAN_VERSION, MESA_VULKAN_VERSION
```

The shared runtime installs AMD's modular `rocm[libraries,device-*]` wheel at
the exact `ROCM_VERSION`. The production builder adds only the `devel` extra,
runs `rocm-sdk init`, and compiles against the SDK's reported CMake and
compiler paths. The final image starts from the unchanged shared runtime and
must not inherit the development payload or PyTorch base. AMD's aggregate
wheel pins its component wheels to the same version; inspect that dependency
metadata when the packaging generation changes.

The installed binaries carry RPATHs to the modular core and libraries roots.
Derive their site-packages prefix from the build interpreter rather than
hard-coding a Python minor version. `rocm-sdk test` is not an image-build gate:
its device probe needs a GPU and its mirrored-payload hardlink check is not
reliable on every container storage driver. The relevant gates are
`pip check`, SDK path checks, the real CMake build, retained-binary `ldd`, and
target-hardware inference.

The Vulkan tuple is separate from AMD's packages but just as observable.
Review Ubuntu's `glslc`, SPIR-V headers, Vulkan loader, and Mesa RADV versions
together. The shader compiler changes generated code at build time, while
Mesa changes the runtime driver.

### llama.cpp HIP MMQ compatibility refresh (2026-11-13)

The compatibility patch retains the pre-`8fc0376` floating-input auxiliary
sum only in `quantize_mmq_q8_1`, for normal and scatter instantiations.
Its `GGML_USE_HIP` guard applies to every HIP device, not only Strix Halo or
Qwen4exp. Quantized activation bytes and scales stay unchanged. The scalar
Q8_1 quantizer, newer few-row producer and non-HIP paths retain upstream's
corrected behavior. F16 KV, model files, matrix precision, samplers and
runtime settings are unchanged. There is no new switch or output filter.

This is a temporary compatibility mitigation for an observed local
execution-path regression, not a mathematical correction or evidence that
upstream is wrong. Remove it only when the corrected HIP sum passes the
unchanged replay, speculative and non-speculative gateway checks, numerical
and state tests, populated long context and applicable hardware acceptance.
Do not weaken exact-output or numerical gates to remove it. Results and
coverage limits belong in the [hardware acceptance record](hardware-acceptance.md).

The [dated refresh and investigation](research/upgrade-history.md#llamacpp-hip-mmq-compatibility-refresh-2026-11-13)
preserve the accepted candidate's source identity, regression controls and
coverage limits. Do not infer broader acceptance from a source build.

### llama.cpp downstream patch ledger

The current image carries eighteen patches. The [contracts](#current-patch-contracts)
below define scope and removal gates; the Containerfile owns exact application
order. [Historical packaging and source reviews](research/upgrade-history.md)
do not override these contracts or current hardware acceptance.

#### Current patch contracts

Treat every row as part of the pinned source rather than as incidental build
machinery. On an update, classify each patch as unchanged, rebased, replaced
upstream, or blocked. A patch that stops applying has not proved that its
protected behavior is fixed.

| Patch | Protected behavior and scope | Removal gate |
| --- | --- | --- |
| `hip-apu-host-buffer.patch` | Prevents unsafe direct computation on `ROCm_Host` buffers on HIP integrated GPUs while retaining pinned allocation. Relevant to `gfx1150` and `gfx1151`. | The selected upstream pin contains an equivalent to [PR 25863](https://github.com/ggml-org/llama.cpp/pull/25863), and long-input server, CLI, tool-call, and concurrent-slot checks remain correct on both APU architectures. |
| `quantized-kv-flash-attention.patch` | Provides the reviewed HIP q8_0/q4_0 tile dequantize-on-load path derived from Nathan Wilson's commit `2a24abc6`. Upstream owns the former Vulkan q8_0 half at commit `dc72703fc`. | Matching upstream HIP code passes the same f16, q8_0, and q4_0 cache, context-depth, performance, and output checks on every applicable hardware class. |
| `hip-strix-halo-graph-recapture.patch` | Reinstantiates each HIP executable chunk after a changed capture on exact `gfx1151`, retaining ordinary graph replay. This corrects the populated direct-reader clear/restore failure isolated during the October source screens. Other architectures retain upstream executable updates. Upstream now owns the legacy-pool chunk-vector diagnostic. | The unpatched upstream path passes populated ordinary, causal-transition and clear/restore logit checks with the managed reader, followed by long-context and older-model checks. |
| `hip-strix-halo-tiled-gdn.patch` | Adds the F32 tiled GDN prefill kernel from Piotr Wilkin's commit `964c6f2f0`, narrowed to exact `gfx1151`, 48 value heads, state dimension 128, one sequence and 16 through 32768 operation tokens. Includes CPU-reference shape and snapshot tests. | The selected upstream implementation passes the numerical operator cases, managed Flash-Next protocol checks and populated near-256K comparison with equivalent performance. Other GPU architectures need their own acceptance before broadening dispatch. |
| `qwen4exp-direct-reader.patch` | Carries the explicit buffered embedding reader from PR 28136 at `c6a9e5c9ae6d6a551217f75c9a04b2e8b1aa62dd`, with Gemma4 hooks omitted and unavailable explicit reads rejected instead of silently reverting to mmap. | Upstream's reader passes the same CPU dequantizer/EOF cases and managed Flash-Next router, tools and populated near-256K checks with the accepted allocation policy. |
| `hip-qwen4exp-qsa-kernels.patch` | Adds selected-key F16 QSA prefill and short-query kernels adapted from `halo-box/strix-llama.cpp` at `7a9196dad8b13b70fb3420886aadaf98fc09b849`. Dispatch requires exact `gfx1151` geometry. Canonical sorting and deduplication make attention independent of upstream top-k ordering. The Qwen4exp graph supplies selected IDs only when `PARACETAMOL_QWEN4EXP_QSA_KERNEL=1`; the entrypoint enables it by default only for Strix Halo ROCm. A container-level value of `0` disables it. | Upstream provides equivalent selected-key attention with the order-invariance and numerical operator tests, Flash-Next correctness, near-256K checks, and paired prefill gain intact, without changing older Qwen paths. |
| `process-allocation-policy.patch` | Adds a native `--no-unified-memory` startup option. Preset inspection is pure, while the selected model process removes the inherited CUDA/HIP allocation variable before loading. | Upstream supports an equivalent per-model allocator opt-out without modifying sibling processes. Recheck ordinary models before and after a Flash-Next router switch. |
| `hip-qwen4exp-graph-optimizations.patch` | Intersects upstream's visibility-filtered selected IDs with its shared causal/sequence mask, avoiding a dense per-layer selection mask. Uses upstream pooled-cache state rather than a private prefix or derived-key cache. Requires supported single-stream F16 geometry and both QSA switches. CPU fallback honors the intersection, and unsupported accelerators decline it. A container-level `PARACETAMOL_QWEN4EXP_QSA_GRAPH=0` disables this and the scoped pipeline fusions. | Upstream equivalents pass the independent selected-mask oracle, unsupported-layout fallback, populated state restore, near-256K retrieval and prefix reuse, and older-model/backend checks. Never silently substitute a private reduced-precision matrix path or persistent derived cache. |
| `hip-qwen4exp-pipeline.patch` | Scoped Qwen4exp convolution, normalization/injection, expert routing/reduction and Q8 scheduling on `gfx1151`. Retains F16 KV and F32 residuals. The separate temporary MMQ compatibility patch below owns the auxiliary-sum exception. The injection dot product remains F32 with numerical reference tests. The 512/10 routing helper rejects invalid expert IDs in both passes and emits upstream tile descriptors, with independent bounds/map/guard fixtures. Selected MMQ shapes traverse those descriptors in folded row-major order without repacking or changing tile arithmetic. The convolution matcher recognizes current overlapping rollback windows and preserves their `SET_ROWS` stores. Unsupported shapes, precision overrides and unsafe fusions fall back. | Upstream equivalents pass the operator and fusion-lifetime checks, current rollback-window fixture and observed kernel activation, near-256K retrieval, state/cancellation screens, and older-model/backend regression checks. No BF16/F16 matrix or residual downcast belongs in this precision-preserving patch. |
| `hip-mmq-activation-sum-compat.patch` | Temporarily retains the pre-`8fc0376` floating-input auxiliary sum in normal/scatter MMQ on all HIP devices. Scalar, non-HIP and newer few-row corrections remain. Quantized bytes and scales are unchanged. This is local numerical-trajectory compatibility, not a mathematical fix or general accuracy claim. | The corrected HIP sum passes the unchanged compact-graph replay, speculative and non-speculative gateway checks, numerical/state gates, populated long context and applicable hardware acceptance without oracle relaxation. |
| `server-stream-keepalive.patch` | Carries [PR 30492](https://github.com/ggml-org/llama.cpp/pull/30492) at `221c0b2e6c815b32b2d629a1830ad04502807ecb`. Reports successful completion after the last SSE chunk instead of closing a reusable HTTP connection. Failed writes still fail. | Upstream contains the equivalent fix, a follow-up request succeeds on the exact same socket without reconnect, and streaming/non-streaming gateway cancellation still idles the child slot. |
| `topk-moe-boundaries.patch` | Uses the inclusive allocation-lifetime endpoint from Strix fork commit `f6ade7dc76c400067c2918b4132fede4198615cf`. Local bounds checks protect terminal and partial normalization groups in the same matcher. No kernel arithmetic or fusion eligibility is widened. | Upstream passes `test-topk-moe-lifetime.cpp` with actual dependency callbacks in all eight terminal/suffix cases, plus numerical top-k MoE and model-level checks. |
| `hip-qwen4exp-decode.patch` | Fuses the single-token Qwen4exp convolution, q/k normalization, gates, recurrent update and weighted norm on exact `gfx1151`. Folds HC gates into the existing norm/injection fusion. Matches upstream's F32 normalization order and retains F16 KV and the existing QSA policy, with explicit shape, consumer, lifetime and alias guards. | An upstream equivalent passes the positive and fallback operator cases, actual model activation, populated state restore, near-256K requests and older-model controls while retaining the measured decode benefit. Do not substitute a post-only HC fusion that displaces the larger prefill fusion. |
| `server-speculative-metrics.patch` | Local reporting-only change exposing the existing `n_draft_verif_steps` as `timings.draft_verif_steps`. The gateway records it as optional request metadata. This counts accounted rounds, not every verification attempt or replay. | Upstream exposes the equivalent explicit counter and the matching-source CPU serialization fixture passes. Do not estimate it from draft-token counts. |
| `dflash-mtmd-position-gap.patch` | Carries [PR 30220](https://github.com/ggml-org/llama.cpp/pull/30220) at `73647e8ade6fb4293d221c4b92aafb7e82976418`. Seeds missing DFlash draft-cache logical positions with zero target-feature rows after M-RoPE media bypasses draft injection. Target image encoding, target verification, model bytes, samplers and precision remain unchanged. | The selected upstream pin fills the equivalent draft-cache gaps and passes image decoding, unchanged/changed-media history, actual prefix reuse, natural text recovery, populated near-256K and relevant slot/rewind checks without draft-process errors. Requalify remaining cross-request/slot limitations; do not remove it merely because the earlier pinned-image skip or `pos_next()` change is present. |
| `kpool-prefix-retention.patch` | Carries [PR 32137](https://github.com/ggml-org/llama.cpp/pull/32137) at `54253df818eb8941ede14647bf412988e02c8027`, including its rollback test. Retains unaffected layout cells and complete pools before a sequence edit, then rescans the tail. `LLAMA_KPOOL_VERIFY=1` checks against a full rebuild and must remain disabled for normal timing. No precision or kernel implementation changes. | Equivalent upstream prefix retention passes the independent integer oracle, broken-layout controls, sanitizers, actual stale/rollback and restore paths, populated gateway histories and applicable device checks. The verifier shares the scanner and does not replace the independent oracle. |
| `qwen4exp-active-pools.patch` | Only commit `4161abc7209db6012fe2ab4badcaa49fa3e481ab` of [PR 32247](https://github.com/ggml-org/llama.cpp/pull/32247). Scores ubatch sequences' pools while retaining global new-pool writes and protecting every sequence's representatives from padded writes. Excludes the later attention operations and NVIDIA row-list kernels. | Equivalent upstream bookkeeping passes the actual-API pool/mask/padding oracle, idle and shared-owner cases, long idle-slot reactivation, concurrent generation and applicable device checks. Fewer scored pools must not mean discarding idle state or narrowing graph reservation incorrectly. |
| `recurrent-state-clear.patch` | Only the shared `build_rs` hunk of [PR 31913](https://github.com/ggml-org/llama.cpp/pull/31913) at `927a48d7d17a2facbfbf20e09e969631cf814f6d`. Uses an explicit zero fill instead of multiplying previous state by zero, which cannot clear NaN/Inf. Excludes the DeepSeek-specific change and upstream test that excludes hybrids. | An equivalent shared reset passes actual-helper finite/poison/empty-clear/copy/ownership oracles and applicable device/model gates. Clearing poisoned state is not a fix for whatever produced the first nonfinite value or evidence that a particular production symptom had this cause. |

The three corrections form one late host-source build group, after the reviewed
GPU and DFlash builds. It checks the exact incoming, intermediate and outgoing
source trees using a private index, then rebuilds and installs once. Every
configured ggml library is named explicitly and hashed before and after in both
the build and installed directories. Missing files fail rather than producing
an empty discovery manifest. Build-tree and installed hashes are separate
identities because installation changes RPATHs.

The old reset and pool-scoring controls run before the prefix patch changes a
private class layout. Candidate fixtures are compiled against the final headers
and installed libraries after the joint rebuild. Do not reuse an old fixture
binary against the new private ABI, or move these patches ahead of GPU
compilation without reviewing the cache and identity consequences. Their native
checks are described in [testing](testing-and-release.md#host-pool-and-reset-regressions).

Requalify optimized Qwen4Exp activation after a matcher/source refresh using
the tracked [dispatch and physical-span protocol](../tools/llama-acceptance/trace.md).
Its preparation fails closed on a changed owning source or patched tree.
Retain actual worker dispatches, not merely matcher eligibility, and qualify
the narrowly reduced `qsa-host-span` control against observed physical spans.
Source markers, diagnostic timing and CPU fixtures cannot replace that
evidence or the separately missing GPU poisoned-reset gate.

### Source-update procedure

For an upstream source update:

1. Resolve and review a full llama.cpp commit and its license.
2. Inspect CMake option changes, server/CLI flags, offline behavior, automatic
   layer fitting, unified-memory controls, HIP/Vulkan device naming, and GGUF
   compatibility.
3. Review every downstream patch against the ledger above. Record its
   classification and evidence. Keep `git apply --check` fail-closed behavior
   for each retained or rebased patch.
4. Keep both `GGML_HIP` and `GGML_VULKAN` enabled,
   `gfx1150;gfx1151;gfx1200;gfx1201`, RPC disabled, examples/tests disabled,
   and `LLAMA_CONNECT` disabled. Keep both `LLAMA_BUILD_UI` and
   `LLAMA_USE_PREBUILT_UI` disabled unless a separately pinned UI supply
   chain is deliberately added.
5. Update the short commit and policy revision in the application image tag.
6. Build `llama-cpp` without cache and confirm the build makes no unpinned
   asset download.
7. Inspect `ldd` for all retained binaries and libraries, including
   `libggml-hip`, `libggml-vulkan`, and the Vulkan loader.
8. Verify that the final Python environment contains core, libraries, and
   exactly the four supported device wheels, but no devel or PyTorch package.
   Confirm that the image retains exactly the intended binaries.
9. Run CPU `--version`, then real server/CLI and `llama-bench` acceptance with
   both backends on all supported hardware classes.

Also compare upstream router preset syntax and controlled arguments
(`--models-preset`, `--models-max`, offline mode, model aliases) with
`applications/llama-cpp/entrypoint.sh` and the catalog renderer. A syntax or
precedence change must fail closed in CPU router startup tests before the pin
moves.
For managed speculative presets, confirm `llama-server` and `llama-cli` still
expose `--spec-type`, `--spec-draft-n-max`, and `--model-draft`, and that the
router accepts the corresponding INI keys without changing their precedence.
Exercise both allowlisted strategies: embedded and separate-draft
`draft-mtp`, plus separate-draft `draft-dflash`. Re-run representative
generation with and without speculative decoding: regressions can affect
speed, memory, or committed output even when startup succeeds.
For presets with `context_override_architectures`, also confirm that
`--override-kv` remains valid in direct launches and router INI sections, that
`--fit off` retains its meaning, and that the target and draft architecture
keys have not changed.
For every preset, confirm that `llama-server` and `llama-cli` still expose
`--reasoning-preserve` and `--no-reasoning-preserve`, that router INI accepts
`reasoning-preserve = true` and `reasoning-preserve = false`, and that a
multi-turn exchange follows the intended reasoning-history policy. Do not
replace this with or infer support for the
separate model-native reasoning controls or their server compatibility bridge.
Inspect `llama-bench --help`, `--list-devices`, and JSON output as well.
Changes to option names, backend device names, result shape, or progress
streams require coordinated updates to
`internal/runtime/llama.go`, `internal/benchmark/`, its benchmark and comparison schema
versions, and tests.

### Managed templates and sampling

Only the templates allowlisted by `internal/catalog/` and
`applications/llama-cpp/entrypoint.sh` are current image inputs. Their files
live in `applications/llama-cpp/chat-templates/`; source/license provenance is
recorded in `THIRD_PARTY_NOTICES.md` and the
[historical template review](research/upgrade-history.md#earlier-extraction-provenance).
Retired Qwen3.6 and Muse templates have no current rebase requirement.

Compare the pinned base-model and selected GGUF templates independently of
tensor bytes before changing an override. Render every supported reasoning
level and off mode, ordinary and non-string history, and complete a multi-turn
tool exchange. Do not import community-template prompt policy as an incidental
compatibility fix. The catalog owns model-specific thinking/non-thinking sampler
tuples; the gateway fills missing values and preserves explicit overrides.
Recheck native parsing and actual generation settings, not only reports.
Client preferences do not change the omitted-reasoning HTTP fallback.

## Upgrade ComfyUI

ComfyUI has coupled source and dependency pins:

- `COMFYUI_VERSION`
- `COMFYUI_COMMIT`
- `applications/comfyui/constraints.txt`
- the exact `comfyui-manager` requirement in the pinned source's
  `manager_requirements.txt`
- the ComfyUI version embedded in the default image tag
- possibly workflow template package pins and benchmark exports

Resolve the release tag to a full commit. Inspect changes to CLI flags,
directory options, database behavior, frontend/workflow packages, and node
schemas before updating.

### Refresh constraints

Build the current ROCm/PyTorch prerequisite as the disposable resolver's base:

```bash
./paracetamol build pytorch-base
```

Use the exact locally built base tag for the disposable container.
Image identities belong to `internal/application/registry.go`; the corresponding
`ROCM_RUNTIME_IMAGE` and `ROCM_BASE_IMAGE` defaults are in `Containerfile`.
Derived builds use the explicitly tagged local prerequisites with pulling
disabled. Update the coupled identities when their inputs change.

In a disposable container, fetch the selected ComfyUI commit, install its
requirements, and capture `pip freeze`. Exclude packages owned by the AMD
ROCm/PyTorch base (`amd-*`, `rocm*`, `torch`, `torchvision`, `torchaudio`,
`triton`, and `wheel`) from the Comfy constraints, while retaining shared
ordinary dependencies such as `filelock`, `fsspec`, and `sympy`.

Review rather than blindly copying resolver output:

- normalize package names consistently;
- retain exact versions;
- update the “resolved on” comment and Python/ComfyUI context;
- check for packages removed from upstream requirements;
- inspect new native/system dependencies;
- inspect license changes.

Install both `requirements.txt` and `manager_requirements.txt` in the
disposable resolver. Manager is optional at runtime but is part of the pinned
image dependency graph. Review and pin every newly introduced transitive
dependency in `applications/comfyui/constraints.txt`; do not loosen the
Manager version declared by the selected ComfyUI commit.

Then update the source commit and build:

```bash
./paracetamol build comfyui --no-cache
```

The build runs `pip check`. Also inspect:

```bash
podman run --rm --entrypoint /opt/venv/bin/python CURRENT_COMFY_IMAGE \
  -m pip freeze
podman run --rm --entrypoint /opt/venv/bin/python CURRENT_COMFY_IMAGE \
  -m pip check
```

### Workflow consequences

An updated ComfyUI constraint set may change
`comfyui-workflow-templates-json`. If its package version or resources change,
follow [the workflow update procedure](content-catalog.md#add-or-update-a-curated-workflow).

Frontend changes can alter exported API-format benchmark graphs even if the
visual workflow is unchanged. Re-export and repin benchmarks deliberately;
never change a benchmark hash merely to silence a failure.

Verify CPU startup on loopback and load the web UI. On GPU hosts, verify that
all curated core node types still exist and run at least one representative
workflow from each family.

Also start once with `-- --enable-manager` on loopback. Confirm that Manager
permits a small registered custom node to be installed and removed, and that a
dependency lands below
`apps/comfyui/custom-node-python/` rather than `/opt/venv`. Repeat startup on a
non-loopback publication and confirm Manager refuses installation. The image
root and image-owned Python environment must remain read-only in both cases.

`applications/comfyui/patch_manager.py` is an exact-match patch against the
pinned Manager package. Review all three adaptations when changing Manager:

- host publication, rather than the container wildcard bind, controls its
  loopback security decision;
- the legacy UI uses the same effective address; and
- package installation uses the persistent child environment's `pip`, which
  can see both image and custom-node packages.

An upstream text mismatch must fail the image build. Update the patch only
after checking whether upstream now provides an equivalent container-aware
mechanism.

## Upgrade bundled ComfyUI extensions

Bundled extensions are immutable parts of the ComfyUI image. They do not
update themselves at runtime. Keep each extension update separate unless an
upstream compatibility change genuinely couples them, and bump the ComfyUI
image revision after changing either pin.

### ComfyUI-GGUF

Update `COMFYUI_GGUF_COMMIT` to a reviewed full commit. Check:

- its current license;
- its Python requirements;
- compatibility with the pinned ComfyUI;
- whether explicit `gguf` and `protobuf` pins remain correct;
- whether its directory and disable behavior still match the entrypoint.

Build ComfyUI without cache and verify `python -m pip check`. Test both normal
startup and:

```bash
./paracetamol run comfyui --profile cpu \
  --listen 127.0.0.1 --disable-bundled-extensions
```

### rgthree-comfy

Resolve the current upstream head, then compare it with the pinned commit:

```bash
git ls-remote https://github.com/rgthree/rgthree-comfy.git HEAD
rg -n 'RGTHREE_COMMIT' Containerfile
```

Review the complete commit range, `LICENSE`, `requirements.txt`, and the
`project.dependencies` list in `pyproject.toml`. The image build currently
requires both dependency lists to remain empty. If rgthree gains a dependency,
pin its complete closure deliberately rather than deleting that guard.

Update `RGTHREE_COMMIT`, bump the ComfyUI image revision in
`internal/application/registry.go`, and build:

```bash
./paracetamol build comfyui --no-layer-cache
podman run --rm --entrypoint /opt/venv/bin/python CURRENT_COMFY_IMAGE \
  -m pip check
```

For the final validation, use `--no-cache` as required by the standard upgrade
loop. Start the image on loopback and allow rgthree during the CPU probe:

```bash
./paracetamol run comfyui --profile cpu --listen 127.0.0.1 \
  -- --whitelist-custom-nodes rgthree-comfy
```

Confirm that rgthree imports without failed nodes and that the startup summary
lists it under `bundled nodes`. Also test with a persistent
`custom_nodes/rgthree-comfy` directory. Startup must list it under
`persistent overrides` and omit the bundled copy, without changing the
persistent directory.

## Upgrade ordinary Python dependencies

Do not mix opportunistic dependency upgrades into an unrelated feature.

For each changed package:

- determine why it is present and which image imports it;
- read breaking changes and supported Python versions;
- retain an exact version;
- check wheel availability for the base architecture;
- review license metadata;
- rebuild from a clean resolver state;
- run `pip check`, import smoke tests, and the relevant application path.

A dependency needed by only one application belongs in that application's
requirements or constraints file.

## Upgrade Hugging Face tooling

There are two distinct Hugging Face roles:

1. The dedicated content-tools image's `hf` CLI performs all managed Hugging
   Face artifact downloads.
2. Application libraries use `huggingface-hub`, Transformers, and Diffusers
   at runtime, normally in offline mode against already installed content.

Do not synchronize their versions automatically. Content transport and
ComfyUI may use different Hugging Face generations because their
responsibilities and upstream compatibility ranges differ.

When changing the content-tools `huggingface-hub` pin, verify the exact command
shapes constructed by `internal/content/`:

```text
hf download REPOSITORY PATH --revision COMMIT --local-dir DIRECTORY
```

Confirm that the new CLI still supports:

- exact revisions;
- resumable local-directory downloads;
- `--max-workers`;
- `HF_TOKEN` inherited only when the host supplies it;
- `HF_HUB_DISABLE_TELEMETRY`.

Use a small public file at a pinned revision for an actual downloader smoke
test before trusting a multi-hundred-GiB installation. Include a nested
repository path so relative staging paths are verified.

For application-side client upgrades, start the application with its network
policy intact and prove that it loads local paths without contacting the Hub.

Model repository revisions, file metadata, and license changes are catalog
work, not Python dependency work. Follow
[content-catalog.md](content-catalog.md) for those upgrades.

## Update image tags

Default image tags are an internal cache boundary owned by
`internal/application/registry.go`, not a promise of versioned project releases.
Change the affected application or prerequisite identity after material base,
source, dependency, patch, entrypoint or bundled-helper changes. Keep managed
prerequisite defaults in `Containerfile` aligned with the registry.
A short `rN` suffix distinguishes packaging changes without moving upstream.
Add every new build-context input to the `.containerignore` allowlist.

Search for stale tags:

```bash
rg -n 'localhost/paracetamol:|rocm[0-9]|COMFYUI_VERSION|_COMMIT' .
```

Old locally built images are not removed automatically by a tag change.
Inspect with `podman images localhost/paracetamol` and remove obsolete images
only as an explicit housekeeping action.

Managed image archives are deliberately tied to these exact current tags.
Import an older backup with the matching Paracetamol source revision, or use
Podman directly for manual recovery. The current launcher does not silently
retag an obsolete archive as a newer build.

## Historical precision-preserving screen

### Precision-preserving ROCm follow-up (2026-11-03)

The [archived follow-up](research/upgrade-history.md#precision-preserving-rocm-follow-up-2026-11-03)
records routing hardening and the rejected isolated performance extractions.
Current protected behavior and removal gates are in the patch contracts above.
