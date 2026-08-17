# Target-hardware acceptance

ROCmplete targets `gfx1201`, `gfx1200`, `gfx1151`, and `gfx1150`, but a
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
| Fedora Linux 44 (non-OSTree), Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash at 4K and 128K context, including the exact DSpark pair; Qwen3.6 MTP tool protocol, OMP probe, and fixed ROCm/Vulkan llama.cpp benchmarks; Muse Glimmer runtime probes; Laguna XS and Ling feasibility controls |
| Ubuntu 26.04, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000 | Strix Halo, `gfx1151` | DwarfStar DeepSeek V4 Flash and the managed Qwen3.6 llama.cpp presets |
| SteamOS 3.8, Radeon RX 9070 XT 16 GB | RDNA 4, `gfx1201` | ComfyUI and the Qwen3 0.6B llama.cpp smoke |

### Fedora 44 Strix Halo model-scoped reasoning fallback (2026-08-17)

ROCmplete commit `0355a73` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
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

ROCmplete commit `84ada22` was exercised with Pi 0.84.2 on the Fedora 44
Strix Halo host with kernel `7.1.7-200.fc44.x86_64`, ROCm 7.14, and
`/dev/dri/renderD128`. The router used the existing
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` image (image
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

ROCmplete commit `492bafb` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` (image ID
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

ROCmplete commit `b90eded` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r28` (image ID
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

ROCmplete commit `fe6d5c3` was built and exercised on the Fedora 44 Strix Halo
host with kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r26` (image ID
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

ROCmplete implementation commit `abec977`, from checkout `53b0032`, was built
and exercised on the Fedora 44 Strix Halo host with kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The resulting image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r27` (image ID
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

ROCmplete commit `f6d4c43` restored the pinned non-MTP and MTP Qwen3.6
35B-A3B catalog paths without changing the managed-client default. The MTP
path was exercised on the Fedora 44 Strix Halo host with kernel
`7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The existing llama.cpp image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r25` (image ID
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

ROCmplete commit `363fedf` was exercised on the Fedora 44 Strix Halo host with
kernel `7.1.7-200.fc44.x86_64`, profile `strix-halo`, ROCm 7.14, and
`/dev/dri/renderD128`. The DwarfStar image was
`localhost/rocmplete:dwarfstar-ubuntu26.04-rocm7.14-84cc882-r7` (image ID
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
`localhost/rocmplete:dwarfstar-ubuntu26.04-rocm7.14-d250a7c-r4` (image ID
`531fdef3be07c78825c573a3b2ceb333ab6b47245158f65894150fe3a54e902d`),
and the managed DeepSeek V4 Flash 0731 Q2 imatrix bundle. The llama.cpp tests
used image `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-ddd4ec1-r14`
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
  llama.cpp image `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r16`
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r15`, and the
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r16` (image ID
  `15aa29c45b41f011f5edacd9f5fb761db26eae488d447453414e2d1b2a9e07a3`)
  subsequently loaded the verified official K-quant and DFlash files through
  the managed 128K preset. Direct startup included `--reasoning-preserve`, a
  bounded request returned exact content `OK`, and router startup accepted
  `reasoning-preserve = true` in all three Muse sections, loaded the 128K
  DFlash section on demand, and returned exact content `ROUTER_OK`. Both
  containers stopped cleanly. This verifies the new wiring, but remains a
  field observation rather than a formal matrix `PASS`.
- The 2026-08-12 Muse ATEM template correction was accepted with image
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r17` (image ID
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r18` had image
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r20` (image ID
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r19` (image ID
  `d4b7065b465a85efbfc5ff0aa10895283bdc5e79b2aae5b528f9f1b6e9647147`).
  Direct and router ROCm startup at the default 262144 context both resolved
  MTP depth three, Flash Attention on, and symmetric Q8_0 target K/V while
  leaving the draft cache F16. Exact bounded requests passed through both
  paths. A 131072-context Vulkan control also returned exact content and
  accepted 114 of 117 MTP proposals. Containers stopped cleanly and the
  kernel recorded no GPU fault. Qwen35-A3B and other hardware profiles remain
  unchanged. Full controls and caveats are in the
  [Qwen tuning snapshot](qwen3.6-strix-halo-llama-cpp-tuning-feasibility.md).
- A 2026-08-14 llama.cpp source update used ROCmplete commit `cad4588`,
  upstream release `b10430` at commit
  `4c1a0af40d88c7fbb3b15c85bf2e8016d1d5b64c`, and the same Fedora Strix
  Halo host. All four downstream patches were rebased and applied fail closed.
  A no-layer-cache candidate build produced image ID
  `ab0c12904be072df89dfc983d1a7a56b4bed55c1dcd251278465e8965789af30`.
  The final cold build rebuilt the complete prerequisite closure without image
  or package-download caches and produced image
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-4c1a0af-r21`, image ID
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
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-4c1a0af-r22`, image ID
  `86351eeda1d4c89f7cc980f0fbbf0a5bddd21bbe0a8e6a109a3f2d5b171be6ea`.
  The image passed `pip check`; CPU startup at 4096 context and direct base,
  direct depth-three MTP, and router ROCm startup on `gfx1151` all passed. The
  direct GPU paths used the native 262144 context. The router advertised both
  presets and returned exact content through the MTP policy.

  The embedded Unsloth template retained three consecutive leading system or
  developer messages, advertised object and parallel tool-call support, and
  rendered ROCmplete's top-level low and high effort choices as the model's
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
Vulkan. Its managed presets retain a conservative 65536-token context,
depth-three MTP, and native medium effort. Dynamic Q8_K_XL remains the
managed-client default.

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

#### Retired coding-agent comparisons (2026-08-17)

The previous cross-model quality tables and retained result references were
removed after model-native sampling, chat templates, harness versions, and
managed defaults changed materially. They are not a current baseline. New
comparisons must be generated from the frozen runner under newly declared
conditions; no model-quality conclusion is carried forward.

## Automated bounded smoke

Start acceptance on a new or updated host with:

```bash
./rocmplete acceptance run --dry-run
./rocmplete acceptance run
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
| llama.cpp offload smoke | `llama-qwen3-0.6b-q8-0` | pending | pending | pending | pending |
| llama.cpp Qwen3.6 tool protocol | `qwen3.6-27b-mtp-q8-0`, complete nested tool round trip at 256K with thinking on (Pi `high`) and off | N/P unless model and context fit the card | N/P unless host memory is deliberately used | pending | pending |
| llama.cpp Qwen3.8 tool protocol | `qwen3.8-27b-mtp-ud-q8-k-xl`, complete nested tool round trip at 256K with off, low, medium, and xhigh | N/P unless model and context fit the card | N/P unless host memory is deliberately used | pending | pending |
| llama.cpp Qwen3.8 Q4 optional path | `qwen3.8-27b-mtp-ud-q4-k-xl`, 64K medium-effort tool round trip; compare ROCm and Vulkan | pending | pending | accepted 2026-08-16 on Vulkan | pending |
| llama.cpp Muse DFlash | `muse-glimmer-30b-kquant-dynamic-q4-k-xl-dflash-256k`, ROCm depth 12 or Vulkan depth 4, high strength | N/P unless model and context fit the card | N/P unless host memory is deliberately used for offload | accepted 2026-08-14 on ROCm | pending |
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
Only after the raw API exchange passes should OpenCode, Pi, OMP, and Maki tasks be
used as the final integration checks.

For a future agent comparison, declare explicit model-native selectors rather
than treating one label as equivalent across families. Run every candidate
through the same harness, tasks, repetition count, and hardware/runtime inputs.
Keep edit and shell approvals equivalent across runs. Compare structured
results only after all candidates complete; record quality, protocol,
state-corruption, or truncation failures instead of treating wall time as the
sole rank. Run per-model native-level sweeps as a separate experiment.

Keep the first Qwen3.8 pass on ROCmplete's pinned official-template adaptation.
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
OpenCode, Pi, OMP, and Maki remain exposed through the same reviewed
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
