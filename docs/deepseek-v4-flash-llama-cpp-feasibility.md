# DeepSeek V4 Flash llama.cpp feasibility snapshot

This is a dated maintainer research record from 2026-08-09. It captures why
the Unsloth DeepSeek V4 Flash 0731 GGUF was not added to ROCmplete's llama.cpp
catalog after target-hardware testing, and what to repeat when the relevant
backends change.

This document is evidence, not a current version or support declaration.
Always inspect `Containerfile`, `catalog/catalog.json`, source, and tests for
the current pins and behavior before using it. Hostnames, usernames, private
paths, and other machine-specific identifiers are intentionally omitted.

## Question and decision

The question was whether to keep the existing DwarfStar DeepSeek V4 Flash
path and also offer a conventional llama.cpp model family. The candidate was
the Unsloth Dynamic `UD-IQ3_XXS` quant of the official 0731 release.

The result was **hold, do not integrate yet**:

- the model fit a 112 GiB GTT allocation and produced correct output through
  Vulkan at 32K and 64K contexts;
- the same verified bytes produced deterministic corrupt output through
  ROCm/HIP under every tested, evidence-backed workaround;
- the default llama.cpp backend is ROCm, and a catalog preset cannot currently
  declare that one model is valid only with Vulkan; and
- the relevant Strix Halo Vulkan device-loss issue and performance fix were
  still open at the time of the test.

Adding the preset would therefore have made a corrupt default path look
supported. DwarfStar was not changed, replaced, or removed.

## Snapshot under test

| Component | Tested value |
| --- | --- |
| ROCmplete base | `1324069b7aff6bd18d3acee8e6a365c221c6d777` |
| Project ROCm | `7.14.0` |
| Original llama.cpp pin | `ddd4ec1428a6201e18975ea52b07c71e0f9aef26` |
| Candidate llama.cpp pin | `0ef6e55edb306fcbcf73e6f1f41923cccb9cf7f8` |
| Candidate pin purpose | DeepSeek V4 Flash 0731 chat template, [upstream PR 26398](https://github.com/ggml-org/llama.cpp/pull/26398) |
| Profile and architecture | Strix Halo, `gfx1151`, Radeon 8060S |
| Host memory | 128 GB installed (122.83 GiB usable), no swap |
| GPU memory policy | 112 GiB TTM/GTT |
| Host software | Ubuntu 26.04, kernel `7.0.0-28`, rootless Podman 5.7 |
| Runtime confinement | Normal ROCmplete read-only, capability-free application container |

The candidate llama.cpp pin built successfully with HIP and Vulkan. All four
ROCmplete llama.cpp patches applied cleanly:

- `hip-apu-host-buffer.patch`;
- `reasoning-effort-budget.patch`;
- `quantized-kv-flash-attention.patch`; and
- `vulkan-f16-kv-contiguize.patch`.

The build used the normal local build path with `--no-layer-cache`. This was a
candidate feasibility build, not the final release-sensitive `--no-cache`
acceptance that would be required before committing a pin update.

## Candidate model and immutable content

Source:
[`unsloth/DeepSeek-V4-Flash-0731-GGUF`](https://huggingface.co/unsloth/DeepSeek-V4-Flash-0731-GGUF/tree/fbbb5b93fb787c21338159b0af3318bb3f4d9768)
at revision `fbbb5b93fb787c21338159b0af3318bb3f4d9768`.

| Shard | Bytes | SHA-256 |
| --- | ---: | --- |
| `DeepSeek-V4-Flash-0731-UD-IQ3_XXS-00001-of-00004.gguf` | 5,257,696 | `dec1cee704800267d9d836d5a61aefc33705be939bbb3058fa9006d98191576d` |
| `DeepSeek-V4-Flash-0731-UD-IQ3_XXS-00002-of-00004.gguf` | 49,910,532,416 | `3064d3c4c1d6363e9f9ad88e90a3e2c5fb2d6f7ae16ca72135c3ce6a5c984da5` |
| `DeepSeek-V4-Flash-0731-UD-IQ3_XXS-00003-of-00004.gguf` | 49,257,859,456 | `2e9b2732eca7da8324f731653624a4f5c9846258926fd9f468cc703afb51a019` |
| `DeepSeek-V4-Flash-0731-UD-IQ3_XXS-00004-of-00004.gguf` | 5,034,198,464 | `4ca79d8e5107dd1b9bb57b176a7c09948837425dee49f0f1dfd6547a3769fea7` |

The total is 104,207,848,032 bytes, or 97.051 GiB. Installation went through
the normal staged content path. All four hashes were checked and a durable
verification receipt was recorded before runtime testing.

`UD-IQ3_XXS` was the highest practical candidate for this memory tier:

- Unsloth lists it as 104 GB decimal, matching the verified 97.051 GiB;
- `UD-IQ3_S` is about 116 GB decimal, roughly 108 GiB before context and
  compute buffers, which leaves too little of a 112 GiB GTT allocation; and
- the roughly 90.9 GB decimal IQ2 choices leave more headroom but make a
  larger quality concession.

The provisional catalog work used one four-artifact bundle, one family recipe,
Jinja chat templating, and a conservative 32,768-token default. It was
completely reverted after the hold decision.

## Capacity control before downloading

An existing 80.76 GiB DwarfStar GGUF was first mounted into the original
llama.cpp image as a zero-download capacity control. ROCm loaded it and
generated at about 15.7 tokens/s, proving that the architecture and memory
policy could load a model in this size class. Its text was malformed, as
expected for an incompatible application/model path, so this was never
treated as correctness acceptance.

The selected Unsloth model then loaded through both the original and candidate
llama.cpp images. The original pin produced malformed output at roughly 24.7
prompt tokens/s and 16.7 generated tokens/s. The candidate pin added the
needed chat-template support but still produced malformed output on ROCm.

This distinction matters: a successful load and plausible token rate were
not evidence of correct inference.

## ROCm/HIP correctness investigation

The initial candidate-image ROCm server loaded at a 4K context, but a
deterministic exact-phrase request returned repeated tokens instead of the
requested phrase.

A normal `2+2` request with high reasoning also returned corrupted mixed text.
The server stayed healthy, making this a silent correctness failure rather
than a crash.

The investigation then changed one relevant factor at a time. Every row used
the same verified GGUF and a fresh server:

| ROCm configuration | Prompt tokens/s | Generated tokens/s | Result |
| --- | ---: | ---: | --- |
| Candidate pin, normal fused ops and Flash Attention auto | about 25.7 | about 16.6 | corrupt |
| DeepSeek V4 HC pre/comb/post fused ops forced off | 19.66 | 9.76 | corrupt repeated fragments |
| Fused HC ops off and Flash Attention off | 20.02 | 9.58 | corrupt mixed-language repetition |
| Normal image plus `ROCBLAS_USE_HIPBLASLT=1` | 24.11 | 18.47 | corrupt repeated fragments |
| Normal image plus `HIP_LAUNCH_BLOCKING=1` | 7.35 | 11.77 | corrupt repeated fragments |
| Both ROCm environment workarounds | 8.28 | 13.11 | corrupt repeated fragments |

Disabling the fused DeepSeek V4 hyper-connection operations required a
disposable source patch because the tested llama.cpp revision exposed no CLI,
environment, or public context parameter for them. That experiment ruled out
the most obvious new kernels but was not suitable as a carried ROCmplete
patch.

`ROCBLAS_USE_HIPBLASLT=1` explains how a throughput-only report can show about
18 to 20 generated tokens/s while inference is still wrong. Always inspect a
known answer and longer output before accepting a backend result.

Upstream's gfx1151 CI work documented `HIP_LAUNCH_BLOCKING=1` as a workaround
for asynchronous HIP correctness failures on other models. It restored those
tests but did not repair DeepSeek V4 in this experiment:
[llama.cpp PR 26544](https://github.com/ggml-org/llama.cpp/pull/26544).

Turning off Flash Attention also failed, so the problem was not reduced to
ROCmplete's quantized-KV Flash Attention patch. F16 K and V caches were used
for the accepted comparison; quantized K is independently known to corrupt
DeepSeek V4 on affected llama.cpp revisions:
[llama.cpp issue 25382](https://github.com/ggml-org/llama.cpp/issues/25382).

### ROCm conclusion

ROCm/HIP was rejected for this candidate. The exact underlying backend defect
was not isolated, but the evidence was sufficient for the product decision:
the default backend returned silent nonsense under all supported or
upstream-documented mitigations tested.

## Vulkan correctness and memory results

The same candidate image and model produced correct output on Vulkan.

At 4K context:

- model load took about 36.6 seconds;
- `2+2` produced coherent reasoning and final content `4`;
- repeating the request used prompt cache and remained correct;
- the exact-phrase request returned the requested text;
- prompt processing was about 15 to 16 tokens/s; and
- generation was about 11.8 to 12.4 tokens/s.

Memory observations used the kernel's GTT accounting rather than relying only
on llama.cpp's fit estimate:

| Context | Load time | GTT used | GTT headroom | System memory available |
| ---: | ---: | ---: | ---: | ---: |
| 4K | 36.6 s | 97.664 GiB | 14.336 GiB | about 22 GiB |
| 32K | 36.4 s | 100.155 GiB | 11.845 GiB | about 19 GiB |
| 64K | 37.1 s | 103.085 GiB | 8.915 GiB | about 16 GiB |

Both 32K and 64K exact-answer requests were correct. The observed generation
rate was about 11.9 tokens/s at 32K and 11.8 tokens/s at 64K. The 32K default
was retained in the provisional preset because it leaves materially safer
headroom for the server and host than 64K.

### Multi-turn and tool protocol probes

Six sequential requests with a growing conversation history completed at a
64K allocation without device loss or output corruption. Prompt-cache reuse
progressed across the sequence. One request consumed its entire 128-token
ceiling in reasoning and therefore had no final answer; later requests were
again correct, distinguishing reasoning-budget exhaustion from backend
corruption.

A complete basic tool round trip also succeeded:

1. the model selected a `get_weather` tool;
2. it emitted valid JSON arguments for the requested sample city;
3. a tool result reported sunny conditions at 21 degrees Celsius; and
4. the model incorporated that result in a correct final answer.

This established basic llama.cpp tool framing. It did not justify setting
`agent_tools: true`: Pi and Maki still require their own complete
tool-call, tool-result, long-task, and failure-path acceptance.

## Vulkan benchmark results

The deep run used:

- Vulkan backend;
- f16 K and V caches;
- Flash Attention `auto`;
- prompt size 512;
- generation size 32;
- batch size 512;
- microbatch size 128;
- context depth 24,576; and
- one repetition.

It took about nine minutes because filling the context was slow, but completed
without OOM, device loss, or a kernel reset:

| Test | Prompt tokens/s | Generated tokens/s |
| --- | ---: | ---: |
| Depth 0 | 75.158964 | 11.997519 |
| Depth 24,576 | 34.854431 | 10.588865 |

At depth, prompt processing fell by about 54 percent and generation by about
12 percent. During the deep run, GTT use was about 97.55 GiB and system memory
available was about 22 GiB.

For a future retest with a local model path, the equivalent benchmark shape
is:

```bash
./rocmplete benchmark llama-cpp \
  --model /ABSOLUTE/PATH/TO/DeepSeek-V4-Flash-0731-UD-IQ3_XXS-00001-of-00004.gguf \
  --profile strix-halo \
  --backend vulkan \
  --repetitions 1 \
  --prompt-tokens 512 \
  --generation-tokens 32 \
  --context-depth 24576 \
  --batch-size 512 \
  --ubatch-size 128 \
  --cache-type-k f16 \
  --cache-type-v f16 \
  --flash-attn auto
```

Run the same command at depth 0 before comparing results. Do not compare a
different image, model revision, cache type, batch shape, or power policy as
if it were the same experiment.

## Vulkan stability caveat

No OOM or GPU reset appeared in kernel logs during the accepted probes. The
deep benchmark and six-turn sequence are useful evidence, but not a long soak
or production acceptance.

At the time of testing, upstream still had an open report of
`vk::DeviceLostError` after several turns with this exact model and hardware
class:
[llama.cpp issue 25664](https://github.com/ggml-org/llama.cpp/issues/25664).

The proposed tiled transpose for the DeepSeek V4 lightning-indexer fallback
was also still open. It reports substantially faster prefill on `gfx1151` and
addresses the slow dispatch associated with watchdog timeouts:
[llama.cpp PR 26585](https://github.com/ggml-org/llama.cpp/pull/26585).

The initial community observations that prompted this work are preserved for
context, but the conclusions above come from the independent tests in this
document:

- [DSpark with DeepSeek](https://www.reddit.com/r/StrixHalo/comments/1vhi0gk/dspark_with_deepseek/)
- [DeepSeek V4 Flash Vulkan and DeviceLost notes](https://www.reddit.com/r/StrixHalo/comments/1vj7fy6/deepseekv4flash_on_strix_halovulkan_notes_on_the/)

## Operational false alarm during testing

One detached server disappeared when its short SSH login ended. This initially
looked like a workload failure, but system and kernel logs showed no OOM or GPU
reset. The host did not have a lingering per-user service manager, so the
login teardown stopped the detached rootless container and left stale Podman
pause state.

The exact rootless Podman state was recovered with:

```bash
podman system migrate
```

No images or persistent content were removed. Later tests kept the controlling
session attached until the request, log inspection, and explicit application
stop had completed. Future remote acceptance should do the same or deliberately
configure a persistent user manager. Do not classify a login-lifetime failure
as a GPU reset without checking the container state and kernel log.

## Why a Vulkan-only catalog entry was not added

The catalog can express model artifacts, context, Jinja policy, tool support,
MTP, and per-profile Flash Attention policy. It cannot currently require or
default one preset to a particular llama.cpp backend. The normal CLI backend
default is ROCm.

Possible future designs are:

1. wait until the same preset is correct on ROCm and Vulkan;
2. add a deliberate, validated backend constraint/default to llama presets;
   or
3. define a clearly experimental backend-constrained content path.

Do not encode the backend into a hardware profile, recipe family name, or
top-level command. Backend, profile, application, mode, and model family must
remain orthogonal. A one-off recipe that merely appends `--backend vulkan`
would also leave direct preset and router behavior inconsistent.

## Retry triggers and acceptance checklist

Revisit this candidate when one of these changes materially:

- llama.cpp merges a DeepSeek V4 HIP correctness fix;
- the Vulkan device-loss issue is closed with an applicable fix;
- the tiled-transpose or Vulkan lightning-indexer work lands upstream;
- ROCm, Mesa/RADV, kernel, or firmware moves; or
- ROCmplete gains a coherent model-level backend policy.

For a retry:

1. Inspect the current project pins and the upstream issues above. Do not
   assume the candidate commit or patches are still appropriate.
2. Re-resolve the model repository revision, sizes, hashes, license, and chat
   template. Keep the exact bytes above only if they remain the intended
   candidate.
3. Confirm every downstream llama.cpp patch applies, then build without cache
   for final acceptance.
4. Install through the normal content verifier and retain its receipt.
5. Test ROCm and Vulkan independently with fresh servers. Require correct
   deterministic output before measuring performance.
6. Test at least 32K and 64K memory footprints, recording kernel GTT totals
   and host memory headroom.
7. Repeat the shallow and 24,576-depth benchmark with identical batch, cache,
   and Flash Attention settings.
8. Exercise multi-turn cache reuse, simultaneous slots, context compaction,
   a complete tool round trip, and a longer soak while watching kernel logs.
9. If advertising agent support, run complete Pi and Maki tasks;
   basic JSON tool syntax is not enough.
10. Record partial success honestly. Plausible token rates with malformed text
    are a failure.

Only add a managed recipe and preset after the supported command path chooses
a correct backend by default and the applicable target-hardware checks pass.

## State left after the study

All provisional source, catalog, recipe, test, and experimental fused-kernel
changes were reverted. Both the development and acceptance-host checkouts
were clean, and no commit was created for the rejected integration.

The verified model, ordinary candidate image, disposable no-fused-kernel
image, and benchmark JSON were retained in the acceptance host's normal
ROCmplete storage for a future retry. Their presence is not support evidence.
Persistent content or images should be removed only through an explicit,
scoped cleanup decision.
