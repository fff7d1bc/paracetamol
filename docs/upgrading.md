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
cat agent-clients/pi/package.json
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
| Managed Pi client | `agent-clients/pi/` | exact npm release plus lockfile integrity |

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

## Upgrade the managed Pi client

Pi is a host-side managed client rather than a container application. Change
the exact `@earendil-works/pi-coding-agent` dependency and matching Node.js
minimum in `agent-clients/pi/package.json`, then resolve the complete npm tree
without running package lifecycle scripts:

```bash
cd agent-clients/pi
npm install --package-lock-only --ignore-scripts --no-audit --no-fund
cd ../..
```

Review the complete lockfile change, including package versions, registry
origins, integrity values, licenses, engine requirements, and native optional
dependencies. Update `THIRD_PARTY_NOTICES.md`, user requirements, and exact Pi
versions in maintained evaluation documentation when applicable.

Install into a disposable data directory first. The command requires system
Node.js and npm rather than a Homebrew runtime, stages the locked tree, and
verifies Pi's reported version before activation:

```bash
./paracetamol agent install pi --data-dir /absolute/disposable/data
./paracetamol agent run pi --data-dir /absolute/disposable/data -- --version
```

Then rerun the Pi launcher and package-management tests, install the new pin on
each acceptance host, and perform the reasoning-transport checks documented in
`testing-and-release.md`. A successful npm install or `pi --version` is not a
substitute for one real tool call through the managed llama.cpp server.

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
  localhost/paracetamol:comfyui-ubuntu26.04-rocm10.0-0.28.0-r12 \
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
| `reasoning-controls.patch` | Adds validated model-preset sampling defaults selected after thinking mode is resolved, with explicit request fields taking precedence. Upstream owns direct OpenAI-compatible effort parsing and aliases the native Qwen `reasoning_effort` and Muse `reasoning_strength` names; model-specific fallback behavior stays in the reviewed template rather than this server patch. | Upstream exposes an equivalent data-driven per-mode sampling-default mechanism with explicit-request precedence. |
| `quantized-kv-flash-attention.patch` | Provides the reviewed HIP q8_0/q4_0 tile dequantize-on-load path derived from Nathan Wilson's commit `2a24abc6`. Upstream owns the former Vulkan q8_0 half at commit `dc72703fc`. | Matching upstream HIP code passes the same f16, q8_0, and q4_0 cache, context-depth, performance, and output checks on every applicable hardware class. |
| `vulkan-f16-kv-contiguize.patch` | Adds the environment-gated f16 KV contiguization path derived from commit `b1a10f981`. Paracetamol enables it only for Vulkan on `gfx1151`. | Equivalent upstream behavior retains the measured long-context improvement without shallow-context or output regressions. Do not broaden the profile gate without results from the additional architecture. |
| `hip-strix-halo-tiled-gdn.patch` | Adds the F32 tiled GDN prefill kernel from Piotr Wilkin's commit `964c6f2f0`, narrowed to exact `gfx1151`, 48 value heads, state dimension 128, one sequence and 16 through 32768 operation tokens. Includes CPU-reference shape and snapshot tests. | The selected upstream implementation passes the numerical operator cases, real Qwen27B MTP protocol checks and populated near-256K comparison with equivalent performance. Other GPU architectures need their own acceptance before broadening dispatch. |

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

At this pin, Flash-Next generation on `gfx1151` is accepted only through
Vulkan. ROCm loads the same shards and can return short retrieval keys, but
meaningful responses become malformed multilingual text at shallow context.
The failure survives resident and lazy loading, deterministic sampling, one
server slot, the GGUF and managed templates, a clean GPU reset, ROCm 7.14 and
10.0 images, and a build without Paracetamol's llama.cpp patches. Vulkan is
coherent with the same image, GGUF, prompt, and preset. This agrees with
[upstream issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797) and
the independent [Unsloth ROCm report](https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/38).
Keep the managed preset's closed Vulkan backend list until a later llama.cpp
or ROCm tuple passes meaningful multi-turn and tool-call acceptance on Strix
Halo. Do not use successful loading, synthetic throughput, or a short-key
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
Paracetamol's 10,219-byte reviewed derivative has SHA-256
`4849b801303b351a82dab37107a665410070cd58315fadccd8f5fde02084bd34`.
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

The bundled `qwen3.6.jinja` is an Apache-2.0 adaptation of the byte-identical
8,057-byte templates embedded in all four pinned Unsloth Qwen3.6 GGUFs. The
embedded source has SHA-256
`55d4931433fe502b794226ee7f4d206a6bdd436ac9f80eb7d8ebb4c639f9ea0c`;
the managed template has SHA-256
`ea69920311f2efccf6343675490b27bd22d03787ebb8ccaf6e9101bfeba72898`.
The adaptation renders system and developer messages after the leading pair
instead of silently dropping them, and does not emit a closed empty reasoning
block before a historical tool call. It deliberately retains the embedded
template's default of discarding reasoning from completed turns. The
2026-08-14 A/B record in the Qwen tuning snapshot found no reason to adopt the
community v22 template's additional prompt policy or default reasoning
preservation. On an upgrade, extract and hash every selected GGUF template,
compare Qwen's current base-model template independently, then render later
developer messages and a complete multi-turn tool exchange before updating or
removing the managed adaptation.

The four Qwen3.6 presets also reference catalog-owned sampling policies. Dense
27B thinking defaults to temperature 1.0, top-p 0.95, top-k 20, min-p 0,
presence penalty 0, and repeat penalty 1. Sparse 35B-A3B uses the same tuple
with presence penalty 1.5. Thinking off for either family defaults to
temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence penalty 1.5, and
repeat penalty 1. Qwen's separate temperature-0.6 precise-coding tuple is not
the general server default. Recheck both model cards, both reasoning modes,
and explicit-request precedence whenever the template, catalog policies, or
server request parser changes.

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
dwarfstar-deepseek-v4-flash-0731-q2-imatrix
dwarfstar-deepseek-v4-flash-0731-q2-imatrix-dspark
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
