# Target-hardware acceptance

Paracetamol targets `gfx1201`, `gfx1200`, `gfx1151`, and `gfx1150`, but a
successful build, CPU startup, or unit test is not GPU inference acceptance.
This matrix defines a finite pre-release gate and prevents one convenient
workflow from standing in for the complete target surface.

Keep machine-specific logs, benchmark JSON, generated media, and measurements
outside the source tree. When a row is accepted, record a short durable
summary here with the date, Git commit, image tag, profile, and result
location. Use `PASS`, `FAIL`, `BLOCKED`, or `N/P`; `N/P` requires a documented
hardware-capacity or application-policy reason.

## Field-tested hosts

The following machines have completed useful runtime testing during
development. These observations establish practical coverage but do not mark
the detailed acceptance rows below as `PASS`. Formal results still need the
commit, image IDs, exact commands, and retained result locations described in
this document.

| Host | Architecture | Observed workload scope |
| --- | --- | --- |
| Fedora Kinoite 44, Ryzen AI 9 HX 370, 128 GB DDR5-5600 SODIMM | Strix Point, `gfx1150` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets; DwarfStar generation was about 3.9 tokens/s |
| Fedora Linux 44 (non-OSTree), Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash at 4K and 128K context, including the exact DSpark pair; Qwen3.6 MTP tool protocol; Qwen3.8 Dynamic Q4_K_XL at 128K; Qwen3.8 Flash-Next Dynamic IQ4_XS and Q4_K_XL through Vulkan at 200K; OMP probe and fixed ROCm/Vulkan llama.cpp benchmarks; Muse Glimmer runtime probes; Laguna XS and Ling feasibility controls |
| Ubuntu 26.04, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| SteamOS 3.8, Radeon RX 9070 XT 16 GB | RDNA 4, `gfx1201` | ComfyUI and the Qwen3 0.6B llama.cpp smoke |

### Four llama.cpp request slots on Strix Halo (2026-10-16)

The request-slot delivery starts from controller `bbbf0a07881afa4e58f2a52d189cfb59389207e9`.
It retains llama.cpp `623c16393c7d53a37df8bb69c6ea8ed6e5171f62`, ROCm 10.1,
the existing source patches, model bytes, F16 KV, templates and sampling policies.
The image advances to `llama-cpp-ubuntu26.04-rocm10.1-623c163-r56`.
The final image ID is
`1cef96272e13dbd09891cbecdce98c9dc56129189b584217ec6a48c95037cfc7`.
The initial direct-server acceptance used image
`3d33928ee26a2c5ad71ac08b58ac3149c7c0ff1b67fd9635171cd17584e45179`.
The final entrypoint removes a redundant overridden KV flag and clarifies an
error message, without changing the effective inference settings.

The host is the same Fedora 44, kernel `7.2.10-200.fc44.x86_64`, Ryzen AI Max+
395, 128 GB `gfx1151` machine described below. No host configuration changed.
Results and probe sources are retained outside Git at
`build/four-slots-20261016-tnfTgB/` on that host.

**Four slots share the existing 262144-token pool. They are not four separate
256K allocations.** A preliminary four-separate-cache configuration consumed
too much memory and was stopped by the test's 8 GiB available-memory guard.
It is not the delivered policy. Shared-cache direct acceptance retained at
least 20.96 GiB available RAM, including the near-ceiling test.

The direct ROCm screen used
`unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl`, four slots, the shared
262144-token pool, and batch/microbatch 2048. Short generation requests used
medium reasoning, temperature zero, seed 42 and exactly 256 output tokens.
Loading and a warm-up request were excluded. Aggregate throughput is total
generated tokens divided by the whole request group's wall time.

| Client workers | Total output tokens | Wall time | Aggregate output rate |
| --- | --- | --- | --- |
| 1 | 256 | 10.63 s | 24.09 tokens/s |
| 4 | 1024 | 18.39 s | 55.67 tokens/s |
| 5 | 1280 | 29.79 s | 42.97 tokens/s |

All four workers emitted tokens within 1.02 seconds in the four-client run,
before any finished. With five clients, four started within 0.65 seconds and
the fifth waited about 18.97 seconds for its first output. Four distinct marker
answers passed both before and after the generation rounds. The fixed-output
task measures throughput, not the quality of the deliberately truncated code.
The final image's `--slots 1` control measured 23.92 tokens/s with one client,
compared with 24.09 above. This short screen shows no meaningful single-client
decode penalty, but does not establish equivalence for every workload.
With four clients queued through that one slot, the same 1024 output tokens
took 44.16 seconds, compared with 18.39 seconds with four slots.

Structured tool call/replay passed. Both 32167-token retrievals and the
247165-token retrieval recovered all six reference values, followed by cached
continuations and a successful short fresh prompt. Near-ceiling prefill was
402.77 seconds. Two warmed 256-token continuations took 14.92 and 14.94 seconds,
or 17.15 tokens/s end to end. This is a small acceptance screen, not evidence
that four fully populated 256K conversations fit, nor a universal speedup claim.

The gateway/router path also passed four simultaneous unequal-context
conversations, approximately 5K, 11K, 21K and 32K tokens, with distinct start/end
keys and four cached follow-ups. Four concurrent xhigh arithmetic requests,
structured tool call/replay and stream cancellation followed by a fresh request
passed. Its short four-client generation screen produced 54.34 tokens/s
combined. The generated native router sections and loaded child logs both
confirmed four slots with the unchanged context pool. The gateway model page
reported four configured slots without loading additional models.

Switching through the same router to
`unsloth-qwen3.8-27b-mtp-ud-q8-k-xl` passed the four-marker, repeated generation,
four-client, five-client and post-generation marker checks. This is a
multi-slot MTP compatibility check, not a quantization or reasoning-quality
comparison. DwarfStar's single-request policy is unchanged. Other GPU classes
were not exercised for this delivery.

The final image passed CPU-only router startup, rendered four-slot presets,
`pip check` and clean removal. Host checks, static and normal builds, race
tests, container-policy tests and the real-browser gateway test passed. The
GPU test window produced no kernel journal entries. Test containers and the
foreground gateway were stopped, leaving the host's configuration untouched.

### Strix Halo ROCm 10.1 refresh (2026-10-16)

The [ROCm 10.1.0 release](https://rocm.docs.amd.com/en/docs-10.1.0/about/release-notes.html)
was compared with the accepted ROCm 10.0.0 stack on Fedora 44, kernel
`7.2.10-200.fc44.x86_64`, Ryzen AI Max+ 395, `gfx1151`, and 128 GB LPDDR5X-8000.
The host retained its balanced platform profile, `balance_performance` CPU
preference and 112 GiB TTM ceiling. No host configuration changed.
The controller baseline is `53224f5758d0d607ef2da428184a8011eb6a12b7`.
The delivery revision is the commit adding this record. Application sources
remain llama.cpp `623c16393c7d53a37df8bb69c6ea8ed6e5171f62`, DwarfStar
`0aaea5a238fb41a35106a551e73c8409dfb751ac` and ComfyUI
`700821e1364eaab0e8f21c538a2131719fec57bf`.

This refresh changes the shared runtime and native SDK together. PyTorch
`2.13.0`, torchvision `0.28.0` and torchaudio `2.11.0.2` retain their framework
versions but use AMD's `+rocm10.1.0` wheels. PyTorch reports HIP
`7.16.26385`. The native HIP compiler advances from Clang 23 to Clang 24.
The Ubuntu base digest, application commits, source patches, model bytes,
templates, samplers and managed F16 KV policy remain unchanged.

All six managed build targets completed a cold `build all --no-cache`.
The five ROCm-bearing outputs are:

| Image suffix under `localhost/paracetamol:` | Image ID |
| --- | --- |
| `runtime-ubuntu26.04-rocm10.1-r4` | `a9cedc98cbc532f7a5ce489a409d1515cc6d8af4d90454e9491c52436473bc2c` |
| `base-ubuntu26.04-rocm10.1-torch2.13-r7` | `5f67bee8829cbc689f5053f16db3e36d2cf49ffbae1e2c347c86f5c386c6683f` |
| `comfyui-ubuntu26.04-rocm10.1-0.28.0-r13` | `b4aeb10bb447eabb9daad82048d790fefa74f527167a2288da72e32993bc1084` |
| `llama-cpp-ubuntu26.04-rocm10.1-623c163-r55` | `d2ff4b98665725b35c28c57e32aed7c55d48507c094e31e5b64930f5ca65b48e` |
| `dwarfstar-ubuntu26.04-rocm10.1-0aaea5a-r12` | `5a90a09a197b2e094b2e1041bb86c80fc1a4256dc3a3fee277fbf1241d4af09c` |

Every Python environment passed `pip check`. Both native images retained
device code for `gfx1150`, `gfx1151`, `gfx1200` and `gfx1201`, had no unresolved
dynamic dependencies, and remained free of PyTorch and the build-only SDK.
The base and ComfyUI images passed `doctor` and FP32/FP16 matrix,
convolution and causal-attention GPU checks.

The first custom random-FP16 convolution check exceeded its FP32-reference
tolerance on both ROCm versions with identical reported errors. It was not a
new regression. A replacement fixture using exactly representable binary
fractions passed with zero tolerance on both versions. The original failures
and baseline controls are retained with the results.

The matching llama.cpp builder passed its 50 reader/allocation checks and
seven upstream parser/template test programs. On the GPU, all 989 targeted
QSA, Qwen, gated-delta-net and top-k operator cases passed, as did eight
top-k allocation-lifetime cases. Flash-Next's fast state screen and
real-source causal/restore screen had zero logit difference. The small-model
state-copy fixture passed four cases with zero skips for each of F16, Q8_0
and Q4_0 KV. Another 32 CPU-reference Flash Attention cases passed for Q8_0
and Q4_0 cache layouts, including prefill, permutations and sparse attention.
These optional-cache tests do not change managed cache precision.

#### Flash-Next comparison

The comparison used the unchanged
`unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl`, llama.cpp commit `623c163`,
one ROCm slot, one serial client, 262144-token capacity, F16 KV, Flash
Attention, the direct embedding reader, and batch/microbatch 2048. Requests
explicitly selected medium reasoning, catalog sampling with temperature zero,
and seed 42. Harness defaults remain xhigh.

Loading is excluded. Retrieval requests start without a cached KV prefix,
but the filesystem cache is not flushed. Prefill is the mean of two requests
at the smaller sizes and one near the context ceiling. Warm decode is 512
generated tokens divided by the total wall time of two cached 256-token
requests, after a separate warm-up request.

| Actual retrieval input | ROCm 10.0 prefill | ROCm 10.1 prefill | ROCm 10.0 warm decode | ROCm 10.1 warm decode |
| --- | --- | --- | --- | --- |
| 3667 tokens | 4.82 s | 4.78 s | 23.19 tokens/s | 23.09 tokens/s |
| 32167 tokens | 43.69 s | 43.09 s | 22.21 tokens/s | 22.14 tokens/s |
| 247165 tokens | 393.85 s | 387.85 s | 17.60 tokens/s | 17.57 tokens/s |

Every retrieval recovered all six dispersed values. Prefill was about 1–1.5%
quicker and decode differed by less than 0.5%. This bounded sample supports
effectively unchanged performance, not a substantial speedup claim. Sampled
available host memory stayed above 23.4 GiB during the candidate's Flash-Next
checks.

Off, low, medium and xhigh requests, nested tools, streaming, Responses,
preserved reasoning history, prefix reuse, cancellation recovery and a
two-client queue all passed on the candidate.

#### Other regression checks

Dense `unsloth-qwen3.8-27b-mtp-ud-q8-k-xl` passed the same short protocol,
history, reuse, cancellation and two-client checks. Its paired 32167-token
retrievals recovered all six values on both versions. With four slots sharing
a 262144-token pool, F16 KV, MTP depth three and one serial measurement client,
mean prefill rose from 106.05 to 109.32 seconds, about 3.1% slower. Warm decode
was effectively unchanged at 14.34 versus 14.38 tokens/s by the same wall-time
method above. This small regression belongs in the record even though
Flash-Next is the primary acceptance workload.

CPU-only ComfyUI and llama.cpp router startups reached their HTTP endpoints
with no GPU devices and stopped cleanly. The formal llama.cpp/DwarfStar smoke
suite `20261016T121526Z-4b4160bc` passed. The tiny Qwen3 benchmark and CLI smoke
also passed separately on ROCm and Vulkan.

Muse's DFlash variant passed its thinking and nested-tool checks. The regular
Flash-Next Vulkan path passed both 32K retrievals, cached continuations and a
clean follow-up request. This is a shorter-context Vulkan regression check,
not a repeat of the near-256K ROCm comparison.

Managed Pi 1.2.0 and Maki 0.6.3 both completed real file-read and shell-tool
round trips through the gateway. Neither client received a model or reasoning
override. The gateway observed regular Flash-Next at xhigh and launched the
new image on an ephemeral loopback-only backend port.

DwarfStar passed short HTTP inference, reasoning, a 6.6K input, nested tools,
streaming, Responses, prefix reuse and cancellation recovery with both
ordinary decoding and DSpark. These servers had 131072-token capacity, but
the tests did not populate that entire context or benchmark DSpark speed.

Huihui's optional Flash-Next variant also passed all supported reasoning
modes, a longer input, nested tools, SSE, Responses and preserved-history
rendering on the new ROCm image. This is a short runtime regression check,
not a new coding-quality comparison.

**PASS for the tested Strix Halo inference paths.** No AMDGPU fault, reset,
timeout or OOM event appeared during the test window. The old ROCm 10.0
PyTorch control runs emitted CPU split-lock warnings, retained in the kernel
log. No host tuning was attempted. All owned test containers stopped and
their listeners disappeared.

Evidence is retained under
`~/.local/share/paracetamol/apps/acceptance/results/20261016-rocm101/` on the
acceptance host. It includes cold-build logs, package inventories, source and
patch snapshots, exact image IDs, helper sources, corpus, raw requests and
responses, numerical results, harness transcripts, kernel logs and a SHA-256
manifest. The canonical controller snapshot and delivery patch identify the
tested source independently of the rsynced checkout's older Git metadata.
The previous images remain available as rollback controls.

#### Deferred coverage

`gfx1150`, `gfx1200` and `gfx1201` remain `N/P` for this runtime revision
because those GPUs were unavailable. Retaining their device code is not
runtime acceptance. ComfyUI image and video generation also remain `N/P` on
Strix Halo because the `qwen-image-2512-fp8-lightning` and
`wan-2.2-t2v-14b-fp8-lightning` fixtures were not installed. CPU startup and
PyTorch operations do not replace those generation checks.

For the deferred GPU classes, use this delivery's source and image tags above,
record the locally built image IDs, and follow the
[acceptance matrix](#representative-application-gate). Required content for the
bounded native smoke is `llama-qwen-qwen3-0.6b-q8-0`. After choosing the exact
render node and profile (`strix-point` or `rdna4`),
run the following with a new result directory below `build/`:

```bash
./paracetamol doctor --no-config --render-node "$render_node"
./paracetamol acceptance --no-config --application llama-cpp \
  --profile "$profile" --render-node "$render_node" --non-interactive \
  --output "$result_dir/acceptance.json"
./paracetamol benchmark llama-cpp throughput --no-config \
  --preset qwen-qwen3-0.6b-q8-0 --profile "$profile" \
  --render-node "$render_node" --compare-backends --repetitions 3 \
  --prompt-tokens 512 --generation-tokens 128 --context-depth 0 \
  --batch-size 2048 --ubatch-size 512 --cache-type-k f16 --cache-type-v f16 \
  --flash-attn on --output "$result_dir/native.json"
```

Expect exact device isolation, successful GPU execution on both backends,
clean shutdown and no GPU fault or reset. Archive the JSON, logs and kernel
window with the image IDs. ComfyUI needs its image and video fixtures and the
normal visual-review acceptance pass on each class with sufficient memory.
The large Flash-Next and DwarfStar models are not small-VRAM acceptance
fixtures.

### Flash-Next harness default and Huihui variant (2026-10-16)

**PASS for Huihui's ROCm integration on Strix Halo.** Regular Unsloth
Flash-Next becomes the Pi and Maki default at `xhigh`. Huihui abliterated v2
is an optional Unsloth-derived UD-Q4_K_XL variant, not the Swift-derived
conversion from the same repository. This is runtime acceptance, not a claim
of equal coding quality or a recommendation to replace the regular model.
Qwen3.6 is removed from the active catalog, recipes, clients and image
template set. Existing downloaded files are not deleted.

The controller starts at `08fac22`. The delivery revision is the commit
adding this record. The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-623c163-r54`, ID
`4437224c72818ea3ddc623e0aeee5ac8959dcca7ea05cb5266affb79cefdfc0c`.
Its llama.cpp source and fourteen patches are unchanged from the preceding
acceptance. The image revision removes the unused Qwen3.6 template and its
entrypoint selector. Runtime dependency closure, `pip check` and CPU-only
router startup pass.

All four Huihui shards at revision
`43b4c1a8871afcc27e37bf996a431528b3d957e5` pass full size and SHA-256
verification, totaling 111334654784 bytes. Every complete GGUF header matches
the corresponding regular Unsloth shard, including tokenizer, embedded
template and tensor layout. The first shard is byte-identical, while the
other three payload hashes differ. Separate directories and basenames prevent
mixing the two sets. The missing license text remains `NOASSERTION`, with
explicit risk acknowledgment before download.

Both variants use the same managed `qwen3.8.jinja`, SHA-256
`eec13618752e4cc957e65efca4c052aac43683bfae884f07299022a1322d0e9a`,
sampling policy, native reasoning choices, preserved history, non-MTP
execution, F16 KV and model-scoped ROCm optimizations. A regression test
compares their complete preset policies after excluding artifact identity.
No reduced-precision arithmetic mode or Huihui-specific engine patch is added.

The unchanged Fedora 44 host uses kernel `7.2.10-200.fc44.x86_64`, `gfx1151`,
128 GB LPDDR5X-8000, balanced platform policy and `balance_performance` EPP.
ROCm tests use one 262144-token slot and one serial client. Off, low, medium
and xhigh answers pass, along with a nested tool round trip, SSE, Responses
and preserved-history rendering. Two 32167-token retrievals and one
247165-token retrieval recover all six dispersed values. Both context-depth
suites also pass fixed-length cached continuations and a short clean request.
The long prefill takes 391.52 seconds, with llama.cpp reporting about 17.85
generated tokens/s afterward.
These deliberately truncated continuations are throughput checks, not coding
tasks. Available host memory never falls below 23.88 GiB in the ROCm run.

The Vulkan control also passes all reasoning modes, tools, SSE, Responses,
history and both 32167-token retrievals, followed by cached continuation and
clean-request checks. It uses four slots sharing a 262144-token pool and one
serial client. This accepts the shorter Vulkan protocol path, not near-256K
Vulkan capacity or performance for Huihui.

Managed Pi 1.2.0 and Maki 0.6.3 select regular Flash-Next at xhigh without
model or reasoning arguments and complete actual file-read and shell-tool
tasks. Huihui completes the same task in Pi at off and xhigh and in Maki at
adaptive medium. Pi off produces no thinking blocks. An initial test wrongly
used Maki's ignored `--thinking off` CLI flag and observed medium instead.
The corrected live test exercises adaptive mode. Installed-client transport
tests cover named choices through Maki's supported Lua configuration.

Gateway tests verify both sampling modes, omitted and null defaults, explicit
zero overrides, provenance reporting and rejection of conflicting reasoning
selectors. Omitted template reasoning renders identically to explicit medium,
not xhigh. Harness preference therefore does not change HTTP fallback policy.
The gateway advertises no Qwen3.6 models, starts unloaded and keeps its child
on an ephemeral loopback port.

Host checks pass `make check`, `make test`, `make static`, forced Go rebuild,
all fourteen Pi extension tests, Python compilation, shell syntax, catalog
JSON and full-install dry-run validation. No new kernel GPU, SVM, OOM or
protection-fault event is recorded. Test containers and listeners are removed.
Raw evidence, including the initial Maki test mistake, is retained on the acceptance host at
`~/.local/share/paracetamol/apps/acceptance/results/20261016-flash-next-models/`
with a SHA-256 manifest. Inference on `gfx1150`, `gfx1200` and `gfx1201` is
**NOT PERFORMED** for this variant.

### Strix Halo llama.cpp refresh and optimization screens (2026-10-16)

**PASS for the ROCm refresh on Strix Halo.** The upstream
refresh preserves the existing Flash-Next performance. Neither separately
tested optimization is added. This is a maintenance update, not another large
speedup, and does not change the GGUFs, F16 KV cache or model defaults.

The source advances from `e36df604703dec1b6ce82f214698725475a57035` to
`623c16393c7d53a37df8bb69c6ea8ed6e5171f62`, a 37-commit upstream range.
Twelve patches remain unchanged and two are rebased for upstream's vectorized
F32 RMS normalization. There are still fourteen patches. The cold-built image
is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-623c163-r53`, ID
`96e50090780b754ff60b40802dfe831966f019aa361a777d80a725444fe00051`.
Its actual builder is checked against source tree
`9c9d386f09f349f98fdc08e47a8316f2070e6f12`. The baseline is the accepted
`e36df60-r52` image, ID
`7313e700a594d3dcf43504c86ce8bfc585e8423d5daed77d0884f465e669fc73`.
The controller starts at `7ea1a4c`. The delivery revision is the commit adding
this record.

The host remains Fedora 44, kernel `7.2.10-200.fc44.x86_64`, ROCm 10.0,
`gfx1151`, 128 GB LPDDR5X-8000, balanced platform policy and
`balance_performance` CPU EPP. No host configuration is changed.

Flash-Next uses the same managed Unsloth Dynamic UD-Q4_K_XL shards, non-MTP,
F16 K/V, direct embedding reader and existing QSA policy. The server has one
262144-token slot and one serial client. Requests use medium reasoning and
the catalog sampler tuple, with temperature zero and seed 42 for measurement.
Cold means no reused KV prefix, not a flushed filesystem cache. Loading is
excluded. Old/new/new/old runs reduce ordering bias at both short and long
contexts. Every retrieval recovers the six dispersed exact values.

| Actual input tokens | Old prefill seconds | Updated prefill seconds | Old cached HTTP tokens/s | Updated cached HTTP tokens/s |
| ---: | ---: | ---: | ---: | ---: |
| 3667 | 4.81 | 4.80 | 22.94 | 23.20 |
| 32167 | 43.79 | 43.59 | 22.01 | 22.25 |
| 247165 | 394.39 | 390.74 | 17.51 | 17.56 |

Prefill is the mean across the paired runs. Cached throughput divides 1024
generated tokens by the total wall time of four fully warm requests for each
build. The first continuation reprocesses a larger tail and is excluded.
The fixed 256-token continuations are deliberately truncated timing probes,
not completed coding tasks or a model-quality benchmark. At 247K, individual
prefill times are 397.53 and 391.25 seconds for the baseline, and 395.55 and
385.94 for the update. The small differences should not be advertised as a
substantial acceleration.

The separate cold-built production rerun agrees. Its 4K, 32K and 247K prefill
times are 4.81, 43.73 and 393.29 seconds, with cached HTTP throughput of 23.16,
22.23 and 17.55 tokens/s. All six retrieval values are correct at each depth.

The final dense Qwen27B Q8 MTP control pair gives 107.07 versus 106.09 seconds
of 32K prefill, and 14.44 versus 14.32 cached HTTP tokens/s, old versus new.
Retrieval is correct on both. This small sample shows no useful speed win and
does not establish a material regression. It is separate from the attention
candidate below, which is not shipped.

Two independent candidates remain outside the image:

- Upstream [PR 30493](https://github.com/ggml-org/llama.cpp/pull/30493),
  `4373bb620120b5fe1b811e1c4a202db9d82fa586`, lengthens paired Flash-Next
  32K prefill from 43.07 to 47.12 seconds, with unchanged decode. It is rejected.
  Its 129 focused matrix cases pass, but that does not offset the performance
  loss. The planned dense-model screen is skipped after this rejection.
- An isolated kernel from [Strix fork PR 172](https://github.com/halo-box/strix-llama.cpp/pull/172),
  `1ea2d675d7977c47e3c6952b3fc2ac376ef17b93`, shortens dense Qwen27B Q8 MTP
  32K prefill to 95.45 seconds, between baseline runs of 105.97 and 106.16.
  Decode does not improve. Flash-Next's control is effectively unchanged.
  The 150 focused attention cases pass, but the roughly 1200-line kernel and
  changed attention evaluation order need further numerical and long-context
  qualification. It is deferred, not production-accepted. No whole fork,
  private HIP runtime or reduced-precision matrix mode is imported.

The final production libraries pass 989 HIP operator cases, including 103
added alignment and row-width cases. The exact normalization comparisons stay
exact, and the existing F32 injection-dot CPU-reference tolerance remains
`1e-10`. Shared weight views are visited before both fixture branches so an
incidental view node cannot select different upstream RMS/MUL fusion paths.
All eight allocation-lifetime cases and seven native parser/chat tests pass.
F16, Q8_0 and Q4_0 device state-copy checks each pass four cases with none
skipped. Those optional-cache checks do not alter the F16 presets.

Populated ordinary, causal-transition and clear/restore checks remain exact.
Three independent updated-build runs agree across 65 full-vocabulary
teacher-forced vectors. The cold production image matches the first of those
runs exactly. Disabling only the rebased GDN or narrow-normalization fusion
also matches all 65 vectors exactly.

Whole-model bit identity is not claimed. The unchanged old image itself varies
between processes. Its HC fused/unfused comparison has one top-1 difference
out of 65 vectors, as does the updated image. Worst raw logit NMSE is about
0.133 and 0.124 respectively. These diagnostics do not establish a new quality
regression or prove universal answer equivalence. Preserve the independent
operator references and within-process state gates rather than relaxing them
to accommodate cross-process variation.

The runtime closure has no missing shared libraries, bundled source tree,
compiler or PyTorch payload, and `pip check` passes. CPU-only router startup
has no GPU devices. The tiny Qwen model completes CLI generation on CPU, ROCm
and Vulkan, plus both GPU throughput smokes. Qwen3.8 dense Q8 MTP and Flash-Next
pass reasoning modes, chat, tool round trips, SSE, Responses, preserved-history
template, prefix reuse, cancellation and queued requests. Qwen3.6 dense MTP
passes its chat, reasoning, tool, template and cache checks. Muse DFlash passes
its bounded reasoning and tool checks.

Flash-Next's Vulkan control uses four slots sharing a 262144-token pool and one
serial client. Old/new 4K prefill is 8.22/8.25 seconds and cached HTTP throughput
is 24.37/24.36 tokens/s, with correct retrieval and tool replies. This is a
short-context control, not renewed near-256K Vulkan performance acceptance.

One initial Vulkan mmap load was interrupted after more than sixteen minutes
of continuing model-file page reads. No GPU fault or OOM accompanied it.
The retained old image subsequently loaded in 140.49 seconds and the updated
image in 46.61 seconds, with the same launch settings and no host changes.
The slow load did not reproduce, but its cause is not established. Do not
present those cache-sensitive startup times as an upgrade speedup or claim
that cold-start loading variability has been fixed. The interrupted run and
the successful controls remain in the raw evidence.

The gateway starts unloaded and completes dense-to-Flash-Next-to-dense model
switching. Both omitted sampling defaults and explicit client overrides pass.
Its backend remains on an ephemeral loopback port. All eighteen streaming and
non-streaming disconnect checks release the actual child slot. Managed Pi 1.2.0
and Maki 0.6.3 perform real file-read and shell-tool rounds through the gateway.
Pi's thinking-off run has no reasoning content. The fixture remains unchanged
and shutdown leaves no test container or listener.

Final host checks pass `make check`, `make test`, `make static`, forced Go
rebuild, Python compilation, shell syntax, catalog JSON parsing and whitespace
validation. No new kernel GPU, SVM, OOM or protection-fault event is recorded.
Raw logs, scripts, image/source identities and numerical vectors are retained
outside the checkout at
`~/.local/share/paracetamol/apps/acceptance/results/20261016-llama-upstream/`
on the acceptance host, with a SHA-256 manifest. The machine is left idle. Inference acceptance
on `gfx1150`, `gfx1200` and `gfx1201` remains **NOT PERFORMED**.

### Managed Pi 1.2.0 (2026-10-15)

**PASS for the managed local-model client.** Pi advances from 1.0.2 to 1.2.0.
The reviewed lockfile contains 132 packages, down from 160 after upstream
removed Chord and its esbuild dependency. Registry origins, integrity values,
licenses and Node requirements remain validated. Installation still disables
package lifecycle scripts. The Node minimum remains 22.19.0.

Disposable and normal managed installations succeed on the development host with system
Node 22.22.1 and the acceptance host with system Node 24.18.0. All 14 extension tests pass on
both. The added picker tests exercise the installed Pi components, including
family grouping, deduplication, supported reasoning choices, native search,
cancellation and non-interactive isolation. The reasoning wrapper now forwards
focus and input to the native component instead of bypassing its search box.

Six sandboxed the acceptance host sessions complete actual file-read and shell-tool round
trips through the gateway and the unchanged `e36df60-r51` ROCm llama.cpp image.
Qwen3.8 dense Q8 MTP passes off, low, medium and xhigh. Qwen3.6 dense Q8 MTP
passes off and high. Captured HTTP requests show Qwen3.8 off maps to `none`
and Qwen3.6 uses `enable_thinking: false`, with `preserve_thinking: true`.
Both off cases have no reasoning blocks. Gateway request records confirm
non-thinking sampler defaults, with no sampler overrides from Pi.

An actual TUI checks `/model`, Ctrl+L, family grouping and immediate reasoning
selection after changing models, including an exact `/model PROVIDER/MODEL`
command. Searching for medium selects it successfully. A tool round trip
produces one durable completion-divider entry and exactly one OSC 777
notification at settlement. Print and RPC output remain valid JSON without
terminal control bytes when notifications are enabled.

Sandboxed `web_fetch` retrieves and extracts `https://example.com/` successfully.
No Brave key is configured, so live search is not accepted by this pass. The
search transport and error fixtures pass without a provider account.

No model, sampler, context, engine image or host configuration is changed.
This is bounded client acceptance, not a new quality or performance benchmark,
cloud-provider/MCP acceptance, or acceptance on other GPU classes. Test services
are stopped afterward. Raw local evidence is under ignored
`build/housekeeping-20261015-hGZkrf/` on the two acceptance hosts.

### Strix Halo Flash-Next decode follow-up (2026-10-15)

**PASS on Strix Halo.** The combined patch gives **about 6% faster decode at
4K and 32K, and 5% at populated 247K** over the existing optimized build. Prefill is
effectively unchanged. This is an incremental gain, not the fork's
whole-branch headline, and introduces no reduced-precision matrix arithmetic.

The baseline is the existing thirteen-patch `e36df60-r51` image, rebuilt on
The acceptance host as `a8f8f81b515c`, with source tree
`c22ae0041d26d7380f29169ddaaad9d74f1de818`. The controller starts at `ffc6273`.
The delivered image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-e36df60-r52`, ID
`7313e700a594d3dcf43504c86ce8bfc585e8423d5daed77d0884f465e669fc73`.
Its fourteenth patch combines the two accepted candidates. The source tree
`43f80efb641e51fe73a0e2398ef00ad19c545a10` is checked against the actual
cold builder. The controller revision is the commit adding this record.
The test host remains Fedora 44, kernel `7.2.10-200.fc44.x86_64`, ROCm 10.0,
`gfx1151`, 128 GB LPDDR5X-8000, balanced platform policy and
`balance_performance` CPU EPP. No host configuration or model content changes.

All candidates use the same four Unsloth Dynamic UD-Q4_K_XL shards of
Qwen3.8-Flash-Next, non-MTP, F16 K/V, the direct embedding reader and the
existing QSA graph and kernel policy. The server has one 262144-token slot
and one serial client. Medium requests send the catalog sampler tuple,
overriding only temperature to zero and seed to 42. Model loading is excluded.
Cold means no reused KV prefix, not a flushed OS page cache.

Each short screen runs the baseline and candidates in forward then reverse
order. The retrieval requests contain 3667 or 32167 actual tokens and must
recover six dispersed exact values. Warm throughput divides 1024 generated
tokens by the total wall time of four cached requests across the two runs.
The first continuation of each run reprocesses a larger tail and is excluded.
The 256-token continuations are intentionally truncated timing probes, not
completed coding tasks or a quality leaderboard.

The combined comparison runs baseline, candidate, candidate, baseline. Each
process repeats the short tests and then receives a 247165-token varied
source archive. All four long requests recover the same six values. The
cached continuation retains 247159 tokens and processes four uncached tokens.

| Context screen | Baseline HTTP tokens/s | Combined HTTP tokens/s | Gain |
| --- | ---: | ---: | ---: |
| Shallow, 41 tokens | 21.49 | 22.74 | 5.8% |
| 3667 tokens | 21.63 | 22.95 | 6.1% |
| 32167 tokens | 20.82 | 22.02 | 5.8% |
| 247165 tokens | 16.63 | 17.47 | 5.1% |

Mean cold prefill is 44.42 versus 44.46 seconds at 32K and 396.11 versus
396.12 seconds at 247K. Long retrieval answers use slightly different token
counts, so their complete request times are not treated as an equal-output
speed comparison. The throughput table uses fixed-length generation instead.

| Independent candidate | 32K baseline / candidate HTTP tokens/s | 32K prefill result | Decision from this screen |
| --- | ---: | --- | --- |
| Shared pooled-input view, upstream PR 30613 | 20.82 / 20.87 | About 1% shorter | Wait for upstream, too small to carry separately |
| Upstream HC post-gate fusion, PR 30602 | 20.82 / 21.15 | About 3.6% longer | Do not carry as-is, it displaces the existing larger fusion |
| Parallel HC pre-mix scheduling | 20.79 / 20.85 | Effectively unchanged | Do not carry, decode improvement is about 0.3% |
| Gate folding adapted to the existing HC norm/injection fusion | 20.82 / 21.13 | Effectively unchanged | Included in the combined candidate |
| Scoped GDN decode-chain fusion | 20.79 / 21.72 | 44.28 / 43.91 seconds | Included in the combined candidate |

The independent GDN candidate also improves shallow HTTP throughput from
21.54 to 22.57 tokens/s and 4K warm throughput from 21.60 to 22.63. Its
kernel preserves the current upstream eight-lane recurrent reduction order,
rather than copying the fork's older reduction unchanged. The model,
quantization, context policy and F16 KV cache remain the same.

Cross-process logit controls matter here. The unchanged baseline itself
produces two distinct teacher-forced vector sets in independent processes.
The HC and GDN candidates match a recorded real-source baseline exactly
across 65 full-vocabulary vectors. The pooled-input candidate matches the
other baseline exactly. Those initial cross-build differences therefore do
not establish candidate regressions. The cause of the baseline variation
is not isolated. Within-process ordinary, causal-transition and
clear/restore checks remain exact and are not relaxed.
The final cold image matches the second unchanged real-source baseline
bit-for-bit across all 65 vectors, with no nonfinite values or top-1 changes.

The actual cold-image libraries pass 886 broad HIP operator cases and a
293-case focused suite. The latter includes 220 GDN cases covering key-head
counts, direct and materialized state, in-place updates, reshaped cache roots
and rejected unsafe or externally observed graphs. The prototype's traces
confirm both fusions activate on the real model, not just the test graphs.
All eight top-k allocation-lifetime cases and seven native parser/chat tests
pass. F16, Q8_0 and Q4_0 device state-copy checks each pass four cases with
none skipped. These optional-cache checks do not change the F16 presets.

Runtime dependency closure and `pip check` pass. The final image contains
neither PyTorch nor the development SDK. CPU-only router startup and cleanup
pass, as do actual tiny-model CPU, ROCm and Vulkan CLI runs and both GPU
throughput smoke tests. Building all four HIP targets is not runtime
acceptance on the other architectures.

The uninstrumented production image repeats the Flash-Next service and
context checks successfully. HTTP decode throughput is 22.89 at 4K, 21.92
at 32K and 17.39 at 247K, within 0.5% of the combined prototype. Its 247K
prefill takes 397.86 seconds and retrieves all six values. Off, low, medium
and xhigh requests, nested tool arguments and replay, streaming, native
Responses, historical-reasoning templates, prefix reuse and cancellation
all pass. Thinking-off responses contain no reasoning text.

The default dense Unsloth Qwen3.8 Q8 MTP control passes the same functional
checks and a 32K retrieval/cached-decode comparison on both images. It uses
its normal four unified-KV slots with a 262144-token per-slot ceiling and
one serial client. The eight paired timing/retrieval responses have identical
content and reasoning hashes. Shallow HTTP throughput is 15.78 versus 15.81
tokens/s, cached 32K throughput is 14.44 versus 14.48, and mean 32K prefill
is 106.99 seconds on both. This is no material regression or speedup for the
older model, not a new dense-model 256K acceptance pass.

The real gateway starts unloaded and passes dense → Flash-Next → dense
switchback, missing/explicit sampler checks and structured tool round trips.
All eighteen streaming/non-streaming cancellations leave the actual child
slots idle. Backend ports remain ephemeral loopback-only. Managed Pi 1.2.0
and installed Maki 0.6.3 both perform real file-read and shell-tool calls with
Flash-Next and return the expected fixture values. Pi thinking-off has no
reasoning output. The initial Maki command retained a removed `--no-rtk`
flag, so it exited before inference. The corrected supported invocation
passes without a product-code workaround.

The full-day kernel log shows no GPU faults, resets or OOM events. The acceptance host is
left idle with all test services stopped. Weights, default model, sampling
policy, F16 KV settings, kernel options, memory limits and power policy are
unchanged.
This is bounded numerical and service acceptance, not proof of identical
answers on every prompt or a new coding-quality benchmark. `gfx1150`,
`gfx1200` and `gfx1201` remain **N/P** in this pass because those target
devices were not used. The new dispatch is restricted to exact `gfx1151`.

Raw requests, outputs, numerical controls, scripts, source patches, build
logs and image identities are retained with checksums on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261015-strix-decode/`.

### Fedora 44 Strix Halo llama.cpp upstream follow-up (2026-10-15)

**PASS on Strix Halo.** The update reduces MTP checkpoint storage and removes
one carried patch. Native decode performance is effectively unchanged.
Flash-Next and the default dense Q8 MTP model pass populated near-256K
requests, cached follow-ups and the applicable service checks.

This follow-up advances llama.cpp by 16 commits to
`e36df604703dec1b6ce82f214698725475a57035`. It includes upstream's MTP
partial-checkpoint fix, PR 30514, and Vulkan KV layout implementation,
PR 27703. The overlapping local Vulkan patch and environment switch are
removed. All thirteen remaining patches apply unchanged. The separate
recurrent-state-copy PR 30139 was tested but is not carried.

The controller revision is the commit adding this record. The final patched
tree is `c22ae0041d26d7380f29169ddaaad9d74f1de818`, checked against the actual
cold builder. The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-e36df60-r51`, ID
`aa1dc81155b2a1435173a009d503c1f1a3c86139636a9c87eef8aea39687d095`.
The baseline is the accepted `23deec0-r50` image from October 14.

The acceptance host remains Fedora 44, kernel `7.2.10-200.fc44.x86_64`, `gfx1151`,
128 GB LPDDR5X-8000 and ROCm 10.0. The 112 GiB GPU allowance, balanced
platform profile and CPU EPP are unchanged. No host packages, kernel options,
memory limits, power settings or services were reconfigured. Models,
catalog pins, templates, samplers, F16 KV and the dense Q8 MTP default are
unchanged. Flash-Next remains non-MTP. No private reduced-precision matrix
path is added.

The native dense Qwen3.8 Unsloth UD-Q8_K_XL comparison uses **non-MTP**
generation, `pp512`, `tg128`, batch/microbatch 2048/512, flash attention and three
repetitions per case. The baseline and candidate run serially in the same
session. Deep rows preload 32768 tokens and restore a CPU checkpoint between
repetitions, separate from the device-copy correctness tests.

| Backend | KV | Existing depth | Baseline pp / tg | Updated pp / tg |
| --- | --- | ---: | ---: | ---: |
| ROCm | F16 | 0 | 380.15 / 7.218 | 375.52 / 7.217 |
| ROCm | F16 | 32768 | 270.58 / 6.771 | 269.98 / 6.767 |
| ROCm | Q8_0 | 32768 | 267.29 / 6.864 | 266.40 / 6.865 |
| Vulkan | F16 | 0 | 369.62 / 7.315 | 365.50 / 7.308 |
| Vulkan | F16 | 32768 | 240.83 / 6.797 | 242.54 / 6.796 |
| Vulkan | Q8_0 | 32768 | 235.31 / 6.974 | 233.53 / 6.970 |

Values are native benchmark means in tokens/s. Decode changes by less than
0.1% in every case, with prefill differences from -1.22% to +0.71%. The
upstream Vulkan implementation retains the long-context benefit of the
removed local patch. This is not a meaningful token-generation speedup.
Optional quantized-cache coverage does not change the F16 defaults.

The MTP memory fix has a directly measured benefit. Two trace-only overlay
images use the same logging level and the same dense Q8 MTP requests. At
31651 and 32163 populated tokens, the old checkpoints are 273.987 and
275.999 MiB. Both updated checkpoints are 149.626 MiB, saving about 251 MiB
across that pair. These are serialized checkpoint sizes, not a claim that
total process memory falls by 46%. The plain draft KV no longer gets copied
into each partial checkpoint. Both 32167-token retrieval repetitions return
the same six values, with identical request and response-choice objects
across builds. Each answer accepts 171 of 177 drafted tokens. This bounded
screen does not establish general coding-quality equivalence.

Flash-Next recovers all six dispersed values from the 247165-token Go/Markdown
archive on both ROCm and Vulkan. These final-image measurements use the same
four Unsloth UD-Q4_K_XL shards, F16 K/V, no MTP, a 262144-token ceiling and
one serial client. The ROCm server has one slot. Vulkan has four available
slots with that ceiling per slot, with only one processing the test request.
Requests use the catalog's medium sampler tuple with temperature zero and
seed 42. Model loading is excluded. Cold means no reused KV prefix, not a
flushed OS page cache.

| Flash-Next backend | Cold request wall time | Prompt processing | Warm HTTP throughput |
| --- | ---: | ---: | ---: |
| ROCm | 397.93 s | 385.94 s | 16.67 tokens/s |
| Vulkan | 1151.30 s | 1139.80 s | 17.38 tokens/s |

Warm throughput is 512 generated tokens divided by the sum of two request
durations, with 247159 cached tokens each. The first continuation, which
processes a larger uncached tail, is excluded. The fixed 256-token outputs
are intentionally truncated timing probes, not completed coding tasks.
These are final-image acceptance measurements rather than a new paired
long-context speedup claim. The previous day's accepted reference remains
in the October 14 record below.

The default dense Qwen3.8 Unsloth UD-Q8_K_XL MTP preset also recovers all
six values at 247165 tokens. Its cold ROCm request takes 1765.52 seconds,
including 1746.46 seconds of prefill. The 227-token answer accepts 168 of 174
drafted tokens. The previous accepted run took 1766.40 seconds and had the
same output-token and draft-acceptance counts. The server exposes four
262144-token slots, with one serial client active in this test. Cached
continuations and the fresh request afterward pass too. The two warm
256-token continuations yield 8.52 aggregate HTTP tokens/s at that populated
depth, again with 247159 cached tokens each.

The separate shallow HTTP control uses three 41-token prompts and 256 output
tokens per request, without cached prefixes. It measures 15.73 aggregate
tokens/s with MTP, accepting 529 of 702 drafted tokens, and 7.11 tokens/s
without speculation. The prior accepted MTP result was 15.78 tokens/s.
This preserves the existing speculative benefit rather than establishing
a new speedup, and the fixed output windows remain timing probes.

The final image also passes the following correctness and runtime checks.

- Cold build for all four compiled GPU targets, runtime dependency and
  native-library checks, CPU router startup and real tiny-model CLI
  generation on CPU, ROCm and Vulkan. Runtime status consumes the new
  schema without reporting the retired Vulkan switch. CPU confinement has
  no GPU devices, a read-only root, read-only content, dropped capabilities,
  no-new-privileges and loopback-only publication.
- Seven native Unicode, Jinja, chat, grammar and argument tests, allocation
  isolation and all 50 reader/dequantizer/EOF checks.
- All eight top-k MoE allocation-lifetime cases and 654 supported ROCm QSA,
  Qwen pipeline, GDN and top-k numerical cases. Three deliberately
  unsupported geometries remain rejected rather than counted as passes.
- The full Flash-Next ordinary, causal-transition and clear/restore state
  fixture produces logit NMSE 0.
- All four context cases, including the new partial-size regression, pass
  separately with F16, Q8_0 and Q4_0 K/V. Every run reports zero skipped cases.
- Both backends pass 310 generic flash-attention cases and all six new
  strided F16/BF16 cases. Copy tests pass 251 supported ROCm cases and 254
  Vulkan cases. Unsupported copy geometries are explicitly skipped, not
  claimed as tested support.
- Three SSE responses each permit a following health request on the same
  raw TCP connection, without reconnecting.
- Flash-Next passes off/low/medium/xhigh, tool replay, streaming, Responses
  and history preservation on ROCm and Vulkan. ROCm also passes explicit
  prefix-cache and direct-cancellation checks.
- Dense Q8 MTP passes the protocol and history suite on both backends,
  with explicit cache and cancellation checks on ROCm. Dense Q4 MTP and
  Qwen3.6 dense 27B and sparse 35B MTP pass their applicable ROCm controls,
  tools, streaming, Responses and history policy checks.
- Muse Glimmer DFlash at its 256K ceiling passes a reasoning answer and
  tool replay, accepting 71 of 252 drafted tokens in the 93-token answer.
  These short Muse requests do not populate the context ceiling or test
  thinking off.
- Gateway lazy startup, frozen inventory and dense-to-Flash-Next-to-dense
  switching pass. The backend remains published on an ephemeral loopback
  port. Omitted, null and explicit-zero sampler values, reasoning controls
  and provenance agree with the catalog. Conflicting controls return HTTP
  400. All 18 streaming/non-streaming cancellation trials observe the actual
  child slot returning to idle within 1.35 seconds.
- Real sandboxed Pi and Maki runs read the fixture with their file tool and
  execute the shell command, then return both actual results. Pi runs with
  thinking off and records zero reasoning tokens. Maki uses its normal
  selected-model controls. This is tool-protocol acceptance, not a coding
  quality or harness-efficiency comparison.
- The public managed acceptance command passes the GPU/device diagnostic
  and Qwen3 0.6B ROCm smoke. Local `make check`, `make test`, `make static`,
  forced normal build and race checks pass, together with Python compilation,
  shell syntax, catalog JSON and diff checks.

Minimum sampled available host memory is 23.91 GiB for Flash-Next ROCm,
32.79 GiB for Flash-Next Vulkan and 66.49 GiB for dense Q8 MTP ROCm during
their long runs. These observations are not a paired process-memory
comparison. The 4 GiB safety guard never fires. No new AMDGPU/SVM faults
appear. The final Python GPU diagnostic emits two previously observed
split-lock notices, with no intervention needed.

The isolated PR 30139 screen passed its bounded correctness checks but
offered only small, mixed gains. Flash-Next's shallow HTTP throughput rose
1.24%, the dense Q8 MTP control fell 0.58%, and Flash-Next's 32K prefill took
4.2% longer. It is deferred, not classified as broken or accepted at long
context. The exact source and rationale are in the
[upgrade ledger](upgrading.md#upgrade-llamacpp).

The exact source, build logs, raw requests and responses, numerical results,
paired benchmarks, checkpoint traces, client transcripts and delivery diff
are retained with checksums on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261015-llama-followup/`.
The previous acceptance archives are untouched. The acceptance host started idle and is
left idle, with no gateway or test container running. The new image is built
and ready for normal use.

`gfx1150`, `gfx1200` and `gfx1201` remain **N/P for hardware acceptance of
this update** because representative hardware was unavailable. The final
image compiles for those architectures, but their applicable GPU, cache,
model and isolation gates still need to run. In particular, upstream's
broader AMD Vulkan dispatch is not a claim of Strix Point acceptance.

### Fedora 44 Strix Halo llama.cpp upstream update (2026-10-14)

**PASS on Strix Halo, with a measured performance tradeoff.** The default
dense Q8 MTP control is effectively unchanged. Flash-Next's cold 247K request
is unchanged, but its warm decode is about 12.3% slower. This update accepts
the newer upstream base and correctness fixes, not an across-the-board speedup.

This update advances llama.cpp by 345 upstream commits to
`23deec025587a58469bdf0120313ff88a1ddcc7b`. The controller revision is the
commit adding this record. The final fourteen-patch source tree is
`c6d3b98410741a5f95033e109ce4734d73b9f7e6`, verified against the actual cold
builder, not just an experimental source checkout. The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-23deec0-r50`, ID
`1107526be13744cd1ab08ad91f149298d2319c7099ffcf29570b34a762bd72e7`.

The acceptance host runs Fedora 44, kernel `7.2.10-200.fc44.x86_64`, `gfx1151`,
128 GB LPDDR5X-8000 and ROCm 10.0. The 112 GiB GPU allowance, balanced
platform profile and CPU EPP are unchanged. No host packages, kernel options,
memory limits, power policy or services were reconfigured. GGUFs, catalog
pins, templates, gateway samplers, the dense Q8 MTP default and F16 KV caches
are unchanged. Flash-Next remains non-MTP.

The earlier partial rebase below did not retain enough of the accepted
pipeline. This update restores the scoped F32 fusions and Q8 scheduling,
corrects selected-ID ordering and replaces the private prepared-key cache
and selection machinery with upstream's pooled-cache state. Three obsolete
patch files are removed. The new compact selection patch only avoids a
redundant mask, with an independent visibility-intersection oracle.

There is no private BF16/F16 shadow-matrix mode. Upstream's chunked GDN uses
compensated BF16 products and F32 state, which is a different algorithm and
was included in numerical and model-level checks. The ledger in
[upgrading](upgrading.md#upgrade-llamacpp) records each remaining patch and
its removal gate.

The final image passes the following completed checks.

- Cold managed build for all four compiled GPU targets, runtime `pip check`,
  native library closure and absence of the SDK/source and PyTorch payload.
  CPU router startup retains loopback-only private networking, read-only
  root, dropped capabilities, no-new-privileges and no GPU devices.
  Tiny-model CLI inference exits successfully on CPU, ROCm and Vulkan.
- Five chat/grammar/argument tests, allocation-policy isolation and all 50
  explicit-reader dequantizer/EOF checks.
- 654 supported ROCm QSA, Qwen pipeline, GDN and top-k MoE numerical cases.
  Three deliberately unsupported geometries are rejected, not counted as
  passes. The full Flash-Next restore, causal-transition and clear/restore
  fixture has logit NMSE 0.
- All 309 selected generic flash-attention cases pass on each of ROCm and
  Vulkan, covering F16, Q8_0 and Q4_0 caches with matched head sizes 64, 128
  and 256. Each cache type executes multi-query cases as well as decode.
- Quantized-cache device save/restore and scattered copies pass all three
  enabled context tests with each of F16, Q8_0 and Q4_0. The unpatched control
  passes F16 but produces incorrect Q8/Q4 logits or a CPU-side scatter crash.
  A filter that initially skipped tests is retained as failed test setup,
  not accepted evidence.
- The streaming keep-alive fix passes three SSE/health rounds on the same
  raw TCP socket without reconnect. The unpatched control closes that socket.
  Eight MoE allocation-lifetime cases execute real optimizer callbacks and
  pass. The negative control reports invalid lifetime endpoints and crashes
  on a terminal normalization suffix.
- Flash-Next passes 247K retrieval, cached continuations, off/low/medium/xhigh,
  tool-call replay, SSE and Responses on ROCm and Vulkan. ROCm also passes
  explicit cache and direct-cancellation follow-up checks.
- The default dense Qwen3.8 Q8 MTP preset recovers the same six values at
  247K, then passes cached continuations and the full protocol/cache/cancel
  suite. Its initial request takes 1766.40 seconds, including 1747.34 seconds
  of prefill. The 227-token answer accepts 168 of 174 drafted tokens. These
  are actual MTP counters, not an inference from the preset's name.
- Dense Q8 MTP also passes the protocol suite on Vulkan. Dense Q4 MTP and
  both dense and sparse Qwen3.6 MTP variants pass on ROCm. Muse Glimmer
  DFlash at its 256K ceiling passes thinking-off, a reasoning answer and tool
  replay, accepting 72 of 240 drafted tokens in the 93-token reasoning answer.
  Those short Muse requests do not populate its context ceiling.
- Gateway lazy startup, frozen inventory and dense-to-Flash-Next-to-dense
  switching pass with the final image. Backend publication remains on an
  ephemeral loopback port. Omitted, null and explicit-zero sampling values,
  thinking controls and provenance agree with the catalog. Conflicting
  selectors return HTTP 400. All 18 streaming/non-streaming cancellation
  trials observe the child slot returning to idle within 0.63 seconds.
- Real Pi and Maki sandbox runs read the fixture with their file tool and
  execute `printf` with their shell tool, then return both actual results.
  Pi explicitly runs with thinking off and reports no reasoning tokens.
  Maki uses its normal selected-model controls in this probe. These are
  tool-protocol checks, not comparative coding-quality benchmarks.
- The public `acceptance` command passes with the final image, GPU/device
  diagnostics and the managed Qwen3 0.6B ROCm smoke. Local `make check`,
  `make test`, `make static`, forced normal build and race checks pass,
  along with Python compilation, shell syntax, catalog JSON and diff checks.

The paired Flash-Next measurement uses the same four Unsloth UD-Q4_K_XL
shards, explicit buffered embedding reads, F16 K/V, no MTP, one ROCm slot,
one serial client and a 262144-token ceiling. Both builds run on the same
kernel. A fixed 143-file Go/Markdown corpus supplies a 247165-token prompt
with six dispersed retrieval values. Both recover all six. Requests send
the catalog's medium-thinking sampler tuple with temperature zero and seed
42. Model load is excluded. Cold means no reused KV prefix, not a flushed OS
page cache.

| ROCm build | Cold request wall time | Prompt processing | Warm continuation throughput |
| --- | ---: | ---: | ---: |
| Previous `2090f60-r49` | 402.49 s | 391.96 s | 18.98 tokens/s |
| Updated `23deec0-r50` | 404.91 s | 392.92 s | 16.65 tokens/s |

Cold wall time differs by 0.6% and prompt processing by 0.25%. Warm decode
is **12.3% slower** in this screen. Warm throughput is 512 generated tokens
divided by the sum of two HTTP request durations, with 247159 cached prompt
tokens in both builds. The first continuation, which also processes a larger
uncached tail, is excluded from that rate. These fixed-256-token continuations
are deliberately truncated timing probes, not completed coding tasks.
The result is a maintenance/correctness update, not an across-the-board
speedup. This small sample does not establish a general quality difference.

The separate shallow HTTP control for the default dense Q8 MTP preset is
effectively flat at 15.73 versus 15.78 aggregate tokens/s. It uses three
41-token prompts with no reused prefix and exactly 256 generated tokens per
request. The request bodies and generated messages match across builds.
This does not turn the timing probe into a completed-task quality benchmark.

Native dense Q8 **non-MTP** benchmarks also show no meaningful decode change.
Each row uses `pp512`, `tg128`, batch/microbatch 2048/512, flash attention and
three repetitions. Deep rows preload 32768 tokens and restore a CPU checkpoint
between repetitions. They are not on-device state-copy tests. Values below
are native benchmark means in tokens/s, not concurrent HTTP throughput.

| Backend | KV | Existing depth | Old pp / tg | New pp / tg |
| --- | --- | ---: | ---: | ---: |
| ROCm | F16 | 0 | 370.56 / 7.19 | 377.23 / 7.21 |
| ROCm | F16 | 32768 | 265.41 / 6.78 | 270.44 / 6.77 |
| ROCm | Q8_0 | 32768 | 261.45 / 6.88 | 266.19 / 6.86 |
| Vulkan | F16 | 0 | 365.24 / 7.30 | 367.39 / 7.31 |
| Vulkan | F16 | 32768 | 243.40 / 6.79 | 242.28 / 6.80 |
| Vulkan | Q8_0 | 32768 | 235.86 / 6.97 | 235.36 / 6.97 |

Decode differences stay within 0.3%. The small prefill differences, including
roughly 2% on ROCm, are not a reason to advertise a speedup. Quantized-cache
rows cover an optional path and do not change the F16 defaults.

Final-image Vulkan also recovers all six values at 247K, taking 1151.70
seconds for the cold request and 1140.15 seconds for prefill. Its managed
server has four slots with a 262144-token ceiling per slot, checked through
live properties, but the test still uses one serial client. This is backend
coverage, not a paired Vulkan speedup claim.

Minimum sampled available host memory during the ROCm long runs is 21.12 GiB
for the old image and 23.76 GiB for the new one. Those observations do not
isolate a memory-saving cause. The 4 GiB safety guard never fires. Expected
CPU crashes from the negative controls appear in the kernel journal. The
final-image checks have produced no new AMDGPU/SVM faults. One ordinary
kernel `perf` sampling-rate reduction notice appeared during the native
benchmarks. The final Python GPU diagnostic emitted the previously recorded
split-lock notices. Neither required intervention.

The cold build, exact patched source, numerical/state controls, raw requests,
responses, benchmark JSON, live slot evidence and client transcripts are
retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261014-llama-update/`,
with checksums and the delivery diff. The earlier held screen's archive is
left intact. The acceptance host started idle and is left idle with no gateway or test
container running. The final image is built and available for normal use.

`gfx1150`, `gfx1200` and `gfx1201` are **N/P for hardware acceptance of this
update** because representative hardware was unavailable for this run.
Compilation for those targets is not inference acceptance. Their applicable
operator, cache, model and isolation gates remain required before claiming
this update is accepted there.

### Fedora 44 Strix Halo llama.cpp refresh screen (2026-10-14)

**Historical HOLD, superseded by the accepted update above.** Upstream
`529ac893e754e95a50469b2122f5efe28dfc08a0` built and passed the bounded
Flash-Next state checks with reviewed experimental corrections. However,
the corrected partial port took about 28% longer than production on the
32K retrieval probe. This screen therefore left the production pin at
`2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`, image `r49`, with no experimental
patch, arithmetic mode, sampler change or model update enabled at that point.

This run follows the host owner's system upgrade and reboot. The acceptance host is still
Fedora 44, `gfx1151`, 128 GB LPDDR5X-8000 and ROCm 10.0, now with kernel
`7.2.10-200.fc44.x86_64`. The 112 GiB GPU allowance, balanced platform profile
and CPU EPP were left alone. No packages, kernel parameters, memory limits,
power settings or system services were changed. The GPU doctor and existing
tiny-model acceptance passed. The Python GPU probe emitted two split-lock
notices, a pattern also recorded before the host upgrade. The subsequent
llama.cpp inference runs produced no new kernel warnings.

The first experiment repeats the October 6 isolation at the new upstream
pin. The direct-reader-only build still fails clear/restore, with logit NMSE
0.00787 to 0.01324. Recreating the HIP executable on graph recapture clears
the bounded test. Ordinary graph replay stays enabled. Narrowing this
workaround to `gfx1151` also passes the full state fixture with NMSE 0.
The old experimental workaround build passes on the new kernel too. This
does not identify the underlying HIP or llama.cpp defect, and it is not
evidence that unpatched upstream with resident embeddings is broken.

A second issue appeared when porting the existing selected-key attention
kernels onto upstream's new pooled-cache graph. Operator comparisons passed,
but restored whole-model logits differed even with HIP graphs disabled.
Disabling only the selected-key path restored exact agreement. Upstream's
unordered top-k output exposed an ordering dependency in the ported decode
kernel. Canonically sorting and deduplicating IDs before splitting attention
restored NMSE 0 for ordinary restore, causal-mode restore and clear/restore.
A new order-invariance test fails both decode cases before that correction
and passes afterward. The corrected build passes 100 GPU operator cases
against CPU references, including the order tests and selected FP64 oracles.
The unsupported maskless ratio-4 case is rejected as intended. Native
allocation-isolation and all 50 reader/dequantizer/EOF checks also pass.

The performance comparison uses the same four managed Unsloth Flash-Next
UD-Q4_K_XL shards, direct buffered embedding reads, ROCm, F16 K/V, no MTP,
one slot and one serial client. The context ceiling is 262144, but the
populated prompts are only 3667 and 32167 tokens. Each size has two cold
requests per build, with all six retrieval values correct and zero cached
prompt tokens. Requests explicitly send the catalog's medium-thinking
sampling tuple, overriding temperature to zero and setting seed 42.

| Build | 3667-token mean HTTP wall time | 32167-token mean HTTP wall time | 32K prompt tokens/s |
| --- | ---: | ---: | ---: |
| Production `2090f60-r49`, full accepted patch set | 14.59 s | 51.82 s | 767.73 |
| New base, seven safety/compatibility patches, upstream attention | 16.20 s | 79.98 s | 465.14 |
| New base, retained GDN and selected-key kernels, canonical ID order | 16.33 s | 66.20 s | 574.82 |

A return-to-production control then repeats both cold 32K requests at
51.32 seconds mean wall time and 778.61 prompt tokens/s. This reproduces the
original baseline after the candidate runs rather than relying on an older
kernel's measurements.

Prompt throughput is total processed prompt tokens divided by total prompt
processing time, not a sum of request rates. Model loading is outside HTTP
timings. A separate three-request, fixed-256-output-token shallow probe is
effectively flat at 21.09 versus 21.23 aggregate output tokens per HTTP wall
second for production and the corrected candidate. Those deliberately
truncated generations are a timing screen, not completed-code quality tests.

The partial port does not contain the private compact selection graph,
prepared-key cache or pipeline fusions. Upstream has its own new pooled-cache
implementation, but this screen does not prove that it replaces every
downstream optimization at equivalent cost. The observed gap cannot be
attributed to one upstream commit or to the kernel upgrade. Do not promote
the candidate by weakening the state gate or silently dropping the remaining
optimizations. Populated near-256K, dense-model MTP, router/client and Vulkan
acceptance remain outstanding for an eventual rebase. Other GPU architectures
are **N/P** for this screen.

The unchanged production dense Qwen3.8 Q8 MTP preset also passed a final
post-reboot check at its 256K ceiling. Off, low, medium and xhigh, a longer
off-mode prompt, nested tool arguments and tool replay, SSE and the Responses
endpoint all passed. This is a small protocol check of production, not
acceptance of the new candidate. All test containers were stopped afterward.

The controller source for this run is `24cff7f`. Production image ID is
`3283bd83444111fc8c5dca240d2124d330320dace47062d6135226c6d467aee9`.
The seven-patch image is
`c932c89217b64eba2f891054594b34cef5ba91d46c47f601a607bc4f25554c20`,
and the corrected candidate with order tests is
`83ba16072c1d774754b9907d8d4bea8d8ecb5c77cbae04196badd0e76b436865`.
These are isolated locally built experiments, not managed image tags. Private
evidence is retained under
`apps/acceptance/results/20261014-llama-refresh/` on the acceptance host, including the exact
candidate patches, image recipes, source snapshot, fixtures, requests,
responses, runtime/build logs and checksum manifest.

### Gufo retirement (2026-10-14)

Gufo is no longer a managed application. Its build target, runtime, model ID,
recipe, and engine-specific sampling policy have been removed without aliases.
The shared Unsloth Flash-Next GGUFs, llama.cpp preset, and gateway sampling
mechanism are unchanged. Existing images, application state, verification
receipts, and experiment results are not deleted. An explicit `gufo`
application selection in a user's configuration must be removed.

This is a scope decision, not a finding that Gufo has no performance advantage.
The October 11 Pi comparison used two small Go fixes, medium and xhigh,
one attempt per condition, identical explicit sampling and the same GGUFs.
Gufo decoded about 28% faster, but the medium pair finished only 2.1% sooner
overall. The xhigh pair finished 31.4% sooner, with an important qualification.
All eight attempts passed the frozen graders, but review found an unrequested
retry limit in Gufo's xhigh EINTR fix. A separate boundary test failed that
patch and passed the other three EINTR patches. Its time is not time to an
approved fix, and this small sample does not establish an engine quality gap.

Generation accounted for roughly 94% of wall time. Populated prompts peaked
at 7K to 36K tokens despite a 256K ceiling, so these are not full-context
throughput measurements. The measured benefit did not justify maintaining a
second Flash-Next engine for the current workload. The earlier acceptance
records below are historical, not descriptions of today's supported engines.
Private evidence remains under
`apps/agent-evaluation/experiments/20261011-gufo-llamacpp-pi/` on the acceptance host.

### Fedora 44 Strix Halo gateway sampling ownership (2026-10-11)

**PASS for the reviewed Qwen paths.** The gateway now supplies missing or
null sampler fields from the model's catalog policy. Explicit client values,
including zero, remain untouched. The existing tuples, GGUFs, templates,
reasoning defaults, F16 KV cache and inference arithmetic are unchanged.
Gufo keeps its reviewed thinking min-p of 0, while llama.cpp Flash-Next keeps
0.05. Direct native-engine endpoints no longer receive gateway policy.

The controller revision is the commit adding this record. The llama.cpp pin
remains `2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`. Removing only
`reasoning-controls.patch` leaves thirteen source patches and produces
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-2090f60-r49`, image
`3283bd83444111fc8c5dca240d2124d330320dace47062d6135226c6d467aee9`.
Gufo retains source `665fc182445dc2685f2a9b2b4314aa7d92a858c2` and image
`d6a3385df7cc9dcef94ad589bf3176430d16f01f68be9d9ef603d026c32ee179`.
The acceptance host runs Fedora 44, kernel `7.1.7-200.fc44.x86_64`, `gfx1151`,
128 GB LPDDR5X-8000, with unchanged balanced power policy and ROCm 10.0.

The HTTP matrix covers dense Qwen3.8 Q8 and Q4 MTP, Qwen3.6 dense and sparse
MTP, and Flash-Next UD-Q4_K_XL on both llama.cpp and Gufo. All passed omitted
controls, off, nested off and their supported positive reasoning levels.
Missing, null and explicit-zero samplers behaved as intended. The llama.cpp
verbose response confirmed the actual six generation settings. Gufo's seeded
outputs matched between gateway-filled and fully explicit tuples at off,
medium and xhigh. Small answer checks passed, with no reasoning output in
off mode. Conflicting selectors returned HTTP 400 before inference.

Six sandboxed Pi 1.0.2 sessions completed read-tool round trips and returned
the exact fixture value, covering off, medium and xhigh on dense Q8 and Gufo.
Two Maki 0.4.8 headless sessions also completed tool round trips, one per
engine. They are off-mode checks, not named-reasoning acceptance for Maki's
known headless limitation. The gateway ledger records the selected policy,
mode and client-versus-gateway provenance without retaining prompt bodies.
The final host implementation passed another dense-Q8 and Gufo HTTP matrix.

Model context ceilings remained 256K, except dense Q4 at 128K. These were
short request and client checks at those configured ceilings, not populated
long-context probes or a performance comparison. There is no new numerical
or general coding-quality claim.

The rebuilt image passed dependency checks and model-free CPU router startup
on loopback. Inspection found a read-only root and no CPU-mode GPU devices.
Direct speculative and coding-evaluation dry runs passed with the retired
engine option absent. Host checks, static build, race tests and catalog
validation passed. No new entries appeared in the test-window kernel log.
All test gateways and containers were stopped. Other GPU architectures are
**N/P** for this run because acceptance hardware was not available.

Evidence is retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261011-gateway-sampling/`.
It includes exact probes, responses, client transcripts, status snapshots,
build and runtime logs, helper source, the retired patch and delivered diff.

### Fedora 44 Strix Halo managed Gufo integration (2026-10-10)

**PASS for the reviewed Flash-Next path.** Gufo is now a separate source-built
application, not a replacement for llama.cpp or DwarfStar. Dense Qwen3.8 27B
Q8 MTP remains the client default. No llama.cpp patch, model file, host tuning,
kernel, or ROCm version changed for this integration.

The source pin is Gufo 0.11.0
`665fc182445dc2685f2a9b2b4314aa7d92a858c2`, built against the existing ROCm
10.0 runtime as `localhost/paracetamol:gufo-ubuntu26.04-rocm10.0-665fc18-r1`.
The long-context and client tests used image
`032f835fc6f8239c84985175489f37265839d052ce0edc634ecb233b507854cb`.
The delivered image is
`d6a3385df7cc9dcef94ad589bf3176430d16f01f68be9d9ef603d026c32ee179`.
It corrects only the entrypoint's port label to distinguish the container
bind from gateway-owned ephemeral host publication. Its Gufo executable is
unchanged, and automated GPU acceptance and runtime checks passed again.
No downstream Gufo patch was needed. The engine and HIP kernels are compiled
from source, not taken from an upstream image or binary distribution.

The acceptance host runs Fedora 44, kernel `7.1.7-200.fc44.x86_64`, `gfx1151`,
128 GB LPDDR5X-8000, 112 GiB GPU allowance, balanced platform profile and
balance_performance EPP. The model is the existing four-shard Unsloth
Flash-Next UD-Q4_K_XL at revision
`c8b5954a88c2775c546b92593eda40ea041d3176`. Gufo references those same
verified files. There is no second download, quant conversion, projector,
draft model, or MTP sidecar.

The managed policy is 262,144 context tokens, F16 K/V, one session, medium
reasoning by default, a 28 GiB RAM snapshot budget, no disk cache, and
speculation off. An 8K direct-server screen passed first. At the full ceiling,
temperature-zero corpus probes gave the following results.

| Check | HTTP wall time | Result |
| --- | ---: | --- |
| 32,167-token retrieval | 28.575 s | All six values correct, 211 output tokens |
| 247,165-token retrieval | 189.116 s | All six values correct, 199 output tokens |
| First long continuation | 11.269 s | 256 output tokens, 245,760 cached input tokens |
| Warm continuation 1 | 10.036 s | 256 output tokens, 247,158 cached input tokens |
| Warm continuation 2 | 10.000 s | 256 output tokens, 247,158 cached input tokens |

The near-limit cold prefill took 181.267 seconds, about 1,364 tokens/s.
Warm decode was about 26.0 tokens/s. These are single-host acceptance probes,
not a fresh head-to-head benchmark or a coding-quality ranking. The minimum
sampled `MemAvailable` during the long checks was 27.59 GiB. The 4 GiB guard
did not trigger. No new GPU faults, resets, or OOM events were found in the
test-window kernel log. The later PyTorch host-GPU acceptance probe emitted
two x86 split-lock notices for its Python process and still passed. Those
notices did not come from Gufo.

Additional checks passed for off/medium/xhigh, typed tool arguments and tool
replay, SSE, cancellation followed by a successful request, and history
isolation. Seeded output with omitted samplers matched explicitly supplied
Gufo defaults in both thinking and non-thinking modes. Real sandboxed Pi
sessions completed file and shell tools at all three reasoning settings.
Maki 0.4.8 completed tool exchanges, and its interactive mode sent the expected
off/medium/xhigh values. Its headless `--print` path instead sent off despite
`always_thinking`, so those runs are not counted as thinking-mode acceptance.

Gateway startup remained lazy. Switching Gufo to the trusted dense Q8 MTP
llama.cpp model and back passed, with only one backend container resident.
Inspection confirmed a read-only root, zero effective capabilities, exactly
`/dev/kfd` and `renderD128`, four read-only shard mounts, and ephemeral loopback
publication. The final model-free runtime checks passed, and the managed
entrypoint rejected absent GPU devices. The new automated Gufo acceptance
case passed in 19.18 seconds including loading, then removed its exact
container. All test services were stopped.

Gufo changes internal matrix arithmetic relative to the precision-preserving
llama.cpp path. F16 KV and unchanged GGUF bytes do not establish numerical or
broad quality equivalence. Other GPUs and lower-memory hosts are **N/P** for
this managed integration. DwarfStar retirement and Gufo speculation are not
part of this change.

Evidence is retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261010-gufo-integration/`.
It includes exact requests and responses, client transcripts, sampling
comparisons, image/container inspection, memory samples, kernel logs,
automated acceptance, build logs, helper sources, and the delivered patch.

### Fedora 44 Strix Halo Flash-Next state isolation (2026-10-06)

**REPRODUCED AND NARROWED.** The failure is not sufficient evidence of a
general upstream regression. Current upstream passes this test with resident
embeddings. Adding only Paracetamol's explicit embedding reader reproduces
the failure. A small experimental HIP graph-update workaround clears it.
MTP is absent throughout, so disabling MTP would not address this reproducer.
The production pin and `r48` image remain unchanged pending full acceptance.

The test uses the same Fedora 44 `gfx1151` host, kernel
`7.1.7-200.fc44.x86_64`, ROCm 10.0, 128 GB LPDDR5X-8000, 112 GiB GPU
allowance and balanced power policy as the earlier retry. No host tuning,
GGUF change or download is involved. The model is the managed Unsloth
Flash-Next UD-Q4_K_XL, with 8K context, F16 K/V, batch and ubatch 2048,
full GPU layer offload and the embedding table assigned to CPU.

The maintained fixture now has a
[`--state-smoke` mode](testing-and-release.md#fast-flash-next-state-screen).
It populates 4098 deterministic tokens, saves the sequence, obtains reference
logits for one more token, restores once normally, then clears and restores
three times. There is no sampler, harness, router or causal-mode switch.
The failure needs none of those extra variables. Explicit synchronization
before clearing does not remove it.

| Source and path | Ordinary restore | Three clear/restore cycles |
| --- | --- | --- |
| Accepted `2090f60`, all production patches and direct reader | NMSE 0 | NMSE 0 throughout |
| Eight-patch `6753a03`, direct reader | NMSE 0 | FAIL, NMSE 0.0021881 to 0.0087478 |
| Eight-patch `6753a03`, resident embeddings | NMSE 0 | NMSE 0 throughout |
| Unpatched `d7a695e`, resident embeddings | NMSE 0 | NMSE 0 throughout |
| `d7a695e` with only the direct-reader patch | NMSE 0 | Same failure as the eight-patch build |
| Reader-only build with llama graph reuse disabled | NMSE 0 | Same failure |
| Reader-only build with fresh HIP executables on graph recapture | NMSE 0 | NMSE 0 throughout |

The full current-upstream pin is
`d7a695ef679138c13d86359b84c1731d36213d32`. The pure-upstream and
reader-only images are separate from the managed image. Both retain the
same locally built ROCm runtime. Removing the other patches rules them out
as necessary causes of this reproduction, not as universally correct.

With the maintained fixture, the reader-only failure takes 35.4 seconds
including a 23.8-second load. The test itself takes 11.6 seconds. The
accepted production control takes 38.5 seconds including loading and 5.7
seconds for the test. Loader/page-cache variation matters, so these numbers
describe reproducer cost rather than an inference-speed comparison.
The unpatched resident control takes 83.2 seconds in the initial run and
reaches about 109 GiB of used host memory. It completes without the mmap
resident-limit failure seen in the earlier retry. This is a safe short
control on this host, not a replacement for SSD streaming at 256K.

The experimental workaround destroys and reinstantiates the executable
after a HIP graph is recaptured instead of calling `hipGraphExecUpdate`.
Unchanged graphs still replay normally. It changes no weights, KV precision,
samplers or matrix arithmetic. The zero-error result repeats with the
diagnostic switch and with a clean standalone workaround patch. The latter
passes the smoke test in 42.1 seconds including loading. The full fixture,
including the causal transition, also passes every comparison with NMSE 0
in 45.4 seconds. This identifies
the update path as a useful workaround boundary, but does not establish
whether the root defect belongs to HIP's update implementation or the graph
llama.cpp supplies to it.

Small random-weight Qwen4exp controls do not reproduce the failure and are
not accepted as substitutes for the real-model screen. The remaining gates
for a source update include the full managed patch selection, server/router
behavior, dense and speculative controls, populated long context, runtime
closure and performance. Other GPU architectures are not tested here.
There is no reason to block the investigation on Flash-Next MTP or to adopt
a blanket graph-disable policy based on this result.

The private evidence archive is
`apps/acceptance/results/20261006-qwen4exp-state-isolation` on the acceptance
host. It retains exact source pins, image identities, build recipes, fixture,
reader and diagnostic patches, logs and a SHA-256 manifest. The clean
workaround remains there as an experiment, not a production patch. Test
containers are removed and the host returns to idle.

### Fedora 44 Strix Halo llama.cpp refresh retry (2026-10-06)

**HOLD.** The accepted source remains
`2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`, image policy `r48`.
No production image tag, source patch, model artifact, sampling policy or
default changes in this retry. Flash-Next MTP is not enabled.

The candidate `6753a033f058fbf778d282556ed9b16c78de7c71` is 146 commits
after the accepted source and 47 after the October 4 held candidate. Relevant
new merges include
[constant pooled-cache graph shapes](https://github.com/ggml-org/llama.cpp/pull/29958),
[shared-pool scatter race fixes](https://github.com/ggml-org/llama.cpp/pull/29994),
and [batch-independent CUDA allocation dependencies](https://github.com/ggml-org/llama.cpp/pull/29986).
Their presence does not by itself establish that the earlier restore failure
is fixed.

The screen uses the same Fedora 44 `gfx1151` host, kernel
`7.1.7-200.fc44.x86_64`, ROCm 10.0, 128 GB LPDDR5X-8000, 112 GiB GPU
allowance, balanced platform profile and `balance_performance` CPU EPP.
The isolated incremental image compiles HIP for all four supported targets
and Vulkan, but only Strix Halo ROCm executes this inference fixture. It is
not a final cold build or a release acceptance result.

As in the previous attempt, the screening source retains the first six
compatibility patches plus the rebased explicit reader and allocation-policy
patches. It uses upstream Qwen4exp attention, not the unaccepted port of the
six downstream attention and pipeline patches. The fixture is adapted to
upstream's new batch API without changing its tokens, sequence positions or
logit tolerance.

At context 8192, batch and ubatch 2048, F16 K/V, one sequence and 4098
populated tokens, the candidate records NMSE 0 for ordinary restore and for
the causal-mode transition followed by restore. Clear-and-restore fails at
NMSE `0.00218812799`, above the `1e-6` gate. Top-1 remains token 248046
throughout. Turning off HIP graphs gives NMSE 0 in all three checks.
Unchanged top-1 is not evidence of broad quality parity, and this fixture
does not demonstrate generally broken generated answers.

A separate ordinary mmap/lazy-read diagnostic encounters repeated AMD SVM
resident-memory mapping-limit errors during loading. It is stopped before
reaching the state test, so it neither reproduces nor clears the logit
failure. The direct reader remains necessary for the accepted policy. No
host memory limit, kernel or power setting is changed. This remains a
downstream screening reproduction, not an unpatched-upstream bug report.

The runtime hold stops further performance, populated-256K and MTP testing
in this retry. Disabling all graphs is not adopted as a production fix.
The accepted `r48` build is retested after the mmap diagnostic and passes
all three restore checks with NMSE 0 and its normal graph optimizations
enabled. All experiment containers are removed and the host returns to idle.

The private evidence archive is below
`apps/acceptance/results/20261006-llama-refresh` on the acceptance host.
It retains the exact eight-patch diff, adapted fixture, build recipe, logs,
image identity and pinned Hugging Face metadata with a SHA-256 manifest.
The experimental image is retained separately and is not a managed default.

#### Unsloth artifact audit and MTP status

Mainline MTP support arrived through
[PR 29761](https://github.com/ggml-org/llama.cpp/pull/29761) on October 1.
The older PR 28243 was closed without merging. The Unsloth MTP README at the
audited revision still describes the older fork-based implementation and
must not substitute for inspecting the selected llama.cpp source.

The audited repository revision is
`766911a6b7369840a91dbcd95f9f997acaab6cd6`, dated October 6. Its four
UD-Q4_K_XL target shards match the managed
`c8b5954a88c2775c546b92593eda40ea041d3176` artifacts byte-for-byte by
their published SHA-256 and size. Across the October 5 refresh, the three
self-contained MTP files change hashes without changing sizes. The three
shared MTP files and all other existing GGUFs remain unchanged. A duplicate
self-contained Q8 head appears at the repository root on October 6.

The potential companion is `MTP/mtp-Qwen3.8-Flash-Next-Q8_0.gguf`,
4,137,429,120 bytes, SHA-256
`58a47d6a216307e9b97172d5bfdc3436a8994b632f5e436b9bb6edf8951da8d1`.
Comparing the old and new GGUF headers finds one metadata change, the final
entry of `qwen4exp.attention.compress_ratios` from 0 to 4. The tokenizer,
template, tensor descriptors and data offset match. Only bounded header
downloads are inspected, so this is not a full tensor-payload comparison or
a verified managed installation. The repack still lacks its declared license
text and retains the existing license-risk boundary.

The other motivating thread describes
[Kyojin](https://github.com/Yamz-Labs/kyojin), an ExLlamaV3-based engine
using EXL3 weights, not a mainline llama.cpp update. Its reported speed and
quality figures are not measurements of Paracetamol's GGUF or runtime. It
is neither built nor integrated in this retry.

### Fedora 44 Strix Halo native engine trials (2026-10-04)

The accepted production change from this investigation is the
[DwarfStar update below](#fedora-44-strix-halo-dwarfstar-0aaea5a-update-2026-10-04),
commit `ba84d69`. The llama.cpp refresh is held at a correctness gate.
Gufo and the custom HIP runtime remain experiments, not new applications or
runtime defaults. The trusted dense Qwen3.8 27B Q8 MTP default is unchanged.

All GPU runs use the acceptance host's `gfx1151`, Fedora 44, kernel
`7.1.7-200.fc44.x86_64`, ROCm 10.0, 128 GB LPDDR5X-8000 and existing
112 GiB GPU allowance. The platform remains `balanced`, with
`balance_performance` CPU EPP. Builds and GPU timings do not overlap.
No model download, host tuning, kernel update or system ROCm replacement is
part of this investigation. Other GPU architectures remain untested.

#### llama.cpp refresh held on populated-state restore

The candidate is `46847e61582097979f539595d893d83d8e1d1af1`, 99 upstream
commits after the accepted `2090f60` pin. The new
[QSA cache/indexer implementation](https://github.com/ggml-org/llama.cpp/pull/29751)
replaces the structure used by several downstream optimizations. It needs a
real port, not patch-context adjustments. The subsequent
[indexer workspace reduction](https://github.com/ggml-org/llama.cpp/pull/29825)
reduces score temporaries by processing heads separately and applying ReLU
in place. It does not halve the GGUF, total KV cache or total model footprint.
Paracetamol's existing compact graph already differs from the upstream
baseline used in that PR's memory measurements.

The eight-patch candidate retains the necessary buffer, protocol, reader and
allocation fixes but uses upstream attention. It passes the short API,
retrieval, tool, cache and cancellation screens. A further selected-key and
pipeline port passes 133 CPU QSA/Qwen operator cases, 131 HIP QSA/Qwen cases
and 75 HIP GDN cases. These successes do not override the state failure.

The state fixture populates more than 4K tokens, saves the session and
compares the next-token logits after restore, a causal-mode transition and
clear-plus-restore. The gate requires finite logits, normalized mean-square
error at most `1e-6` and unchanged top-1 prediction.

| Build or diagnostic | Ordinary restore | Causal transition and restore | Clear and restore |
| --- | --- | --- | --- |
| Accepted `r48` | NMSE 0 | NMSE 0 | NMSE 0 |
| New eight-patch base | NMSE 0 | NMSE 0 | **FAIL**, NMSE 0.005675 |
| Port with canonical selected-ID ordering | NMSE 0 | NMSE 0 | **FAIL**, NMSE 0.008493 |

Turning off HIP graphs or all fusion makes the eight-patch base pass.
Disabling only the new shared-expert fusion does not. The first selected-key
port also needed canonical ID ordering to restore deterministically.
The remaining failure is not attributed to a particular upstream PR. This
is not an unpatched-upstream reproduction, and unchanged top-1 in this
fixture is not evidence of either broad quality parity or user-visible
garbage output.

The production pin, fourteen patches and `r48` image are untouched. The
incomplete rebase, exact fixture and diagnostic changes are archived for a
focused follow-up. Disabling all graphs or fusion is not adopted as a
production workaround.

#### Gufo comparison with the accepted llama.cpp baseline

[Gufo v0.7.0](https://github.com/gufo-org/gufo/tree/aedc129ffa98f00ebb1b1c8b75df60b2d987c48d)
is built locally from MIT-licensed C++/HIP source at
`aedc129ffa98f00ebb1b1c8b75df60b2d987c48d`. The inspected build does not
fetch an opaque inference-engine binary. Its isolated image is
`f6bedb12272d483fd6aa5f4e874cd76adad36d7d1a7e9e094df1455ecd96678f`.

Both engines load the exact same four managed Unsloth Flash-Next
`UD-Q4_K_XL` shards, without MTP, at a 262144-token ceiling with one active
client. Actual request JSON is replayed, with temperature zero and seed 42.
Retrieval uses medium reasoning and six exact marker values in a varied
text corpus. Model-reported prompt counts agree. Gufo uses its reviewed
one-hardware-queue setup, while llama.cpp uses the accepted production
configuration. This compares configured engines, not identical kernels or
arithmetic.

| Workload | Current llama.cpp HTTP wall time | Gufo HTTP wall time |
| --- | --- | --- |
| 32,167-token retrieval, mean of two | 51.470 s | 33.486 s |
| 247,165-token retrieval, one cold call | 415.957 s | 215.238 s |
| Short fixed generation, three calls / 768 output tokens | 36.416 s | 30.112 s |
| Warm near-247K generation, two calls / 512 output tokens | 26.988 s | 22.299 s |

Both engines recover all six markers at 4K, 32K and near 247K. The long
retrieval produces 199 completion tokens in each engine. Reported long
prefill throughput is 609.8 tokens/s for llama.cpp and 1196.1 for Gufo,
about 1.96 times higher. Warm fixed-generation wall time is 17.4% lower.
Aggregate throughput is output tokens divided by total HTTP wall time, not
the sum of individual request rates. Fixed generation windows deliberately
truncate output and are not graded completed coding tasks.

Cache capacity matters. The initial Gufo run uses a 4 GiB RAM-cache budget
and completes the long retrieval correctly. Its next request can restore
only 61,440 tokens, so that continuation is deliberately stopped rather than
reported as a comparable warm timing. A separate 28 GiB-cache run first
processes the continuation cold, then measures two fully warmed calls.
Those reuse 247,163 tokens. The corresponding llama.cpp calls reuse 247,159
and reevaluate four. Only the warmed calls enter the last table row.

The larger Gufo cache run reaches about 8.63 GiB host memory available in
backend logs, while five-second host samples reach 9.13 GiB. The comparable
llama.cpp host samples reach 21.54 GiB. The 28 GiB setting is a chosen cache
budget, not a demonstrated minimum or intrinsic model footprint. Neither
run triggers the 4 GiB host-available guard.

Gufo also passes thinking-mode, math, typed-tool/replay, streaming, Responses,
cache and cancellation/recovery checks. That is useful compatibility
evidence, not a coding-quality grade. Its source narrows some F32 tensors
to F16 and uses F16 intermediate matrix paths. F16 KV and identical GGUF
files therefore do **not** establish the arithmetic parity required for
Paracetamol's precision-preserving llama.cpp patches. No quality regression
is demonstrated by these small checks, but no broad equivalence is either.
Gufo remains unintegrated pending that separate decision and evaluation.

#### Isolated retained-PM4 runtime trial

The runtime experiment builds HIP and ROCr from
[`pwilkin/rocm-systems`](https://github.com/pwilkin/rocm-systems/tree/7dda3ac6cfe6bbe0b7f08c23a67cfa118d8641a1)
at `7dda3ac6cfe6bbe0b7f08c23a67cfa118d8641a1`, using the local toolbox recipe
as a reference. Private libraries live only in the experimental image.
The accepted `r48` llama.cpp binaries, model shards, F16 KV and matrix
arithmetic are unchanged.

All short arms set `GPU_MAX_HW_QUEUES=1`. The custom-runtime off/on arms both
use `DEBUG_HIP_GRAPH_CLASSIC_PATH=0`, changing only
`DEBUG_HIP_GRAPH_PM4`. Thus their comparison does not also change graph
executor. All three pass the original populated-state fixture with NMSE 0,
plus two 32K retrievals, cache reuse and cancellation/recovery.

| Short generation arm | Total HTTP wall time, 768 output tokens |
| --- | --- |
| Stock runtime | 36.474 s |
| Custom runtime, PM4 off | 35.599 s |
| Custom runtime, PM4 on | 31.547 s |

The on arm reduces wall time by 13.5% against stock and 11.4% against the
same custom runtime with PM4 off. All nine fixed-generation outputs are
byte-identical across the three arms. There is no short-prefill gain.
Aggregate 32K prefill is 777.5, 773.6 and 766.7 tokens/s respectively.
Repeating the stock short control after the long run gives 36.414 s, within
0.2% of its initial result.

The on arm also passes the 247,165-token retrieval, three fixed-generation
continuations, fresh-request recovery, cache and cancellation checks. Cold
retrieval takes 404.655 s, with 625.1 tokens/s reported prefill, versus
415.957 s and 609.8 on the original baseline. It generates 195 rather than
199 completion tokens, so wall time is not a fixed-output speed comparison.
The last two fully warm 256-token calls total 23.972 s versus 26.988 s,
11.2% less wall time. Those calls have matching cache-hit counts and output
lengths. This long comparison also changes the hardware-queue setting from
the original production baseline. Only the short three-arm comparison
isolates the PM4 switch itself. Five-second host samples retain at least
21.34 GiB available memory, without triggering the 4 GiB stop guard.

The diagnostic run confirms `[hipGraph][PM4] retained 2924 dispatches in
84233 dwords`, rather than inferring activation from an environment flag.
Live process mappings also confirm the private HIP/ROCr libraries are
loaded. Diagnostic logging is excluded from the timing table. This remains
a small sequential screen, not a general model or runtime qualification.

The reference recipe explicitly warns that managed allocation combined with
retained PM4 corrupts its MTP context. This investigation therefore stays on
Flash-Next's existing no-unified-memory, non-MTP path. It does not test or
change the default dense Q8 MTP path. A custom runtime requires wider
correctness, model and maintenance review before production adoption.

The final kernel window contains no GPU fault, SVM failure, reset or OOM
event. It contains the Python split-lock notices documented in the
DwarfStar update below. All experiment containers are stopped and removed,
and the acceptance host is left idle with its original host settings and main checkout.

Raw requests, exact source diffs, build inputs, images, logs and results are
retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261004-native-refresh/`.
Raw corpus requests are private and must not be published with this summary.

### Fedora 44 Strix Halo DwarfStar `0aaea5a` update (2026-10-04)

This update advances DwarfStar from
`6289c516273979173abbc062209a81dd3706b804` to
`0aaea5a238fb41a35106a551e73c8409dfb751ac`, an 80-commit range, on top of
Paracetamol `757ba19`. The delivery is identified by this section's
introducing commit. ROCm 10.0, Ubuntu 26.04, managed model bytes, context
defaults and DSpark policy are unchanged.

The acceptance host remains on Fedora 44, kernel `7.1.7-200.fc44.x86_64`, Ryzen AI Max+ 395
`gfx1151`, `/dev/dri/renderD128`, 128 GB LPDDR5X-8000 and its existing
112 GiB GPU allowance. Platform profile is `balanced` and CPU EPP is
`balance_performance`. No host setting changes or overlapping GPU workloads
are part of these measurements.

The final `--no-layer-cache` build produces
`localhost/paracetamol:dwarfstar-ubuntu26.04-rocm10.0-0aaea5a-r11`, image ID
`a6387f5dfdcf0093781486021a733387080d5d48444f79f3c34832084fe009d7`.
Runtime dependency closure, `pip check`, retained-binary help, all four HIP
code objects and minimal-image checks pass. The MIT and Iris notices remain
in the image. Compared with the measured candidate, the CLI and benchmark
are byte-identical. The server matches after stripping debug/build-ID data
following a whitespace-only patch cleanup. The final tagged image is tested
again with GPU acceptance and the client/gateway checks below.

The ordinary and DSpark servers both pass at the managed 131072-token
ceiling. Checks cover thinking off/on, a 6,618-token retrieval input, nested
typed tool arguments and tool-result replay, streaming, Responses,
conversation-cache reuse and cancellation followed by a correct fresh
answer. This is a 128K allocation test, not a 128K populated conversation.
The automated acceptance case and ordinary/DSpark CLI checks also pass.

The short performance screen uses three sequential requests with the same
33-token prompt, temperature zero, seed 42, no prompt-cache hits and 256
generated tokens each. The partial Go worker-pool outputs hold generation
length fixed. They are not graded coding tasks.

| Path | Previous total HTTP wall time | Updated total HTTP wall time |
| --- | --- | --- |
| Ordinary, 768 output tokens | 51.786 s | 51.520 s |
| DSpark, 768 output tokens | 50.241 s | 50.263 s |

There is no meaningful speed change in this screen. DSpark remains opt-in.
The ROCm kernel files themselves are unchanged. Shared server, cache routing,
reasoning and tool-rendering code is updated. The gfx11 WMMA fallback still
applies unchanged, and the GLM typed-argument patch is rebased onto the new
shared parser signatures. Upstream's model-free server tests and additional
memory, session-state, sampling, TCP failure and image-decoding tests pass.

Real Pi and Maki runs through the gateway both execute file-read and shell
tools and return the two actual fixture values. Pi's off run emits no
reasoning. The gateway switches from the unchanged dense Q8 llama.cpp model
to DwarfStar and back successfully. It starts unloaded, publishes the backend
only on an ephemeral loopback port, retains the read-only container and
removes its exact owned container on exit.

Host Tier 1 checks and a forced normal Go build pass. The kernel journal
through the DwarfStar checks has no GPU fault, reset, SVM or OOM event. It
does contain Python split-lock notices during the unchanged PyTorch preflight
probe. Those notices are not silently counted as a clean kernel journal.

This does not add Flash-Next or DeepSeek V4.1 to the DwarfStar catalog. The
[selected upstream source](https://github.com/antirez/ds4/blob/0aaea5a238fb41a35106a551e73c8409dfb751ac/ds4.c)
explicitly rejects both on ROCm, despite its Metal/CUDA implementations.

`gfx1150`, `gfx1200` and `gfx1201` inference remains `BLOCKED` for this
update because representative memory-capable hardware is unavailable.
The [previous DwarfStar handoff](#fedora-44-strix-halo-dwarfstar-6289c51-update-2026-09-09)
still applies with the new source and image. Four compiled device targets
do not constitute four hardware passes.

Source identities, build output, raw requests, client events, runtime
inspection and kernel logs are retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261004-native-refresh/`.
The same archive separates the unaccepted llama.cpp rebase and Gufo/PM4
experiments from this accepted DwarfStar update.

### Managed Pi 1.0.2 and sandbox networking (2026-10-04)

Client acceptance is `PASS` on the acceptance host with the unchanged `r48` llama.cpp image
and managed model files. Pi advances from 0.84.2 to 1.0.2, with all 146 npm
packages pinned by registry integrity and license metadata. Installation
passes on the development host's Node.js 22.22.1 and the acceptance host's Node.js 24.18.0 without package
lifecycle scripts. No host package or inference setting changes.

Ten live managed-Pi runs cover Qwen3.8 dense Q8 MTP at off, low, medium and
xhigh, Qwen3.6 dense Q8 MTP at off and high, and Muse DFlash 256K at low,
medium, high and xhigh. Captured requests retain the correct native reasoning
fields. Qwen requests omit sampling overrides, leaving the existing
server-side mode-aware defaults authoritative. Both off cases contain no
reasoning. Every run executes real file-read and shell calls successfully.

The Qwen3.8 off run also fetches an HTTPS page through `curl` inside the
ordinary sandbox. Interactive `!curl` works too. Separate comparisons on both
hosts get HTTP 200 inside and outside the exact managed sandbox. DuckDuckGo
HTML and Lite instead return HTTP 202 bot challenges in both environments.
Those failures are not evidence that the sandbox blocks networking.

The fullscreen UI retains family-grouped model selection, the immediate
reasoning selector and the settled-run completion divider. The sandbox hides
the host SSH directory and GPU device while keeping its project and private
state writable. No sandbox tier, search service or plugin is added.

These are client integration checks, not a model-performance comparison or
new GPU-architecture acceptance. Commands, raw requests, Pi events and
network comparisons are retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20261004-pi-refresh/`.

### Fedora 44 Strix Halo router cancellation (2026-10-01)

Router cancellation is `PASS` on the acceptance host's `gfx1151`. The accepted change adds
[upstream PR 29707](https://github.com/ggml-org/llama.cpp/pull/29707) at
`31b5b49bf600d43ea3b95a52f6b05650a71679c9` to Paracetamol
`d58a7fc03479630c2e7916c96ed8e3a1a74f3567`. The llama.cpp source pin remains
`2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`.

The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-2090f60-r48`, ID
`5b273803ca046d955bbeb3c50600678000c06d1f55d57d0efa15d734ba815853`.
It was built and tested on September 30 without application layer cache,
including all four HIP targets and Vulkan. The October 1 integration uses
the exact same patch and build inputs. Runtime library closure, code-object
checks, `pip check` and CPU-only empty-router startup pass. The CPU router
has a read-only root and no GPU devices.

The baseline `r47` router kept generating after a non-streaming client
disconnected. Its child was still busy after 5.04 seconds and had generated
105 tokens. The test waits for actual generation before closing the socket
and requests 8192 tokens with EOS ignored, so a natural completion cannot
stand in for cancellation. Streaming cancellation already passed.

With `r48`, both streaming and non-streaming disconnects return the child
slot to idle. The public gateway passes 18 such checks across dense Q8 MTP,
Flash-Next and a switch back to dense Q8 MTP. Observed cancellation time is
0.21–0.72 seconds. Follow-up requests, tool exchanges and streaming pass.
The gateway starts with no backend loaded, publishes its backend only on an
ephemeral loopback port, and removes its owned container on exit. Pi and
Maki each perform real file-read and shell calls and return both exact
fixture values.

The native ROCm router passes the reasoning, tools, Responses, sampling,
history-template and cache-reuse screens for dense Qwen3.8 Q8 with and
without MTP and for Flash-Next. Two 32K Flash-Next retrieval requests recover
all six exact values. Both Qwen3.6 MTP variants, Muse with and without
DFlash, Qwen3.8 Q4 MTP, Gemma MTP and KAT pass their applicable protocol
screens. Concurrent clients and model switchback also pass.

There is no new model-performance claim. `libllama.so.0`, `libggml-hip.so.0`
and `libggml-vulkan.so.0` are byte-identical between `r47` and `r48`. Model
files, templates, sampling defaults, F16 K/V and inference arithmetic are
unchanged. The earlier 247K acceptance is not repeated for this
server-lifecycle change. Other candidate performance patches are not
included.

September 30 host Tier 1 checks, the forced normal build and the Go race
pass succeed. No new kernel errors appear during the final acceptance
interval. The acceptance host remains on kernel `7.1.7-200.fc44.x86_64`, balanced platform
profile, `balance_performance` CPU EPP and its existing 112 GiB GPU memory
allowance. Other GPU architectures have no new hardware acceptance from
this change.

The source snapshots, baseline failure, raw requests, image and library
manifests, build output and regression logs are retained on the acceptance host below
`~/.local/share/paracetamol/apps/acceptance/results/20260930-llama-pr-trials/`.

The October 1 delivery repeats the gateway checks on that exact image. All
18 disconnects pass in 0.21–0.73 seconds, as do dense → Flash-Next → dense
tool exchanges, Pi/Maki tool execution, clean shutdown, CPU-only startup and
runtime dependency checks. Host Tier 1, the forced normal build and the Go
race pass also succeed. No new kernel journal entries appear, and the acceptance host is
left idle. Delivery logs are retained beside the earlier archive under
`20261001-router-cancellation/`.

### Fedora 44 Strix Halo llama.cpp source refresh (2026-09-30)

Acceptance is `PASS` on the acceptance host's `gfx1151`. This refresh advances llama.cpp from `7ab4ee7`
to `2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`, 193 upstream commits, on top
of Paracetamol `574dc693ef15cf57015fc6e9f8b2a86371804a9f`. All 13 downstream
patches remain. Eight apply unchanged and five are rebased. The
[patch ledger](upgrading.md#llamacpp-downstream-patch-ledger) records the
upstream audit and the API changes preserved during rebasing.

The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-2090f60-r47`, ID
`8e5f3ce367aa7ff026aaee0043f67c6001c5b95a327d37c161ec65d13341213c`.
It was rebuilt without application layer cache for all four HIP targets and
Vulkan. ROCm 10.0, Ubuntu 26.04, model artifacts, templates, samplers, default
dense Q8 MTP model and F16 K/V are unchanged. The retired mixed-arithmetic
feature is not reintroduced.

On the acceptance host's `gfx1151`, native operator checks pass 165 QSA/Qwen GPU cases,
41 CPU reference cases, 18 selection-disabled cases, 75 GDN cases, both
prefix policies and prepared-key invalidation tests. The nine selected
model-free upstream tests pass. Runtime library closure, four-target code
objects and `pip check` pass. Managed Qwen3 0.6B GPU acceptance passes.
Upstream HC numerical controls pass 38 HIP and 26 Vulkan cases, including
the new gated post-combine fusion. Installed OS and Python package versions
match the baseline image.

A new hardware fixture populates the real Flash-Next cache beyond 4K tokens,
seeds its derived keys and compares logits after restore, a non-causal then
causal transition, and clear plus restore. All three comparisons have zero
normalized mean squared error and the same top token. This directly tests
the downstream graph-buffer reservation retained around upstream's changed
causal-mode API.

The initial operator invocation stopped at an obsolete fixture argument.
Upstream's MMQ source-precision API now rejects F32 there. The fixture now
uses the supported explicit Q8 override while retaining its exact comparison
against the unfused upstream path. A separate state-driver invocation
rejected the allocator flag before loading a model because it selected the
generic rather than CLI option set. Both initial logs are retained. Neither
was a model-output failure, and neither fix changes production arithmetic.

Paired timings use the previous `r46` image, identical managed Unsloth Qwen3.8
27B Dynamic Q8_K_XL bytes, batch 2048, microbatch 512, F16 K/V and three
repetitions. These native cases do not use MTP. Each measures 512 prompt
tokens or 128 generated tokens, with either empty context or 32768 cached
tokens. The acceptance host remains on its balanced platform profile and
`balance_performance` CPU EPP.

| Backend and cached depth | Old prompt tokens/s | New prompt tokens/s | Old decode tokens/s | New decode tokens/s |
| --- | ---: | ---: | ---: | ---: |
| ROCm, empty | 370.917 | 366.825 | 7.203 | 7.214 |
| ROCm, 32K | 263.969 | 264.255 | 6.759 | 6.778 |
| Vulkan, empty | 280.747 | 366.907 | 7.313 | 7.313 |
| Vulkan, 32K | 201.091 | 242.123 | 6.788 | 6.790 |

These ROCm differences are within ordinary variation. The shallow
Vulkan prefill gain is about 31%, and the 32K gain is about 20%, with no
decode gain. The other native cases pass for Q8 cache on both backends and
Q4 cache on HIP, all at 32K. HIP changes are below 1%. Vulkan Q8-cache prefill
rises from 169.072 to 233.469 tokens/s, while decode remains 6.967 tokens/s.
These are native timing controls, not task-quality comparisons.

The ROCm router passes reasoning off and named efforts, streaming, nested
tool calls and follow-up, native Responses, per-mode sampling defaults,
explicit overrides, history templates and cache reuse on dense Qwen3.8 Q8
with and without MTP and on Flash-Next. Both 32K Flash-Next archive requests
recover all six exact values. Switching back to dense Q8, two concurrent
clients and cancellation recovery also pass. Both Qwen3.6 MTP variants,
Muse with and without DFlash, Qwen3.8 Q4 MTP, Gemma MTP and KAT pass their
applicable protocol screens.

The Vulkan router passes the dense Q8 MTP/non-MTP and Flash-Next functional
screens, both 32K Flash-Next retrievals, switchback, concurrency and
cancellation recovery too.

On ROCm, the dense server exposes four slots with unified KV and a
262144-token per-sequence limit. Flash-Next exposes one slot with non-unified
KV at the same limit. Timing clients are sequential. The separate two-client
check tests correctness, not a throughput claim.

The populated-context comparison uses the same 247165-token public Go/docs
corpus on both images, medium reasoning, temperature zero and seed 42. Both
models recover all six distributed archive values and stop naturally. Dense
Q8 produces identical answer and reasoning text on both images, including
all three subsequent cached continuations. Flash-Next returns the same exact
retrieval answer but its reasoning is not identical, using 199 versus 195
completion tokens. These are retrieval and recovery screens, not broad
quality-parity evidence.

| ROCm model at 247165 input tokens | Old prompt tokens/s | New prompt tokens/s | Old cold HTTP seconds | New cold HTTP seconds |
| --- | ---: | ---: | ---: | ---: |
| Dense Q8 MTP | 139.417 | 139.468 | 1792.161 | 1791.398 |
| Flash-Next Dynamic Q4_K_XL | 621.271 | 628.821 | 408.695 | 403.505 |

For each image/model pair, three cached continuations generate 768 tokens
in total. Aggregate tokens divided by total HTTP wall time are 7.767 → 7.802
tokens/s for dense Q8 and 16.859 → 17.141 for Flash-Next. Those windows
intentionally end at 256 output tokens each and are not completed-task
scores. Cache reuse counts match between images. There is one cold request
and three cached windows per pair, so the small differences do not establish
a meaningful ROCm speedup. Cold means fresh prompt state, not an emptied
filesystem cache. Fresh requests, cancellation and model switchback pass
after the populated-context tests.

One-shot CLI generation passes for the tiny model on ROCm and Vulkan, and
for dense Q8 MTP and Flash-Next on ROCm. The 32K Q8-cache controls on both
backends and Q4-cache control on ROCm pass output and tool-call checks too.
These optional cache tests do not change the F16 preset defaults.

The public gateway starts with no backend loaded, passes dense → Flash-Next
→ dense tool exchanges and streaming, and keeps its child publication on
an ephemeral loopback port. Managed Pi and Maki each perform real file-read
and shell calls and return both exact results. The gateway stops cleanly and
removes its owned backend. The exact final image also passes CPU-only empty
router startup with a read-only root and no GPU devices.

Host Tier 1 checks and the forced normal build pass. The full source-file
manifest is verified against the acceptance host's checkout, since its Git metadata predates
this delivery. All test containers are stopped and removed, the previous
image is retained, and no gateway was running before the tests to restore.
The kernel remains `7.1.7-200.fc44.x86_64` with the same 112 GiB GPU memory
allowance and balanced power policy. No new AMDGPU fault or reset is logged.
The journal retains the initial failed operator fixture's libc trap and a
Python diagnostic's CPU split-lock notification, rather than being reported
as entirely empty.

Actual GPU acceptance of this source update on `gfx1150`, `gfx1200` and
`gfx1201` is `BLOCKED` by unavailable hardware. Their code-object builds do
not replace inference evidence. The extracted Flash-Next fast paths remain
disabled outside their accepted `gfx1151` dispatch. Raw results, initial
failures, test drivers, source manifest and image identities are retained on
The acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20260930-llama-upstream-refresh/`.

### Fedora 44 Strix Halo mixed-math removal (2026-09-30)

The `r46` image removes the downstream mixed-arithmetic feature introduced
in Paracetamol `03ba79b62f66b36bd5e6f8b6fe35caa8e66418ac`. Its source patch,
build wiring, CLI flag, config setting and runtime-report field are gone.
There is no compatibility alias or hidden enable switch. The five earlier
precision-preserving Flash-Next extractions still apply automatically on
their accepted Strix Halo ROCm path. Model pins, dense Qwen default, F16 K/V,
upstream matrix arithmetic and host power policy are unchanged.

The build reused the accepted `r44` layers. The resulting image
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r46` is
byte-identical to that image, with ID
`af12785843f2d8271a4e6c3d95676e83cdd0eaf2e181b0c737cbed478312e486`.
This is an exact restoration, not a fresh native compilation or a new
performance result. The earlier near-256K retrieval, state-recovery and
four-HIP-target/Vulkan build evidence applies to these same image bytes.
The slower precision-preserving prefill numbers apply, not the retired
experiment's near-fork results.

Fresh checks against the restored image pass 165 GPU operator cases,
41 CPU reference cases, 18 selection-disabled cases and both prefix
policies. Library closure and `pip check` pass. The removed fast-math gate
is absent from the image's HIP and model libraries.

The updated normal launcher and runtime report pass. A 32501-token
Flash-Next request and its cached repeat return the exact eight-value JSON
and stop naturally at 164 output tokens. Tool call/follow-up, cache reuse,
cancellation recovery, two clients queued through one slot and sampling
defaults/overrides pass. These are functional screens, not broad task-quality
evidence or a new performance comparison.

Dense Qwen3.8 Q8 MTP passes retrieval, medium required-tool output and
sampling checks. Both the native router and public gateway pass dense →
Flash-Next → dense tool exchanges. The gateway reports six successful
requests and stops cleanly. Vulkan passes 4K Flash-Next retrieval. CPU-only
startup retains its read-only root, no GPU devices, zero effective/bounding
capabilities and `no-new-privileges`.

These fresh checks are `PASS` on the acceptance host's `gfx1151`. Other GPU classes remain
`N/P` for the extracted fast paths because dispatch is disabled there. No
near-256K run was repeated for this removal because the image is identical
to the already accepted one. All test containers were removed. The kernel
journal has no new entries during the 11:40–11:47 CEST acceptance window.
Balanced platform power and `balance_performance` CPU EPP remain unchanged.
Host Tier 1 checks, forced normal build and the race detector pass. Rejected
removed-flag/config tests pass. Raw results, image identity, test drivers and
the source diff are retained on the acceptance host under
`~/.local/share/paracetamol/apps/acceptance/results/20260930-qwen4exp-mixed-removal/`.

### Fedora 44 Strix Halo retired mixed arithmetic experiment (2026-09-30)

This experiment was **removed entirely in `r46`**. Its kernels and public
controls are no longer shipped. The screen below did not demonstrate a
quality regression, but was too small to establish broad quality parity.
The project chose to keep the precision-preserving optimizations without a
reduced-operand-precision mode. The near-fork performance below describes the
retired experiment, not the supported image.

The experimental `r45` image added an explicit mixed-math option on top of
Paracetamol `e500049cdd9e257a3a2c7ed5fc4611a8e78234b1`. The default arithmetic, default
dense Qwen model, GGUF bytes, F16 K/V and balanced host power settings stay
unchanged. This is a scoped extraction from the pinned MIT Strix fork, not a
switch to that fork. Upstream llama.cpp remains
`7ab4ee7baad2d920464cbacfad4f4b07cf111fd2` and the fork reference remains
`7a9196dad8b13b70fb3420886aadaf98fc09b849`.

The image is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r45`,
ID `c46b4e6366177df248d53866872d8d5309b1bf04b2ea2e015f9116cdc4ec89a2`.
The mixed-math patch SHA-256 is
`fed411acb56285192bc2139f2eba621d39e3f0a7929b7058bb609900c87b336b`.

The same-image comparison uses the pinned Unsloth Flash-Next Dynamic
Q4_K_XL at revision `c8b5954a88c2775c546b92593eda40ea041d3176`. The context
ceiling is 262144, K/V is F16, batch and microbatch are 2048, and there is one
slot and one sequential client. Direct embedding reads, allocation policy,
temperature zero, seed 42 and reasoning off match the preceding selection
comparison. The isolated timing requests retain upstream sampler defaults
otherwise. Normal managed sampling is checked separately.

| 32501 input tokens | Default math | Mixed math | Pinned full fork |
| --- | ---: | ---: | ---: |
| Cold prompt tokens/s | 769.867 | 967.461 | 1030.857 |
| Cold HTTP wall, including 256 output tokens | 54.584 s | 45.498 s | 43.192 s |

At 32K, mixed prefill is 25.7% faster than default and 6.1% below the full
fork. Cold request time falls by 16.6%. Three cached requests generate 768
tokens in 36.548 s with default math and 35.878 s with mixed math, or 21.013
and 21.406 tokens/s respectively. That 1.9% difference is not evidence of a
meaningful decode gain. Both modes pass all four fixed-output retrieval
checks and two separate natural-stop exact-JSON checks.

| 246701 input tokens | Default math | Mixed math | Pinned full fork |
| --- | ---: | ---: | ---: |
| Cold prompt tokens/s | 609.708 | 758.481 | 822.616 |
| Cold HTTP wall, including 256 output tokens | 417.915 s | 338.237 s | 312.406 s |

Near the 256K ceiling, mixed prefill is 24.4% faster than default and 7.8%
below the full fork. Cold request time falls by 19.1%. The three cached
requests take 40.361 s and 39.320 s for 768 output tokens, or 19.028 and
19.532 tokens/s. That small difference is not a meaningful decode claim.
Both modes pass all four fixed retrievals and two natural-stop exact-JSON
checks. Mixed math also passes the eight output checks around prefix reuse,
tail and early edits, cancellation, replacement and slot save/erase/restore.
The restored request performs a fresh prefill rather than claiming serialized
derived-cache reuse.

The fork reference uses `HIP_LAUNCH_BLOCKING=1`, which was needed for its
accepted run. The Paracetamol image does not. Cold means fresh server/prompt
state, not an emptied OS file cache. Fixed-output timing ignores EOS and is
not a finished-task quality benchmark. Each policy has one cold measurement
and three cached repetitions per context size, not a broad variance study.

The new policy uses BF16 operands for eligible Q8 and expert matrices, F16
operands for selected F32 projections, and a paired F32 normalization and
injection reduction. Accumulators, outputs and residuals remain F32. It
does not import the fork's BF16-only residual storage, shadow tensors or a
persistent conversion cache. It can change answers and does not claim
bitwise equivalence or universal quality parity.

The remaining fork gap is not simply an unported optimization. A diagnostic
fork build with its BF16-only output marks disabled reaches 778.112 prompt
tokens/s at the same near-256K input, only 2.6% above the clean mixed path.
That is evidence that the fork's more aggressive intermediate storage matters,
not an exact additive attribution of every difference. A separate 256 MiB
conversion-cache and producer-shadow prototype gave 706.513 rather than
709.050 tokens/s at long context despite verified cache hits. It was rejected,
as were a slower raw-BF16 indexer and an unhelpful normalization-loop unroll.
The integrated option deliberately avoids their state and maintenance cost.

The image builds all four HIP targets and Vulkan. Its own libraries pass
221 mixed-policy GPU cases plus 19 exact decode/fallback cases, all 244
default-policy GPU cases, 143 CPU reference cases and both prefix policies.
The mixed cases include independent dequantized-F32 matrix references,
explicit F32 precision fallback, expert quant formats, extra-reader fallback,
F32 residual preservation and unsupported geometry. Library closure and
`pip check` pass.

The first combined test invocation also ran four old bitwise Q8 prefill
comparisons under the deliberately different BF16 policy. Those asserted
equivalence that the opt-in does not promise, while every new numerical
oracle passed. The suites are now
partitioned by precision contract. Those four Q8 equality tests remain
unchanged and pass under the default policy. The paired F32 normalization
checks separately allow a maximum absolute delta of 5e-6 for the changed
summation order, while residual outputs must still match exactly.
The initial log is retained alongside the final results.

The public managed preset with `--qwen4exp-math mixed` independently passes
the 246701-token natural-stop exact-JSON request in 334.541 s, with 757.392
prompt tokens/s and 164 output tokens. Its cached repeat takes 8.808 s with
246697 prompt tokens reused. Tool call/follow-up, live cache reuse,
cancellation recovery, two clients queued through one slot and sampling
defaults/overrides pass. The runtime report and copyable reproduction command
both retain the resolved mixed policy.

A frozen eight-request precision screen compares both policies on ledger
arithmetic, event-state tracking, Go interval merging and an EINTR-aware
reader, each with thinking off and medium. Both score 6/8 overall and 4/4
at medium. Both pass both Go tasks at both levels, and both fail the same
two thinking-off bookkeeping tasks. Every answer stops naturally without
reaching the 4096-token limit. Each request performs an uncached roughly
8K-token prefill with temperature zero and seed 42. The task-spec SHA-256 is
`9e9b3f1a89d97308a96584472cd8c8367b815401f5fdc4d558a07c77d3478459`.
Generated code is graded offline in bubblewrap without access to the home
directory or checkout. Positive and negative grader controls pass.

This is a small one-shot regression screen, not the full Pi coding-agent
evaluation or evidence of universal quality parity. The answers and output
lengths differ. Faster task completion here cannot be attributed entirely
to kernel speed when the generated work also changes.

With mixed math enabled, dense Qwen3.8 Q8 passes the 32K retrieval screen
at 310.247 prompt tokens/s, compared with 310.273 in the preceding `r44`
check. The dense MTP preset passes retrieval, tool call/follow-up, medium
required-tool output and sampling defaults/overrides. Its retrieval accepts
54 of 81 draft tokens. The native router passes dense → Flash-Next → dense
tool exchanges. These are regression checks, not a dense-model speedup claim.
Vulkan with default math passes its 4K Flash-Next retrieval. CPU-only startup
passes the read-only root, no GPU devices, zero effective/bounding
capabilities and `no-new-privileges` checks.

The public gateway also passes dense → Flash-Next → dense tool exchanges
with mixed math enabled, reports six successful requests and stops cleanly.
All test containers are removed. The uninstrumented production acceptance
window from 09:43 through 10:52 CEST has no new kernel-journal entries.
Earlier profiler bus-lock/exit warnings remain in the experimental evidence
and are not presented as clean production runs. The acceptance host remains on balanced
platform power and `balance_performance` CPU EPP.

The retired opt-in passed this finite acceptance screen for the pinned
Flash-Next model on the acceptance host's `gfx1151`.
Other GPU classes are `N/P` because dispatch is disabled there, despite their
successful compilation. This does not accept future Qwen4 models, Flash-Next
MTP or broad task-quality equivalence. Both the default dense model and
default arithmetic remained unchanged. Host Tier 1 checks, forced normal build
and the race detector pass. Raw results, frozen graders, numerical logs,
source snapshots, diagnostic ablations and provenance are retained on the acceptance host
under `~/.local/share/paracetamol/apps/acceptance/results/20260930-qwen4exp-mixed-math/`.

### Fedora 44 Strix Halo Qwen4exp selection extraction (2026-09-30)

The `r44` image adds precision-preserving selected-block expansion and an
equivalent CPU singleton-set check on top of the four earlier extractions.
The source baseline is Paracetamol `8f0a65c6413f15a89a72ab38108674765fe9b10e`.
Upstream llama.cpp, the MIT Strix fork reference, model pins, balanced power
profile, F16 KV and matrix arithmetic are unchanged. The new patch SHA-256 is
`42ff460ebf1c663f7c2b463e0e7d73f7bb26caee66c53e246a018fe7badc7bd5`.
The image is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r44`,
ID `af12785843f2d8271a4e6c3d95676e83cdd0eaf2e181b0c737cbed478312e486`.

The same-image control sets `PARACETAMOL_QWEN4EXP_QSA_SELECTION=0`. Both
runs use the pinned Unsloth Flash-Next Dynamic Q4_K_XL, a 262144 context
ceiling, F16 K/V, 2048 batch and microbatch, one slot and one sequential
client. The fixed-output timing uses temperature zero, seed 42 and reasoning
off. Other upstream sampler defaults, direct embedding reads and allocation
policy match the preceding pipeline comparison. Cold means a fresh server
and prompt cache, not an emptied OS file cache.

| Matched 246701-token request | Selection off | Selection on |
| --- | ---: | ---: |
| Cold prompt tokens/s | 605.180 | 625.626 |
| Cold prompt processing | 407.649 s | 394.327 s |
| Cold HTTP wall, including 256 output tokens | 421.793 s | 407.253 s |
| Three cached requests, aggregate output tokens / total HTTP time | 18.042 tokens/s | 19.405 tokens/s |
| Three cached requests, total HTTP time | 42.567 s | 39.578 s |

This is a 3.4% prefill gain and a 3.4% reduction in cold request time. The
cached rate improves by 7.6% in this screen. There is one cold pair and three
cached repetitions per variant, not a broad variance study. Each cached
request reuses 246697 input tokens. All fixed-output requests recover the
eight values. Both policies also pass two natural-stop exact-JSON checks.
Fixed-output timing ignores EOS and is not a task-quality benchmark.

The final image builds all four HIP targets and Vulkan. Its own libraries
pass 165 supported GPU cases, 41 selected CPU cases and 18 GPU selection
opt-out cases. Selection tests cover the actual sliced-tail view/reshape,
large block counts, STEP boundaries and an additional reader that prevents
fusion. Both CPU prefix policies pass every singleton sequence bit and
shared-bit pair. Library closure and `pip check` pass. Eight natural-output
state checks pass, including cancellation, prompt replacement and slot
save/erase/restore. No new persistent state or precision downcast is added.

The normal managed preset passes the same 246701-token natural JSON task in
408.006 s at 618.256 prompt tokens/s, stopping after 164 output tokens. The
repeat finishes in 8.945 s with 246697 cached input tokens. Tools and tool
continuations, prefix reuse, cancellation recovery, two clients queued
through one slot and mode-dependent sampling checks pass.

Dense Qwen3.8 27B Dynamic Q8 passes 32K retrieval at 310.273 prompt tokens/s,
matching the preceding 310.159 screen. The default dense MTP preset passes
retrieval with 54/81 draft tokens accepted, sampling and a medium-effort
required tool call. Native router switching from dense MTP to Flash-Next and
back passes tool calls and continuations. Vulkan Flash-Next passes 4K
retrieval. CPU-only startup retains a read-only root, no GPU devices, zero
effective/bounding capabilities and `no-new-privileges`.

This is `PASS` for the pinned Flash-Next ROCm path on `gfx1151`, not future
Qwen4 models or Flash-Next MTP. Other architectures are `N/P` for this
dispatch. The uninstrumented final-image kernel-journal window has no new
entries. Earlier instrumented experiments are not included in that claim.
All test containers are removed and the original balanced power settings
remain in place. Detailed results, sources, commands and provenance are kept
under `~/.local/share/paracetamol/apps/acceptance/results/20260930-qwen4exp-selection/`
on the acceptance host.

### Fedora 44 Strix Halo Qwen4exp pipeline extraction (2026-09-30)

Paracetamol commit `b79f202884ac255489cac7a80e560d69b4166c71` adds a fourth
scoped extraction from the MIT Strix fork at
`7a9196dad8b13b70fb3420886aadaf98fc09b849`. Upstream
llama.cpp remains pinned at `7ab4ee7baad2d920464cbacfad4f4b07cf111fd2`.
The image is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r43`,
ID `f9f244a6cd06079ab191160e0d6ecaa8b074c39ef24dc7e44c1359a74b109160`.
The pipeline patch SHA-256 is
`12322a8740cf828c3dd881100cb95025ff5f3bf8f59748bf287d8b1ef12d523a`.

This extraction accelerates prefill without changing the GGUF, F16 K/V,
F32 residuals or upstream quantized matrix arithmetic. It adds direct
convolution with tail-only history copies, compact expert routing and tiles,
ordered vector reduction, normalization/injection fusion, grouped decode
projections and private Q8 matrix prefetch. The injection projection uses a
numerically tested parallel F32 reduction. It does not import BF16/F16 matrix
experiments or the fork's persistent reduced-precision residual machinery.

The paired comparison uses the same final image with the new pipeline
enabled and disabled. All three earlier optimizations remain enabled in both.
The pinned Unsloth Flash-Next Dynamic Q4_K_XL runs with a 262144 context
ceiling, F16 K/V, 2048 batch and microbatch, one slot and one sequential client.
Input, model pins, SSD reader, allocation policy and balanced platform profile
are unchanged. The isolated timing requests use temperature zero, seed 42,
reasoning off and other upstream sampler defaults. Normal managed sampling
is tested separately.

| Matched 246701-token request | Pipeline off | Pipeline on |
| --- | ---: | ---: |
| Cold prompt tokens/s | 474.285 | 606.960 |
| Cold prompt processing | 520.153 s | 406.454 s |
| Cold HTTP wall, including 256 output tokens | 534.419 s | 420.432 s |
| Three cached requests, aggregate output tokens / total HTTP time | 17.813 tokens/s | 18.037 tokens/s |
| Three cached requests, total HTTP time | 43.115 s | 42.579 s |

Prefill throughput improves by 28.0% and cold request time falls by 21.3%.
The 1.3% cached-rate difference is not evidence of a meaningful decode win.
Each cached request reuses 246697 input tokens. All eight responses recover
all requested values and match byte-for-byte. The separate fork and native
arithmetic prototype return those same fixed outputs too.

Fixed-output timing ignores EOS and is not a finished-task quality benchmark.
There is one cold pair and three cached repetitions per variant, not a broad
variance study.

The final image builds all four HIP targets and Vulkan. Its own libraries
pass 148 supported GPU operator cases and 24 selected CPU fallback cases,
including unsupported maskless geometry. The GPU backend rejects that
geometry as intended. Native prefix, derived-cache ledger, all 50 allocation
and reader tests, library closure and `pip check` pass. Regression fixtures
also detect an unsafe intervening convolution-input mutation in the earlier
prototype and pass with the final guard. Descriptor capacity and explicit
matrix source precision are bounded, with ordinary fallbacks.

Eight natural-output state checks pass, covering reuse, beginning/end edits,
cancellation recovery, prompt replacement and save/erase/restore. The normal
managed preset returns all eight exact record/code pairs from 246701 tokens
and stops naturally after 164 output tokens. It takes 420.594 s total at
599.619 prompt tokens/s. The identical JSON repeat reuses 246697 input tokens
and finishes in 9.377 s. Tools and tool-result continuation, prefix reuse,
cancellation, two clients queued through one slot and sampler-policy checks
also pass. These managed checks use actual mode-dependent defaults except
the long probe's explicit temperature zero, not another on/off timing pair.

Dense Qwen3.8 27B Dynamic Q8_K_XL passes 32K retrieval at 310.159 prompt
tokens/s, essentially unchanged from the preceding 309.365 screen. The
default Q8 MTP preset passes retrieval, sampling and medium-effort required
tools, with 54/81 draft tokens accepted in retrieval. Native router switching
from dense MTP to Flash-Next and back passes tools and continuations. Vulkan
Flash-Next returns the correct 4K code. CPU-only startup has no GPU device
arguments, a read-only root, zero effective/bounding capabilities and
`no-new-privileges`.

Separate arithmetic experiments were tested, not enabled. Matching the
fork's native CPU build flags gives the following uninstrumented results.

| Input tokens | BF16/F16 matrix prototype | Pinned full fork | Prefill gap |
| --- | ---: | ---: | ---: |
| 129701 | 807.431 tokens/s | 919.120 tokens/s | 12.2% |
| 246701 | 714.680 tokens/s | 829.999 tokens/s | 13.9% |

The prototype passes 85 numerical cases and fixed/natural exact retrieval
at both depths. Those narrow checks do not establish broad quality parity.
It uses BF16 quantized-matrix operands and F16 high parts for selected F32
projections, while retaining F32 residuals and F16 K/V. The fork uses further
BF16 shadow/residual paths and requires launch blocking in this reference.
Its kernel name `f32split` does not mean exact F32 arithmetic. An independently
tested compensated version was more accurate but slower. Native CPU flags
and launch blocking did not close the gap. The tested prototype still does
not meet the requested 5-10% prefill target. The production F32 path remains
26.9% below the fork near 256K. Conversion reuse and persistent
reduced-precision state are remaining candidates, not established safe wins.

These are the earlier `r43` findings. The September 30 selection and retired
mixed-math records above supersede that remaining-gap assessment and document
the rejected cache experiments. Only the precision-preserving extractions
remain in the supported image.

The pinned Flash-Next ROCm path is `PASS` on the acceptance host's `gfx1151`, not acceptance
for future Qwen4 models or Flash-Next MTP. Other GPU classes are `N/P` for
these optimizations because their dispatch is disabled there. The production
kernel-journal window is clear. Earlier instrumented profiling caused CPU
bus-lock traps, and two test-fixture crashes were fixed. Those diagnostic logs
are retained and are not represented as a clean full experiment. Performance
rates come only from uninstrumented runs. All test containers are removed,
and the acceptance host is left idle on its original balanced power settings.

Detailed sources, commands, image identities, numerical tests and measurements
are retained on the acceptance host at
`~/.local/share/paracetamol/apps/acceptance/results/20260930-qwen4exp-pipeline/results.md`.

### Fedora 44 Strix Halo Qwen4exp prepared-key reuse (2026-09-29)

Paracetamol commit `e449985dcc1608e20c3b61d5ca6e7944e453b1ee` keeps upstream
llama.cpp and the first two patches intact. This third extraction adds a
local derived-key cache inspired by the pinned Strix fork,
not the fork's physical-cell cache, fast-prefill machinery or BF16 paths.
The image is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r42`,
ID `82d09bef38455a36d59150d20853a83b8dcbadb55e94995054a63a28a1b5e664`.
The new patch SHA-256 is
`0dbf5f1258c1a9630edb383b7a7bd71d6fb3bbc98b992df32bbd7589426c525b`.

Only successful single-token appends reuse derived F32 indexer keys. The
last complete block and tail sentinel are refreshed with the original
ordered operations. Prefills keep the ordinary graph. Prompt edits, rewinds,
memory mutations, failed evaluation and restores invalidate derived state.
The cache is not serialized. It costs about 384 MiB at 256K for this model
and changes neither F16 K/V nor weights, residual precision or sampling.

The matched comparison uses the same final image with
`PARACETAMOL_QWEN4EXP_QSA_CACHE=0` and `=1`. The first two optimizations remain
enabled in both. The pinned Unsloth Flash-Next Dynamic Q4_K_XL runs at 262144
context, F16 K/V, 2048 batch and microbatch, one slot and one sequential client.
Temperature is zero, seed is 42 and reasoning is off. Other sampler settings
are upstream defaults for this isolated comparison, not the managed preset's
mode-dependent defaults. The buffered direct reader and allocation policy
are unchanged. The acceptance host remains on its balanced platform profile.

| Matched 246701-token request | Cache off | Cache on |
| --- | ---: | ---: |
| Cold prompt tokens/s | 496.286 | 480.984 |
| Cold prompt processing | 497.095 s | 512.908 s |
| Cold HTTP wall, including 256 output tokens | 516.960 s | 527.080 s |
| Three cached requests, aggregate output tokens / total HTTP time | 12.932 tokens/s | 17.950 tokens/s |
| Three cached requests, total HTTP time | 59.386 s | 42.785 s |

Cached generation throughput improves by 38.8% and waiting time falls by
28.0%. Prefill is 3.1% slower in this pair and the cold request is 2.0% slower.
This is a decode improvement, not a prefill win or full-fork parity. Each
cached request reuses 246697 input tokens. All eight responses are
byte-identical and contain the eight requested verification values, spread
across the prompt. Ignoring EOS for fixed 256-token timing deliberately
produces truncated output, not a completed-task quality benchmark. There is
one cold pair and three cached repetitions, not a broad variance study.

The final image's ROCm libraries pass 59 supported operator cases and reject
unsupported maskless geometry. All 24 selected CPU fallback cases pass without
GPU devices. Ten prepared-key cases compare full and selective preparation
exactly, through 65535 blocks with and without the tail sentinel. Fourteen
extra Q8 matrix cases exercise unchanged production kernels because the
experimental matrix prefetch code was not retained. All four AMD targets and
Vulkan build, and the native prefix, cache-ledger and 50 existing allocation
and reader checks pass. Library closure and `pip check` also pass.

Eight natural-output state checks return exact JSON maps. They cover a
repeated prompt, changed beginning and end, cancellation recovery, shorter
and original replacements, and save/erase/restore. Restore correctly
recomputes the prompt when the hybrid snapshot lacks its prior rewind
checkpoint. It does not assume the derived cache survived serialization.

The normal managed preset also returns all eight exact record/code pairs
from 246701 input tokens and stops naturally after 164 output tokens. It
takes 499.671 s total, with 503.247 prompt tokens/s and 17.407 generated
tokens/s. The repeat returns identical JSON, reuses 246697 input tokens and
finishes in 9.419 s. This uses the real mode-dependent sampler defaults except
the explicit temperature of zero. It is a correctness/reuse screen, not
another on/off timing pair. Nested tool calls, tool-result continuation,
cancellation recovery, prefix reuse, two clients queued through the single
slot and sampler-policy checks also pass on the managed Flash-Next preset.

Dense Qwen3.8 27B Dynamic Q8_K_XL returns the correct 32K retrieval code at
309.365 prompt tokens/s, matching the preceding 309.785 screen. The default
Q8 MTP preset passes retrieval, sampler policy and medium-effort required
tools, with 54/81 draft tokens accepted in the 4K retrieval. Native router
switching from dense MTP to Flash-Next and back passes tool calls and their
continuations with one loaded model allowed. Vulkan Flash-Next returns the
correct 4K retrieval code. CPU startup passes with no GPU device arguments,
a read-only root, dropped capabilities and `no-new-privileges`.

The pinned Flash-Next ROCm path is `PASS` on the acceptance host's `gfx1151`. This is not
acceptance for future Qwen4 models or Flash-Next MTP. Other GPU classes are
`N/P` for this optimization because the entrypoint gates it off there. The
root kernel journal has no new entries during the production tests. All
test containers are removed and the acceptance host is left idle on its original balanced
power settings.

Q8 matrix prefetch was tested separately and excluded. One version regressed
prefill. A tile-adjusted version needed a coupled warp-mapping correction to
pass its numerical tests, then improved one matched 129K prefill by only 3.7%
with unchanged decode. That small result did not justify the additional
shared-kernel changes. The remaining prefill gap to the fork is not resolved.

Detailed results are retained on the acceptance host at
`~/.local/share/paracetamol/apps/acceptance/results/20260929-qwen4exp-prepared-keys/results.md`.

### Fedora 44 Strix Halo Qwen4exp graph optimizations (2026-09-29)

The second extraction from the Strix fork builds on the selected-key kernels
below. Paracetamol commit `e18bc898e1573f055b155b20d1d512feddb2608f` keeps
the first patch intact and adds compact single-prefix selection, scorer strips, ordered F32 pooling
and score fusions, scoped Q8 matvec prefetch and small expert-matrix tiles.
It does not import the fork as another engine. The image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r41`, ID
`bcc2eacd59e7daf50055621977dcb47597178f2b2e050e891cb5e35d5a62ab61`.
The pinned Unsloth Flash-Next Dynamic Q4_K_XL is `PASS` on the acceptance host's `gfx1151`
ROCm path. This is not acceptance for future Qwen4 models or Flash-Next MTP.

The paired comparison uses this same image with
`PARACETAMOL_QWEN4EXP_QSA_GRAPH=0` and `=1`. The original selected-key kernels
remain enabled in both runs. Both use
the same pinned Unsloth Flash-Next Dynamic Q4_K_XL, 262144 context, F16 K/V,
2048 batch and microbatch, one slot and one sequential client. Temperature
is zero, seed is 42 and reasoning is off. These isolated timing requests use
upstream sampler defaults except the explicit temperature, rather than the
managed model's mode-dependent sampler defaults. Model loading, allocation
and the host's balanced power policy are unchanged.

| Matched 129648-token request | First patch only | Both patches |
| --- | ---: | ---: |
| Cold prompt tokens/s | 417.325 | 523.438 |
| Cold prompt processing | 310.665 s | 247.685 s |
| Cold HTTP wall, including 128 output tokens | 323.894 s | 255.724 s |
| Three cached requests, aggregate output tokens / total HTTP time | 9.664 tokens/s | 15.783 tokens/s |
| Three cached requests, total HTTP time | 39.736 s | 24.330 s |

The second patch improves prefill throughput by 25.4%, cuts cold request time
by 21.0% and cuts cached request time by 38.8%. Each cached request generates
128 tokens and reuses 129644 input tokens. All eight responses recover the
correct verification code. Ignoring EOS for fixed-output timing is intentional
and is not a completed-task quality benchmark. These are one paired cold run
and three cached repetitions per variant, not a broad variance study.

The fork still has a measurable lead. A separate run of its actual pinned
binary reached 921.731 prompt tokens/s at 129K. Disabling its BF16 shadow
matrix and residual machinery reduced that to 645.412 tokens/s, still ahead
of this extraction. The remaining gap is therefore not explained solely by
precision changes. Persistent pooled-key state, cross-split recurrent
fusions, broad scheduler/kernel changes and BF16 shadow/residual paths are
not included. A tested F16-input BLAS alternative for Q8 prefill was slower
and was discarded.

The normal managed preset also recovers all eight exact record/code pairs
from 246701 input tokens, including the beginning and end of the prompt.
It finishes naturally after 164 output tokens. Prefill takes 496.038 s
(497.343 tokens/s), decode runs at 12.714 tokens/s and total HTTP time is
508.943 s. A repeat returns the identical JSON, reuses 246697 input tokens
and finishes in 13.073 s. This longer test uses the real managed sampling
defaults and is a correctness/reuse screen, not an on/off timing comparison.
Nested tool calls, tool-result continuation, cancellation recovery, prefix
reuse, two clients queued through the single slot and sampler-policy checks
also pass on the final managed Flash-Next preset.

Compact selection follows the model reference's budget of complete blocks
plus the partial tail. The prior expanded-token selection could include an
extra partial block, so the new graph does not promise bit-identical answers.
It retains F16 K/V, stored GGUF weights and F32 residuals. Shared, gapped or
otherwise unsupported cache layouts use the ordinary graph.

All four managed AMD targets and Vulkan build successfully. The final image's
ROCm libraries pass 35 supported operator cases against CPU references and
independent oracles, including nine Q8 cases exactly matching the original
GPU result. An unsupported maskless attention geometry is rejected by HIP.
Fourteen CPU fallback cases also pass against the final libraries, including
that unsupported geometry.

The prefix eligibility test and existing native allocation/reader tests pass
during the image build. The ordinary dense Qwen3.8 27B Dynamic Q8_K_XL preset
passes 32K retrieval at 309.785 prompt tokens/s, essentially unchanged from
`r40` at 309.806. The default Q8 MTP preset passes 4K retrieval, nested tools,
a medium-effort required function call and managed sampler checks. Its
retrieval accepts 54 of 81 draft tokens, matching the previous screen.
Vulkan Flash-Next returns the correct 4K answer without the new HIP graph.
A CPU-only small-model server reaches `/health` with no GPU devices, no
effective or bounding capabilities and the normal read-only confinement.
All test containers are stopped. No GPU faults appear in the kernel journal
during the production-image test interval. Other hardware classes are `N/P`
for this optimization because dispatch is intentionally limited to `gfx1151`.

Detailed results are retained on the acceptance host at
`~/.local/share/paracetamol/apps/acceptance/results/20260929-qwen4exp-graph-optimizations/results.md`.

### Fedora 44 Strix Halo Qwen4exp selected-key kernels (2026-09-29)

The scoped Qwen4exp HIP attention change in Paracetamol commit
`734811e11505147d6427dbc6a8f88fb30fb52a6e` is `PASS` for the pinned
Unsloth Qwen3.8 Flash-Next Dynamic Q4_K_XL GGUF on the acceptance host's `gfx1151` ROCm
path. It is not acceptance for future Qwen4 models or MTP. The locally built
image is `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r40`,
ID `175803f83890045015f4a337d316c00830475803af2241d91d874b96bf52deeb`.
The llama.cpp pin, GGUF, 262144 context, F16 K/V cache, direct model reader,
and allocation policy are unchanged. Only exact Strix Halo ROCm receives the
new selected-key kernels. Vulkan and older Qwen models retain their previous
attention paths.

The paired runs use the same experimental binary with
`PARACETAMOL_QWEN4EXP_QSA_KERNEL=0` and `=1`, one slot, 2048 batch and
microbatch, and the same uncached deterministic retrieval requests. Every
run returned the exact verification code. These are single measurements, not
a variance estimate or a model-quality comparison.

| Prompt tokens | Kernel off, prompt tokens/s | Kernel on, prompt tokens/s | HTTP wall, off | HTTP wall, on |
| ---: | ---: | ---: | ---: | ---: |
| 32448 | 442.765 | 526.723 | 80.172 s | 68.030 s |
| 129648 | 224.929 | 442.318 | 585.830 s | 302.423 s |

At 129K the selected-key kernels nearly double prompt throughput and reduce
request wall time by 48%. This is valuable, but it does not reproduce the
fork's roughly 3x prefill claim. The broader fork was not imported because its
other changes regressed the trusted Q8 path in the comparison. A separately
tested scorer-strip addition did not materially improve the kernel-only result.

The final managed image returned the correct code from a cold 246648-token
prompt in 717.160 s total, with 350.212 prompt tokens/s. A byte-identical
repeat reused 246644 prompt tokens and completed in 12.749 s. The regular
Qwen3.8 27B Dynamic Q8_K_XL preset returned the correct 32K retrieval code at
309.806 prompt tokens/s, matching its earlier `r39` screen near 309 tokens/s.
The default Q8 MTP preset also passed retrieval and a medium-effort required
function call. The final image's Vulkan Flash-Next preset returned the correct
4K answer without the HIP kernel. A CPU-only small-model server reached
`/health` with no GPU devices and normal confinement. All managed test
containers were stopped, and no AMD GPU faults appeared in the kernel log
during the test interval.

The five selected-key operator cases passed on ROCm against the CPU reference,
including poisoned masked cells, duplicate key IDs, and an FP64 oracle for
short-query decode. The detailed run notes are retained on the acceptance host at
`~/.local/share/paracetamol/apps/acceptance/results/20260929-qwen4exp-qsa-kernels/results.md`.
Other hardware classes are `N/P` for this optimization because dispatch is
intentionally gated to `gfx1151`. The general application matrix below remains
separate from this focused acceptance.

### Fedora 44 Strix Halo Flash-Next QSA gather (2026-09-25)

The `gfx1151` Flash-Next inference screen is `PASS` on ROCm and Vulkan. This
qualifies one pinned Unsloth Dynamic Q4_K_XL GGUF, not the proposed
Flash-Next MTP path or future Qwen4 models. Paracetamol commit
`cad1549dd0d33be3c96dc8af2227764a500b2ec3` contains the accepted change.
The llama.cpp source stays at
`7ab4ee7baad2d920464cbacfad4f4b07cf111fd2`. The `r39` packaging revision
adds the upstream QSA gather proposal at `beed2f78ac42cf16710b763e6f3ba20665c6d233`
without changing models, templates, sampling, context, K/V precision or the
allocation policy. The final cold-built image is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r39`, ID
`b9b51434825c4b147240990a2d1a4ecd5c31365e074b1b5cbf98539440731d7a`.

On the acceptance host's 128 GB Ryzen AI Max+ 395, the matched ROCm control uses the same
earlier `r39` image ID
`dfcec82e5b52d7636149b23ff87e17dd41208a574a113ba4aa21d4b91c7c1e17`.
The only container-command difference is `QWEN4EXP_QSA_GATHER=0` for the
control. The final cold build has byte-identical `llama-server`, `libllama`,
and all five ggml shared libraries to that measured image. Both runs use the
managed Strix Halo ROCm preset, one slot, F16 K/V, 262144 context, one
sequential client, medium reasoning, temperature zero and seed 42. The
247165-token prompt is assembled from the same 143-file public corpus, SHA-256
`fd8ef9463952604f8a497b0d99b66d2e50d63394cf39e8e48d00f855f49b2b6b`.

| Matched 247K ROCm measure | Gather on | Gather off |
| --- | ---: | ---: |
| Initial prefill | 1759.787 s | 1769.492 s |
| Three cached 256-token continuations, backend decode | 124.683 s, 6.160 tokens/s | 154.987 s, 4.955 tokens/s |
| Same continuations, total HTTP wall time | 149.579 s | 180.035 s |

Gather improves this deep-context decode rate by 24.3% and cuts the three
continuations' wall time by 16.9%. Initial prefill is effectively unchanged;
the first retrieval request is only about 1% quicker because its prefill
dominates. Both runs return all six distributed keys exactly, then pass a
fresh-request recovery check. The retrieval uses 199 output tokens with
gather and 204 without it, so the reasoning traces are not bit-identical.
The truncated 256-token continuations are timing inputs, not a code-quality
grade. This is one paired long run, not a variance estimate.

Short 4K and 32K retrievals also return the exact keys on ROCm and Vulkan.
The measured `r39` Vulkan image returns the six keys at 247165 prompt tokens,
with 1368.986 s prefill and 6.869 tokens/s retrieval decode. Three cached
continuations take 118.361 s of total HTTP time, close to the archived `r38`
run's 116.591 s. That historical comparison is a regression screen, not a
matched Vulkan speedup claim. Minimum sampled available memory is 27.65 GiB
on ROCm and 34.77 GiB on Vulkan; no memory guard fires.

The final cold image passes dependency closure, `pip check`, `llama-cli`
version startup and shared-library resolution. Both ROCm and Vulkan pass
Flash-Next nested tools, cache reuse, cancellation recovery and concurrent
requests on the final image. The default Unsloth Dynamic Q8_K_XL 27B MTP
preset passes the same protocol checks, plus managed sampler defaults and
explicit overrides. A CPU-only lazy router reaches `/health` with no GPU
devices, a read-only root and the expected confinement. Test containers are
stopped afterward. No AMD GPU faults appear in the test interval's kernel log.
The real gateway also passes Q8 MTP to Flash-Next to Q8 MTP switching with a
nested tool round-trip at each stop. It removes its backend on shutdown.

The request/response traces, container inspections, memory samples and test
driver are retained on the acceptance host and the development host under
`~/.local/share/paracetamol/apps/acceptance/results/20260925-qsa-gather/`.

Other hardware classes remain `N/P` for this patch until inference tests run
on `gfx1150`, `gfx1200` and `gfx1201`. The general application acceptance
matrix below is not made `PASS` by this focused test.

On 2026-09-25, the Fedora Strix Halo host verified the pinned Swift 1.5
Q8_0 GGUF in place and served its new publisher-prefixed preset through the
ROCm gateway with 256K context and three MTP draft tokens. A medium-effort
HTTP function call and tool-result continuation completed, followed by Pi and
Maki one-shot responses. Pi without a model override still loaded the
publisher-prefixed Unsloth Q8/MTP default. This was a focused integration
smoke on the existing llama.cpp `7ab4ee7` r38 image, not a full quality,
`xhigh`, streaming, or cross-hardware acceptance pass.

Both Swift presets were subsequently retired from the catalog. The earlier
runtime smokes remain valid historical observations, but did not predict
the outcome of a longer Pi coding task. See the [retirement
record](swift-retirement.md).

### Fedora 44 Strix Halo llama.cpp `7ab4ee7` update (2026-09-22)

The reviewed native checks are `PASS` on Strix Halo. This is a maintenance
update, not a universal speedup. Default Q8 MTP short decode is essentially
unchanged, while its near-256K prefill is 3.4% slower in one paired run.
Flash-Next prefill improves by 5.0% on ROCm and 11.2% on Vulkan at the same
populated context. Model files and managed defaults stay unchanged.

This update starts from Paracetamol
`64d23cccf291b2008e963b6827ed83f88ba06650` and moves llama.cpp from
`8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f` to
`7ab4ee7baad2d920464cbacfad4f4b07cf111fd2`. Five downstream patches apply
unchanged. Two need context-only rebases. Model files, templates, sampling,
speculative depths, F16 K/V and allocation policies are unchanged.

Both images were rebuilt on the same day, with identical installed OS and
Python package versions. The control is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r37`, image ID
`49ee2f93b467d51fc7fe4b3ad9c9a501efa042250f70a2e9e33c0a3260cd0cfc`.
The final `--no-cache` build is
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-7ab4ee7-r38`, image ID
`121e3f12891e8908a87c300e37c6e897698d0768781038e851aa98b8f6ad2901`.

The host is the 128 GB Ryzen AI Max+ 395 above, using `gfx1151`,
`/dev/kfd` and `/dev/dri/renderD128`. Fedora 44, kernel
`7.1.7-200.fc44.x86_64`, ROCm 10.0, balanced platform profile,
`balance_performance` CPU EPP and the 112 GiB GPU allowance remain unchanged.
File-by-file manifests verify the source inputs shared by the local checkout
and remote build mirror, independently of the mirror's older Git metadata.

#### Fixed throughput controls

Four tiny smoke cases and fourteen representative native cases pass. The
representative workload uses the unchanged **Unsloth Dynamic Q8_K_XL**
Qwen3.8 27B, without MTP, 512 prompt tokens, 128 generated tokens and three
repetitions. Batch is 2048, microbatch 512 and Flash Attention is explicit.
These are native benchmark rates in tokens/s, not whole-request HTTP rates.

| Backend | Populated depth | K/V | Old PP | New PP | Old TG | New TG |
| --- | --- | --- | --- | --- | --- | --- |
| ROCm | 0 | F16 | 371.07 | 371.14 | 7.171 | 7.201 |
| ROCm | 32768 | F16 | 266.97 | 265.13 | 6.772 | 6.768 |
| ROCm | 32768 | Q8_0 | 262.71 | 261.06 | 6.864 | 6.858 |
| ROCm | 32768 | Q4_0 | 261.37 | 260.68 | 6.849 | 6.850 |
| Vulkan | 0 | F16 | 282.44 | 278.94 | 7.305 | 7.305 |
| Vulkan | 32768 | F16 | 202.39 | 201.39 | 6.789 | 6.790 |
| Vulkan | 32768 | Q8_0 | 170.57 | 170.16 | 6.966 | 6.966 |

There is no meaningful dense-model throughput change in this screen. The
quantized-KV rows exercise retained patch paths, not new managed defaults.

Three short 256-token continuations through the ROCm server take 49.633s
before and 49.432s after the update with Q8 MTP. This is 15.473 to 15.536
generated tokens per second of total HTTP time. All texts match and both
builds accept 529 of 701 draft tokens, or 75.46%. The roughly 0.4% timing
change is not a useful speedup claim.

Flash-Next Dynamic Q4_K_XL takes 39.180s before and 37.461s after on the same
short timing workload, about 4.4% less time, with differing output text.
Two uncached 32167-token ROCm retrievals per build return all six distributed
keys. Their answers and reasoning match exactly. Combined prefill falls
from 165.529s to 147.503s, about 10.9% less time, and combined HTTP time falls
from 192.092s to 173.610s, about 9.6% less.

#### Populated context and recovery

All six long retrievals recover the six distributed keys from 247165 prompt
tokens. The table gives seconds, with positive prefill change meaning slower.
Cached time is total HTTP time for three 256-token continuations.

| Model and backend | Old prefill | New prefill | Prefill change | Old cached time | New cached time |
| --- | --- | --- | --- | --- | --- |
| Q8 MTP, ROCm | 1747.480 | 1806.262 | +3.4% | 98.783 | 100.095 |
| Flash-Next, ROCm | 1824.899 | 1732.751 | -5.0% | 176.649 | 174.072 |
| Flash-Next, Vulkan | 1529.110 | 1357.789 | -11.2% | 118.529 | 116.591 |

Q8 returns 227 completion tokens on both builds. All its retrieval and
continuation texts, including reasoning, match exactly, as do cache and MTP
acceptance counts. Its roughly 1.3% longer cached time and 3.4% longer prefill
are small observed slowdowns, not erased by the unchanged short decode rate.
Total retrieval HTTP time rises from 1766.688s to 1826.030s.

Vulkan Flash-Next returns 199 tokens on both builds with identical retrieval
answers and reasoning. Total retrieval HTTP time falls from 1558.244s to
1386.721s. ROCm Flash-Next returns 199 tokens before and 204 after, with
different reasoning. Its HTTP time falls from 1864.440s to 1772.849s, but
prefill is the cleaner comparison. Flash-Next continuation texts differ on
both backends. Their cached-time improvements are only about 1.5% and 1.6%.
Fresh queries, prefix reuse, cancellation and meaningful Q8 MTP recovery pass
after every long suite.

Performance requests use one sequential client. Long retrievals use medium,
temperature 0 and seed 42, with an initially uncached prompt and three
intentionally cached follow-ups. The long result is one retrieval per image,
not a repeated statistical estimate or a cold-filesystem-cache test.
Fixed-length continuations include reasoning tokens and deliberately stop at
the output limit. They are timing evidence, not completed-code quality grades.
The assembled 143-file public Go/documentation corpus has SHA-256
`fd8ef9463952604f8a497b0d99b66d2e50d63394cf39e8e48d00f855f49b2b6b`.
The evidence archive retains complete request bodies as well as responses.

The managed Q8 MTP and Vulkan Flash-Next routers have four logical slots,
262144 context and unified KV. ROCm Flash-Next has one non-unified slot,
batch/microbatch 2048, explicit CPU embedding reads and the existing
model-scoped allocator opt-out. These settings match within each old/new
pair. The different backend policies and historical one-slot Vulkan runs
must not be presented as an otherwise identical backend comparison.

#### Functional coverage and limits

- The final image passes dependency closure, `pip check`, non-root CPU lazy
  router startup and numerical tests against its actual runtime libraries.
  ROCm passes 75 GDN and 35 HC cases. Vulkan passes its 23 supported HC cases.
  The twelve non-four-head HC_PRE shapes remain unsupported on Vulkan.
- Q8 MTP, non-MTP and Flash-Next pass Chat Completions, native Responses,
  reasoning choices, nested tools, streaming, sampler defaults and explicit
  overrides, history, prefix reuse, cancellation and model switchback.
  Separate Qwen3.6 dense and MoE MTP templates also pass. Muse with and
  without DFlash passes math, tools and history. Q4 MTP, Gemma MTP and KAT
  pass nested tools. Two concurrent Q8 requests test correctness, not a
  throughput gain.
- Real answer-and-tool checks pass with ROCm Q8_0/Q4_0 and Vulkan Q8_0 KV.
  One-shot GPU CLI runs return their requested answers and exit cleanly for
  the tiny model on both backends and Q8 MTP/Flash-Next on ROCm.
- The real gateway passes Q8 to Flash-Next to Q8 switching, meaningful
  multi-turn answers, tools and streaming. Its private backend is published
  only on loopback. Pi 0.84.2 and Maki 0.4.8 each execute a fixture read and
  shell command and return the actual results. Pi's explicit thinking-off
  run reports zero reasoning tokens. The non-interactive test shell supplies
  Maki's existing executable path without changing host configuration.
- Host Tier 1, a forced normal build, the static build, Go race tests,
  upstream CPU chat/Jinja/PEG/argument/schema tests, 199 JSON-schema
  assertions and 50 native-policy cases pass.

The general automated acceptance command is `BLOCKED` by the absent PyTorch
diagnostic image. Native image and GPU checks above ran independently. Do
not count that command as passing, or a four-target build as inference
acceptance of `gfx1150`, `gfx1200` or `gfx1201`. Those hardware checks remain
deferred under the [standard handoff](#deferred-acceptance-handoff).
This refresh does not qualify unreleased Qwen4 artifacts or enable Flash-Next
MTP.

Minimum available RAM ranges from 27.59 to 27.87 GiB across the paired long
suites. The largest sampled increase in swap use is 0.45 GiB, without a
memory-guard stop. No new kernel entries appear during testing. Test
containers are removed, and the normal loopback/LAN gateway is restored
without an eagerly loaded backend. Remote status access from the other host
also succeeds.

Raw requests, responses, client tool events, memory traces, image inspections,
build logs, source manifests and the small probes are retained on both hosts
under
`~/.local/share/paracetamol/apps/acceptance/results/20260922-llama-refresh/`.
The old image is retained as a rollback control. The
[source and patch review](upgrading.md#llamacpp-downstream-patch-ledger)
records the included upstream work and proposals left out of this update.

### Fedora 44 Strix Halo Flash-Next managed ROCm integration (2026-09-13)

The follow-up to the direct-reader screen is `PASS` through the real managed
gateway and native router. It starts from Paracetamol `ea46185` and builds
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r37`, image ID
`62409b7ee2e07ff17741cf4023d48fdfb311a4fc6e38430b81b4c226bf0ed4fb`.
All five libggml shared-library hashes, including CPU, HIP and Vulkan, match
the retained `r36` control. Native model-loading and process-startup policy
change, not the compute kernels. The kernel, power policy, ROCm, GGUF,
F16 KV and 262144-token ceiling remain unchanged.

Only the Flash-Next child uses explicit CPU embedding reads, device allocation
without inherited unified-memory allocation, batch/microbatch 2048, one
non-unified KV slot and no RAM prompt archive. The parent does not mutate its
environment when inspecting that preset. The catalog restricts this ROCm
path to Strix Halo, with host exact-node discovery and a second container
profile check. Other presets retain their accepted policies.

Qwen3.8 Q8 MTP passed before and after Flash-Next, followed by Qwen3.6 dense
MTP, Muse DFlash and a Flash-Next reload in the same router with
`--models-max 1`. Checks included meaningful output, multi-turn memory,
reasoning modes, nested tools and results, streaming, sampler defaults and
overrides, reasoning history, prefix reuse and cancellation as applicable.
Real Pi and Maki each completed a read-and-shell tool loop on Flash-Next.
Pi's off run generated zero reasoning tokens. Native Responses diagnostics
used the private backend, not an unsupported public gateway route.

Two uncached 32167-token retrievals returned all six keys at a combined
377.11 prompt tokens/s. The populated 247165-token retrieval also recovered
all six keys, with prefill 1861.003s and total HTTP 1903.357s. Three cached
256-token continuations took 183.465s in total, or 4.19 output tokens per second
of HTTP time. Fresh requests, prefix reuse, cancellation and Q8 recovery
passed afterward. Minimum sampled available RAM was 27.65 GiB over the long
suite. Swap use grew by at most 0.134 GiB, without a guard stop.

The unchanged Vulkan gateway recipe passed short protocol, two 32K retrievals,
cache and cancellation checks. The actual managed single-prompt CLI selected
ROCm and returned its sentinel. Image dependency closure, non-root CPU
entrypoint startup, native reader/preset tests, Tier 1 and the Go race pass
succeeded. No new kernel warnings appeared. Test containers were removed and
the normal loopback/LAN gateway was restored without loading a backend.

These are bounded execution and capacity checks, not a coding-quality grade
or a long-duration reliability guarantee. Flash-Next still has no MTP.
Other GPU architectures are `N/P` for this model-scoped ROCm path and are
not enabled by its catalog restriction. The [full follow-up and
limits](qwen3.8-flash-next-direct-reader-feasibility.md#managed-rocm-integration)
include retained Vulkan behavior, failed packaging/probe attempts and exact
evidence locations.

### Fedora 44 Strix Halo Flash-Next direct-reader screen (2026-09-13)

Starting from Paracetamol `01492e62986bdab408b5c46195635b540f4be805`, an
isolated build of [PR 28136](https://github.com/ggml-org/llama.cpp/pull/28136)
at `c6a9e5c9ae6d6a551217f75c9a04b2e8b1aa62dd` was compared with the current
`8172e65-r36` image. ROCm 10.0, kernel `7.1.7-200.fc44.x86_64`, balanced
power policy, the existing Unsloth Dynamic Q4_K_XL GGUF, F16 KV and the
262144-token ceiling remained unchanged. Only `gfx1151` was inference-tested.

The experimental allocation-absent path was `PASS` for short protocol checks,
varied-source 4K/32K retrieval, populated 247K retrieval, cached continuations
and recovery. Direct reads raised minimum available RAM from 8.04 to
35.06 GiB in the paired long suites, without a meaningful prefill gain.
Using a 2048-token microbatch then improved 32K prompt throughput by 41.5%
and reduced 247K prefill from 2096.668 to 1848.348 seconds. The latter run
retained at least 27.68 GiB available. Decode did not improve, and replacing
the tail of a cached long prompt required more prefix work.

Restoring `GGML_CUDA_ENABLE_UNIFIED_MEMORY` was still `FAIL`, with corrupt
prose, bad tool arguments and HTTP 500s despite a passing stream sentinel.
No kernel warnings appeared. The reader is not a fix for that allocation
mode. At the end of this isolated screen, managed Flash-Next remained
Vulkan-only, without MTP, and production was unchanged. The later managed
integration above adds a model-scoped exception rather than fixing that
ordinary allocation path.

The [full result](qwen3.8-flash-next-direct-reader-feasibility.md) records the
candidate image, controls, numerical checks, microbatch trade-offs and the
remaining router/client integration boundary. Raw evidence is retained on
both hosts under
`~/.local/share/paracetamol/apps/acceptance/results/20260913-flash-next-direct-reader/`.
The default Q8 MTP production recovery checks passed, test containers and
the candidate image tag were removed, and the original gateway was restored.

### Fedora 44 Strix Halo tiled GDN prefill (2026-09-13)

This screen starts from Paracetamol
`1d0da4b0d21369402b0b3fe2d2841854dcf93c06`. The introducing commit adds one
narrow HIP kernel patch without changing llama.cpp `8172e65`, ROCm 10.0,
model artifacts, F16 KV, samplers, MTP depth or power policy. The host was
the 128 GB Ryzen AI Max+ 395 above, with `/dev/dri/renderD128`, balanced
platform profile and `balance_performance` CPU EPP.

The normal affected-target rebuild produced
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r36`, image ID
`a034d9f0fe848d7d21e1a0facd00c46f70aebc09635714a21ae4c91cdb3f6196`.
All four GPU targets compiled. Ubuntu package versions matched the control.
Dependency closure, `pip check`, CPU lazy-router startup and tiny Qwen3 GPU
acceptance passed. The final runtime passed 75 numerical GDN/cache-fusion
cases against the CPU reference, including 34 new shape, layout, tail and
snapshot cases carried with the patch.

The isolated prototype improved default Qwen3.8 Dynamic Q8 MTP prompt
throughput by 10.2% at 3,897 input tokens and 8.6% at 33,047, with three
repetitions. At 247,627 tokens, one paired medium-reasoning retrieval saved
73.746 seconds of prefill, a 4.2% throughput gain. All six distributed keys
were correct. Retrieval and fixed continuation choices, including reasoning
and MTP counts, matched byte for byte. Generation was essentially unchanged.
The cleaned production image repeated the short performance and protocol
checks and retained the gain. Both dense Qwen3.6 aliases and the Q8/Q4 router
regressions also passed. Simultaneous Q8 requests were a correctness check,
not a concurrency throughput claim.

Flash-Next Dynamic Q4 gained 5.2%, 3.6% and 2.6% in the corresponding
experimental ROCm screen. Both long retrievals returned the correct keys,
but their truncated code continuations differed. This does not remove
Flash-Next's managed Vulkan-only restriction. The experimental allocation
policy is not the ordinary managed ROCm policy.

The [full comparison](qwen3.8-tiled-gdn-strix-halo-feasibility.md) separates
prototype timing from final-image acceptance and records limits and source
provenance. Timed runs had no new kernel warnings. Two CPU split-lock
warnings named Python during final hardware diagnostics, with no AMDGPU
fault and no failed acceptance case. Other GPU architectures were not
inference-tested, and the new dispatch is restricted to exact `gfx1151`.

Raw evidence is retained on both hosts under
`~/.local/share/paracetamol/apps/acceptance/results/20260913-tiled-gdn/`.
Temporary test containers and experiment image tags were cleaned up, and
the original gateway was restored with no backend eagerly loaded.

### Fedora 44 Strix Halo llama.cpp `8172e65` update (2026-09-11)

The comparison is based on Paracetamol
`bd56ebddcaaca72675975cc5aac1dd0ef0f3cd5b`, with the source update recorded in
this section's introducing commit. The host was the 128 GB Ryzen AI Max+ 395
above, using `/dev/dri/renderD128`, Fedora 44 and kernel
`7.1.7-200.fc44.x86_64`. ROCm 10.0, the balanced platform profile,
`balance_performance` CPU EPP, the 112 GiB GTT allowance and both TTM page
limits of 29360128 were unchanged. Model pins, sampling, speculative depths
and F16 target K/V defaults were unchanged.

The `--no-cache` build produced
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r35`, image ID
`66cb660ee864929a5981ec6adf17c0ade7ddedc9610afe4734949ac5b96e3eb7`, from
source `8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f`. The retained control was
the September 9 `6d9c82e-r34` image below. Installed Ubuntu package versions
were identical between these images. Both GPU backends compiled, `pip check`
passed, and the final image retained the three intended executables without
the build toolchain or PyTorch payload. Actual HIP device code was present
for all four managed architectures. Only `gfx1151` inference was tested.

All four downstream patches remain. Three apply unchanged. The Vulkan F16
patch needed only a rebase of its surrounding shader-table context.
[PR 28102](https://github.com/ggml-org/llama.cpp/pull/28102) is included in
the new upstream pin, not added as a fifth patch.

The controlled native comparison used the same Unsloth Dynamic Q8_K_XL
artifact and non-MTP alias as the September 9 record. Each case used
512 prompt tokens, 128 generated tokens, three repetitions, batch 2048,
microbatch 512 and Flash Attention on. Depth means populated history, not
the allocation ceiling. No other GPU workload or build ran alongside a
timed case. Values are average tokens per second.

| Backend | Populated depth | K/V type | Previous prompt | Updated prompt | Previous decode | Updated decode |
| --- | --- | --- | --- | --- | --- | --- |
| ROCm | 0 | F16 | 343.70 | 340.93 | 7.206 | 7.205 |
| ROCm | 32768 | F16 | 199.76 | 247.35 | 6.766 | 6.766 |
| ROCm | 32768 | Q8_0 | 207.67 | 243.76 | 6.864 | 6.861 |
| ROCm | 32768 | Q4_0 | 196.75 | 243.10 | 6.855 | 6.853 |
| Vulkan | 0 | F16 | 276.87 | 278.96 | 7.307 | 7.304 |
| Vulkan | 32768 | F16 | 199.80 | 200.30 | 6.780 | 6.788 |
| Vulkan | 32768 | Q8_0 | 168.02 | 169.27 | 6.960 | 6.967 |

ROCm F16 prompt processing improved 23.8% at 32K history. The optional
Q8_0 and Q4_0 cache controls improved 17.4% and 23.6% respectively. Decode,
shallow-context performance and Vulkan were essentially unchanged. Four
tiny Qwen3 0.6B controls on both images and backends completed the 18-case
matrix. These are native non-MTP measurements, not default MTP server speed
or coding-task quality scores.

The actual default `qwen3.8-27b-mtp-ud-q8-k-xl` was compared separately
through a direct ROCm server at a 262144-token ceiling, one slot and the
unchanged three-token draft depth. Three sequential uncached requests each
generated 256 tokens with thinking off, temperature zero and seed 42. All
six partial code outputs were byte-identical. Total HTTP time was 49.014
seconds before and 49.300 after, equivalent to 15.67 and 15.58 generated
tokens/s across the three requests. Backend-timed decode was 16.05 and
15.97 tokens/s. Both differences are below 1%, not a meaningful speed change.
These requests followed readiness, so neither timing includes model loading.

Direct Q8 MTP checks passed for thinking off, low,
medium and xhigh, structured nested tools and their result continuation,
streaming termination and usage, Responses, reasoning-history preservation,
prefix-cache reuse, and cancellation followed by a correct fresh request.
Effective sampler checks passed for omitted fields, partial overrides and
nulls in both thinking modes. Both 33,058-token retrieval runs returned the
two keys exactly, in 119.127 seconds directly and 117.567 through the router.
Two simultaneous router requests returned their correct sentinels. The
router reported four slots, unified KV and a 262144-token per-slot ceiling.
This was concurrency correctness coverage, not a throughput comparison.

The lazy router passed Qwen3.6 dense and sparse MTP, Muse DFlash at
256K, Qwen3.8 MTP and non-MTP Q8, Qwen3.8 MTP Q4 at 128K, and Gemma 4 MTP tool
regressions. Separate real-answer and nested-tool checks passed with ROCm
Q8_0 and Q4_0 cache and Vulkan Q8_0 cache. CPU-only lazy-router startup and
the automated tiny GPU smoke passed. Explicit dependency checks covered
both GPU libraries and the Vulkan loader as well as all three executables.

#### Populated near-256K Q8 MTP comparison

Each image ran one 247,627-token synthetic archive retrieval. Both returned
all six distributed keys correctly, with medium reasoning, temperature zero
and seed 42. The model, F16 cache, one-slot 262144-token ceiling and three-token
MTP depth were unchanged. Requests followed model readiness but began without
a cached prompt prefix, so these are not cold model-loading measurements.

| Measure | Previous image | Updated image |
| --- | --- | --- |
| Prompt processing time | 2751.641 s | 1817.242 s |
| Prompt tokens/s | 89.99 | 136.27 |
| Complete retrieval HTTP time | 2767.682 s | 1833.862 s |
| Three cached 256-token continuation requests, total HTTP time | 94.944 s | 97.087 s |
| Continuation generated tokens divided by total HTTP time | 8.09 tokens/s | 7.91 tokens/s |
| Backend-timed continuation decode | 9.32 tokens/s | 8.74 tokens/s |

The cold prompt took 34.0% less time, a 51.4% increase in prompt throughput.
This is a useful long-context gain, not a blanket generation-speed improvement.
The fixed continuations were 2.3% slower over HTTP and 6.3% slower in
backend-timed decode. Their reasoning differed between images, with MTP
acceptance changing from 171 of 251 proposed tokens to 165 of 265 per
request, so this is not a byte-identical decode-kernel comparison. All three
requests within each image produced identical reasoning and reused over
247K prompt tokens. All six windows contained only reasoning, deliberately
truncated at 256 tokens, not completed code. Both fresh unrelated requests
afterward returned exactly `AFTER_LONG_OK`. There were no new kernel warnings
during the paired Q8 runs.

#### Populated near-256K Flash-Next Vulkan comparison

Both images passed the ordinary managed Flash-Next Dynamic Q4_K_XL Vulkan
path with F16 K/V, one slot and a 262144-token ceiling, without MTP. Loading
remained lazy mmap with the CPU ngram-embedding override, and the RAM prompt
archive retained its default. Real-answer, nested-tool, reasoning-mode,
streaming, Responses and history checks passed. The 33,058-token retrieval
returned both keys correctly in 109.880 seconds before and 105.000 after.

Each image then ran the same single 247,627-token retrieval and three cached
256-token continuations used above. All six keys were correct, and the fresh
unrelated request afterward passed on both images.

| Measure | Previous image | Updated image |
| --- | --- | --- |
| Prompt processing time | 1245.578 s | 1212.747 s |
| Prompt tokens/s | 198.80 | 204.19 |
| Complete retrieval HTTP time | 1270.283 s | 1237.304 s |
| Three cached continuation requests, total HTTP time | 118.715 s | 118.128 s |
| Continuation generated tokens divided by total HTTP time | 6.47 tokens/s | 6.50 tokens/s |
| Backend-timed continuation decode | 6.89 tokens/s | 6.93 tokens/s |

The 2.6% prefill-time improvement is modest in this single paired screen,
and generation changed by less than 1%. This is not a major Flash-Next speed
improvement. The continuation outputs differed and were deliberately
truncated, so they are not a code-quality comparison. Both cases retained
more than 28.9 GiB of sampled host available memory, with no new kernel
warnings and clean shutdown. These are controlled old/new-image comparisons
within Vulkan. The ROCm experiment below uses different loading and cache
policies and must not be treated as a backend-only A/B test.

#### Flash-Next ROCm memory-policy screen

The managed Flash-Next Dynamic Q4_K_XL preset remains Vulkan-only, without
MTP. The normal ROCm memory policy still produced corrupt prose, incorrect
tool arguments and HTTP 500s on both images. The candidate also encountered
a kernel page-allocation warning during loading. A tiny streaming sentinel
passed despite the corruption, so it is not a sufficient acceptance test.

An allocation-mode comparison on the old image isolated a useful difference.
Resident loading with `GGML_CUDA_ENABLE_UNIFIED_MEMORY` present was corrupt,
while the otherwise matched absent-flag case produced coherent responses
and correct tools. Setting the variable to `0` is not equivalent to removing
it, because upstream tests its presence. Simply removing it while retaining
lazy mmap loading instead hit repeated SVM resident-limit failures. Disabling
pinned buffers did not rescue that lazy-loading case. These experiments
narrow the failure to allocation/loading behavior, not a proven underlying
driver defect. They are consistent with the allocation-mode reports in
[issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797).

A separate candidate experiment used the following native settings with the
same verified four-shard GGUF, managed template, F16 K/V and medium reasoning.
These are recorded diagnostic settings, not new Paracetamol CLI options.

| Setting | Experimental value |
| --- | --- |
| Allocation environment | `GGML_CUDA_ENABLE_UNIFIED_MEMORY` absent, enforced by an isolated entrypoint overlay |
| Model loading | Resident, `--load-mode none` |
| Large ngram embedding | `--override-tensor per_layer_token_embd.weight=CPU` |
| CPU weight buffers | `--no-host`, without globally setting `GGML_CUDA_NO_PINNED` |
| Context and slots | `--ctx-size 262144 --parallel 1`, non-unified KV |
| Automatic fitting | `--fit off` |
| RAM prompt archive | `--cache-ram 0`, retaining live-prefix reuse |
| Safety guard | Cancel this experiment below 4 GiB of sampled host available memory |

The first resident candidate kept upstream's 8192 MiB RAM prompt-cache
default. Short protocol checks and 33K retrieval passed, but archiving earlier
prompts consumed the remaining headroom and triggered the safety guard as
the large request began. Disabling that archive avoided the problem.
It did not reduce model precision or the context ceiling.

The cache-disabled retry passed thinking off/low/medium/xhigh, sampler
precedence, history, nested tools, streaming, Responses, cache reuse and
cancellation. Its 247,627-token synthetic archive request used medium effort,
temperature zero and seed 42, and recovered all six distributed keys exactly.
Prompt processing took 1940.037 seconds at
127.64 tokens/s, and total HTTP time was 1972.339 seconds. Three subsequent
fixed 256-token continuations reused over 247K tokens and took 159.708 seconds
in total, or 4.81 generated tokens/s including HTTP and prompt work.
Backend-timed decode was 5.08 tokens/s. A fresh unrelated request afterward
returned exactly `AFTER_LONG_OK`. Sampled available memory stayed above
8.4 GiB, with no kernel warnings in this retry's window and clean container
removal. No reboot, GPU reset or host memory/power-policy change was needed.

This is useful experimental ROCm evidence, not a new managed backend claim
or a coding-quality grade. The coherent short Go explanation still contained
a false claim about preemption. The high-context check used a repeated
synthetic archive and deliberately truncated performance continuations.
Only one slot on `gfx1151` was exercised with this policy. Normal router
children inherit their parent's allocation environment, so promoting this
policy requires a reviewed per-model boundary and router/client acceptance.
The accepted ordinary presets must not inherit a global memory-policy change
on the strength of this one model's result.

#### Delivery checks and retained evidence

Real managed Pi and Maki both passed read-file and shell-tool round trips
through a private candidate gateway with the default Q8 MTP preset. The actual
tool events, successful tool results and final answers were inspected, not
just client exit codes. Maki's read offsets and limits were numeric. Pi's
thinking-off run reported no reasoning tokens. The native tiny-model CLI
returned its requested sentinel and exited normally. The test gateway and
its exact backend container were removed cleanly.

Tier 1 checks passed, including forced normal and static host builds, Go and
container-policy tests, Python compilation, shell syntax, catalog JSON and
diff checks. The ordinary accepted workload windows had no GPU faults. Two
Python bus-lock notices occurred during the earlier short-check window.
The deliberately failing Flash-Next ROCm allocation controls are documented
separately above and must not be described as a fault-free acceptance run.

The source update is accepted on this `gfx1151` host with the four existing
patches. Model pins, precision, context, samplers and the managed Flash-Next
Vulkan-only restriction remain unchanged. Inference acceptance on `gfx1150`,
`gfx1200` and `gfx1201` remains blocked by unavailable representative hardware.
The normal gateway is restored using the updated image selection.

Raw commands, image IDs, build and backend logs, native benchmark JSON,
HTTP requests and responses, client events, memory samples and kernel windows
are retained on the acceptance host and the development host under
`~/.local/share/paracetamol/apps/acceptance/results/20260911-llama-upgrade/`.
Its result manifest distinguishes accepted paths from intentionally failed
diagnostics. One-off probe sources are retained there, without compiled Go
helpers, upstream checkouts or build caches.

### Fedora 44 Strix Halo DwarfStar typed GLM tools (2026-09-11)

The parser correction is based on Paracetamol
`701c32eb2c8021527679119af3854143d80b8331`, with the changes recorded in this
section's introducing commit. DwarfStar remains pinned at
`6289c516273979173abbc062209a81dd3706b804`, now carrying upstream PR 1016 at
`9db96f0e96928e2245664b83e0498145e86a4c25`. The existing WMMA patch, ROCm
10.0, model files and September 9 host policies were unchanged.

The `--no-layer-cache` build produced
`localhost/paracetamol:dwarfstar-ubuntu26.04-rocm10.0-6289c51-r10`, image ID
`ce024e0bcf3a9da29185f984247e2365667e00fd2ac78a3014f5f125f99f38cb`.
The old `r9` image was retained. The new image passed model-free server tests,
`pip check`, help and dynamic dependencies for all three retained binaries,
payload inspection and device-code inspection for all four managed targets.
The server suite also passed host address/undefined-behavior sanitizers.
Model-free server tests are now an image build gate.

Antirez's exact GLM-5.3 Flash Q2 conversion below was tested at a 262144-token
allocation, resident with MTP off. The original image failed all three
mixed-type tool cases by quoting every argument. The patched image passed
buffered/off, streamed/off and streamed/on calls, including nested objects,
arrays, numeric and boolean values, nulls and JSON-looking strings. All three
tool-result continuations passed. Real Pi and Maki read/shell exchanges also
passed with numeric read offsets and limits.

Three matched 256-output-token GLM requests averaged 19.264 seconds before
and 19.250 seconds after, with byte-identical generated code. This is not a
meaningful speed improvement. The known exact-case `True` response failure
remains. Patched GLM protocol/client cases retained at least 24.80 GiB of
sampled host available memory without new GPU faults in their kernel windows.

DeepSeek ordinary and DSpark server regressions at the managed 128K ceiling
both passed tools, thinking, retrieval, streaming, Responses, cache reuse and
cancellation checks. The normal gateway passed a DeepSeek tool round trip
and was restored unloaded. GLM is still a direct experimental model, not a new
managed preset or gateway inventory entry. No model defaults changed, and no
new GPU inference claim is made for the other architectures.

The [GLM follow-up](glm-5.3-flash-dwarfstar-feasibility.md#parser-correction-2026-09-11)
has the detailed boundaries and results. Raw evidence is retained under
`~/.local/share/paracetamol/apps/acceptance/results/20260911-glm-tool-types/`.

### Fedora 44 Strix Halo GLM-5.3 Flash feasibility (2026-09-10)

The unchanged DwarfStar `6289c51-r9` ROCm 10.0 image below loaded and ran
Antirez's 89.88 GiB GLM-5.3 Flash Q2 conversion resident, with the normal
112 GiB TTM/GTT policy and memory guard enabled. At a 256K context ceiling,
planned memory was 95.96 GiB and sampled host available memory stayed above
25.02 GiB. Three fixed shallow 256-token responses decoded at about
14 tokens/s. A retrieval request with 30,048 actual input tokens returned
both keys correctly in 589.769 seconds. This is not fully populated 256K
acceptance.

Basic inference and capacity passed this screen, but typed tools are `FAIL`.
The server converted an object-valued argument into a string, matching an
open upstream GLM parser issue. Exact lowercase-answer checks also failed
on capitalization. This is not managed-model or agent acceptance. GLM was
not added to the gateway inventory and existing defaults were unchanged.

The [feasibility record](glm-5.3-flash-dwarfstar-feasibility.md) contains the
exact model pin, image ID, context coverage, protocol failures and retest
gate. Raw evidence and the verified local content pack are retained under
`~/.local/share/paracetamol/apps/acceptance/results/20260910-glm53-flash/`.
No inference claim is made for the other GPU classes.

### Fedora 44 Strix Halo llama.cpp `6d9c82e` update (2026-09-09)

The source update from `0eadefebd3f8f92a86d634a0e5b8fffc9dc792c0` to
`6d9c82ea2bb34e277c0664b8dd3434bfb4dcfb27` passed the following `gfx1151`
checks. The control-plane source was based on Paracetamol `300cc510`, with
the update recorded in this section's introducing commit. Hardware, kernel,
ROCm 10.0, memory policy and balanced power settings match the September 9
DwarfStar record below. No model pins, presets, sampler defaults or backend
restrictions changed.

The final `--no-cache` build produced
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-6d9c82e-r34`, image ID
`875ac24eeed4e44eecbad8f23150fc402ed5b0c2c35dce010e87018ea3b738e3`.
The retained baseline was `llama-cpp-ubuntu26.04-rocm10.0-0eadefe-r33`,
image ID
`a4d31f78ffeb2b305275dc0ef3c7bae5d161f9c6e9f212e0c37961a0ba5720af`.
Both GPU backends compiled. The final image passed `pip check`, dynamic
dependency checks for its three executables, retained-payload inspection,
and inspection of actual HIP device code for all four managed targets.

Three local patches needed rebasing. The reasoning patch follows upstream's
new history-policy fields, HIP quantized-KV launch calls explicitly disable
the new sparse-attention argument, and the Vulkan F16 contiguization patch
follows the changed shader table. The HIP host-buffer patch applies unchanged.
None is removed on the strength of one GPU's results.

Upstream now preserves reasoning history by default. Direct launches and
router INI sections explicitly set the existing catalog policy both ways,
keeping Qwen3.6 history preservation off and Qwen3.8/Muse preservation on.
Live rendered-template checks confirmed both behaviors. The status command
now calls this `Reasoning history`, distinct from current-response thinking.

The controlled native benchmark used Unsloth Dynamic Q8_K_XL at revision
`4604b899a826000505a834e623272db5b7fd62f6`, SHA-256
`af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377`,
through the non-MTP `qwen3.8-27b-ud-q8-k-xl` alias. Each case used 512 prompt
tokens, 128 generated tokens, three repetitions, batch 2048, microbatch 512,
Flash Attention on, and `/dev/dri/renderD128`. Context depth is populated
history, not just the allocation ceiling. No other GPU workload or build ran
alongside a timed case. Values below are native benchmark averages in tokens
per second, not coding-task scores.

| Backend | Populated depth | K/V type | Previous prompt | Updated prompt | Previous decode | Updated decode |
| --- | --- | --- | --- | --- | --- | --- |
| ROCm | 0 | F16 | 338.93 | 337.13 | 7.222 | 7.209 |
| ROCm | 32768 | F16 | 196.86 | 196.35 | 6.783 | 6.771 |
| ROCm | 32768 | Q8_0 | 205.46 | 207.84 | 6.875 | 6.865 |
| ROCm | 32768 | Q4_0 | 193.85 | 193.86 | 6.869 | 6.854 |
| Vulkan | 0 | F16 | 231.92 | 277.94 | 7.297 | 7.298 |
| Vulkan | 32768 | F16 | 176.37 | 200.49 | 6.784 | 6.781 |
| Vulkan | 32768 | Q8_0 | 174.44 | 168.22 | 6.970 | 6.963 |

ROCm and decode performance were essentially unchanged. Vulkan F16 prompt
processing improved 19.8% at shallow depth and 13.7% at 32K. Optional Vulkan
Q8_0-cache prompt processing regressed 3.6% in this screen. That limitation is
retained in the record, not hidden by the F16 improvement. Defaults remain
F16. Separate real-answer and tool-round-trip probes passed with ROCm Q8_0
and Q4_0 cache and Vulkan Q8_0 cache. Tiny Qwen3 0.6B controls also passed
on both images and both backends, completing 18 native benchmark cases.

The actual default `qwen3.8-27b-mtp-ud-q8-k-xl` was compared separately
through the lazy ROCm router at its 262144-token ceiling and three-token
draft depth. Three sequential, uncached, temperature-zero, seed-42 requests
each generated a fixed 256-token partial code continuation. Total generated
tokens divided by total backend generation time were 15.97 tokens/s before
and 15.96 after, a 0.1% difference. HTTP wall time for the first request
includes lazy loading and is not treated as decode time. The partial code
outputs are a fixed-length performance workload, not a quality evaluation.

Qwen3.8 Q8 MTP passed direct-server and lazy-router operation at the normal
256K ceiling, thinking-off/medium/xhigh and compatibility aliases, nested
tool arguments and tool-result continuation, streaming termination and usage,
the Responses endpoint, prefix-cache reuse, and cancellation followed by a
correct fresh request. Effective sampler checks covered omitted fields,
explicit partial overrides and nulls, in both thinking modes. Two concurrent
requests returned correct sentinels. The server reported four slots with
unified KV and `n_ctx_slot = 262144`. That concurrency check is correctness
coverage, not a throughput comparison.

The long retrieval request populated 33,058 tokens and recovered both keys
exactly. It took 136.783 seconds wall time, with 243.38 prompt tokens/s.
This is not a fully populated 256K acceptance run. Lazy-router regressions
also passed for Qwen3.6 dense and sparse MTP, Muse DFlash at 256K, Qwen3.8
non-MTP Q8, and Qwen3.8 MTP Q4 at its existing 128K ceiling. Flash-Next
Dynamic Q4_K_XL passed thinking, tools, streaming, Responses, history and a
6K input through Vulkan. Its ROCm restriction and lack of managed MTP remain
unchanged.

CPU lazy-router startup, a real tiny ROCm CLI answer, and the automated GPU
acceptance case passed. Managed Pi 0.84.2 and Maki 0.4.8 both completed actual
file-read and shell-tool exchanges through the gateway with the default Q8
MTP model. Backend switching and clean gateway shutdown passed. The kernel
journal contained no matching GPU, mapping, protection, timeout, reset or OOM
fault during the test window. Tier 1 checks and the forced normal Go build
passed.

Raw requests/responses, exact commands, native JSON, image build and closure
logs, and gateway client events are retained under
`~/.local/share/paracetamol/apps/acceptance/results/20260909-native-upgrade/`.
See `result-manifest.md` there for the evidence map and limitations. The
retained old images were not pruned.

`gfx1150`, `gfx1200`, and `gfx1201` inference is `BLOCKED` pending access to
representative hardware. Start with the tiny smoke below, replacing `rdna4`
and the render node with the exact target selection. A host with sufficient
model and context capacity should then repeat the Q8 command with both
backends, depth 0 and 32768, and matched F16 and quantized caches. Do not infer
these patch-removal gates from compiled code alone.

```bash
./paracetamol build llama-cpp --no-cache
./paracetamol acceptance --application llama-cpp --profile rdna4 \
  --render-node /dev/dri/renderD128 --non-interactive
./paracetamol benchmark llama-cpp throughput \
  --preset qwen3.8-27b-ud-q8-k-xl --profile rdna4 \
  --render-node /dev/dri/renderD128 --backend rocm \
  --prompt-tokens 512 --generation-tokens 128 --repetitions 3 \
  --context-depth 32768 --batch-size 2048 --ubatch-size 512 \
  --cache-type-k f16 --cache-type-v f16 --flash-attn on
```

Replay the retained tool, history, sampler, cache and cancellation probes at
the managed model's context ceiling. Expect correct output, no numerical or
GPU faults, and recorded same-workload performance rather than only startup.

### Fedora 44 Strix Halo DwarfStar `6289c51` update (2026-09-09)

The DwarfStar source update from `84cc882352757baf628a1776badf7cc54d584e28`
to `6289c516273979173abbc062209a81dd3706b804` passed the checks below on
`gfx1151`. The control-plane source was based on Paracetamol `300cc510`,
with the upgrade changes recorded in this section's introducing commit.
Models, ROCm, context defaults, and host tuning were held fixed.

The host was Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, Ryzen AI Max+
395 Radeon 8060S at `/dev/dri/renderD128`, with 128 GB LPDDR5X-8000 and a
112 GiB TTM/GTT ceiling. IOMMU remained enabled, platform profile remained
`balanced`, and CPU energy preference remained `balance_performance`.
The shared runtime remained ROCm 10.0.

The final `--no-layer-cache` build produced
`localhost/paracetamol:dwarfstar-ubuntu26.04-rocm10.0-6289c51-r9`, image ID
`1c2841f6899e648260313cce63188b4fa468bfd3e6ce8fea2c5dbc11f9234d8e`.
The retained baseline was `dwarfstar-ubuntu26.04-rocm10.0-84cc882-r8`, image
ID `2022f91220c5107889b65e409377095e635e77ed8d876b5ec8b33d8d3c9cad34`.
The new image passed `pip check`, dynamic dependency checks for all three
retained executables, and device-code inspection for all four managed HIP
targets. No source checkout, development dependency closure, PyTorch payload,
or extra agent/evaluation executable leaked into the final stage. The
upstream MIT notice and separate Iris MIT notice are retained.

The upgrade required direct rocBLAS linkage, rebasing the gfx11-only WMMA
fallback, and changing the external DSpark support argument to
`--mtp-model`. Bare `--mtp` now selects an embedded GLM drafter upstream.
The managed DeepSeek target and support pins did not change. Model-free
upstream CPU tests passed for memory, sampling, session state, server
protocol, and image decoding. Extracted production-helper fixtures also
passed 7,217 DSpark EOS cases and 162 cache-window metadata assertions.
Those are code checks, not GPU inference evidence.

GPU acceptance passed ordinary and DSpark CLI at a 4K ceiling. Ordinary and
DSpark servers at the managed 131072-token ceiling both passed thinking-off
and thinking-on answers, a 6,618-token retrieval input, nested tool arguments
and tool-result continuation, streaming usage and completion termination,
the Responses endpoint, continuation-cache reuse, and cancellation followed
by a fresh correct response. The DSpark support file reported all 81 tensors
with no missing, invalid, or metadata-error entries. These checks exercise a
128K allocation, not 128K of populated conversation history.

The controlled short-generation screen sent the same 33-token prompt three
times sequentially, each with temperature zero, seed 42, a 256-token output
limit, and no prompt-cache hits. Each output was a partial Go worker-pool
implementation used to hold generation length fixed, not a graded coding
task. Only one GPU workload ran at a time and no build overlapped a timed run.

| Path | Three request wall times, seconds | Total wall time, seconds |
| --- | --- | --- |
| Previous ordinary | 18.663, 16.996, 16.994 | 52.653 |
| Updated ordinary | 19.965, 17.154, 17.153 | 54.272 |
| Previous DSpark | 24.815, 24.599, 24.119 | 73.533 |
| Updated DSpark | 20.832, 16.687, 16.662 | 54.181 |

Ordinary generation took 3.1% more total wall time, with the warmed requests
about 1% slower. DSpark took 26.3% less wall time than the old DSpark build,
but only caught up with ordinary generation overall. Output was not
byte-identical across source versions or between ordinary and DSpark paths.
This is not evidence of identical greedy decoding or sampled distributions.

One matched 6,618-token retrieval request returned the same three-token
answer in 33.540 seconds on the old ordinary path and 24.599 seconds on the
updated ordinary path, a 26.7% wall-time reduction. Updated DSpark took
48.980 seconds for that request. This is a useful prefill observation, not a
multi-workload performance claim. DSpark remains opt-in at temperature zero.

Managed Pi and Maki both completed actual file-read and shell-tool exchanges
through the gateway using the ordinary DeepSeek model. The gateway switched
from llama.cpp to DwarfStar on demand and removed its owned backend on clean
shutdown. The kernel journal for the test window had no matching AMDGPU,
SVM, page-fault, protection-fault, reset, timeout, or OOM event.

Results, exact commands, raw API requests/responses, and backend logs are
retained under
`~/.local/share/paracetamol/apps/acceptance/results/20260909-native-upgrade/`.
The `dwarfstar-*-perf`, `dwarfstar-*-full`, continuation-cache and cancellation
artifacts distinguish ordinary and DSpark cases. An initial cache assertion
incorrectly expected DwarfStar to rewind a completed live prefix when the
same independent prompt was resent. Both source versions reject that reuse.
The corrected conversation-continuation probe reused 713 cached tokens.

`gfx1150`, `gfx1200`, and `gfx1201` inference remains `BLOCKED` for this
source update because representative hardware was unavailable. Compiled
device code is not acceptance. On a sufficiently provisioned host, start
with the following commands, replacing the profile and exact render node
with `strix-point` or `rdna4` and the verified device path.

```bash
./paracetamol build dwarfstar --no-layer-cache
./paracetamol acceptance --application dwarfstar --profile strix-point \
  --render-node /dev/dri/renderD128 --non-interactive
./paracetamol run dwarfstar server --profile strix-point \
  --render-node /dev/dri/renderD128 --context 131072
```

Use the unchanged `flash-0731-q2-imatrix` and optional
`flash-0731-q2-imatrix-dspark` content. Replay the retained temperature-zero
API probes with ordinary and `--dspark` servers, compare the same fixed
three-request workload, and inspect kernel logs. Expect correct tool and
multi-turn behavior, clean interruption, and no GPU or memory faults.
Discrete GPUs need explicitly provisioned host-memory capacity for the
80.76 GiB target. A small VRAM device alone cannot satisfy this handoff.

### Fedora 44 Strix Halo Qwen3.8 Flash-Next (2026-08-29)

Paracetamol commit `153932c` integrated the three-shard Unsloth Dynamic
IQ4_XS conversion at revision
`c8b5954a88c2775c546b92593eda40ea041d3176`. The files total
93,682,584,224 bytes, or 87.2 GiB. Their pinned conversion license remains
`NOASSERTION`, with the official Qwen source-model license recorded only as
lineage. The managed preset is separate from the dense 27B family and leaves
that family's Q8 MTP client default unchanged.

Paracetamol commit `b8900bc` added the four-shard Unsloth Dynamic Q4_K_XL
conversion from the same revision. Its 111,334,654,784 bytes, or 103.7 GiB,
retain the same provenance and license-risk boundary. The guided Flash-Next
recipe selects Q4_K_XL. The catalog subsequently retired IQ4_XS because its
87.2 GiB payload does not fit practical 32 or 64 GB device classes, while the
accepted Q4_K_XL fits the 128 GB target and provides materially better
publisher-reported quantization fidelity. Neither change affects the dense
27B Q8 MTP client default.

Acceptance used Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, the Ryzen AI
Max+ 395 Radeon 8060S at `/dev/dri/renderD128`, and a Samsung SSD 980 PRO 2TB.
The no-cache llama.cpp build was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-c9ca51c-r32`, image ID
`5101e70960002092b23865de0711a8fa6ba9171dfbe771e0a92f359b430220f5`.
It used ROCm 10.0 and llama.cpp commit
`c9ca51c1f6b18427cde490c7c7eba11d87a96b2d`. The image passed `pip check`,
its retained binaries had no unresolved dynamic dependency, and both ROCm and
Vulkan compiled with all four managed patches.

The accepted preset uses Vulkan, native 262144 context, F16 K/V cache, Flash
Attention, and mmap with lazy tensor reads. Only
`per_layer_token_embd.weight` is assigned to CPU. Current upstream target
support does not include the separate MTP work, so the preset has no
speculative alias. Direct startup selected Vulkan when the backend was
omitted. An explicit ROCm request failed before container creation. The ROCm
router and gateway exposed 17 verified models and omitted Flash-Next. Their
Vulkan counterparts exposed 18, loaded it on demand, and returned coherent
responses.

Vulkan thinking-off and medium requests were coherent at shallow context.
Representative decode ranged from 23.30 to 24.99 tokens/s. A structured call
supplied integer arguments 37 and 19 to a multiply tool, and its continuation
reported the returned value 703. Four simultaneous non-thinking requests each
returned a coherent two-sentence answer. This was a slot-correctness stress,
not a concurrent throughput measurement. Managed Pi then used its sandboxed
bash tool through the Vulkan gateway and returned exact output
`PARACETAMOL_FLASH_NEXT_PI_OK` across the two-request tool exchange.

The long-context control sent a 199,879-token non-thinking prompt. It returned
exact key `MAGENTA-9137` and correctly stated that the key appeared before the
final 100 filler words. Prefill measured 173.54 tokens/s over 1,151.769
seconds. The 25-token answer decoded at 5.976 tokens/s, and whole-request wall
time was 1,156 seconds. After completion, 51 GiB remained available and zram
swap use was 706 MiB. The kernel journal for the test window contained no
matching AMDGPU, SVM, page-fault, protection-fault,
general-protection-fault, OOM, or llama process event.

Q4_K_XL then passed the same acceptance ladder. Native 262144-context Vulkan
startup completed in 38.44 seconds with 33 GiB available before inference.
Thinking-off returned exact `Q4-OK`; medium reasoning produced a correct
two-sentence Go channel-ownership answer at 23.83 tokens/s. A structured call
supplied integer arguments 37 and 19 and continued with result 703. Four
simultaneous requests returned their exact sentinels at 13.12 to 13.14
tokens/s per slot. The Vulkan gateway exposed 19 verified presets and managed
Pi returned exact `PARACETAMOL_FLASH_NEXT_Q4_PI_OK` through a sandboxed bash
tool exchange.

The Q4_K_XL long-context control sent 199,872 non-thinking prompt tokens. It
returned exact key `COBALT-8421`, correctly located it before the final 100
blue filler words, and was not truncated. Prefill measured 164.58 tokens/s
over 1,214.446 seconds; the 27-token answer decoded at 5.783 tokens/s; curl
measured 1,219.113 seconds wall time. Afterward, 34 GiB remained available and
zram swap use was 822 MiB. The kernel journal remained clear of the same fault
classes. The earlier IQ4_XS prompt used different filler, so its roughly 5.5%
shorter wall time is an operating-envelope reference, not a controlled quant
A/B.

The publisher's [quant-fidelity
table](https://unsloth.ai/docs/models/qwen3.8-next) reports 92.255% top-1
agreement and 0.046893 mean KLD for Q4_K_XL, versus 89.554% and 0.083630 for
IQ4_XS. These are logit-fidelity measurements rather than downstream agent
scores, but they establish a material quantization improvement. Q5_K_XL is
43.73 GiB larger than Q4_K_XL, exceeding the Q4 run's 34 GiB available-memory
margin before changed runtime allocation. Q4_K_XL is therefore the highest
accepted reasonable Flash-Next quant on this the acceptance host configuration; IQ4_XS
is retained here only as a historical accepted comparison.

ROCm is `FAIL` for this exact Flash-Next tuple. It loaded the same shards and
could return a short retrieval key, but meaningful shallow responses became
malformed multilingual text. The failure survived resident and lazy loading,
deterministic sampling, one server slot, the GGUF and managed templates, a
clean GPU reset, ROCm 7.14 and 10.0 images, and a build without Paracetamol's
llama.cpp patches. Vulkan stayed coherent with the same image, model, prompt,
and policy. This is consistent with
[llama.cpp issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797) and
an independent [Unsloth ROCm report](https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/38).
The managed Vulkan-only restriction remains until a later llama.cpp or ROCm
tuple passes meaningful multi-turn and tool-call acceptance on `gfx1151`.

As a regression control, `qwen3.8-27b-mtp-ud-q8-k-xl` remained coherent on
ROCm at medium reasoning and accepted 55 of 75 MTP proposals. The managed
Q4_K_XL Vulkan preset is `PASS` on `gfx1151`; its ROCm path is `FAIL`;
`gfx1150`, `gfx1200`, and `gfx1201` are `N/P` pending capacity and inference
evidence. The retired IQ4_XS preset was also `PASS` through Vulkan and `FAIL`
through ROCm on this host. MTP remains `N/P` until its separate upstream
support lands and passes acceptance. The historical IQ4_XS machine-local
summary is
`~/.local/share/paracetamol/apps/llama-cpp/acceptance/20260829-qwen38-flash-next.md`,
SHA-256 `44f585dbe6151210b881ca7436a9c2c5a30e34f02e7dd1799750731267e78c7b`.
The Q4_K_XL summary is
`~/.local/share/paracetamol/apps/llama-cpp/acceptance/20260829-qwen38-flash-next-q4-k-xl.md`,
SHA-256 `3225e813f97e9b8ec2f7a73546ed2931f390d8da41faebb756484f969e7817ed`.

### Fedora 44 Strix Halo llama.cpp `0eadefe` retest (2026-09-01)

The llama.cpp source update from `c9ca51c` to
`0eadefebd3f8f92a86d634a0e5b8fffc9dc792c0` was built without layer cache on
the Fedora 44 Strix Halo host. The accepted image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-0eadefe-r33`, image ID
`2ac38332bea33fce4f5c885a74b59d02a2808da3312010274b335500cee82ed6`.
Both HIP and Vulkan compiled for all four managed GPU targets, all four
Paracetamol patches applied unchanged, `pip check` passed, and the retained
llama.cpp binaries had no unresolved dynamic dependency. Upstream renamed
the lazy tensor option, so direct and router presets now use `--lazy-mode on`
with the same mmap and CPU token-embedding policy.

The dense `qwen3.8-27b-mtp-ud-q8-k-xl` ROCm control returned its exact
sentinel with coherent medium reasoning. The Qwen3.8 Flash-Next Q4_K_XL
model still failed meaningful ROCm generation immediately with malformed
multilingual text. Vulkan returned coherent text through both the direct
server and lazy router, and the router emitted the new `--lazy-mode on`
argument. The managed Vulkan-only restriction therefore remains correct.
The kernel journal contained no AMDGPU, SVM, page-fault,
general-protection-fault, OOM, or llama process event during the test window.

An isolated candidate then applied the three commits from draft llama.cpp
[pull request 27836](https://github.com/ggml-org/llama.cpp/pull/27836) and the
detached-head loader correction at commit
`a82a58a57fc307e5cec0dc68db64d143339be4f2`. It used Unsloth's
2,786,204,800-byte `mtp-Qwen3.8-Flash-Next-Q4_K_M.gguf` from repository
revision `eb2a07ecb33b5495cbc8dc3183b9421a1dda4b46`, SHA-256
`b646ef60eaae2a9ed849e75f15f399629ca22633555e99e809959e95f22a1575`.
The base model remained the managed Dynamic Q4_K_XL conversion. This was a
Vulkan-only feasibility test because the base model's ROCm output is invalid.

At a shallow 4K ceiling, no speculation decoded at 22.61 tokens/s. MTP depth
2 reached 25.08 tokens/s with 45.4% acceptance, while depth 3 reached 28.43
tokens/s with 47.2% acceptance. At a 262144-token server ceiling, a seeded
512-token medium-reasoning request improved from 23.52 to 34.30 tokens/s at
depth 3, with 59.9% draft acceptance. The generated reasoning remained
coherent.

The long-history control populated 46,857 prompt tokens at the same 262144
ceiling and requested 512 non-thinking tokens. Without MTP, prefill was
245.80 tokens/s, decode was 15.54 tokens/s, and total server time was 223.505
seconds. Depth-3 MTP measured 227.82 prompt tokens/s, 26.15 decode tokens/s,
60.9% draft acceptance, and 225.212 seconds total. MTP therefore improved
deep-context decode by 68.3%, but reduced cold prefill by 7.3%. The two effects
cancelled for this one-shot request. Cached multi-turn use can retain the
decode benefit without paying full history prefill on every turn.

The MTP responses were coherent and terminated normally, but a
temperature-zero response was not byte-identical to the non-speculative
control. Disabling backend draft sampling did not restore equivalence. The
loader and model support are also still outside upstream master. The
candidate is therefore not part of the managed image or catalog, and managed
Flash-Next MTP remains `N/P` pending an upstream implementation and a repeat
of the greedy-equivalence, multi-turn, structured-tool, and long-context
gates. The separate recurrent-state rollback merged in
[pull request 28123](https://github.com/ggml-org/llama.cpp/pull/28123) is
retained in the accepted non-MTP source pin.

### Fedora 44 Strix Halo ROCm 10.0 upgrade (2026-08-28)

Paracetamol commit `1d083eb35b5646764b0925feccf0f3aed9808e64`
advanced the complete managed stack from ROCm 7.14 to AMD's
[ROCm 10.0 release](https://rocm.blogs.amd.com/ecosystems-and-partners/rocm-x-blog/README.html).
The runtime and native SDK now use the documented ROCm 10 aggregate wheel
index. PyTorch applications use the exact tuple PyTorch
`2.13.0+rocm10.0.0`, torchvision `0.28.0+rocm10.0.0`, and torchaudio
`2.11.0.2+rocm10.0.0`. PyTorch reports the underlying HIP version as
`7.15.26333`; this is distinct from the ROCm 10.0 distribution and wheel
version.

Every image was built without cache on the Fedora 44 Strix Halo host. The
ROCm-bearing outputs were:

- `runtime-ubuntu26.04-rocm10.0-r3`, image
  `31df9d6bb890d6660244b46ba127a29e76a7323394ae37168d301e57b270b661`;
- `base-ubuntu26.04-rocm10.0-torch2.13-r6`, image
  `578df0bfed360358f2a5460ca9d2245350b2db09ec7e78befc7ab6da10fc0328`;
- `comfyui-ubuntu26.04-rocm10.0-0.28.0-r12`, image
  `275debb609368c1e62f630f9fe323729f460481942177752fc81eeb9bf63d61a`;
- `llama-cpp-ubuntu26.04-rocm10.0-5d5cb4c-r31`, image
  `7bf44e29b485c17c175adb6554c9af60084582b6054486f1113ffb5c9e039ba6`;
- `dwarfstar-ubuntu26.04-rocm10.0-84cc882-r8`, image
  `e3dcac5acaece642a20c0db95e189e2182ae36dfa79f8412bb247ee5435e0380`.

All five Python environments passed `pip check`. The unchanged llama.cpp and
DwarfStar source patches applied against the existing pinned commits and both
native builds completed with ROCm's Clang 23 toolchain. Retained llama.cpp
HIP and Vulkan libraries and DwarfStar executables had no unresolved dynamic
dependency. ComfyUI reached HTTP 200 in a confined CPU startup and stopped
without leaving a container.

`doctor` identified the Radeon 8060S as `gfx1151` and completed its GPU
operation through both the PyTorch base and final ComfyUI images. Formal
device-isolation and llama.cpp smoke acceptance finished `PASS` as suite
`20260828T200702Z-85516a66`. Its retained result is
`~/.local/share/paracetamol/apps/acceptance/results/20260828T200702Z-0176687e.json`
on the host.

A same-day A/B comparison retained the exact llama.cpp commit, verified
Unsloth Dynamic Q8 Qwen3.8 27B bytes, 32K populated context, F16 K/V cache,
Flash Attention, 512 prompt tokens, 128 generated tokens, and three
repetitions. The old ROCm 7.14 image measured 196.21 prompt and 6.789
generation tokens/s. The ROCm 10.0 image measured 200.48 and 6.784 tokens/s,
respectively: prompt processing improved by 2.17%, while generation changed
by −0.08%. The records remain at
`~/.local/share/paracetamol/apps/llama-cpp/benchmarks/rocm-upgrades/20260828-qwen38-rocm714-f16-32k.json`
and
`~/.local/share/paracetamol/apps/llama-cpp/benchmarks/rocm-upgrades/20260828-qwen38-rocm100-f16-32k.json`.

The preferred Qwen3.8 Q8 MTP server also returned the exact requested answer
at medium reasoning. It generated 17.25 tokens/s and accepted 29 of 33 draft
tokens. The server stopped cleanly, and the kernel journal for the complete
test window contained no matching AMDGPU, SVM, page-fault, reset, timeout,
general-protection-fault, or OOM event.

This accepts the ROCm 10.0 upgrade for the current Strix Halo llama.cpp path
and the shared PyTorch GPU operation. DwarfStar inference remains `N/P`
because its local 80.8 GiB model was unverified and the DSpark bundle was
partial; successful compilation and linking are not substituted for model
execution. A ComfyUI generation workflow and inference on `gfx1150`,
`gfx1200`, and `gfx1201` also remain pending.

### Fedora 44 Strix Halo llama.cpp b10631 update (2026-08-26)

Pinned llama.cpp was advanced from `b10453` (`3cb7ffb1`) to upstream tag
`b10631` (`5d5cb4c3a4ea8769490d39a275ee49a45184774d`), which was also the upstream
default branch head when selected. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-5d5cb4c-r30` (image ID
`32d13da5104fd9ca375c8c0daa0ab4a1d221a8e221709cf09f0bb2dbed163dcf`).
It was built and exercised on the Fedora 44 Strix Halo host with profile
`strix-halo`, ROCm 7.14, balanced memory policy, and
`/dev/dri/renderD128`.

All four managed patches were compared with the intervening upstream changes
before the build. The HIP host-buffer patch remained applicable without a
semantic change. The reasoning-controls and Vulkan F16-KV patches were
rebased onto changed upstream interfaces. Upstream now owns the Vulkan Q8-KV
dequantization path, so that duplicate portion was removed from the managed
quantized-KV patch while its HIP Q8 and Q4 specializations were retained.
Both ROCm and Vulkan compiled, the image passed `pip check`, its retained
executables and backend libraries had no unresolved dynamic dependency, and
CPU CLI and lazy-router startup controls passed.

Direct GPU benchmarks used the receipt-verified Unsloth Dynamic Q8 Qwen3.8
27B bundle at 32K context with three repetitions. F16 KV measured 202.50
prompt and 6.79 generation tokens/s on ROCm, versus 181.60 and 6.79 on Vulkan.
Q8 KV measured 205.74 and 6.88 on ROCm, versus 178.80 and 6.98 on Vulkan.
The retained HIP Q4 KV path separately measured 192.50 prompt and 6.86
generation tokens/s. A Qwen3 0.6B smoke also completed on both backends. The
benchmark records remain outside the source tree below
`~/.local/share/paracetamol/apps/llama-cpp/benchmarks/` on the host.

The preferred `qwen3.8-27b-mtp-ud-q8-k-xl` ROCm server then passed native
thinking-off and medium reasoning, Chat Completions and Responses transports,
a forced structured tool call with a tool-result continuation, and MTP draft
decoding. The representative medium request accepted 36 of 39 draft tokens.
A non-MTP ROCm CLI control also completed. Foreground SIGINT removed its
container and listener, and the kernel journal for the test window contained
no matching AMDGPU, SVM, protection-fault, general-protection-fault, or OOM
event.

Formal acceptance finished `PASS` as suite
`20260826T070855Z-86d0c5c1`, with result JSON
`~/.local/share/paracetamol/apps/acceptance/results/20260826T070855Z-9aded9c6.json`
on the host. It accepted exact device isolation and tiny-model GPU offload on
`gfx1151`. This source update remains `N/P` on `gfx1150`, `gfx1200`, and
`gfx1201` until those hardware classes run their applicable acceptance rows;
successful compilation for their targets is not substituted for inference.

### Fedora 44 Strix Halo native gateway (2026-08-20)

Paracetamol commit `ba1cf83` was exercised between the Fedora 44 Strix Halo
host and a separate Linux Pi client. The GPU host used kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. Its llama.cpp image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
`db4ff8651e1bea7e3d2b805e1738e8a344f0eb1b72dbbbbbe788f7952f006da3`).
Host-side logs remain outside the repository at
`/tmp/paracetamol-gateway-ba1cf83.log` and
`/tmp/paracetamol-gateway-ba1cf83-remote.log` on the GPU host.

Loopback startup advertised exactly 17 receipt-verified llama.cpp presets with
inventory fingerprint
`9e70cfd3dee9e2927f747378693850566289e86a73f5542b6c9f611427aee9a1`.
The versioned status endpoint reported `unloaded`, and no gateway backend
container existed before the first request. A medium-effort request for
`qwen3.8-27b-mtp-ud-q8-k-xl` then lazily started the backend on dynamic private
port 38719 and returned exact content `gateway-ok`. Generation reported 16.86
tokens/s, with 24 of 30 draft proposals accepted. The 18.21-second first-request
wall time includes backend and model startup.

With that same backend resident, five alternating gateway/direct requests used
identical thinking-off, no-prompt-cache, eight-token request bodies. Excluding
the first connection establishment sample, the four-request mean was 0.538828
seconds through the gateway and 0.537539 seconds against its private backend:
1.289 ms or 0.24% measured proxy overhead. The first gateway sample was 83.7 ms
slower than its direct pair. This is a short latency control, not a sustained
throughput benchmark.

The gateway was then published on the host's trusted-LAN address. Managed Pi
on the separate client discovered the live inventory, generated one
`paracetamol` provider, retained its default sandbox, selected the same Qwen
preset at medium effort, and returned exact content `remote-pi-ok`. The request
reported 5,484 input and 20 output tokens. SIGINT removed the dynamic backend
container and both public listeners after each run. The kernel journal for the
test window contained no matching AMDGPU fault/reset/timeout, SVM mapping
failure, general-protection fault, or OOM event.

Starting the same commit with both applications failed before opening a
listener or creating a backend container because the reviewed DwarfStar model
had no current local artifact or receipt. This accepts fail-closed inventory,
lazy llama.cpp lifecycle, transparent proxying, remote Pi integration, and
cleanup. Cross-engine switching and live DwarfStar forwarding remain
`BLOCKED` on this host snapshot until that large pinned bundle is installed;
they are not inferred from unit tests or earlier direct DwarfStar runs.

### Fedora 44 Strix Halo Maki native reasoning transport (2026-08-18)

Paracetamol commit `cf7e75e` was exercised with a Maki 0.4.8 build containing
commit `a9495e1` on the Fedora 44 Strix Halo host with kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The existing llama.cpp image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
`7c5d6efa0df5c5965db38fd59911ab4856a02620674e62ff62dfc55b9ca58d3f`).

Fresh Maki TUI sessions and a loopback request capture confirmed the exact
generated fields. Qwen3.8 off, low, high, and xhigh sent effort none, low,
medium, and xhigh respectively; Qwen3.6 off and high sent nested
`enable_thinking` false and true; Muse off and high sent strength low and
high; and DwarfStar off and high sent effort none and high to the generated
`--dwarfstar-port`. None of these named requests also carried a numeric
thinking budget. Family-wide configuration tests cover all four Qwen3.6 and
all four Qwen3.8 MTP, non-MTP, and precision presets that share those paths.

Live router inference then returned exact text `TRUE` for Qwen3.8 Q8 MTP at
off and xhigh, Qwen3.6 27B MTP at off and enabled, and Muse Glimmer DFlash
256K at its forced-low and high paths. The off Qwen sessions contained no
reasoning, while their enabled counterparts and both Muse requests retained a
separate reasoning block. DwarfStar's request mapping was captured but its
model was not started for this acceptance. The kernel journal for the live
test window contained no matching AMDGPU, SVM mapping, protection-fault, or
OOM event, and the temporary router stopped cleanly.

Maki's `--print` path at this commit does not apply `always_thinking`; these
mode results therefore use fresh interactive sessions with automatic exit.
This accepts generated model metadata, Maki's native request transport, and
representative GPU inference wiring, not agent-task quality or performance.

### Fedora 44 Strix Halo model-scoped reasoning fallback (2026-08-17)

Paracetamol commit `0355a73` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
`7c5d6efa0df5c5965db38fd59911ab4856a02620674e62ff62dfc55b9ca58d3f`).
The complete managed patch set applied to pinned llama.cpp commit `3cb7ffb`,
the image passed `pip check`, and the three direct-preset containers stopped
cleanly.

Qwen3.8 direct startup rendered native off, medium, and xhigh correctly;
numeric budgets zero and 3276 retained the managed medium template default
instead of being decoded as native choices. A bounded native-off ROCm request
returned exact content `true` without reasoning. Qwen3.6 rendered both its
native toggle and OpenAI-compatible off correctly, did not decode a numeric
zero, and returned exact text-only `true` through live off-mode inference.
These two templates cover their MTP, non-MTP, precision, and dense/sparse
siblings; those variants retain separate performance and capacity acceptance.

Muse rendered low, medium, high, and xhigh strengths correctly. Its generic
off request resolved to model-owned low, while a numeric zero retained the
managed high default. A bounded ROCm request returned exact content `true`
with low-strength reasoning preserved. This acceptance supersedes the Maki
numeric-budget interpretation described in earlier same-day records: numeric
budgets remain sampler ceilings and are no longer evidence of a native model
mode. It accepts template selection, server-side sampling-mode selection, and
live GPU inference wiring, not comparative model quality.

### Fedora 44 Strix Halo Pi Qwen reasoning transports (2026-08-17)

Paracetamol commit `84ada22` was exercised with Pi 0.84.2 on the Fedora 44
Strix Halo host with kernel `7.1.7-200.fc44.x86_64`, ROCm 7.14, and
`/dev/dri/renderD128`. The router used the existing
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` image (image
ID `24e307ca7006bc093d20111e18e71d41256c4e8fda0a74c5df2729d417ecd1a3`);
the correction changed generated Pi metadata and required no image rebuild.

The generated Pi configuration assigned all four exposed Qwen3.6 presets to
`qwen-chat-template` and all four exposed Qwen3.8 presets to `openai`, with
off mapped to `none` throughout. A structured Pi request through
`qwen3.8-27b-mtp-ud-q8-k-xl` at off returned only a text block containing
`true`; medium returned a separate thinking block followed by `true`. The
same paired check through `qwen3.6-27b-mtp-q8-0` returned text-only `true` at
off and a thinking block plus `true` at its generic high/on setting.

This accepts one live representative for each distinct managed Qwen template
and Pi transport. The family-wide configuration test covers the MTP,
non-MTP, Q8, Q4, dense, and sparse preset variants that reuse those two
paths. The managed container stopped cleanly, and the kernel journal for the
test window contained no matching AMDGPU fault, SVM mapping failure,
general-protection fault, or OOM event.

### Fedora 44 Strix Halo llama.cpp runtime report (2026-08-17)

Paracetamol commit `492bafb` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
`24e307ca7006bc093d20111e18e71d41256c4e8fda0a74c5df2729d417ecd1a3`).
The image passed `pip check`, and its labels identified the pinned llama.cpp
revision and all four applied downstream patches.

Direct startup of `qwen3.8-27b-mtp-ud-q8-k-xl` with profile `auto` printed the
matching status command. The live report resolved `strix-halo` and `gfx1151`,
identified the exact image and launcher revisions, and reproduced the 262144
context, managed Qwen3.8 template, medium reasoning default, thinking/off
sampling tuples, reasoning preservation, and ROCm MTP draft depth three. Its
exact PID 1 command agreed with the direct launch. A bounded medium-effort
request returned exact content `OK` with reasoning preserved and 29 of 33
draft proposals accepted.

Router startup exposed 16 mounted managed presets with a two-model loaded
limit. The overview listed those exact identifiers, while selecting the same
Qwen3.8 preset reproduced its model policy and the router's exact PID 1
command. An on-demand medium-effort request again returned exact content `OK`
with 29 of 33 draft proposals accepted. Both containers stopped cleanly; the
kernel journal for the test window contained no matching AMDGPU fault, SVM
mapping failure, general-protection fault, or OOM event. This accepts the live
report and its direct/router inference wiring, not model quality or sustained
performance.

### Fedora 44 Strix Halo catalog-driven reasoning sampling (2026-08-17)

Paracetamol commit `b90eded` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r28` (image ID
`4d432e66ee54226bb8ada1d938fc264832ceabd2be657e12ccde2f7e791f5dba`).
The complete managed patch set applied to the pinned llama.cpp source and
compiled its ROCm and Vulkan backends. The image passed `pip check`; its three
managed llama.cpp executables were present and had no unresolved dynamic
library dependency.

Router discovery showed the dedicated `--sampling-defaults-by-reasoning`
JSON generated from the catalog on Qwen3.8, dense Qwen3.6, and sparse Qwen3.6
child commands. Bounded ROCm requests and live `/slots` state confirmed that
Qwen3.8 medium and dense Qwen3.6 thinking used temperature 1.0, top-p 0.95,
top-k 20, min-p 0, presence penalty 0, and repeat penalty 1. Sparse Qwen3.6
thinking used the same tuple with presence penalty 1.5. Thinking off for all
three policies used temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence
penalty 1.5, and repeat penalty 1. Thinking prompts were respectively open or
preclosed.

Maki's zero-token thinking budget selected the Qwen3.8 non-thinking defaults.
A request temperature of 0.25 overrode only that field, while an explicit null
top-p selected the catalog fallback. Direct-preset Qwen3.8 startup
independently reproduced the medium and off tuples, accepting the same policy
through its environment-to-entrypoint path. Every request completed, both
managed containers stopped cleanly, and the kernel journal for the test window
contained no matching AMDGPU fault, reset, timeout, SVM mapping failure,
device loss, protection fault, general-protection fault, or OOM event. This
accepts the generic catalog-to-server transport and fallback semantics; it is
not a model-quality or performance comparison.

### Fedora 44 Strix Halo Qwen3.8 mode-aware sampling (2026-08-17)

Paracetamol commit `fe6d5c3` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r26` (image ID
`0b1f72908463ed2bf243bb6ac3a412872f520264b257d2c6caa1ed2fec4869dc`).
The image passed `pip check`, and the pinned llama.cpp source accepted and
compiled the complete managed patch set.

The installed `qwen3.8-27b-mtp-ud-q8-k-xl` preset was tested through both the
router and direct-preset ROCm server paths at 262144 context with MTP draft
depth three. Router discovery showed the private Qwen3.8 sampling-profile
marker in the child command. Default and Maki medium requests resolved to
temperature 1.0, top-p 0.95, top-k 20, min-p 0, presence penalty 0, and repeat
penalty 1, with the thinking prompt open. `reasoning_effort: none`, explicit
`enable_thinking: false`, and Maki's zero-token thinking budget each resolved
to temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence penalty 1.5, and
repeat penalty 1, with the template's preclosed thinking block.

A partial request override changed only temperature to 0.25 while retaining
the remaining off-mode defaults. A request overriding all six managed fields
retained every caller value, and an explicit null temperature selected the
server default. The direct-preset path independently reproduced the medium and
off tuples. Both managed containers stopped cleanly, and the kernel journal for
the test window contained no matching AMDGPU fault, reset, timeout, SVM mapping
failure, device loss, or OOM event. This accepts server-side mode resolution,
client override precedence, and GPU inference wiring; it is not a quality or
performance comparison.

### Fedora 44 Strix Halo Qwen3.6 mode-aware sampling (2026-08-17)

Paracetamol implementation commit `abec977`, from checkout `53b0032`, was built
and exercised on the Fedora 44 Strix Halo host with kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r27` (image ID
`30e73b218a52de1891be90c787317deeddd991370442f28a34db3dea233bb775`).
The downstream patch applied to the pinned llama.cpp source and compiled for
all four managed GPU targets. The image passed `pip check`, retained only the
three intended llama.cpp executables, and had no unresolved dynamic-library
dependency.

Router rendering assigned `qwen3.6-27b` to both dense presets,
`qwen3.6-35b-a3b` to the installed sparse MTP preset, and `qwen3.8-27b` to the
Qwen3.8 presets. Live `/slots` state after bounded ROCm requests confirmed the
effective values. Dense Qwen3.6 thinking on used temperature 1.0, top-p 0.95,
top-k 20, min-p 0, presence penalty 0, and repeat penalty 1. Sparse 35B-A3B
thinking on used the same tuple with presence penalty 1.5. Thinking off for
both used temperature 0.7, top-p 0.8, top-k 20, min-p 0, presence penalty 1.5,
and repeat penalty 1. The generation prompt was open for thinking and
preclosed for off.

Maki's zero-token thinking budget selected the off tuple. A partial caller
override changed temperature to 0.25 while an explicit null top-p selected the
off-mode default; the other fields retained their server defaults. Direct
dense-preset startup independently reproduced both the on and off tuples, and
a Qwen3.8 medium router request retained its existing thinking tuple. Exact
off-mode replies completed through dense router, sparse router, and direct
startup. All containers stopped cleanly. The kernel journal for the build and
test window contained no matching AMDGPU fault, reset, timeout, SVM mapping
failure, device loss, protection fault, or OOM event. This accepts the new
sampling-policy wiring and inference path, not comparative model quality.

### Fedora 44 Strix Halo Qwen3.6 35B-A3B restoration (2026-08-17)

Paracetamol commit `f6d4c43` restored the pinned non-MTP and MTP Qwen3.6
35B-A3B catalog paths without changing the managed-client default. The MTP
path was exercised on the Fedora 44 Strix Halo host with kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The existing llama.cpp image was
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r25` (image ID
`55b2ed6796687891d18e77b86bf8d1ded883ce3a03c5e97769da92ddb21a803c`).
Its managed `qwen3.6.jinja` matched SHA-256
`ea69920311f2efccf6343675490b27bd22d03787ebb8ccaf6e9101bfeba72898`.
The installer downloaded and verified the 39,099,447,584-byte MTP artifact at
SHA-256
`6c6b816537abad90b250a0972b345466028d861ddfe316d5f0de31ca6440f781`.

Direct ROCm startup resolved native 262144 context, MTP draft depth three,
F16 K/V, and llama.cpp's default Flash Attention policy. Exact template
rendering retained a later developer message and omitted old reasoning before
a historical tool call. Live inference returned a parsed
`record_value("TEMPLATE_35")` call, accepted 21 of 21 draft proposals at 72.62
generated tokens/s, and completed the tool-result continuation with exact
content `FINAL_TEMPLATE_35`. Separate thinking-off and thinking-on requests
reached the model; the latter returned distinct reasoning content.

Router mode advertised the restored MTP preset unloaded, generated the same
template, context, and speculative policy, loaded it on demand, and returned
exact content `ROUTER_QWEN36_35_OK` while accepting 9 of 9 draft proposals at
69.19 generated tokens/s. These short exact-output timings accept wiring and
GPU inference, not comparative performance. Both containers stopped cleanly,
and the kernel journal for the test window contained no matching AMDGPU page
fault, SVM mapping failure, GPU reset, timeout, or device-loss event. The
non-MTP artifact remained an exact optional bundle and was not downloaded for
this restoration check.

### Fedora 44 Strix Halo DwarfStar DSpark observation (2026-08-17)

Paracetamol commit `363fedf` was exercised on the Fedora 44 Strix Halo host with
kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The DwarfStar image was
`localhost/paracetamol:dwarfstar-ubuntu26.04-rocm7.14-84cc882-r7` (image ID
`246657a79924b937e6cf641852b8ae01066d2e19980f58851f453b073f077570`).
The installed pair combined the verified 80.76 GiB target with the independently
verified 5.58 GiB support GGUF from revision
`86bb38ce2ba7a98ab0e550359fec5f48859dc723`.

The host retained the general 112 GiB TTM/GTT configuration and did not use
`amd_iommu=off`. The final image passed `pip check`, all three retained binaries
had complete dynamic-library resolution, and the image contained no
`ds4-agent`, `ds4-eval`, GCC toolchain, source checkout, ROCm development wheel,
or PyTorch payload.

Observed results:

- Two 4K servers received the same direct-answer request at temperature zero
  three times, with a 64-token ceiling. Target-only decode was 16.25 tokens/s
  in all three runs. DSpark decode was 13.27, 13.49, and 13.49 tokens/s: a
  13.49 tokens/s median and a 17.0% regression from the target-only median.
  All six responses contained the same 64 output tokens. This accepts the
  wiring and output check, not a performance win.
- DSpark recognized all 81 support tensors with zero missing, invalid, or
  metadata-error entries. Target startup preparation took 17.407 seconds and
  the separate 5.58 GiB support mapping took 1.226 seconds in the 4K run.
- The managed 131072-context DSpark server planned 83.80 GiB for the target,
  KV cache, and buffers, with the 5.58 GiB support mapping loaded separately.
  A greedy direct-answer request returned exactly `DSPARK_128K_OK`.
- All three server instances stopped cleanly. The kernel journal from the test
  contained no matching AMDGPU mapping/page fault, GPU reset, ring timeout,
  process protection fault, or OOM event.

DSpark therefore remains a separately installed, explicitly selected path.
These results provide no reason to make it the DwarfStar default on Strix Halo.

### Fedora 44 Strix Halo observation (2026-08-09)

This field observation used kernel `7.1.7-200.fc44.x86_64`, profile
`strix-halo`, and `/dev/dri/renderD128`. The final bounded DwarfStar smoke used
source commit `a333de2`, DwarfStar image
`localhost/paracetamol:dwarfstar-ubuntu26.04-rocm7.14-d250a7c-r4` (image ID
`531fdef3be07c78825c573a3b2ceb333ab6b47245158f65894150fe3a54e902d`),
and the managed DeepSeek V4 Flash 0731 Q2 imatrix bundle. The llama.cpp tests
used image `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-ddd4ec1-r14`
(image ID
`e3459ab8f0d7f96d3b846ebd90b29f8fd5187a7eb7689226cf69877c1ba8fd29`).

The host used the general 112 GiB TTM/GTT configuration:
`amdgpu.gttsize=114688`, `ttm.pages_limit=29360128`, and
`ttm.page_pool_size=29360128`. IOMMU remained enabled; the kernel command line
did not contain `amd_iommu=off`. Doctor passed the GPU operation and exact
device-isolation probes with enforcing SELinux and `container_use_devices`
enabled.

Observed results:

- The committed 4K DwarfStar smoke loaded 80.76 GiB of model spans in 18.106
  seconds, planned 81.18 GiB, passed its exact direct-answer check, and measured
  33.87 prompt tokens/s and 15.68 generated tokens/s. Its retained result is
  `apps/acceptance/results/20260809T191506Z-79592f44.json`, with the adjacent
  Markdown report.
- A 2026-08-13 provider-identity probe on the same image started the managed
  model at 4K context and sent
  `model: deepseek-v4-flash-0731-q2-imatrix` through Chat Completions. The
  server accepted and echoed that exact ID, returned the requested
  `DWARFSTAR_ID_OK` response at 15.67 generated tokens/s, and stopped cleanly
  without a recorded GPU fault. Its `/v1/models` endpoint continued to expose
  the generic engine aliases; request compatibility, not discovery output,
  establishes the exact harness identity.
- The managed 131072-context DwarfStar server planned 83.80 GiB. Direct-answer
  and normal-thinking requests passed, a two-turn continuation reused 78
  cached tokens, and a 640-token direct decode completed at 15.78 tokens/s.
  The receipt stayed current across runtime, explicit stop removed the
  container, and the kernel recorded no GPU reset, page fault, ring timeout,
  device-loss, or OOM event. This was a manual exploratory run, not a formal
  matrix `PASS`; the engine-reported memory plan was not an independently
  measured peak.
- The managed Qwen3.6 27B Q8_0 MTP server ran at its 262144-token context and
  completed nested tool-call/result round trips in both streaming and
  non-streaming modes. A fixed three-repetition pp512/tg128 comparison on the
  non-MTP model measured 343.40/7.82 tokens/s with ROCm and 254.60/7.84
  tokens/s with Vulkan. ROCm prompt processing was 34.9% faster; decode was
  effectively tied. Results are retained as
  `apps/llama-cpp/benchmarks/20260809T184312Z-f70ab4d7.json`,
  `20260809T184422Z-3a1edea5.json`, and
  `20260809T184422Z-backend-comparison-860c0bbf.json` below the same benchmark
  directory.
- A 2026-08-11 OMP 17.2.12 probe used the managed launcher and
  llama.cpp image `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r16`
  to load Qwen3.6 35B-A3B MTP at 262144 context.
  OMP called its `read` tool inside the default bubblewrap boundary, replayed
  the 318-line `AGENTS.md` result, and returned the exact requested heading.
  The first turn processed 21,358 prompt tokens at 708.34 tokens/s and decoded
  158 tokens at 66.94 tokens/s, with 112 of 138 MTP proposals accepted. The
  cached follow-up processed 4,702 new tokens at 535.68 tokens/s and decoded
  44 at 64.43 tokens/s, with 33 of 39 proposals accepted. Neither turn was
  truncated. A separate managed DwarfStar pass through the same OMP sandbox
  called `read`, replayed its result, and returned the same exact heading; its
  tool and final turns took 111.4 and 128.1 seconds. Both containers stopped
  cleanly. This verifies OMP model selection, tool replay, and sandbox behavior
  on two providers, not the complete agent matrix.
- A later manual Muse Glimmer run used source commit `9de3587`, llama.cpp
  commit `62bf73d25c53b8161f8a22894d4f90c4aebbd7d0`, image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r15`, and the
  then-managed Unsloth Q8 target through preset
  `muse-glimmer-30b-ud-q8-k-xl-dflash` at 128K. OpenCode 1.18.15 completed a five-turn
  read-only repository task with structured grep and read calls, replayed
  their results, recovered from one lookup that found no files, and returned
  the requested catalog schema and llama.cpp pin exactly. DFlash accepted
  20.5% to 40.7% of proposed tokens across the turns, with reported generation
  rates of roughly 20 to 35 tokens/s. This accepts the representative
  OpenCode tool contract for those historical bytes at 128K.
- A 2026-08-10 comparison at source commit `6d59816` used the same llama.cpp
  commit and host to compare Meta's official dynamic K-quant, the former
  Unsloth Q8, and an Unsloth BF16 GGUF. Fixed pp512/tg128 results were
  341.17/10.32, 376.80/7.35, and 483.19/4.11 tokens/s respectively. Fresh
  128K DFlash servers used about 30.64 GB for K-quant and 66.77 GB for BF16.
  No run produced a GPU reset, OOM, or device loss. This selected K-quant for
  the catalog but did not mark the formal Muse matrix row `PASS`; immutable
  inputs and runtime caveats are recorded in
  [the feasibility snapshot](muse-glimmer-llama-cpp-agent-feasibility.md).
- The candidate integration image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r16` (image ID
  `15aa29c45b41f011f5edacd9f5fb761db26eae488d447453414e2d1b2a9e07a3`)
  subsequently loaded the verified official K-quant and DFlash files through
  the managed 128K preset. Direct startup included `--reasoning-preserve`, a
  bounded request returned exact content `OK`, and router startup accepted
  `reasoning-preserve = true` in all three Muse sections, loaded the 128K
  DFlash section on demand, and returned exact content `ROUTER_OK`. Both
  containers stopped cleanly. This verifies the new wiring, but remains a
  field observation rather than a formal matrix `PASS`.
- The 2026-08-12 Muse ATEM template correction was accepted with image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r17` (image ID
  `98369219e680a5e44517ba1955a4fb3ce18fbcbf80cc3d89961e76648ddcb193`).
  Direct and routed 128K DFlash servers both received the pinned managed
  template and completed required structured tool calls; the direct path also
  completed a tool-result continuation. This is a template-regression field
  observation rather than a formal matrix `PASS`.
- A 2026-08-13 follow-up on the same Fedora Strix Halo system and llama.cpp
  revision compared the official dynamic target with Meta's current 17 GB
  Q4_K_M target, and compared DFlash depths on both backends. Controlled 128K
  workloads selected depth 15 for ROCm and depth 4 for Vulkan. At a 64347-token
  prompt, the 17 GB target reached 27.35 generated tokens/s on ROCm and 27.23
  on Vulkan, versus 22.08 and 20.68 for the dynamic target. Managed direct
  Vulkan and routed Vulkan startup both resolved depth 4 and returned exact
  bounded content, while a ROCm control exercised depth 15. Containers
  stopped cleanly and the kernel journal showed no matching GPU fault. Exact
  inputs, the AMD reference caveats, complete matrix, and cleanup warning are
  in the
  [Muse feasibility record](muse-glimmer-llama-cpp-agent-feasibility.md).
- The 2026-08-13 two-variant Muse follow-up temporarily promoted both
  separately pinned target/draft pairs into the guided family while retaining
  dynamic 128K DFlash as its launch default. At that revision, the new 17 GB
  base and forced-256K policies appeared with the existing 128K DFlash policy
  in all four agent-client catalogs. Direct ROCm, routed ROCm, and direct
  Vulkan forced-256K servers
  loaded four 262144-token slots with automatic fitting disabled. ROCm used
  draft depth 15 and returned exact content `MUSE_256K_OK` and
  `ROUTER_256K_OK`; Vulkan used depth 4 and returned `VULKAN_256K_OK`. The
  short requests generated at 37.90, 39.59, and 31.67 tokens/s respectively,
  but are wiring probes rather than performance comparisons. All containers
  were removed cleanly and the kernel journal contained no matching GPU
  fault. Full details are in the
  [Muse feasibility record](muse-glimmer-llama-cpp-agent-feasibility.md).
- A 2026-08-12 KAT-Coder template audit used the same Fedora Strix Halo host,
  llama.cpp commit, KAT Q8_0 artifact, Pi 0.84.1, ROCm backend, 131072-token
  evaluation context, and high thinking. Candidate image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r18` had image
  ID `98b81f0ab4b1cf948de4564e3c01bd722acc360a1c25538380e1b9f9361f0a04`.
  Its managed template matched Kwaipilot base revision
  `3a7d874090df0cd4399401982eca67df2c5a7e82` byte for byte and rendered a
  non-leading system message that the template embedded in the unchanged
  GGUF rejects. A direct server completed a required structured tool call and
  tool-result continuation. The final router policy loaded KAT on demand with
  the exact managed template, without `--reasoning-preserve`, then completed
  another required tool call and continuation before stopping cleanly. The
  same image's Qwen3 0.6B override rendered array-form text content and
  returned exact bounded content `TEMPLATE_OK`.

  The compatibility fix is retained, but reasoning preservation is not
  enabled and no quality or speed promotion is claimed.
- A 2026-08-14 Qwen3.6 template audit on the same Fedora Strix Halo class
  compared the common template embedded in all four managed GGUFs, a narrow
  compatibility correction, and a neutralized Froggeric v22 control. Exact
  llama.cpp rendering showed that the embedded baseline silently omitted
  later system and developer instructions and replayed an empty historical
  reasoning block. The narrow candidate fixed both without preserving old
  reasoning or changing the tool prompt. V22-neutral also retained old
  reasoning by default and expanded a representative tool continuation from
  305 to 416 prompt tokens. In a deterministic two-turn probe that raised the
  second prompt from 128 to 645 tokens; cache reuse rose from 101 to 616, but
  wall time rose from 6.16 to 6.75 seconds.

  Final image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r20` (image ID
  `ad6d419895b89e050c5e813e6cb0e2ed82261d2887bea27b0a92734fd1774992`)
  contained managed `qwen3.6.jinja` with SHA-256
  `ea69920311f2efccf6343675490b27bd22d03787ebb8ccaf6e9101bfeba72898`.
  Direct startup retained a later developer message, completed a structured
  tool call and tool-result continuation, and returned exact content
  `FINAL_TEMPLATE_OK`. Router startup mapped the same template into all four
  Qwen3.6 sections and returned `ROUTER_TEMPLATE_OK` from the dense non-MTP
  control. Both paths stopped cleanly and the kernel recorded no matching GPU
  fault. Complete inputs, candidate hashes, prompt/cache measurements, and
  selection rationale are in the
  [Qwen3.6 tuning record](qwen3.6-strix-halo-llama-cpp-tuning-feasibility.md).
- A 2026-08-11 Laguna XS 2.1 probe used the same pinned llama.cpp commit and
  Fedora Strix Halo host with Poolside's official Q4_K_M GGUF. Under the
  project's Strix policy, Flash Attention off, F16 K/V cache, batch 2048, and
  microbatch 512, a fixed three-repetition pp512/tg128 run measured
  885.81/60.59 tokens/s. The raw API returned exact arithmetic, a valid
  structured tool call, and a correct tool-result continuation. A fresh 256K
  allocation used about 58.42 GB of container memory and left about 68 GiB
  available, but did not fill the window with a 256K prompt. A read-only Pi
  task sustained valid tool loops through roughly 30K context and compaction;
  an overly broad review prompt did not converge before manual interruption.
  The candidate catalog then reused and rehashed the retained 18.88 GiB file
  through the normal mirror installer. Direct managed startup completed a
  structured tool-call/result round trip. Router startup exposed all 16
  installed presets and loaded Laguna XS on demand with `--jinja`,
  `--reasoning-preserve`, 262144 context, Flash Attention off, and
  `--load-mode none`; its bounded tool request was also valid. Both startup
  modes warned that `special_eos_id` and `special_eot_id` were absent from the
  special EOG set, but the accepted requests terminated normally. Revisit the
  warning if longer runs show premature or missing termination. Both
  containers stopped and were removed. No run produced an OOM, GPU reset,
  device loss, or kernel fault.
- The same investigation rejected Ling 3.0 Flash on ROCm. Atomic Q4 and Q5
  GGUFs were coherent through CPU and Vulkan, while the patched TurboQuant
  ROCm path returned corrupted text and malformed tool arguments on `gfx1151`.
  Exact inputs, backend controls, benchmarks, and retest criteria are in the
  [Ling feasibility snapshot](ling-3.0-flash-llama-cpp-feasibility.md).
- The 2026-08-13 Qwen27 tuning acceptance used the candidate bytes subsequently
  committed as `9fa54a5` and image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r19` (image ID
  `d4b7065b465a85efbfc5ff0aa10895283bdc5e79b2aae5b528f9f1b6e9647147`).
  Direct and router ROCm startup at the default 262144 context both resolved
  MTP depth three, Flash Attention on, and symmetric Q8_0 target K/V while
  leaving the draft cache F16. Exact bounded requests passed through both
  paths. A 131072-context Vulkan control also returned exact content and
  accepted 114 of 117 MTP proposals. Containers stopped cleanly and the
  kernel recorded no GPU fault. Qwen35-A3B and other hardware profiles remain
  unchanged. Full controls and caveats are in the
  [Qwen tuning snapshot](qwen3.6-strix-halo-llama-cpp-tuning-feasibility.md).
  This records the tested historical candidate; the managed target cache
  returned to F16 on 2026-08-26 as a precision-first policy decision.
- A 2026-08-14 llama.cpp source update used Paracetamol commit `cad4588`,
  upstream release `b10430` at commit
  `4c1a0af40d88c7fbb3b15c85bf2e8016d1d5b64c`, and the same Fedora Strix
  Halo host. All four downstream patches were rebased and applied fail closed.
  A no-layer-cache candidate build produced image ID
  `ab0c12904be072df89dfc983d1a7a56b4bed55c1dcd251278465e8965789af30`.
  The final cold build rebuilt the complete prerequisite closure without image
  or package-download caches and produced image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-4c1a0af-r21`, image ID
  `94d426c19c6ea20270168e91da53346c7c4dd951349788426060138cdf11aa67`.
  The final image reported the exact upstream revision and all four target
  architectures, passed `pip check` and retained-binary `ldd`, and contained
  only `llama-cli`, `llama-server`, `llama-bench`, and the entrypoint below
  `/usr/local/bin`.

  CPU CLI and router startup passed on the candidate. Fixed three-repetition
  pp512/tg128 runs on Qwen3.6 27B Q8_0 at 32768 context with Q8_0 K/V and Flash
  Attention measured 206.24/7.39 tokens/s on ROCm and 184.61/7.46 on Vulkan.
  The retained results are
  `apps/llama-cpp/benchmarks/20260814T134219Z-91cb1848.json`,
  `20260814T134604Z-98dfe58f.json`, and
  `20260814T134604Z-backend-comparison-19dd2c39.json` in the same directory.
  These settings differ from the older backend observation above, so the
  measurements verify the rebased quantized-KV paths rather than establish a
  performance trend.

  The managed Qwen3.6 27B MTP preset completed a nested structured tool call
  and tool-result continuation at its default 262144 context. The managed Muse
  M 128K DFlash preset returned exact bounded content at 31.05 generated
  tokens/s and accepted 69 of 255 draft proposals. Four simultaneous long
  Qwen requests returned four distinct requested nonces without cross-slot
  replay or corruption. The final cold image then loaded Qwen3.6 27B through
  the managed router and returned exact content through both Chat Completions
  and Responses before clean removal. No tested path produced a matching GPU
  reset, page fault, ring timeout, device-loss, or OOM kernel event. This is
  `gfx1151` field coverage for the source update. The `gfx1150`, `gfx1200`,
  and `gfx1201` hardware rows remain deferred.

  A same-image follow-up tuned only the dynamic XL forced-256K DFlash preset.
  With a fixed 38,244-token repository prompt, 768 generated tokens, seed,
  sampler, F16 target KV, and 2048/512 batch policy, depth 12 generated at
  15.16 tokens/s versus 11.87 at the managed depth 15 and reduced total server
  time from 190.83 to 176.06 seconds. Depth 8 reached 12.75 tokens/s. Forced
  Flash Attention was neutral, Q8 target KV regressed, confidence cutoffs did
  not beat depth 12 with `p_min=0`, disabling backend sampling was neutral,
  and a 4096 microbatch improved prefill but regressed decode and total time.
  Longer depth-12 probes sustained about 13.4 tokens/s, but remained entirely
  in Muse reasoning through 4,096 tokens and through a manually interrupted
  7,406-token run. The result therefore accepts depth 12 as a `gfx1151` ROCm
  performance policy for this preset, not as new forced-256K quality evidence.

- A 2026-08-14 Qwen3.8 candidate acceptance used the pinned Unsloth
  `Qwen3.8-27B-UD-Q8_K_XL.gguf` at revision
  `4604b899a826000505a834e623272db5b7fd62f6`, SHA-256
  `af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377`.
  The one 31,457,991,680-byte file reports the `qwen35` architecture, native
  262144 context, and embedded MTP tensors. The reasoning-template forwarding
  change produced image
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-4c1a0af-r22`, image ID
  `86351eeda1d4c89f7cc980f0fbbf0a5bddd21bbe0a8e6a109a3f2d5b171be6ea`.
  The image passed `pip check`; CPU startup at 4096 context and direct base,
  direct depth-three MTP, and router ROCm startup on `gfx1151` all passed. The
  direct GPU paths used the native 262144 context. The router advertised both
  presets and returned exact content through the MTP policy.

  The embedded Unsloth template retained three consecutive leading system or
  developer messages, advertised object and parallel tool-call support, and
  rendered Paracetamol's top-level low and high effort choices as the model's
  low and xhigh instructions. Medium selected the template's intentionally
  unadorned middle policy. Low, medium, and high requests all returned the
  correct bounded result. A required function call produced the exact nested
  string argument and its tool-result continuation returned exact content;
  both requests used the MTP path.

  Three fixed 256-token, no-thinking repetitions generated at 19.25, 19.07,
  and 19.06 tokens/s with depth three. The matching non-speculative control
  generated at 7.19, 7.20, and 7.20 tokens/s. MTP therefore improved this
  narrow decode workload by 2.66x and accepted 187 of 201 proposals in every
  repetition. This is a runtime-policy result, not a model-quality benchmark.

#### Qwen3.8 speculative runtime tuning (2026-08-15 to 2026-08-16)

A follow-up on the same `gfx1151` host measured the stock r23 image, image ID
`bf8a00950a0ea1611cb95486eb8517c17e0d8d3261a3bfa643c28c56ee9d2fb1`,
with Qwen3.8 medium reasoning, its reviewed sampling policy, one server slot,
and a fresh server for every request. The complete MTP depth screen covered
depths one through eight at approximately 4K, 32K, 64K, and 120K populated
context, with three seeds and 512 generated tokens in every cell: 96 requests
completed without a failure.

Depth three generated at 11.86 tokens/s over the complete matrix versus 11.49
for depth two, a 3.22% aggregate advantage. It also won at every context size:
13.29, 12.54, 11.92, and 10.15 tokens/s versus depth two's 12.77, 12.17,
11.26, and 10.10. Depths four through eight regressed progressively, while
depth one reached only 9.81 tokens/s. All requests reached the 512-token limit
while still emitting reasoning, so this accepts depth three as the throughput
policy across the tested context range, not as answer-quality evidence.

Focused follow-ups did not establish another production win. AMD-oriented GDN
layout and chunking candidates improved native `pp4096` by 7.53% but improved
managed-server generation by only 0.56%; the matching native generation result
was flat. Full input-layer GPU offload improved native `pp4096` by 3.86% but
changed server generation by 0.02% and made total request time 0.09% worse.
Default polling also beat `--poll 0` end to end. Graph disabling, graph
optimization, host-buffer disabling, simple n-gram drafting, draft-backend
sampling control, and draft minimum probabilities 0.05 and 0.10 produced no
repeatable benefit.

An exploratory four-request screen made draft `p_min=0.20` look 2.14% faster,
so it received a stock-image ABBA confirmation at 4K and 32K with eight 1,024-
token requests per condition. It changed aggregate generation from 12.693 to
12.726 tokens/s, only +0.27%, while prompt processing fell 1.03% and aggregate
request time increased 0.36%. It helped the short-context cells but regressed
the 32K cells, and only two of eight paired response hashes matched. The
candidate therefore fails the performance gate without requiring a separate
quality run.

Retain depth three, draft `p_min=0`, the default poll value, current input
placement and graph policy, backend draft sampling, and no n-gram augmentation
for this preset. No candidate patch is accepted into the application image.
The complete campaign produced no matching GPU reset, page fault, ring timeout,
device loss, SVM mapping failure, general-protection fault, or OOM kernel event.

#### Qwen3.8 Dynamic Q4_K_XL optional path (2026-08-16)

The accepted smaller path uses Unsloth's
`Qwen3.8-27B-UD-Q4_K_XL.gguf` from revision
`4604b899a826000505a834e623272db5b7fd62f6`. The managed installer verified
the exact 17,923,394,624-byte file as SHA-256
`bee238bbeb3dc0a34bde4d0dedbaee1f98c009e8bb4226f03070054c12fb1372`.
Tests used source commit `9fb84ba89d34f6065af815d6700bdb51637c8889`
and the stock r23 image
`sha256:bf8a00950a0ea1611cb95486eb8517c17e0d8d3261a3bfa643c28c56ee9d2fb1`.
The artifact loaded with working embedded MTP initialization on both ROCm and
Vulkan. Its managed presets use depth-three MTP and native medium effort.
Dynamic Q8_K_XL remains the managed-client default.

The previous 17,106,773,984-byte Q4_K_M artifact was retained as a controlled
comparison and then retired from the catalog. A fixed native control at 4K
depth used three repetitions, 512 prompt tokens, 256 generated tokens, f16
K/V, and forced Flash Attention:

| Backend | Quantization | Prompt tok/s | Generated tok/s | Estimated request |
| --- | --- | ---: | ---: | ---: |
| ROCm | Q4_K_M | 306.49 | 12.18 | 22.69 s |
| ROCm | Dynamic Q4_K_XL | 320.91 | 11.62 | 23.63 s |
| Vulkan | Q4_K_M | 276.34 | 12.57 | 22.22 s |
| Vulkan | Dynamic Q4_K_XL | 267.90 | 11.79 | 23.62 s |

Dynamic Q4_K_XL was 4.1% slower end to end on ROCm and 6.3% slower on Vulkan.
The server-side MTP comparison used a fresh server per request, medium effort,
the reviewed sampling policy, depth three, and 512 generated tokens:

| Context | Backend | Quantization | Prompt tok/s | Generated tok/s | Acceptance | Request |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| 4K | ROCm | Q4_K_M | 307.27 | 16.40 | 51.7% | 89.27 s |
| 4K | ROCm | Dynamic Q4_K_XL | 320.01 | 16.98 | 52.2% | 86.04 s |
| 4K | Vulkan | Q4_K_M | 301.27 | 22.56 | 53.7% | 72.77 s |
| 4K | Vulkan | Dynamic Q4_K_XL | 256.99 | 22.22 | 56.7% | 78.19 s |
| 32K | ROCm | Q4_K_M | 235.58 | 16.49 | 61.0% | 170.19 s |
| 32K | ROCm | Dynamic Q4_K_XL | 249.37 | 15.44 | 52.5% | 164.59 s |
| 32K | Vulkan | Q4_K_M | 265.79 | 18.20 | 47.7% | 151.44 s |
| 32K | Vulkan | Dynamic Q4_K_XL | 226.42 | 20.07 | 60.1% | 170.29 s |

The new quantization ranged from 3.6% faster to 12.4% slower by whole-request
time. Vulkan won the Dynamic Q4_K_XL 4K requests by 9.1%, while ROCm won the
32K request by 3.5%; this mixed result does not justify the Q4_K_M path's old
blanket Vulkan recommendation. The global ROCm default therefore applies and
Vulkan remains an explicit workload choice.

The default Dynamic Q8_K_XL's matched ROCm depth-three trials generated 13.60
tokens/s at 4K and 11.91 at 32K. Dynamic Q4_K_XL generated 16.98 and 15.44,
respectively: 25% and 30% faster. Its request time improved 15% at 4K and 7.5%
at 32K. This visible smaller-tier speedup, plus the close Q4_K_M comparison,
is the basis for replacing Q4_K_M rather than offering two similar 17 GB
variants. It is not an equivalent-quality claim against Dynamic Q8_K_XL.

The actual lazy Vulkan router rendered the candidate with depth three, the
reviewed Qwen3.8 template, preserved reasoning, and 64K context. A required
function call emitted the exact nested semantic argument `outer.value = 7`;
its tool-result continuation returned exact `TOOL_OK_42`.

All transient containers were removed. The kernel journal for the complete
download, ROCm/Vulkan benchmark, router, and test interval had no
matching GPU reset, page fault, ring timeout, device loss, SVM mapping failure,
general-protection fault, or OOM event.

#### Qwen3.8 Dynamic Q4_K_XL 128K context follow-up (2026-08-18)

The Fedora Strix Halo host then compared the same pinned Dynamic Q4_K_XL
artifact at 64K, 128K, and 256K with ROCm, embedded MTP depth three, native
medium effort, Pi 0.84.2, and the balanced platform profile. Tests used
Paracetamol commit `80f2e2f3d6bf6406c0f2ef3f9d195e8bb93cad6c` and image
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
`7c5d6efa0df5c5965db38fd59911ab4856a02620674e62ff62dfc55b9ca58d3f`).

Each context received two fresh attempts at the same hidden-graded synthetic
Go storage-planning task. The 64K pair solved zero attempts: one produced an
incorrect candidate, while the other exhausted its 16,384-token response
allowance, compacted at 58,764 total tokens, and settled without an edit. The
256K pair produced two candidates without compaction and solved one. Both
128K attempts produced clean candidates without compaction and passed the
ordinary tests, independent hidden tests, and build. They ended at 58,807 and
65,522 total tokens, took 1,438.906 and 2,327.271 seconds, and generated
20,526 and 37,055 tokens at 16.254 and 17.555 tokens/s respectively.

Fresh idle servers used 25.98 GB at 64K, 30.83 GB at 128K, and 40.52 GB at
256K according to `podman stats`. Observations during the active 128K attempts
were 31.73-31.82 GB (up to about 29.63 GiB). This provides a reasonable
capacity basis for adopting 128K as the Q4 preset default while retaining
substantially more headroom than 256K on Strix Halo. It suggests, but does not
prove, that a headless 32 GiB discrete GPU can contain the preset; gfx1201
acceptance remains pending on that hardware.

#### Qwen3.8 Dynamic v3 Q4_K_XL update (2026-08-19)

Unsloth replaced the Q4 artifact while leaving the managed Q8 bytes unchanged.
The accepted Q4 pin is repository revision
`27af057ecb382ddfea5d12837360a8980560e3ed`, exact size 17,559,178,144
bytes (16.35 GiB), and SHA-256
`3f227079003add2511437e5b1e94812e363385225bf6a9b47b0054a72bc8b01e`.
It contains 866 tensors, including the embedded block-64 MTP heads, so the Q4
presets continue to share one file and do not use Unsloth's separately
published MTP artifact. The changed destination lets this file coexist with
the earlier 17,923,394,624-byte preview instead of overwriting it.

Tests used Paracetamol source `1507f54dba3de141fa4592a9fdfd92d8f39809c1`,
Pi 0.84.2, and stock r29 image
`sha256:7c5d6efa0df5c5965db38fd59911ab4856a02620674e62ff62dfc55b9ca58d3f`
on the Fedora Strix Halo host. A matched native ROCm benchmark used 4K
populated context, `pp512`, `tg256`, Q8_0 target K/V, forced Flash Attention,
and three repetitions. The new file measured 303.47 prompt and 11.89
generated tokens/s versus 329.09 and 11.62 for the preview: prompt processing
fell 7.8% while native decoding improved 2.4%.

The more representative embedded-MTP screen used depth three, medium effort,
128K server context, 256 generated tokens, two seeds, and 4K plus 64K populated
prompts. Dynamic v3 aggregated 17.73 generated tokens/s, 64.1% draft
acceptance, and 197.65 prompt tokens/s. The preview aggregated 17.16, 63.0%,
and 201.54 respectively. The update therefore improved MTP generation 3.3%
and reduced prompt processing 1.9%; it is a modest trade rather than a large
speed release.

Direct API acceptance returned exact bounded results at off, medium, and
`xhigh`; off emitted no reasoning, while both enabled modes emitted reasoning.
A required nested `record_value` call carried the requested string and integer
arguments, and its tool-result continuation returned exact `FINAL_TOOL_OK`.
No request required a separate MTP file, and the server stopped cleanly.

Fresh Pi evaluation used the frozen `paracetamol-coding-v5` suite at the Q4
preset's 128K default and native medium effort. Dynamic v3 strictly solved
`re-align`, `fz-symlink`, and `proxy-late-probe`; `re-cancel` and
`rc-selinux-verify` completed but failed hidden grading. The earlier artifact
was rerun only on those two misses under identical conditions. It reproduced
the same hidden cancellation failure, then took 2,610.0 seconds and 37,608
output tokens before failing both visible-regression and hidden grading on the
SELinux task. Dynamic v3 took 1,031.6 seconds and 15,229 output tokens on that
task with no visible regression. This small stochastic control does not prove
a general quality improvement, but it found no regression on either observed
miss and found a large practical efficiency gain on the harder one.

The complete verification, benchmarks, API probes, and seven Pi attempts
produced no matching GPU page fault, reset, device loss, ring timeout, SVM
mapping failure, general-protection fault, or OOM kernel event. This accepts
the Dynamic v3 Q4 bytes on `gfx1151`; Q8 remains the managed-client default,
and dedicated `gfx1201` 32 GiB capacity remains untested.

#### Retired coding-agent comparisons (2026-08-17)

The previous cross-model quality tables and retained result references were
removed after model-native sampling, chat templates, harness versions, and
managed defaults changed materially. They are not a current baseline. New
comparisons must be generated from the frozen runner under newly declared
conditions; no model-quality conclusion is carried forward.

## Automated bounded smoke

Start acceptance on a new or updated host with:

```bash
./paracetamol acceptance --dry-run
./paracetamol acceptance
```

The checkpointed suite covers exact GPU/CPU device isolation, short ComfyUI
image and video workloads, one llama.cpp workload, and a bounded DwarfStar
generation. DwarfStar is included by default on Strix Halo and must be
selected explicitly elsewhere because of its memory footprint. Generated
media requires explicit human review before its case becomes `PASS`; the
review phase starts only after all selected automated workloads finish.
Prompts and the Markdown report retain case-specific functional criteria so
aesthetic defects are not confused with broken inference. Unattended runs
leave those cases `BLOCKED`. Preserve the JSON and Markdown result below
`apps/acceptance/results/` and resume an interrupted run with `--resume`.

This command is the fast recurring gate. It intentionally does not replace
the rows below: edit and I2V behavior, additional model families, precision
comparisons, forced-profile failures, memory policy comparisons, and sustained
performance still require the finite manual matrix.

## Host diagnostics

Complete every row on all target hosts before describing their profiles as
accepted.

| Check | RX 9060 family / `gfx1200` | R9700 or RX 9070 family / `gfx1201` | Strix Halo / `gfx1151` | Strix Point / `gfx1150` |
| --- | --- | --- | --- | --- |
| `doctor` reports the exact architecture | pending | pending | pending | pending |
| `auto` resolves the expected profile | pending | pending | pending | pending |
| forcing the expected profile succeeds | pending | pending | pending | pending |
| forcing either other profile fails closed | pending | pending | pending | pending |
| exactly the selected render-node set is exposed | pending | pending | pending | pending |
| CPU mode exposes no GPU devices | pending | pending | pending | pending |
| device access needs no broader container privileges or disabled SELinux labels | pending | pending | pending | pending |
| RAM, TTM module/ceiling, and effective GTT are reported | N/P: dedicated VRAM | N/P: dedicated VRAM | pending | pending |

Record for each host:

```text
Date:
Git commit:
Kernel and distribution:
GPU and system RAM:
Render node:
ROCm/PyTorch base image ID:
Application image IDs:
Persistent-data location:
Result/log location:
```

## Representative application gate

Use the catalog-owned workflow or preset and its default runtime policy unless
the row says otherwise. For every generation row, inspect output sanity as
well as successful process completion. Exercise I2V/edit rows with a fixed
local source image.

| Application path | Exact content or comparison | RX 9060 family | R9700 or RX 9070 family | Strix Halo | Strix Point |
| --- | --- | --- | --- | --- | --- |
| ComfyUI image | `qwen-image-2512-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI edit | `qwen-image-edit-2511-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI Wan T2V | `wan-2.2-t2v-14b-fp8-lightning` | pending | pending | pending | pending |
| ComfyUI Wan I2V | `wan-2.2-i2v-14b-fp8-lightning` | pending | pending | pending | pending |
| LTX-2 T2V camera graph | `ltx-2-t2v-19b-fp8-full`, ordinary and one enabled camera adapter | pending | pending | pending | pending |
| LTX-2 I2V camera graph | `ltx-2-i2v-19b-fp8-full`, ordinary and one enabled camera adapter | pending | pending | pending | pending |
| Hunyuan T2V | `hunyuan-video-1.5-t2v-480p-cfg-distilled` | pending | pending | pending | pending |
| Hunyuan I2V | `hunyuan-video-1.5-i2v-480p-step-distilled` | pending | pending | pending | pending |
| llama.cpp offload smoke | `llama-qwen-qwen3-0.6b-q8-0` | pending | pending | pending | pending |
| llama.cpp Qwen3.8 tool protocol | `unsloth-qwen3.8-27b-mtp-ud-q8-k-xl`, complete nested tool round trip at 256K with off, low, medium, and xhigh | N/P unless model and context fit the card | N/P unless host memory is deliberately used | pending | pending |
| llama.cpp Huihui Flash-Next | `huihui-ai-qwen3.8-flash-next-125b-a6b-abliterated-v2-ud-q4-k-xl`, shared regular Flash-Next policy with off, low, medium and xhigh | ROCm N/P by model policy, Vulkan requires high-memory host offload | ROCm N/P by model policy, Vulkan requires high-memory host offload | accepted 2026-10-16 on ROCm at 256K capacity and Vulkan at 32K input | ROCm N/P by model policy, Vulkan pending |
| llama.cpp Qwen3.8 Q4 optional path | `unsloth-qwen3.8-27b-mtp-ud-q4-k-xl`, 128K medium-effort tool round trip; compare ROCm and Vulkan | pending | pending | accepted 2026-08-18 on ROCm; 64K Vulkan protocol accepted 2026-08-16 | pending |
| llama.cpp Muse DFlash | `meta-models-muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k`, ROCm depth 12 or Vulkan depth 4, high strength | N/P unless model and context fit the card | N/P unless host memory is deliberately used for offload | accepted 2026-08-14 on ROCm | pending |
| DwarfStar direct-answer smoke | DeepSeek V4 Flash 0731 Q2 imatrix (routed IQ2_XXS/Q2_K, Q8 attention/shared/output), 4K context, 64-token ceiling | N/P unless host memory offload is deliberately provisioned | pending | pending | pending |

DwarfStar remains experimental after the bounded smoke. Before promoting it,
also run the 128K server default, normal thinking and direct-answer requests,
multi-turn cache reuse, a long enough generation to expose decode faults, and
clean interruption/removal. Record model-load peak memory and sustained
generation speed. On non-Halo profiles, invoke the smoke with
`--application dwarfstar`; that explicit selection is a capacity opt-in, not
evidence of prior acceptance. Record whether an APU host used the general 112
GiB TTM/GTT starting point or DwarfStar's roughly 124 GiB upstream
recommendation, and whether `amd_iommu=off` was enabled. The initial 112 GiB
manual 128K run used
it, so memory-capacity acceptance and IOMMU performance remain separate
questions. The exact DSpark pair is an experimental opt-in contract; arbitrary
MTP files, multi-GPU, distributed execution, and SSD streaming are not part of
the current application contract.

For the Qwen tool-protocol row, start the managed router and inspect `/props`
for the expected template capabilities and context. Send a developer message
and a tool schema with a nested object, require a structured tool call, return
the result as a tool message, and require a final answer. Repeat the exchange
with streaming enabled. If either MTP preset fails, repeat it with its matching
non-MTP preset to separate template handling from speculative decoding.
Only after the raw API exchange passes should Pi and Maki tasks be used as the
final integration checks.

For a future agent comparison, declare explicit model-native selectors rather
than treating one label as equivalent across families. Run every candidate
through the same harness, tasks, repetition count, and hardware/runtime inputs.
Keep edit and shell approvals equivalent across runs. Compare structured
results only after all candidates complete; record quality, protocol,
state-corruption, or truncation failures instead of treating wall time as the
sole rank. Run per-model native-level sweeps as a separate experiment.

Keep the first Qwen3.8 pass on Paracetamol's pinned official-template adaptation.
Do not replace it mid-comparison in response to anecdotal release reports. The
froggeric community template is a separate candidate because it changes
history rendering, role handling, tool serialization, control tags, and
failure-recovery prompt policy. If the official-template baseline exposes a
matching failure, run a Qwen3.8-only A/B with the target GGUF, image, native
reasoning choice, sampling, task, and repetition count fixed; identify the
template revision in the result notes.

For the Muse row, begin with the forced-256K DFlash default for a complete
managed tool loop, then repeat the task with the 128K DFlash and
non-speculative controls when behavior or output is suspect. Exercise prompts
extending beyond 128K and inspect retrieval quality, tool selection, draft
acceptance, latency, and memory rather than treating successful startup as
acceptance.
Pi and Maki remain exposed through the same reviewed
function-tool contract, but protocol compatibility does not establish
comparative quality. Complete the intended live task in each client before
allowing unattended writes. Confirm `reasoning-preserve = true` in
router mode or `--reasoning-preserve` in direct-server startup; do not confuse
that history policy with Muse's client-selectable reasoning strength.

For a host with two matching GPUs, add these single-workload checks:

| Multi-GPU path | Check |
| --- | --- |
| Doctor | select both render nodes; verify both named devices, one architecture, and a passed tensor operation on each |
| llama.cpp | run one server with both render nodes; verify managed layer split, activity on both cards, successful generation, and enough per-card headroom for KV/runtime buffers |
| ComfyUI component placement | run one graph with model, CLIP, or VAE deliberately placed on different cards; verify both activity and sane output |
| ComfyUI CFG split | run one graph with `MultiGPU CFG Split`; verify both activity and sane output without claiming pooled VRAM |

## Precision and policy coverage

The representative gate above establishes application and task behavior. Add
these comparisons before making performance or memory recommendations:

| Comparison | RX 9060 family | R9700 or RX 9070 family | Strix Halo | Strix Point |
| --- | --- | --- | --- | --- |
| one Qwen Image FP8/BF16 pair with the same workflow | pending | pending | pending | pending |
| one Wan FP8/FP16 pair with the same workflow | pending | pending | pending | pending |
| one LTX-2 FP8/BF16 full-model pair | pending | pending | pending | pending |
| Wan T2V Seko V1.1 versus V2.0 with identical inputs | pending | pending | pending | pending |
| balanced versus conservative memory policy | pending | pending | pending | pending |
| default versus experimental kernel policy | pending | pending | pending | pending |
| persistent versus isolated benchmark cache | pending | pending | pending | pending |

Use the same image, catalog commit, workflow, prompt, input, dimensions,
frames, seed, and render-node set for each comparison. Performance conclusions
require repeated measurements, not merely successful inference.

## Per-run result

For a manual inference result that is not already captured by managed
benchmark JSON, record:

```text
Status:
Date:
Git commit:
Application image ID:
Catalog bundle and source revisions:
Profile and render-node set:
Runtime policies:
Prompt/input identity:
Dimensions, frames, steps, and seed:
Peak memory:
Prompt/generation timing:
Output sanity:
Warnings or deviations:
Result/log location:
```

## Deferred acceptance handoff

When one contributor cannot reach every required hardware class, leave a
bounded, reproducible handoff instead of a generic request to “test on another
GPU.” Keep hostnames and personal infrastructure out of this document; record
the architecture and observable requirements.

```text
Change under test:
Source commit and application image ID:
Already accepted on:
Deferred architecture/profile:
Behavior or patch path requiring coverage:
Required managed content:
Exact commands:
Expected success criteria:
Result and log destination:
Known warnings or capacity constraints:
```

Commands must name the profile, render-node set, backend, preset, context,
cache policy, prompt and generation sizes, and repetition count whenever those
values affect the conclusion. The person completing the handoff should append
the observed result to the original change or its review record and retain
the generated JSON rather than returning only “works for me.”

The first publication may call uncompleted rows experimental, but it must not
present them as accepted. A later dependency, runtime-policy, ROCm/PyTorch, or
model change invalidates only the coupled rows; repeat those rows on every
hardware class where practical.
