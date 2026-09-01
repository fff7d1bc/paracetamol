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
accepted reasonable Flash-Next quant on this Aion configuration; IQ4_XS
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
| llama.cpp offload smoke | `llama-qwen3-0.6b-q8-0` | pending | pending | pending | pending |
| llama.cpp Qwen3.6 tool protocol | `qwen3.6-27b-mtp-q8-0`, complete nested tool round trip at 256K with thinking on (Pi `high`) and off | N/P unless model and context fit the card | N/P unless host memory is deliberately used | pending | pending |
| llama.cpp Qwen3.8 tool protocol | `qwen3.8-27b-mtp-ud-q8-k-xl`, complete nested tool round trip at 256K with off, low, medium, and xhigh | N/P unless model and context fit the card | N/P unless host memory is deliberately used | pending | pending |
| llama.cpp Qwen3.8 Q4 optional path | `qwen3.8-27b-mtp-ud-q4-k-xl`, 128K medium-effort tool round trip; compare ROCm and Vulkan | pending | pending | accepted 2026-08-18 on ROCm; 64K Vulkan protocol accepted 2026-08-16 | pending |
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
