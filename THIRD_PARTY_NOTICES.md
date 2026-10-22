# Third-party notices

Paracetamol's own source code is licensed under BSD-3-Clause. A locally built
container and content downloaded by `paracetamol content install` contain
independent third-party components under their own licenses.

Paracetamol does not distribute model weights or a prebuilt Paracetamol image.
Model artifacts are downloaded directly from the repositories and immutable
revisions recorded in `catalog/catalog.json`.

## Direct components

- The host gateway uses `modernc.org/sqlite` v1.60.1, under BSD-3-Clause,
  for local usage statistics. It embeds SQLite 3.53.4, which is public domain,
  without depending on system SQLite or CGO. Module versions and checksums
  are pinned in `go.mod` and `go.sum`. The driver license and its full upstream
  third-party notices are retained in `licenses/modernc-sqlite/`. That upstream
  notice distinguishes linked components from test and build-tool dependencies.
  Paracetamol does not import the optional vector-search or VFS extensions.
- ComfyUI is pinned to an exact source revision and is licensed under
  GPL-3.0. Its source and license are present at `/opt/ComfyUI` in the locally
  built image.
- ComfyUI Manager 4.2.2 is installed from the exact requirement declared by
  the pinned ComfyUI source and is licensed under GPL-3.0. It is disabled
  unless the user passes ComfyUI's `--enable-manager` flag.
- ComfyUI workflow templates are licensed under MIT. Derived installed
  workflows carry their source, modification status, and license notice in
  `extra.paracetamol`. Managed benchmark graphs are mechanically exported from
  those same rendered templates.
- ComfyUI-GGUF is pinned to an exact source revision and licensed under
  Apache-2.0. Its source and license remain in the locally built image under
  `/opt/paracetamol/custom_nodes/ComfyUI-GGUF`. The pinned `gguf` package
  declares MIT and `protobuf` declares BSD-3-Clause.
- rgthree-comfy is pinned to an exact source revision and licensed under MIT.
  Its source remains in the locally built image under
  `/opt/paracetamol/custom_nodes/rgthree-comfy`, with its license copied to
  `/usr/share/licenses/paracetamol/rgthree-comfy`.
- llama.cpp is built from the MIT-licensed `ggml-org/llama.cpp` repository at
  commit `e197cef4554d466e0becac20e453a355daba8e5d`. The license is installed
  at `/usr/local/share/licenses/paracetamol/llama-cpp/LICENSE`. Paracetamol builds
  the server, CLI, and benchmark binaries locally. RPC, remote UI assets and
  optional prebuilt llama-connect helper downloads are disabled. It applies
  the narrowly scoped host-buffer correction from
  upstream PR 25863 at commit
  `ce82541acbaf5c532c0727d6ccb6de2b0b0c948d` to avoid unsafe direct
  `ROCm_Host` computation on integrated HIP devices while preserving pinned
  host allocation.
  The router disconnect correction is included upstream through PR 29707 at
  `4e201388c319fc9f747fbe6a666793e2dc2a6d8e`. It closes the child connection
  when the proxy request ends and bounds the response queue's wait across
  unrelated wakeups. No separate router source patch is applied.
  The quantized on-device KV-state copy correction comes from upstream
  PR 30541 at `0e3dbf0f23cf0ba00832a0697909e9eb8b214caf`, also under MIT.
  It counts tensor elements rather than quantization blocks when copying
  saved sequence state. Managed presets still use F16 KV.
  The successful-stream connection reuse correction and its regression test
  come from PR 30492 at `221c0b2e6c815b32b2d629a1830ad04502807ecb`, under MIT.
  Failed writes still terminate the response.
  The top-k MoE allocation-lifetime endpoint correction derives from the
  MIT-licensed Strix fork commit `f6ade7dc76c400067c2918b4132fede4198615cf`.
  Local bounds checks also protect terminal and partial normalization groups.
  A local gfx1151 correction reinstantiates HIP executable graphs after a
  changed capture, while retaining ordinary unchanged graph replay.
  The HIP build also carries the tiled F32 gated-delta-net prefill kernel
  from Piotr Wilkin's MIT-licensed llama.cpp fork at commit
  `964c6f2f0b66008243883298f3f535bc99f5998c`. Paracetamol restricts its dispatch
  to the accepted Strix Halo shape, removes diagnostic logging, and adds
  numerical operator regression cases.
  The explicit Qwen4exp embedding reader derives from llama.cpp PR 28136
  at `c6a9e5c9ae6d6a551217f75c9a04b2e8b1aa62dd`, also under MIT.
  Paracetamol omits its Gemma4 hooks and rejects an unavailable explicit reader
  instead of falling back to mmap. A separate local startup option lets one
  model process opt out of inherited CUDA/HIP managed allocation without
  changing the router parent's policy.
  The gfx1151 selected-key Qwen4exp attention kernels are adapted from the
  MIT-licensed `halo-box/strix-llama.cpp` snapshot
  `7a9196dad8b13b70fb3420886aadaf98fc09b849`. Paracetamol narrows their
  dispatch to the accepted F16 shape and leaves other models on the existing
  attention path.
  Local canonical sorting and deduplication keep selected-key attention
  independent of upstream top-k output order. A smaller local graph patch
  intersects those IDs with upstream's causal mask, reusing upstream pooled
  keys and visibility filtering. It replaces the former private prefix,
  derived-key-cache and selection machinery. The separate generic QSA gather
  patch is retired in favor of the backend attention paths.
  The scoped Q8 matvec prefetch and small MoE tiles remain in the pipeline
  extraction. They use existing upstream quantized kernels rather than the
  fork's global tile table changes. Dispatch stays limited to reviewed
  Qwen4exp graphs on exact `gfx1151`. The fork's BF16 shadow weights,
  residual downcasts, and physical-cell pooled-key cache are not included.
  The pipeline extraction adapts the same snapshot's direct convolutions,
  grouped Q8 decode launches, compact expert routing, vectorized reduction,
  narrow normalization and register-prefetched Q8 matrix tiles. Its combined
  hyper-connection normalization targets upstream's HC operation and retains
  its F32 reduction order. The fused injection uses an ordinary parallel F32
  dot product and preserves the public residual labels used by control vectors
  and imatrix. This patch does not include the separate BF16 or
  F16 matrix experiments, shadow weights or reduced-precision residuals.
  The single-token Qwen4exp GDN decode fusion derives from Strix fork
  PR 106 at `9fbe5308fabbc79a53ddee5eadfad74ae0e4710e`, under MIT.
  Paracetamol retains the pinned upstream eight-lane F32 recurrent reduction
  and adds scoped graph, lifetime and alias checks with fallback tests.
  HC gate folding derives from llama.cpp PR 30602 at
  `681ae40b5688343dd1a2c6c44b903ba8d5e5ac68`, also under MIT. It is adapted
  to preserve the existing combined normalization and injection path.
- PyTorch is BSD-3-Clause. ROCm components have component-specific licenses
  supplied with AMD's packages. Python packages retain the license metadata
  installed with their distributions.
- Qwen Image, Qwen Image Edit, and the selected lightx2v acceleration LoRAs
  declare Apache-2.0 at the pinned Hugging Face revisions in the catalog.
- The managed Qwen3 0.6B Q8_0 GGUF is downloaded directly from Qwen's pinned
  official Hugging Face revision and declares Apache-2.0. The llama.cpp image
  includes Qwen's Apache-2.0 chat template from base-model revision
  `7e4ae267688d671ddfca3122e4528ee980cf3234` so the older unchanged GGUF
  receives the later content-type and tool-response hardening.
- The managed Qwen3.8 27B Dynamic Q8_K_XL and Q4_K_XL GGUFs are downloaded
  from full pinned Unsloth revisions whose model cards declare Apache-2.0.
  Paracetamol records each file's exact size and SHA-256 and does not
  redistribute the weights.
  The llama.cpp image includes an Apache-2.0 adaptation of Qwen's chat template
  from base-model revision
  `1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0`; it changes the omitted-effort
  fallback from native xhigh to native medium.
- Qwen3.8 Flash-Next 125B-A6B Dynamic Q4_K_XL is downloaded in four shards
  from one full pinned Unsloth revision.
  That repository declares the Qwen
  Community License in metadata but does not contain the referenced license
  text at the pinned revision. Paracetamol therefore records the conversion
  as `NOASSERTION`, preserves the official Qwen source-model license as
  lineage only, and requires explicit acknowledgment before downloading it.
  The managed Qwen3.8 template also matches Qwen3.8 Flash-Next revision
  `de4b8e4d43b917e7706784d8bb445c9af86a3540` before Paracetamol's common
  medium-default adaptation.
- Huihui Qwen3.8 Flash-Next abliterated v2 uses the four Unsloth-derived
  UD-Q4_K_XL shards from `huihui-ai/Huihui-Qwen3.8-Flash-Next-abliterated-v2-GGUF`
  at `43b4c1a8871afcc27e37bf996a431528b3d957e5`. Its model card explicitly
  distinguishes these from the separate Swift-derived files. The card names
  the Qwen Community License but its linked LICENSE file is absent, so the
  modified conversion remains `NOASSERTION` with an explicit acknowledgment
  gate. The official Qwen license is recorded as lineage only. Every shard
  has its own size and SHA-256 pin. Paracetamol does not redistribute the
  weights, and uses its existing Qwen3.8 template without modification.
- KAT-Coder V2.5 Dev Q8_0 is downloaded from one full pinned Bartowski GGUF
  revision derived from Kwaipilot's public text-only checkpoint. The
  conversion repository declares Apache-2.0 and records its upstream model
  lineage. Paracetamol records the exact size and SHA-256 and does not
  redistribute the model. The llama.cpp image includes Kwaipilot's Apache-2.0
  chat template from base-model revision
  `3a7d874090df0cd4399401982eca67df2c5a7e82`, which accepts non-leading system
  messages instead of rejecting the agent conversation.
- The managed Gemma 4 31B IT Q8_0 target and matching Q8_0 MTP draft are
  downloaded together from one full pinned llama.cpp project revision. The
  repository declares Apache-2.0. Paracetamol records both files' exact sizes
  and SHA-256 hashes and does not redistribute them.
- Muse Glimmer 30B dynamic K-quant and its DFlash k-quant draft are downloaded
  together from one full pinned Meta GGUF revision. The repository declares
  Apache-2.0. Paracetamol records both files' exact sizes and SHA-256 hashes and
  does not redistribute them. The llama.cpp image includes Meta's Apache-2.0
  Muse Glimmer ATEM chat template from base-model revision
  `a4e59da52a7bc87ae7251dd5545c0dd437c44b68` so the unchanged GGUF receives
  Meta's later duplicate-reasoning-directive correction.
- Tencent's official HY-MT1.5 7B Q8_0 GGUF is downloaded from one full pinned
  revision under the Tencent HY Community License. That license excludes the
  EU, United Kingdom, and South Korea from its territory. Paracetamol requires
  explicit terms acceptance and does not redistribute the weights.
- TranslateGemma 27B Q8_0 is downloaded from one full pinned
  `mradermacher/translategemma-27b-it-GGUF` revision. The repository declares
  the Gemma license and identifies Google's TranslateGemma 27B instruction
  model as its base. Paracetamol requires acceptance of the Gemma terms, adds a
  string-only Gemma turn adapter for manually prompted text translation, and
  does not redistribute the weights.
- Shisa V2.1 Llama 3.3 70B Q8_0 is downloaded from one full pinned
  `mradermacher/shisa-v2.1-llama3.3-70b-GGUF` revision. The conversion
  repository declares the Llama 3.3 license and identifies Shisa V2.1 Llama
  3.3 70B as its base. Paracetamol records the exact size and SHA-256, requires
  acceptance of the Llama 3.3 Community License Agreement, and does not
  redistribute the weights.
- lightx2v's Wan 2.2 Lightning LoRAs declare Apache-2.0. The Comfy-Org
  FP8/FP16 repack used by optional ComfyUI Wan bundles does not currently
  declare a license. Paracetamol records it as `NOASSERTION`, retains its
  Wan-AI upstream lineage, does not redistribute it, and requires explicit
  risk acknowledgment before downloading it.
- LTX-2 artifacts from Lightricks use the LTX-2 Community License. The selected
  Gemma-derived text encoder is also subject to the Gemma terms; the Comfy
  conversion declares no license and is recorded as `NOASSERTION`.
- HunyuanVideo 1.5 uses Tencent's HunyuanVideo 1.5 Community License, which
  defines territorial and use restrictions. The selected Comfy conversion
  declares no license and is recorded as `NOASSERTION`.
- Krea 2 Turbo is downloaded from Comfy-Org's exact hosted revision under the
  KREA 2 Community License Agreement. Paracetamol presents the pinned agreement
  URL and requires explicit acceptance before downloading it.

The catalog is the authoritative per-artifact provenance record, including
repository, revision, path, size, SHA-256, license status, and upstream
lineage.

AMD ROCm™ is a trademark of Advanced Micro Devices, Inc. Paracetamol is an
independent project and is not affiliated with or endorsed by AMD.
