# Upgrading dependencies and upstream applications

For a periodic read-only inventory or a concise end-to-end checklist, start
with the [routine upgrade runbook](routine-upgrade-runbook.md). This document
provides the detailed procedure after one compatibility unit is selected.

Upgrade one layer at a time. A combined Ubuntu, ROCm, PyTorch, ComfyUI, and
model refresh makes failures difficult to attribute and weakens review of
licenses and source patches.

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
| Ubuntu base | first line of `Containerfile` | registry digest |
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

For any layer:

1. Start from a clean tree and run the baseline tests.
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
set, and update `CONTENT_TOOLS_IMAGE` in `internal/config/config.go`.

Build through one application so the normal prerequisite path is exercised:

```bash
./paracetamol build comfyui --no-cache
podman run --rm --entrypoint /opt/venv/bin/python \
  localhost/paracetamol:content-ubuntu26.04-huggingface1.27-r1 \
  -m pip check
```

Use the current tag from `internal/config/config.go`. Then dry-run at least one direct
Hugging Face artifact and one authenticated Civitai artifact. A dry run
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
official Ubuntu image publishes dated tags such as `resolute-20260724.1` as well
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
package identity matters for a release candidate.

Validate with:

```bash
./paracetamol build all --no-cache
podman run --rm --entrypoint /bin/bash \
  localhost/paracetamol:comfyui-ubuntu26.04-rocm10.1-0.28.0-r13 \
  -lc 'cat /etc/os-release; python --version'
```

Use the actual current image tag from `internal/application/registry.go`, not
the example tag above, after versions change.

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
[ROCm PyTorch installation documentation](https://rocm.docs.amd.com/en/latest/)
and the exact package inventory at the release's documented aggregate index.
ROCm 10.0 and later use
`https://stable.repo.amd.com/rocm/whl-next/`; older TheRock releases remain at
the legacy multi-architecture index. Confirm that the selected release still
supplies all four device extras. Ordinary upstream PyPI wheels are not a
substitute for this multi-architecture image.

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

### llama.cpp downstream patch ledger

Treat every row as part of the pinned source rather than as incidental build
machinery. On an update, classify each patch as unchanged, rebased, replaced
upstream, or blocked. A patch that stops applying has not proved that its
protected behavior is fixed.

| Patch | Protected behavior and scope | Removal gate |
| --- | --- | --- |
| `hip-apu-host-buffer.patch` | Prevents unsafe direct computation on `ROCm_Host` buffers on HIP integrated GPUs while retaining pinned allocation. Relevant to `gfx1150` and `gfx1151`. | The selected upstream pin contains an equivalent to [PR 25863](https://github.com/ggml-org/llama.cpp/pull/25863), and long-input server, CLI, tool-call, and concurrent-slot checks remain correct on both APU architectures. |
| `router-disconnect.patch` | Carries [PR 29707](https://github.com/ggml-org/llama.cpp/pull/29707) at `31b5b49bf600d43ea3b95a52f6b05650a71679c9`. Stops the child HTTP connection when its proxy closes and uses one response-wait deadline across wakeups. Includes the upstream non-streaming disconnect regression. | Upstream passes both streaming and non-streaming disconnects through the router and gateway. Observe the child slot becoming idle, not just a successful follow-up request. |
| `quantized-kv-flash-attention.patch` | Provides the reviewed HIP q8_0/q4_0 tile dequantize-on-load path derived from Nathan Wilson's commit `2a24abc6`. Upstream owns the former Vulkan q8_0 half at commit `dc72703fc`. | Matching upstream HIP code passes the same f16, q8_0, and q4_0 cache, context-depth, performance, and output checks on every applicable hardware class. |
| `hip-strix-halo-graph-recapture.patch` | Reinstantiates the HIP executable after a changed capture on exact `gfx1151`, retaining ordinary graph replay. This corrects the populated direct-reader clear/restore failure isolated during the October source screens. Other architectures retain upstream executable updates. | The unpatched upstream path passes populated ordinary, causal-transition and clear/restore logit checks with the managed reader, followed by long-context and older-model checks. |
| `hip-strix-halo-tiled-gdn.patch` | Adds the F32 tiled GDN prefill kernel from Piotr Wilkin's commit `964c6f2f0`, narrowed to exact `gfx1151`, 48 value heads, state dimension 128, one sequence and 16 through 32768 operation tokens. Includes CPU-reference shape and snapshot tests. | The selected upstream implementation passes the numerical operator cases, real Qwen27B MTP protocol checks and populated near-256K comparison with equivalent performance. Other GPU architectures need their own acceptance before broadening dispatch. |
| `qwen4exp-direct-reader.patch` | Carries the explicit buffered embedding reader from PR 28136 at `c6a9e5c9ae6d6a551217f75c9a04b2e8b1aa62dd`, with Gemma4 hooks omitted and unavailable explicit reads rejected instead of silently reverting to mmap. | Upstream's reader passes the same CPU dequantizer/EOF cases and managed Flash-Next router, tools and populated near-256K checks with the accepted allocation policy. |
| `hip-qwen4exp-qsa-kernels.patch` | Adds selected-key F16 QSA prefill and short-query kernels adapted from `halo-box/strix-llama.cpp` at `7a9196dad8b13b70fb3420886aadaf98fc09b849`. Dispatch requires exact `gfx1151` geometry. Canonical sorting and deduplication make attention independent of upstream top-k ordering. The Qwen4exp graph supplies selected IDs only when `PARACETAMOL_QWEN4EXP_QSA_KERNEL=1`; the entrypoint enables it by default only for Strix Halo ROCm. A container-level value of `0` disables it. | Upstream provides equivalent selected-key attention with the order-invariance and numerical operator tests, Flash-Next correctness, near-256K checks, and paired prefill gain intact, without changing older Qwen paths. |
| `process-allocation-policy.patch` | Adds a native `--no-unified-memory` startup option. Preset inspection is pure, while the selected model process removes the inherited CUDA/HIP allocation variable before loading. | Upstream supports an equivalent per-model allocator opt-out without modifying sibling processes. Recheck ordinary models before and after a Flash-Next router switch. |
| `hip-qwen4exp-graph-optimizations.patch` | Intersects upstream's visibility-filtered selected IDs with its shared causal/sequence mask, avoiding a dense per-layer selection mask. Uses upstream pooled-cache state rather than a private prefix or derived-key cache. Requires supported single-stream F16 geometry and both QSA switches. CPU fallback honors the intersection, and unsupported accelerators decline it. A container-level `PARACETAMOL_QWEN4EXP_QSA_GRAPH=0` disables this and the scoped pipeline fusions. | Upstream equivalents pass the independent selected-mask oracle, unsupported-layout fallback, populated state restore, near-256K retrieval and prefix reuse, and older-model/backend checks. Never silently substitute a private reduced-precision matrix path or persistent derived cache. |
| `hip-qwen4exp-pipeline.patch` | Scoped Qwen4exp convolution, normalization/injection, expert routing/reduction and Q8 scheduling on `gfx1151`. Retains F16 KV, F32 residuals and upstream quantized matrix arithmetic. The injection dot product remains F32 with numerical reference tests. Unsupported shapes, precision overrides and unsafe fusions fall back. | Upstream equivalents pass the operator and fusion-lifetime checks, near-256K retrieval, state/cancellation screens, and older-model/backend regression checks. No BF16/F16 matrix or residual downcast belongs in this precision-preserving patch. |
| `quantized-kv-state-copy.patch` | Carries [PR 30541](https://github.com/ggml-org/llama.cpp/pull/30541) at `0e3dbf0f23cf0ba00832a0697909e9eb8b214caf`. Counts tensor elements rather than quantization blocks for on-device sequence save, restore and scattered range copies. Protects optional quantized-cache use without changing F16 presets. | Upstream contains the equivalent correction and its `test-llama-context` device-copy and scatter cases pass with F16, Q8_0 and Q4_0 caches. |
| `server-stream-keepalive.patch` | Carries [PR 30492](https://github.com/ggml-org/llama.cpp/pull/30492) at `221c0b2e6c815b32b2d629a1830ad04502807ecb`. Reports successful completion after the last SSE chunk instead of closing a reusable HTTP connection. Failed writes still fail. | Upstream contains the equivalent fix, a follow-up request succeeds on the exact same socket without reconnect, and streaming/non-streaming gateway cancellation still idles the child slot. |
| `topk-moe-boundaries.patch` | Uses the inclusive allocation-lifetime endpoint from Strix fork commit `f6ade7dc76c400067c2918b4132fede4198615cf`. Local bounds checks protect terminal and partial normalization groups in the same matcher. No kernel arithmetic or fusion eligibility is widened. | Upstream passes `test-topk-moe-lifetime.cpp` with actual dependency callbacks in all eight terminal/suffix cases, plus numerical top-k MoE and model-level checks. |
| `hip-qwen4exp-decode.patch` | Fuses the single-token Qwen4exp convolution, q/k normalization, gates, recurrent update and weighted norm on exact `gfx1151`. Folds HC gates into the existing norm/injection fusion. Matches upstream's F32 normalization order and retains F16 KV and the existing QSA policy, with explicit shape, consumer, lifetime and alias guards. | An upstream equivalent passes the positive and fallback operator cases, actual model activation, populated state restore, near-256K requests and older-model controls while retaining the measured decode benefit. Do not substitute a post-only HC fusion that displaces the larger prefill fusion. |

The October 16 update pins `623c16393c7d53a37df8bb69c6ea8ed6e5171f62`,
37 upstream commits after `e36df60`, in image `r53`. Twelve patches apply
unchanged. `hip-qwen4exp-pipeline.patch` and `hip-qwen4exp-decode.patch` are
rebased for upstream's vectorized
F32 RMS normalization from
[PR 29720](https://github.com/ggml-org/llama.cpp/pull/29720). The decode,
HC and narrow-row fusions now reproduce the aligned float4 reduction tree.
Misaligned tensors and unsupported row shapes retain upstream's fallback.
Exact fused/reference checks remain exact, with added alignment and row-width
cases. A clean patch application did not catch this arithmetic coupling, but
the native operator tests did. Review reduction order as well as types on
future normalization updates.

Upstream also includes the previously deferred pooled-input view reuse from
[PR 30613](https://github.com/ggml-org/llama.cpp/pull/30613), Vulkan device
initialization serialization, bounded pinned-host allocation, and server
slot/draft-state fixes. This is not permission to remove the APU host-buffer,
graph-recapture, router-disconnect or KV-state-copy patches. Their removal
gates remain independent.

Two separately screened optimizations remain outside this update:

- The activation-copy change from
  [PR 30493](https://github.com/ggml-org/llama.cpp/pull/30493), head
  `4373bb620120b5fe1b811e1c4a202db9d82fa586`, lengthens Flash-Next's
  32K prefill by about 9.4% without improving decode. Do not carry it on
  the strength of results from another model or GPU.
- An isolated F16 attention kernel from
  [Strix fork PR 172](https://github.com/halo-box/strix-llama.cpp/pull/172),
  head `1ea2d675d7977c47e3c6952b3fc2ac376ef17b93`, shortens dense Qwen27B's
  32K prefill by about 10% in the initial screen, but shows no meaningful
  Flash-Next gain. Its roughly 1200-line kernel and changed attention
  evaluation order require more qualification than that result warrants.
  It is not a production-accepted patch. No fork-wide changes or private
  HIP runtime were imported.

The [October 16 hardware record](hardware-acceptance.md#strix-halo-llamacpp-refresh-and-optimization-screens-2026-10-16)
contains the paired Flash-Next timings and numerical limitations.

The October 15 decode follow-up keeps the `e36df604703dec1b6ce82f214698725475a57035`
source pin and adds a fourteenth patch in image `r52`. The GDN implementation
derives from [Strix fork PR 106](https://github.com/halo-box/strix-llama.cpp/pull/106)
at `9fbe5308fabbc79a53ddee5eadfad74ae0e4710e`. Its older recurrent reduction
is replaced with the selected upstream pin's eight-lane F32 ordering. Only
the reviewed single-token Qwen4exp geometry is eligible. There is no private
HIP runtime, asynchronous embedding reader or lower-precision matrix path.

The HC change adapts [upstream PR 30602](https://github.com/ggml-org/llama.cpp/pull/30602)
at `681ae40b5688343dd1a2c6c44b903ba8d5e5ac68` to the existing downstream
norm/injection fusion. Applying its post-only fusion unchanged loses that
larger fusion and slows prefill on Aion. Preserve this ordering when rebasing.
The [hardware record](hardware-acceptance.md#strix-halo-flash-next-decode-follow-up-2026-10-15)
separates the independent candidate screens from the combined result.

At that October 15 screen, the pooled-input view change in
[PR 30613](https://github.com/ggml-org/llama.cpp/pull/30613), tested at
`870e2b09de9a2e9f61d1b679a053baaff2a2ca9f`, was not carried. Its measured gain
is too small to justify another patch. The same applies to the parallel HC
pre-mix scheduling idea from fork commit
`389d446be5fd2babf97d2d98b3cdf39c4742a5fc`. These are deferred for value,
not classified as broken. The pooled-input change is now supplied upstream
by the October 16 pin. The HC scheduling idea remains deferred.

The October 15 follow-up pins `e36df604703dec1b6ce82f214698725475a57035`,
16 upstream commits after the October 14 refresh, in image `r51`. All thirteen
remaining patches apply unchanged. Upstream now owns the partial-checkpoint
correction from [PR 30514](https://github.com/ggml-org/llama.cpp/pull/30514)
and Vulkan KV contiguization from
[PR 27703](https://github.com/ggml-org/llama.cpp/pull/27703).

The dedicated Vulkan shader, its source patch and environment override are
removed. The merged implementation reuses bit-preserving copy shaders,
supports F16 and BF16 scratch, and selects eligible AMD devices except RDNA 4.
Its minimum query batch is 256, compared with 64 in the former downstream
path. This is upstream dispatch policy, not a newly enabled Paracetamol
tuning switch. Keep cross-architecture acceptance distinct from the local
Strix Halo measurements. The container runtime report moves to schema 2 and
drops the obsolete local-fix boolean. Rebuild the image with the matching
controller before using `status llama-cpp`.

The [October 15 hardware record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-upstream-follow-up-2026-10-15)
records the checkpoint-memory measurements, both backend comparisons and
the hardware acceptance limits.

The separate recurrent-state-copy experiment,
[PR 30139](https://github.com/ggml-org/llama.cpp/pull/30139) at
`10a57dd921c19152bc9121e1ff5aafba32169843`, is **not carried**. It passes
the bounded state and 4K/32K request screens, with identical request and
response-choice objects across eighteen paired requests. On Aion, shallow
dense Q8 non-MTP decode improves from 7.222 to 7.290 tokens/s, about 0.95%.
The default dense Q8 MTP HTTP control changes from 15.60 to 15.51 tokens/s.
Flash-Next changes from 21.53 to 21.79 tokens/s, while its 32K prefill takes
about 4.2% longer. These small, mixed results do not justify another carried
graph-state patch. The in-place shortcut also excludes speculative rollback
snapshots. Revisit it when upstream merges or the evidence changes, not as a
known-bad proposal or an accepted long-context optimization.

The October 14 refresh pinned
`23deec025587a58469bdf0120313ff88a1ddcc7b`, 345 upstream commits after
`2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`, and used image `r50`.
That fourteen-patch stack replaced the earlier partial rebase. Its
[screen record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-refresh-screen-2026-10-14)
remains useful evidence of why a successful short request was insufficient.
The [final-image record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-upstream-update-2026-10-14)
keeps the performance qualification explicit. The default dense Q8 MTP
control is effectively unchanged, as is Flash-Next's cold 247K request.
Flash-Next's warm decode is about 12.3% slower in the paired screen. This is
a maintenance and correctness update, not a universal speed improvement.

Upstream now owns Qwen4exp pooled-key state and visibility filtering through
[PR 29751](https://github.com/ggml-org/llama.cpp/pull/29751), with reduced
indexer workspace from [PR 29825](https://github.com/ggml-org/llama.cpp/pull/29825).
The private prepared-key cache, prefix eligibility machinery and selection
fusion are retired. The replacement compact patch only removes redundant
mask materialization around upstream's selection. The former generic QSA
gather is also retired. ROCm uses the retained selected-key kernels, while
Vulkan uses its upstream sparse-attention path from
[PR 28105](https://github.com/ggml-org/llama.cpp/pull/28105), reached through
the new graph's selected-width hint. These are behavior replacements, not
claims that the old proposals themselves were merged.

Canonical ID ordering is required with upstream's unordered top-k output.
Keep the negative-control order tests, independent mask-intersection tests,
and populated restore checks when rebasing these kernels. The scoped Q8
prefetch and small MoE tiles now live in the pipeline patch. Their private
instantiations explicitly retain Q8 activation precision and honor upstream
precision overrides. No retired BF16/F16 shadow-matrix mode is restored.

`applications/llama-cpp/test-qwen4exp-state.cpp` remains the hardware state
gate. It populates more than 4K tokens, decodes, switches causal mode and
compares full logits after restore and clear. Run its
[fast screen](testing-and-release.md#fast-flash-next-state-screen) before the
larger matrix, then the full fixture. Compile against the matching patched
source and libraries and enable both QSA kernel and graph switches. There is
no private QSA cache switch or cache-lifecycle test to maintain now.

Sampling policy remains at the Go gateway boundary, moved there in image
`r49`. It applies the catalog's per-thinking-mode defaults and preserves
explicit request values. Direct engine endpoints intentionally use native
defaults, so direct benchmarks send their selected catalog tuple explicitly.
Verify the gateway's resolver against native parsing and templates for off,
omitted controls, nested booleans and named effort levels. Router cancellation
acceptance must observe the child slot becoming idle, not merely a successful
follow-up request.

The reviewed upstream pin includes the
[F16 attention race fix](https://github.com/ggml-org/llama.cpp/pull/30103),
[RDNA 3.5 MMQ tile tuning](https://github.com/ggml-org/llama.cpp/pull/28195),
[chunked GDN kernel](https://github.com/ggml-org/llama.cpp/pull/29353) and
[Vulkan AMD iGPU checkpoint-read fix](https://github.com/ggml-org/llama.cpp/pull/30049).
Their presence alone is not evidence that the entire new build is faster.
The GDN implementation uses compensated BF16 terms with
F32 state, rather than the retired private BF16 shadow-weight mode. It still
requires numerical and model-level acceptance.

The additional open PRs selected on October 14 are the small quantized-state
copy and successful-stream connection reuse corrections in the ledger.
Other reviewed proposals remain deferred.
[HIP sparse attention](https://github.com/ggml-org/llama.cpp/pull/30370), at
`1ad19e56872d60cbee26c442a449bee7f0ba8fa9`, dispatches on GCN, not Strix Halo.
The [Vulkan KV layout proposal](https://github.com/ggml-org/llama.cpp/pull/27703),
reviewed at `3fdbb7132e8ed37fa785e5c74b870b5b01cf969a`, subsequently merged
and is included in the October 15 follow-up above.
[MMQ copy vectorization](https://github.com/ggml-org/llama.cpp/pull/30493)
and [vectorized RMS norm](https://github.com/ggml-org/llama.cpp/pull/29720)
remain watch candidates rather than established end-to-end Strix Halo wins.

The Strix fork was also reviewed at
`deb5e2f50f79b812f5b957321c7b511c054043e0`. Its allocation-boundary fix is
retained above. Newer mixed-matrix paths, incremental-cache machinery and
Flash-Next MTP-specific fusions are not imported. A decode HC-mix fusion at
`389d446be5fd2babf97d2d98b3cdf39c4742a5fc` is a possible separate experiment,
not a measured gain for this update. Its supporting kernels and numerical
assumptions need their own review rather than copying the dispatch change.

The October 6 isolation follow-up changes the interpretation of the refresh
hold. Unpatched `d7a695ef679138c13d86359b84c1731d36213d32` passes with
resident embeddings. Adding only the explicit reader reproduces the logit
failure, while recreating HIP executable graphs on recapture clears the
bounded test without disabling ordinary replay. This is a reader/runtime
interaction, not evidence that MTP or every new upstream build is broken.
The resulting narrowed workaround is now listed in the patch ledger. The
[isolation record](hardware-acceptance.md#fedora-44-strix-halo-flash-next-state-isolation-2026-10-06)
preserves the original controls and limits of that diagnosis.

The upstream review also distinguishes closed proposals from merged fixes.
[PR 25863](https://github.com/ggml-org/llama.cpp/pull/25863) and
[PR 28136](https://github.com/ggml-org/llama.cpp/pull/28136) were closed without
merging. The newer direct-reader proposal,
[PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030), was also closed
without merging. Its replacement,
[PR 29599](https://github.com/ggml-org/llama.cpp/pull/29599), provides
mmap prefetching, not the buffered explicit-read policy accepted on this host.
[QSA gather](https://github.com/ggml-org/llama.cpp/pull/28213) is closed without
merging, while [pooled QSA keys](https://github.com/ggml-org/llama.cpp/pull/28699)
remains open. These statuses were checked on October 14. Neither a closed
proposal nor a similarly named upstream feature is grounds to remove a guard
without checking equivalent behavior. The newer Flash-Next MTP merge is
covered below and does not enable MTP in the managed preset.
The [current refresh acceptance record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-upstream-update-2026-10-14)
contains the paired ROCm/Vulkan measurements, populated-context checks and
hardware-coverage limits.

The October 4 refresh attempt at `46847e61582097979f539595d893d83d8e1d1af1`
was **held** and is not the current production pin. Upstream replaced the QSA
cache/indexer implementation and reduced indexer-score workspace. The
rebased candidate failed the populated clear-and-restore logit gate even
with upstream attention. Model-free operator tests and short API success
are insufficient to accept it. The
[native-engine trial record](hardware-acceptance.md#fedora-44-strix-halo-native-engine-trials-2026-10-04)
preserves the failure, diagnostics and private source archive.
Do not drop downstream safeguards or silently disable all graphs
to make this refresh pass.

The October 6 retry at `6753a033f058fbf778d282556ed9b16c78de7c71`
was also **held**. It included the newer pooled-cache scatter and graph-shape
fixes, but the eight-patch screening build still changed logits after
clear-and-restore with HIP graphs enabled. Disabling graphs passed the
fixture and remains a diagnostic, not an accepted workaround. See the
[retry record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-refresh-retry-2026-10-06).

Flash-Next MTP is now upstream through
[PR 29761](https://github.com/ggml-org/llama.cpp/pull/29761), merged on
October 1. It supersedes the closed, unmerged PR 28243. At Unsloth revision
`766911a6b7369840a91dbcd95f9f997acaab6cd6`, all four managed UD-Q4_K_XL
target shards still have their existing sizes and hashes. Only the three
self-contained MTP files changed, with a duplicate Q8 head also added at the
repository root. The shared heads did not change. Do not follow the stale
MTP README's fork-only requirement or shared-head recommendation as evidence
of current mainline compatibility.

The candidate self-contained Q8 head has a corrected draft-layer compression
ratio in its GGUF metadata. Header inspection is not inference acceptance.
The source refresh does not accept that draft or enable Flash-Next MTP.
It still needs a separate content review, populated-context correctness and
paired performance test. The existing target GGUFs need no new download.

#### Earlier extraction provenance

The following `r40` through `r48` notes describe earlier implementations and
their evidence. The current patch ledger above is authoritative. In particular,
the private prepared-key cache, prefix checks and selection controls described
below were removed in `r50` and must not be added to current deployments.

The `r44` selection patch builds on the four earlier extractions. The graph
already selected whole blocks, but its ordinary gather, sort, cast and
validity operations still ran separately. The new kernel combines those
operations without changing which blocks or tail cells are selected. It
recognizes the model's sliced-tail view/reshape as well as direct and
scheduler-copied inputs. A model marker, exact shapes, input/output types,
reader checks and physical-range guards constrain dispatch. Unsupported
graphs use the existing operations. All exposed intermediate outputs remain
materialized and live through the fusion.

`PARACETAMOL_QWEN4EXP_QSA_SELECTION=0` inside the container disables both
the GPU fusion and the equivalent CPU singleton check for a same-image
control. The CPU check still rejects shared, foreign, gapped or non-scalar
prefixes. It adds no persistent state. The broader QSA graph policy remains
the prerequisite, and there is no change to model pins, F16 KV, F32 residuals,
matrix arithmetic, sampling or context limits.

The downstream BF16/F16 matrix experiment introduced in `r45` was removed
entirely in `r46`, including its kernels, flag and config setting. The small
quality screen did not show a regression, but it did not establish broad
quality parity either. The supported image retains the precision-preserving
extractions with no user opt-in. Remove `--qwen4exp-math` from any saved
commands and `qwen4exp_math` from `[gateway.llama-cpp]` if you copied either
from the experimental version. Both are rejected rather than ignored.
The retired measurements in
[hardware acceptance](hardware-acceptance.md#fedora-44-strix-halo-retired-mixed-arithmetic-experiment-2026-09-30)
are historical evidence, not performance claims for the current image.

The `r43` pipeline patch builds on those three extractions. It adapts direct
GDN and PLE convolution, HC_POST normalization and injection, narrow RMS/gate fusion,
compact expert tiles, expert-ID histogram routing, vectorized weighted
reduction, grouped Q8 decode projections and register-prefetched Q8 matrix
tiles from the same pinned MIT Strix fork. Every fast path requires Qwen4exp
markers or hints and exact `gfx1151`. Unsupported geometry or extra readers
keep the upstream path. Fusion matching protects aliases and allocation
lifetimes. Normalization and expert reductions retain their original F32 sum
order. The fused injection projection uses a tested parallel F32 reduction,
not a lower-precision matrix path. Explicit matrix source precision takes
precedence over the custom Q8 paths.

The patch is `hip-qwen4exp-pipeline.patch`. Its removal gate is an upstream
equivalent retaining operator and fallback checks, populated long-context
retrieval, state restore and interruption, and older-model/backend acceptance.
The Q8 prefetch escape hatch is `PARACETAMOL_QWEN4EXP_MMQ_PREFETCH=0` inside
the container. `PARACETAMOL_QWEN4EXP_HC_INJECT=0` disables the injection
fusion and its graph reordering. The broader graph opt-out disables the pipeline by
withholding model hints and fusion dispatch. Do not fold BF16/F16 matrix
experiments, shadow weights or reduced-precision residuals into this patch.

The `r42` prepared-key cache keeps the first two extractions intact. It adopts
the fork's reuse idea with a smaller local state contract, not its physical-cell
cache or fast-prefill machinery. Only a verified append of one token reuses
keys. The last complete block and tail sentinel are refreshed with the same
ordered F32 operations. Prefills keep their original graph without repeated
copies into derived storage. The first decode then prepares all keys normally
and seeds the cache. The cache is excluded from saved sessions and invalidated on
restore. It adds about 384 MiB at the current Flash-Next 256K ceiling, with no
change to the GGUF, F16 KV, residual precision, samplers or context limit.

The September 29 `r41` follow-up keeps the first selected-key patch intact.
It adds a narrowly scoped extraction from the same pinned Strix fork, not
another engine or a replacement source tree. Compact selection uses a budget
of complete blocks plus the partial tail, matching the model reference rather
than taking a fixed number of independently expanded token scores. F16 K/V,
stored GGUF weights, F32 residuals, sampling and allocation policy are unchanged.
That `r41` extraction excludes the fork's BF16 matrix/residual paths, persistent
pooled-key cache and global kernel tuning. See the [hardware record](hardware-acceptance.md)
for measured gains and the remaining performance gap.
The compact graph also requires at most 262140 padded KV cells because the
prefill kernel reserves a 16-bit block sentinel. A larger padded cache keeps
the ordinary graph rather than overflowing that representation.

The September 29 `r40` image keeps the llama.cpp pin and model presets fixed.
It adds only the gfx1151 HIP selected-key attention path for Qwen4exp and
does not import the fork's broader indexer, cache, or model-loading changes.
The scorer-strip variant was measured separately and discarded because its
129K result was indistinguishable from the kernel-only result. Recheck this
patch against upstream QSA development during the next llama.cpp refresh.

The September 25 `r39` image keeps llama.cpp pinned at `7ab4ee7` and adds
only the QSA gather patch. No model, quantization, sampling, context or MTP
default changes. The matched Strix Halo test uses the same image with the
gather path enabled and disabled; see the
[hardware record](hardware-acceptance.md#fedora-44-strix-halo-flash-next-qsa-gather-2026-09-25).
The larger direct-reader rewrite in
[PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030) was then under
active review and changed lazy-read semantics beyond this model. The pooled
QSA key cache in [PR 28699](https://github.com/ggml-org/llama.cpp/pull/28699)
changes persistent inference state. Neither is included in `r39`, and
Flash-Next MTP remains disabled.

The September 22 update to
`7ab4ee7baad2d920464cbacfad4f4b07cf111fd2`, packaging revision `r38`, reviews
186 upstream commits. Five patches apply unchanged. The reasoning patch
only moves past new server-process includes, and the tiled GDN test hunk
only follows an upstream test-class rename. Neither changes its protected
behavior, and all seven patches remain.
The [Strix Halo acceptance record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-7ab4ee7-update-2026-09-22)
includes the paired ROCm/Vulkan results, the small Q8 long-prefill slowdown,
Flash-Next gains, client checks and deferred hardware coverage.

The range includes Muse's first-tool-call grammar fix in
[PR 29242](https://github.com/ggml-org/llama.cpp/pull/29242), RDNA 3.5 MoE tile
selection in [PR 28935](https://github.com/ggml-org/llama.cpp/pull/28935),
MTP graph reuse in [PR 28549](https://github.com/ggml-org/llama.cpp/pull/28549),
and Qwen4exp hyper-connection and normalization fusions. Sparse Flash
Attention is enabled for Qwen4exp, with a new Vulkan implementation. The
CUDA sparse kernel still explicitly excludes HIP, so its advertised gain
must not be attributed to ROCm. Server child-process monitoring and the
shared JSON-schema representation also change. Recheck router switching,
shutdown, nested tool arguments and managed sampling rather than treating
this as a kernels-only update. This pin is preparation for new models, not
acceptance of unreleased Qwen4 artifacts.

The September 22 external review leaves these proposals outside the pin:

- [PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030) rewrites the
  direct-reader proposal already carried from PR 28136. Its ownership and
  parallel-read design remain under review. A replacement must retain the
  existing EOF, dequantization and per-process policy checks.
- [PR 28243](https://github.com/ggml-org/llama.cpp/pull/28243) is the newer
  Flash-Next MTP proposal. Detached draft loading still has outstanding
  review discussion. Target-model acceptance does not enable this draft path.
- [PR 28699](https://github.com/ggml-org/llama.cpp/pull/28699) caches QSA
  pooled keys. It changes persistent inference state and needs isolated
  rollback, prefix-reuse and long-context qualification.
- [PR 28613](https://github.com/ggml-org/llama.cpp/pull/28613) changes RDNA
  3.5 small-batch thresholds, but reviewers report shape-dependent
  regressions. Do not apply its benchmark gains to every model.
- [PR 26419](https://github.com/ggml-org/llama.cpp/pull/26419) now limits
  its head-dimension-256 dispatch change to RDNA 4. That is not a new Strix
  Halo optimization or evidence that its earlier regression disappeared.

The host-buffer PR 25863 was closed without merging. Upstream points to the
conservative integrated-device rollback in PR 28604, already in our previous
pin, and the still-draft scheduler work in PR 27311. This does not satisfy
the ledger's cross-APU removal gate.

The `r37` packaging revision adds the two model-scoped patches without moving
the source pin. `test-native-policy.cpp` runs during image building without a
GPU or model weights. It checks server/CLI preset isolation and 50 direct-reader
reference/EOF cases. CPU startup must also exercise the non-root managed
entrypoint, not only the native binary, so bundled helper permissions are
covered. Flash-Next's ROCm policy is restricted to Strix Halo. Other models'
allocation and reader settings remain unchanged. The [managed integration
follow-up](qwen3.8-flash-next-direct-reader-feasibility.md#managed-rocm-integration)
supersedes the historical Vulkan-only restriction described below.

The September 13 [isolated tiled GDN screen](qwen3.8-tiled-gdn-strix-halo-feasibility.md)
adds one patch without changing the source pin, ROCm, model artifacts or
Flash-Next's managed Vulkan-only policy. Numerical regression cases live in
the same patch. Enable upstream `LLAMA_BUILD_TESTS`, build
`test-backend-ops`, and run `test -b ROCm0 -o
GATED_DELTA_NET,GATED_DELTA_NET_CACHE_FUSION` against the actual ROCm device
when requalifying it. Check the executed case count as well as the exit
status. A normal production build does not run those GPU tests.

The 2026-09-11 update to `8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f`
retains the host-buffer, reasoning-controls and HIP quantized-KV patches
unchanged. The Vulkan F16 patch needs only a rebase of its surrounding shader
table context. None of the four protected behaviors has an accepted upstream
replacement. The 32-commit range includes RDNA Flash Attention tuning in
[PR 28102](https://github.com/ggml-org/llama.cpp/pull/28102), removal of an
unused indexer V allocation in
[PR 28330](https://github.com/ggml-org/llama.cpp/pull/28330), and Vulkan
matrix, MoE and argsort fixes. Speculative-decoding position handling and MTP
cache filtering also change, so retained speculative presets need real
server and router regression checks. Open Flash-Next MTP, gather and pooled
indexer-cache proposals are not included in this pin.
The [September 11 memory-policy screen](hardware-acceptance.md#flash-next-rocm-memory-policy-screen)
also distinguishes the failing normal Flash-Next ROCm path from a coherent
resident-loading experiment. It does not remove the managed Vulkan-only
restriction or change other presets' allocation policy.

The 2026-09-09 update to `6d9c82ea2bb34e277c0664b8dd3434bfb4dcfb27`
rebases the reasoning, quantized-KV, and Vulkan-contiguization patches. The
HIP quantized tile calls now pass upstream's new sparse-attention argument
explicitly as false. The host-buffer patch still applies unchanged. Upstream
[PR 28604](https://github.com/ggml-org/llama.cpp/pull/28604) conservatively
disables the internal integrated-device flag again, which also prevents the
unsafe direct host-buffer computation. The local guard and pinned-allocation
handling remain in place pending the full cross-APU removal gate.

Upstream also enables reasoning-history preservation by default. Both direct
launches and router presets now explicitly apply the catalog's boolean policy
instead of inheriting that default. Qwen3.6 still drops earlier reasoning,
while Qwen3.8 and Muse keep it. This is distinct from the request's thinking
level and from sampling defaults.
The [Strix Halo acceptance record](hardware-acceptance.md#fedora-44-strix-halo-llamacpp-6d9c82e-update-2026-09-09)
includes the fixed ROCm/Vulkan comparison, default Q8 MTP control, API and
client checks, and deferred architecture handoff. The observed Vulkan
Q8_0-cache prefill regression is recorded separately from the F16 improvement.

The 2026-08-14 update from llama.cpp commit `62bf73d` to release `b10430`,
commit `4c1a0af`, initially classified all four then-current patches as
**rebased**. None was replaced upstream. A subsequent native-reasoning audit
replaced `reasoning-effort-budget.patch` with `reasoning-controls.patch`; the
other three retain that update classification. The 81-commit range changed
adjacent server, speculative-decoding, and backend code, including upstream
Vulkan TQ2 support, but did not provide the protected host-buffer, reasoning
transport, quantized-KV, or f16-contiguization behavior. Exact range comparison
and fail-closed patch application passed before the retained patches were
exercised on `gfx1151`. The host-buffer and reasoning changes still track the
open upstream pull requests linked in the ledger. Re-run the removal gates
rather than carrying this classification forward to a later pin by assumption.

The 2026-08-17 update from release `b10430`, commit `4c1a0af`, to release
`b10453`, commit `3cb7ffb`, classified the host-buffer, quantized-KV, and f16
contiguization patches as **unchanged** and the reasoning patch as **rebased
and reduced**. Upstream now parses top-level `reasoning_effort` and its
template-capability layer aliases both native effort and strength names. A
follow-up removed the remaining Maki numeric-budget decoder rather than
guessing native labels from output-window-dependent percentages. The patch
now owns only data-driven sampling defaults; Muse's unsupported off-to-low
fallback is scoped to its reviewed template. The 23-commit range also includes
a quadratic Jinja rendering fix and a server queue redesign; exercise
structured tools, reasoning variants, router concurrency, and latency rather
than treating a successful build as acceptance.

The 2026-08-26 update from release `b10453`, commit `3cb7ffb`, to release
`b10631`, commit `5d5cb4c`, classified `hip-apu-host-buffer.patch` as
**unchanged**, `reasoning-controls.patch` and
`vulkan-f16-kv-contiguize.patch` as **rebased**, and
`quantized-kv-flash-attention.patch` as **partly replaced upstream and
reduced**. Upstream commit `dc72703fc` now provides the Vulkan q8_0
dequantize-and-transpose path, with its own dense-layout, buffer-range,
coopmat2, and Intel gates. Paracetamol removed those duplicate Vulkan hunks
and retains only the distinct HIP tile q8_0/q4_0 path. Upstream commit
`60addddf3` corrects HIP unified-memory reporting but does not replace the
integrated-GPU host-buffer safety patch. The source still has no data-driven
per-reasoning-mode sampling defaults, so the reasoning patch remains and now
uses the bounded `common_json` request API. The f16 Vulkan path now extends
upstream's shared transpose scratch implementation and remains opt-in on
`gfx1151`.

The 2026-08-29 update from release `b10631`, commit `5d5cb4c`, to commit
`c9ca51c1f6b18427cde490c7c7eba11d87a96b2d` retained all four patches
**unchanged**. Each exact patch applied to a clean source tree and the complete
ROCm and Vulkan image build passed. The selected source contains upstream
Qwen4exp target-model support from
[pull request 27742](https://github.com/ggml-org/llama.cpp/pull/27742), which
is required for Qwen3.8 Flash-Next. Its
[separate MTP work](https://github.com/ggml-org/llama.cpp/pull/27836) remains
outside upstream, so the managed Flash-Next preset is intentionally
non-speculative. Recheck that boundary before adding an MTP alias rather than
assuming architecture support also covers the draft heads.

The 2026-09-01 update from commit `c9ca51c` to commit
`0eadefebd3f8f92a86d634a0e5b8fffc9dc792c0` retained all four patches
**unchanged**. Each exact patch applied cleanly to the selected 48-commit
range. Upstream renamed `--tensor-read-lazy` to `--lazy-mode`, so the managed
direct-model and router configuration emit the new spelling while preserving
the same lazy token-embedding policy. This range adds Qwen4exp indexer-head
prompt-processing work, ROCm long-row radix top-k, Strix Halo Vulkan batched
inference tuning, and recurrent-state rollback for Qwen3.8 Flash-Next. The
rollback support removes a target-side speculative-decoding bottleneck, but
does not add a draft-model loader. Flash-Next MTP remains in
[draft pull request 27836](https://github.com/ggml-org/llama.cpp/pull/27836)
and is not part of this pin. Keep base-model ROCm acceptance and MTP acceptance
as separate gates.

At the September 1 pin, Flash-Next generation on `gfx1151` was accepted only
through Vulkan. Ordinary ROCm allocation loads the same shards and can return
short retrieval keys, but meaningful responses become malformed multilingual
text at shallow context.
The failure survives resident and lazy loading, deterministic sampling, one
server slot, the GGUF and managed templates, a clean GPU reset, ROCm 7.14 and
10.0 images, and a build without Paracetamol's llama.cpp patches. Vulkan is
coherent with the same image, GGUF, prompt, and preset. This agrees with
[upstream issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797) and
the independent [Unsloth ROCm report](https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/38).
The later `r37` model-scoped allocation and reader policy has its own
acceptance gate. It does not fix the ordinary ROCm allocation path.
Do not use successful loading, synthetic throughput, or a short-key
retrieval as evidence that the ROCm output path is fixed.

An isolated same-day evaluation rejected open pull request
[#26419](https://github.com/ggml-org/llama.cpp/pull/26419) as a downstream
patch for `gfx1151`. Its head-dimension-256 AMD WMMA Flash Attention path
reduced Qwen3.8 Q8 F16-KV prompt throughput by 5.1% at 4K, 21.7% at 32K, and
32.9% at 128K while leaving decode unchanged. Do not infer Strix Halo benefit
from the pull request's RDNA 4 results. The exact candidate, controls, early-
stop decision, and retest gate are preserved in the
[feasibility snapshot](qwen3.8-rdna-head-dim-256-flash-attention-strix-halo-feasibility.md).

The bundled `muse-glimmer-atem.jinja` derives from Meta's 9,992-byte template
at base-model revision `a4e59da52a7bc87ae7251dd5545c0dd437c44b68`, SHA-256
`cfc67e5f349f37690dfd31ed1f18bc4442a9dd32fe39a648f993cb4eb3cae678`.
Paracetamol's 10,221-byte reviewed derivative has SHA-256
`7802290a21155a7966436866dc34c06293719eeccdace751ded893cb752f39b1`.
It retains Meta's duplicate-directive correction and adds one scoped policy:
because Muse cannot disable reasoning, `enable_thinking=false` renders native
low instead of changing another model through a server-wide fallback. Meta
later repacked the official GGUFs at
revision `43c7eadd41352a299ea8e0a36b3157978dd63596` with this fixed template and
canonical Q4_K filenames. The dynamic target and DFlash candidate at that
revision retained their tensor inventories and byte-identical tensor payloads;
the only changed GGUF metadata key was the chat-template value. Paracetamol
therefore did not replace the behavior-equivalent target and draft merely to
acquire the embedded copy. On upgrades, compare the base-model template, GGUF
metadata, and tensor payload independently, then repeat direct and router
rendering plus a complete structured tool round trip before changing or
removing the override.

The bundled `kat-coder-v2.5.jinja` is byte-for-byte Kwaipilot's template from
base-model revision `3a7d874090df0cd4399401982eca67df2c5a7e82`, SHA-256
`e409e9daee03f51b2612d96f0a253027baec06abd1c2429e184380479662d416`.
The official Bartowski GGUF predates its one-line non-leading-system-message
fix. On an upgrade, test template rendering with a system message after the
first turn, then complete a multi-turn structured tool exchange. Do not infer
that the template's optional historical-thinking support should be enabled:
the 2026-08-12 trial described in the hardware-acceptance record did not
establish a quality benefit from reasoning preservation.

Qwen3.6 and its dedicated template were retired on 2026-10-16. Historical
acceptance records remain available, but there is no current Qwen3.6 policy
to rebase or preserve.

The bundled `qwen3.8.jinja` is an Apache-2.0 adaptation of Qwen's
byte-identical official templates at Qwen3.8-27B revision
`1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0` and Qwen3.8-Flash-Next revision
`de4b8e4d43b917e7706784d8bb445c9af86a3540`. The source template has SHA-256
`c3cf9e34abf4f9e36c2d72165aa9c132d3e2a725b6c2586aaa3a8af9d7a81041`;
the managed template has SHA-256
`eec13618752e4cc957e65efca4c052aac43683bfae884f07299022a1322d0e9a`.
The only prompt-policy change is the omitted-effort fallback from upstream
`xhigh` to native `medium`, with the validation message updated to identify the
managed default. Qwen3.8 supports native low, medium, and xhigh effort plus a
separate off toggle; it does not support high. On an upgrade, compare both the
base-model and selected GGUF templates, render all three levels plus off, and
complete a multi-turn tool exchange before changing the override or default.
The Qwen3.8 presets reference the catalog's `qwen3.8-27b` sampling policy:
thinking requests default to temperature 1.0, top-p 0.95, top-k 20, min-p 0,
presence penalty 0, and repeat penalty 1; off defaults to temperature 0.7,
top-p 0.8, top-k 20, min-p 0, presence penalty 1.5, and repeat penalty 1.
Recheck both official tuples and explicit-request override precedence whenever
the model card, template, catalog policy, or server request parser changes.
Flash-Next uses its separate `qwen3.8-flash-next` policy. It changes thinking
min-p from 0 to 0.05 while retaining the other Qwen3.8 thinking and
non-thinking defaults. Recheck the two families independently.

Huihui's Unsloth-derived abliterated v2 uses that same Flash-Next policy and
template. Keep its preset policy identical to regular Flash-Next apart from
the artifact and bundle identity. Compare every GGUF header and tensor layout,
then run reasoning, tools, history, sampling and long-context checks on the
actual modified weights. The regular model's successful tests do not establish
the derivative's quality. Harness preference for regular Flash-Next at xhigh
is separate from the template's medium fallback and must not leak into raw
HTTP requests that omit reasoning.

The 2026-08-14 community-template candidate at
`froggeric/Qwen-Fixed-Chat-Templates` revision
`9f14778c92c3b5ed3e0738085694c0d3452802dd` has a 19,262-byte
`chat_template.jinja` with SHA-256
`398edf5b5bb802fb6b9c9a8dba670d09f2aaeef6fdcaa0b2ca307265f59f78dc`.
Its model card claims broader local-engine, history, role, tool-argument, and
agent-loop fixes, but the template also introduces control tags, failure
escalation, payload truncation, and other prompt policy. Treat it as a pinned
experimental A/B input rather than an in-place repair or an upstream source of
truth. Reproduce a concrete failure with the managed official adaptation
before evaluating it, and change only the template in that comparison.

The bundled `qwen3-0.6b.jinja` is byte-for-byte the `chat_template` value from
Qwen base-model revision `7e4ae267688d671ddfca3122e4528ee980cf3234`, SHA-256
`a55ee1b1660128b7098723e0abcd92caa0788061051c62d51cbe87d9cf1974d8`.
The unchanged official GGUF predates its safer handling of non-string message
content, reasoning values, and tool responses. This remains a small smoke
model rather than a managed coding agent. On an upgrade, render both ordinary
text messages and the hardened non-string history before removing the
override.

The September 22 content audit found no changed bytes in the selected Qwen
GGUFs, including all four Qwen3.6 artifacts, both Qwen3.8 Dynamic Quants and
all four Flash-Next shards. The selected Gemma, KAT, translation, Shisa and
DeepSeek target/support files are also unchanged. Muse's current filenames
still point to the already-reviewed template-only repack described above.
Its base template and the official Qwen3.8 templates have not changed.
Keep the existing immutable artifact revisions and managed overrides rather
than invalidating receipts for newer repository metadata alone.

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
   and both `LLAMA_BUILD_UI` and `LLAMA_USE_PREBUILT_UI` disabled unless a
   separately pinned UI supply chain is deliberately added.
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

The [2026-08-23 Qwen3.8 DFlash2 Strix Halo
snapshot](qwen3.8-dflash2-strix-halo-feasibility.md) deliberately left the
then-open DFlash2 implementation out of the source pin and catalog. Its
Q4_K_M draft produced a useful near-256K Vulkan result, but ROCm draft
acceptance collapsed with both Q4_K_M and Q8_0. A future llama.cpp update that
merges or materially changes DFlash2 is therefore a retest trigger, not
permission to integrate it. Follow the snapshot's backend, context, quality,
and fail-closed promotion gate before adding a managed preset.

## Upgrade DwarfStar

DwarfStar is a native multi-architecture application and must stay on the
project `ROCM_VERSION`. Its source pin, image tag, runtime policy, verified
model, and acceptance case form one reviewed compatibility unit:

```text
DWARFSTAR_COMMIT
ROCM_VERSION
the dwarfstar image in internal/application/registry.go
dwarfstar-antirez-deepseek-v4-flash-0731-q2-imatrix
dwarfstar-antirez-deepseek-v4-flash-0731-q2-imatrix-dspark
applications/dwarfstar/entrypoint.sh
```

For an upstream source update:

1. Resolve and review a full `antirez/ds4` commit. Inspect the complete diff
   from the current pin, with particular attention to ROCm kernels, GGUF
   compatibility, server request rendering, cache behavior, model aliases,
   thinking controls, shutdown, and signal handling.
2. Check current open upstream ROCm work. The selected pin may intentionally
   include a reviewed commit that is newer than upstream's default branch
   when a demonstrated Strix Halo decode regression is still being fixed.
   Keep the exact commit identity visible rather than replacing it with a
   branch name or release label.
3. Keep the local build on `native-rocm-sdk`, the canonical `gfx1150`,
   `gfx1151`, `gfx1200`, and `gfx1201` target set, and the common
   `ROCM_VERSION`. Preserve the modular-runtime RPATH and do not add upstream
   setup scripts or prebuilt runtime binaries. Recheck
   `multiarch-wmma-fallback.patch`: it must still fail closed, retain the
   optimized direct WMMA kernel only on `gfx11`, and route RDNA 4 through the
   existing generic Q8 batch path. Remove it when upstream owns that device
   selection.
   Recheck `glm-tool-argument-types.patch`, pinned from upstream PR 1016 at
   `9db96f0e96928e2245664b83e0498145e86a4c25`. It must apply exactly and
   preserve schema-declared GLM arguments in buffered and streamed responses
   without changing DeepSeek DSML handling. Keep the build-time model-free
   `ds4_test --server` gate. Remove the patch when the selected upstream source
   owns the correction, then rerun typed arguments, malformed literals,
   string preservation, tool-result replay, and DeepSeek ordinary/DSpark
   acceptance. A parser fix alone does not establish a new managed model.
4. Review the supported command surface. Paracetamol currently retains only
   `ds4`, `ds4-server`, and `ds4-bench`, and exposes only server and CLI mode.
   Do not inherit new upstream flags by forwarding arbitrary arguments.
5. Update the short commit and policy revision in the image tag. Update the
   third-party notice and the built-in guide when behavior changes.
6. Build `dwarfstar --no-layer-cache`. Run `pip check`, help for every retained
   binary, and `ldd`; verify that the final image has no GCC build toolchain,
   Git checkout, ROCm development wheel, PyTorch payload, or extra DwarfStar
   executables. The minimal ROCm core wheel itself includes its HIP compiler
   driver and LLVM runtime, so their presence is not evidence that the
   development closure leaked into the image.
   The current source directly links rocBLAS as well as hipBLAS and hipBLASLt.
   Retain the upstream MIT notice, which includes the vendored ggml authors,
   and the separate `third_party/iris/LICENSE` in the final image.
7. Re-run
   `content install dwarfstar flash-0731-q2-imatrix --dry-run`. Change the
   model pin only after reviewing the exact replacement model card, license,
   byte size, SHA-256, filename, architecture, and compatibility with the
   selected DwarfStar source.
8. Run the automated DwarfStar acceptance case explicitly on every
   memory-capable target architecture, followed by the manual 128K server,
   thinking, direct-answer, multi-turn cache, long decode, and interruption
   checks in `hardware-acceptance.md`. A successful multi-architecture build
   is not GPU acceptance.

The 2026-08-17 source update from `d250a7c` to `84cc882` reviewed the complete
112-commit range. It retained Paracetamol's three-binary surface and existing
multi-architecture WMMA fallback while incorporating upstream parser and
server hardening, Flash 0731 fixture/version handling, DeepSeek ROCm attention
and indexer work, and the final ROCm DSpark implementation. The follow-up
integration pins the separate 0731 support GGUF and exposes only the exact
managed target/support pair through `--dspark`. Revalidate temperature-zero
sampling, target/support compatibility, output quality, throughput, memory,
and kernel logs whenever the source or either artifact changes.

The 2026-09-09 update to `6289c516273979173abbc062209a81dd3706b804`
incorporates upstream ROCm prefill and DSpark work, usable-pinned-memory
allocation checks, and server/tool parser fixes. The multi-architecture
fallback now guards the renamed 8-wave and 16-wave Q8 row-tile kernels.
Version-specific hipBLASLt/rocBLAS solutions remain upstream-gated rather
than being forced for an unknown library build. DSpark stays opt-in and its
managed contract stays at temperature zero. Upstream's opportunistic sampled
DSpark measurements are not evidence of identical sampling behavior.
The support path is passed through `--mtp-model`. Upstream now uses bare
`--mtp` for an embedded GLM drafter, not for a DeepSeek support filename.
The [Strix Halo acceptance record](hardware-acceptance.md#fedora-44-strix-halo-dwarfstar-6289c51-update-2026-09-09)
separates ordinary and DSpark performance from protocol checks and deferred
hardware coverage. This update does not add an unreleased model family.

The 2026-10-04 update to `0aaea5a238fb41a35106a551e73c8409dfb751ac`
reviews the next 80 upstream commits. The ROCm kernel files are unchanged,
while shared server, cache routing, reasoning and tool-rendering code moves
forward. The WMMA fallback still applies unchanged. PR 1016 remains open at
the same head, so the GLM type correction is rebased rather than removed.
Upstream now retains per-property schemas for Qwen, but does not own the GLM
correction. The shared syntax dispatcher must pass the request's schema
orders through to both parsers. Upstream's tool replay test helper already
forwards those schemas and no longer needs a patch hunk. The build-time
model-free server test gate is retained.

This source includes Qwen3.8 Flash-Next and DeepSeek V4.1 implementations,
but explicitly refuses those models on ROCm. Their Metal/CUDA support does
not expand Paracetamol's managed DwarfStar model set. The existing DeepSeek
V4 Flash 0731 artifacts, DSpark opt-in, temperature-zero DSpark contract and
three-binary image remain unchanged. See the
[October 4 acceptance record](hardware-acceptance.md#fedora-44-strix-halo-dwarfstar-0aaea5a-update-2026-10-04)
for measured coverage and the other-architecture handoff.

Arbitrary MTP support, multi-GPU, distributed execution, SSD streaming,
evaluation, and the upstream native agent remain outside this procedure until
Paracetamol deliberately adopts one of those surfaces.

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

Build the current ROCm base as a temporary resolver environment:

```bash
podman build --target rocm-base \
  --tag localhost/paracetamol-maintenance:rocm-base .
```

Normal Paracetamol builds tag this target using `ROCM_BASE_IMAGE` from
`internal/config/config.go`, then build application targets from that local
image with pulling disabled. When changing Ubuntu, ROCm, PyTorch, or the base
dependency set, update the managed base tag so its visible version remains
honest.

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
`internal/config/config.go`, and build:

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
hf download REPOSITORY --revision COMMIT --local-dir DIRECTORY \
  --include PATH [--include PATH ...]
```

Confirm that the new CLI still supports:

- exact revisions;
- resumable local-directory downloads;
- repeated `--include` filters;
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

Default image tags are an internal cache/version boundary. Change them when a
material base, application source, or application dependency pin changes. A
short `rN` suffix is the packaging revision for dependency or image-assembly
changes made without changing the upstream application revision:

```go
"comfyui": {
    ID: "comfyui",
    Image: "localhost/paracetamol:comfyui-ubuntuX.Y-rocmX.Y-COMFY_VERSION",
    // Keep the remaining established fields unchanged.
},
```

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
