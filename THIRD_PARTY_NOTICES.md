# Third-party notices

ROCmplete's own source code is licensed under BSD-3-Clause. A locally built
container and content downloaded by `rocmplete content install` contain
independent third-party components under their own licenses.

ROCmplete does not distribute model weights or a prebuilt ROCmplete image.
Model artifacts are downloaded directly from the repositories and immutable
revisions recorded in `catalog/catalog.json`.

## Direct components

- ComfyUI is pinned to an exact source revision and is licensed under
  GPL-3.0. Its source and license are present at `/opt/ComfyUI` in the locally
  built image.
- ComfyUI Manager 4.2.2 is installed from the exact requirement declared by
  the pinned ComfyUI source and is licensed under GPL-3.0. It is disabled
  unless the user passes ComfyUI's `--enable-manager` flag.
- ComfyUI workflow templates are licensed under MIT. Derived installed
  workflows carry their source, modification status, and license notice in
  `extra.rocmplete`. Managed benchmark graphs are mechanically exported from
  those same rendered templates.
- ComfyUI-GGUF is pinned to an exact source revision and licensed under
  Apache-2.0. Its source and license remain in the locally built image under
  `/opt/rocmplete/custom_nodes/ComfyUI-GGUF`. The pinned `gguf` package
  declares MIT and `protobuf` declares BSD-3-Clause.
- rgthree-comfy is pinned to an exact source revision and licensed under MIT.
  Its source remains in the locally built image under
  `/opt/rocmplete/custom_nodes/rgthree-comfy`, with its license copied to
  `/usr/share/licenses/rocmplete/rgthree-comfy`.
- llama.cpp is built from the MIT-licensed `ggml-org/llama.cpp` repository at
  commit `3cb7ffb1a1f612d5e4a46244ae5a3c77ad934a70`. The license is installed
  at `/usr/local/share/licenses/rocmplete/llama-cpp/LICENSE`. ROCmplete builds
  the server, CLI, and benchmark binaries locally with RPC and remote UI
  assets disabled. It applies the narrowly scoped host-buffer correction from
  upstream PR 25863 at commit
  `ce82541acbaf5c532c0727d6ccb6de2b0b0c948d` to avoid unsafe direct
  `ROCm_Host` computation on integrated HIP devices while preserving pinned
  host allocation.
- DwarfStar is built locally from the MIT-licensed `antirez/ds4` repository at
  commit `84cc882352757baf628a1776badf7cc54d584e28`. The final image keeps only
  its CLI, HTTP server, benchmark binary, and license. The managed DeepSeek V4
  Flash 0731 IQ2XXS GGUF is downloaded separately from
  `antirez/deepseek-v4-gguf` revision
  `1cd7b564460821938add0475a60b942c409295e0`, which declares MIT. ROCmplete
  records its exact size and SHA-256 and does not redistribute it. The
  optional DSpark support GGUF comes from the same MIT-licensed repository at
  revision `86bb38ce2ba7a98ab0e550359fec5f48859dc723` and is independently pinned
  by exact size and SHA-256.
- Pi Coding Agent 0.84.2 is installed on explicit request from the
  MIT-licensed `@earendil-works/pi-coding-agent` npm package. ROCmplete pins
  the package and its transitive npm tree with registry integrity values in
  `agent-clients/pi/package-lock.json`; it does not redistribute the installed
  packages. Their package metadata remains in the managed runtime tree.
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
- Managed Qwen3.6 27B and Qwen3.6 35B-A3B GGUF conversions are downloaded
  from full pinned Unsloth revisions. Each pinned model card explicitly
  declares Apache-2.0 and links its corresponding Qwen source-model license.
  Their four embedded templates are byte-identical.
  The llama.cpp image includes an Apache-2.0 adaptation of that template which
  retains later system and developer messages and omits empty historical
  reasoning blocks. ROCmplete records every GGUF shard's exact size and
  SHA-256 and does not redistribute those weights.
- The managed Qwen3.8 27B Dynamic Q8_K_XL and Q4_K_M GGUFs are downloaded
  from one full pinned Unsloth revision whose model card declares Apache-2.0.
  ROCmplete records each file's exact size and SHA-256 and does not
  redistribute the weights.
  The llama.cpp image includes an Apache-2.0 adaptation of Qwen's chat template
  from base-model revision
  `1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0`; it changes the omitted-effort
  fallback from native xhigh to native medium.
- KAT-Coder V2.5 Dev Q8_0 is downloaded from one full pinned Bartowski GGUF
  revision derived from Kwaipilot's public text-only checkpoint. The
  conversion repository declares Apache-2.0 and records its upstream model
  lineage. ROCmplete records the exact size and SHA-256 and does not
  redistribute the model. The llama.cpp image includes Kwaipilot's Apache-2.0
  chat template from base-model revision
  `3a7d874090df0cd4399401982eca67df2c5a7e82`, which accepts non-leading system
  messages instead of rejecting the agent conversation.
- The managed Gemma 4 31B IT Q8_0 target and matching Q8_0 MTP draft are
  downloaded together from one full pinned llama.cpp project revision. The
  repository declares Apache-2.0. ROCmplete records both files' exact sizes
  and SHA-256 hashes and does not redistribute them.
- Muse Glimmer 30B dynamic K-quant and its DFlash k-quant draft are downloaded
  together from one full pinned Meta GGUF revision. The repository declares
  Apache-2.0. ROCmplete records both files' exact sizes and SHA-256 hashes and
  does not redistribute them. The llama.cpp image includes Meta's Apache-2.0
  Muse Glimmer ATEM chat template from base-model revision
  `a4e59da52a7bc87ae7251dd5545c0dd437c44b68` so the unchanged GGUF receives
  Meta's later duplicate-reasoning-directive correction.
- Tencent's official HY-MT1.5 7B Q8_0 GGUF is downloaded from one full pinned
  revision under the Tencent HY Community License. That license excludes the
  EU, United Kingdom, and South Korea from its territory. ROCmplete requires
  explicit terms acceptance and does not redistribute the weights.
- TranslateGemma 27B Q8_0 is downloaded from one full pinned
  `mradermacher/translategemma-27b-it-GGUF` revision. The repository declares
  the Gemma license and identifies Google's TranslateGemma 27B instruction
  model as its base. ROCmplete requires acceptance of the Gemma terms, adds a
  string-only Gemma turn adapter for manually prompted text translation, and
  does not redistribute the weights.
- Shisa V2.1 Llama 3.3 70B Q8_0 is downloaded from one full pinned
  `mradermacher/shisa-v2.1-llama3.3-70b-GGUF` revision. The conversion
  repository declares the Llama 3.3 license and identifies Shisa V2.1 Llama
  3.3 70B as its base. ROCmplete records the exact size and SHA-256, requires
  acceptance of the Llama 3.3 Community License Agreement, and does not
  redistribute the weights.
- lightx2v's Wan 2.2 Lightning LoRAs declare Apache-2.0. The Comfy-Org
  FP8/FP16 repack used by optional ComfyUI Wan bundles does not currently
  declare a license. ROCmplete records it as `NOASSERTION`, retains its
  Wan-AI upstream lineage, does not redistribute it, and requires explicit
  risk acknowledgment before downloading it.
- LTX-2 artifacts from Lightricks use the LTX-2 Community License. The selected
  Gemma-derived text encoder is also subject to the Gemma terms; the Comfy
  conversion declares no license and is recorded as `NOASSERTION`.
- HunyuanVideo 1.5 uses Tencent's HunyuanVideo 1.5 Community License, which
  defines territorial and use restrictions. The selected Comfy conversion
  declares no license and is recorded as `NOASSERTION`.
- Krea 2 Turbo is downloaded from Comfy-Org's exact hosted revision under the
  KREA 2 Community License Agreement. ROCmplete presents the pinned agreement
  URL and requires explicit acceptance before downloading it.

The catalog is the authoritative per-artifact provenance record, including
repository, revision, path, size, SHA-256, license status, and upstream
lineage.

AMD ROCm™ is a trademark of Advanced Micro Devices, Inc. ROCmplete is an
independent project and is not affiliated with or endorsed by AMD.
