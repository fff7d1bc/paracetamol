# Qwen3.8 DFlash2 llama.cpp Strix Halo feasibility snapshot

This maintainer research record captures a 2026-08-23 isolated evaluation of
the then-open llama.cpp DFlash2 pull request with Qwen3.8 27B on the Fedora 44
Strix Halo host. It preserves the exact candidate, artifacts, measurements,
decision, and retest gate without making DFlash2 part of Paracetamol's source
pin or managed catalog.

This document is historical, machine-specific evidence rather than a current
support or performance declaration. Inspect `Containerfile`, the catalog,
source, and current hardware-acceptance records for active policy.

## Decision

Do not enable Qwen3.8 DFlash2 in Paracetamol yet. Keep the managed
`qwen3.8-27b-mtp-ud-q8-k-xl` ROCm path as the default.

The candidate established that DFlash2 can work on Strix Halo through Vulkan.
At 250,317 actual input tokens, its Q4_K_M draft reduced same-backend request
time by 4.0% and increased decode throughput by 26.7% relative to embedded MTP
on Vulkan. It reduced request time by 12.6% relative to the MTP/ROCm control in
the same candidate image. Most of that larger cross-backend difference came
from Vulkan's better prompt-processing rate at this extreme depth, not from
DFlash2 alone.

That result is not enough to ship:

- the DFlash2 ROCm path accepted only 1.1% to 3.7% of draft proposals in the
  shallow screen, whether the draft was Q4_K_M or Q8_0;
- the same Q4_K_M draft accepted 62.0% through Vulkan under the matched
  shallow condition, strongly suggesting a backend-specific candidate defect
  rather than an inherently unsuitable draft;
- Vulkan DFlash2 lost complete request time to MTP/ROCm at both 8K and 65K
  actual input tokens, so the practical win appeared only much deeper in the
  context; and
- every near-256K condition has only one measured repetition, exhausted its
  1,024-token output allowance while reasoning, and was not a coding-agent or
  tool-protocol quality evaluation.

An upstream merge by itself is not a promotion signal. Revisit the candidate
when DFlash2 has materially changed, especially in the ROCm path, then repeat
the gate at the end of this record.

## Snapshot under test

- Paracetamol baseline:
  `a78c4dac2c59241b6c424ec8fcb17a8cb7e8f289`
- isolated candidate fingerprint:
  `a78c4dac2c59241b6c424ec8fcb17a8cb7e8f289+dirty.43bace2f2a7dcb9bdf8d0f44d2cf19400227477dc0c547e84c7164701ea88c0a`
- llama.cpp DFlash2 candidate:
  [pull request 27342](https://github.com/ggml-org/llama.cpp/pull/27342) at
  commit
  [`1deefcca395743049c3820ab8f9b15043f3e9446`](https://github.com/ggml-org/llama.cpp/commit/1deefcca395743049c3820ab8f9b15043f3e9446)
- candidate image:
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-1deefcc-dflash2-probe`
- candidate image ID:
  `6a394b7edc8bbae0e2030f1f25ed924a3968fafbfbbec359f189c0648789ed58`
- ROCm: 7.14.0
- host: Fedora Linux 44, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000,
  Strix Halo `gfx1151`
- render node: `/dev/dri/renderD128`

The candidate changed the llama.cpp source pin only in an ignored disposable
checkout, added exact test-only catalog entries, and gave the image a distinct
tag. Paracetamol's four llama.cpp patches applied cleanly in production order:

```text
hip-apu-host-buffer.patch
reasoning-controls.patch
quantized-kv-flash-attention.patch
vulkan-f16-kv-contiguize.patch
```

The candidate passed `make check`, `make test`, and a no-layer-cache image
build before GPU testing. The MTP controls below used the same candidate image
as DFlash2, not the production llama.cpp image, so source revision was held
constant across the comparisons.

## Exact model artifacts

Every condition used the existing managed Qwen3.8 target:

| Role | Repository and revision | File | Bytes | SHA-256 |
| --- | --- | --- | ---: | --- |
| Target and embedded MTP | `unsloth/Qwen3.8-27B-GGUF` at `4604b899a826000505a834e623272db5b7fd62f6` | `Qwen3.8-27B-UD-Q8_K_XL.gguf` | 31,457,991,680 | `af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377` |
| DFlash2 Q4 draft | `incoai/Qwen3.8-27B-DFlash2-GGUF` at `6cb5872e2cee6b4e780a8414922350be8e42d65c` | `Qwen3.8-27B-DFlash2-Q4_K_M.gguf` | 1,143,006,752 | `18a380efc9b7ed8d88677fc895f5c11ae170653434ee378f7348f715c14d0594` |
| DFlash2 Q8 diagnostic draft | `incoai/Qwen3.8-27B-DFlash2-GGUF` at `6cb5872e2cee6b4e780a8414922350be8e42d65c` | `Qwen3.8-27B-DFlash2-Q8_0.gguf` | 2,056,414,752 | `7f1c9a31a6ed40044c69f6508b50fd63b87abd8e1fb7fe4290303df549153751` |

Every target was Dynamic Q8_K_XL. The Q4 and Q8 labels on DFlash2 rows describe
only the separate draft quantization. The target and speculative K/V caches
retained llama.cpp's F16 defaults; this was not a quantized-KV experiment.
Q8_0 DFlash2 was used only to determine whether the ROCm failure was peculiar
to the smaller draft. The complete Vulkan screen and deep comparison used
Q4_K_M.

## Workload and measurement rules

The server-side speculative benchmark used one slot, a fresh server per
condition, native Qwen3.8 medium reasoning, the managed Qwen3.8 chat template
and sampling policy, seed 42, batch 2048, physical batch 512, and a 262,144-
token server allocation. MTP used its accepted depth three. DFlash2 screened
depths four and five before the deep run retained depth four.

The benchmark's synthetic Go repository packet expands to roughly twice its
nominal `--context-depth`. The exact measured input sizes were:

| Nominal depth | Actual prompt tokens | Generated tokens |
| ---: | ---: | ---: |
| 4,096 | 8,172 | 256 |
| 32,768 | 65,115 | 256 |
| 126,000 | 250,317 | 1,024 |

The final shape deliberately left room for the generated response and
template framing inside the 262,144-token server context. `Request` below is
the measured HTTP request and excludes model startup. Prompt and generation
rates come from llama.cpp's server timings; acceptance is accepted draft
tokens divided by proposed draft tokens. Every cell is one repetition, so
small differences are directional rather than stable estimates.

## ROCm compatibility screen

At 8,172 actual prompt tokens, embedded MTP behaved normally while both
DFlash2 quantizations produced almost no useful proposals:

| Strategy | Draft depth | Request | Prompt | Generate | Accepted |
| --- | ---: | ---: | ---: | ---: | ---: |
| MTP/ROCm | 3 | 46.78 s | 298.35 t/s | 13.17 t/s | 158/290 (54.48%) |
| DFlash2 Q4/ROCm | 4 | 73.35 s | 294.90 t/s | 5.59 t/s | 11/967 (1.14%) |
| DFlash2 Q4/ROCm | 5 | 69.74 s | 296.01 t/s | 6.05 t/s | 36/1,080 (3.33%) |
| DFlash2 Q8/ROCm | 4 | 72.88 s | 292.90 t/s | 5.67 t/s | 13/963 (1.35%) |
| DFlash2 Q8/ROCm | 5 | 68.57 s | 295.71 t/s | 6.23 t/s | 39/1,065 (3.66%) |

The nearly identical Q4 and Q8 failure shape makes over-quantization an
unlikely explanation. No deep ROCm DFlash2 run was justified after this
screen. A future retest should treat similarly collapsed acceptance as a
failed backend path even if generation still returns syntactically valid
text.

## Vulkan screens

The same Q4_K_M draft behaved normally through Vulkan. Depth four beat depth
five in the shallow screen and was retained:

| Actual prompt | Strategy | Depth | Request | Prompt | Generate | Accepted |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 8,172 | MTP/Vulkan | 3 | 52.44 s | 226.04 t/s | 15.67 t/s | 173/243 (71.19%) |
| 8,172 | DFlash2/Vulkan | 4 | 50.91 s | 220.89 t/s | 18.34 t/s | 181/292 (61.99%) |
| 8,172 | DFlash2/Vulkan | 5 | 52.12 s | 219.01 t/s | 17.23 t/s | 179/373 (47.99%) |
| 65,115 | MTP/Vulkan | 3 | 385.90 s | 177.67 t/s | 13.17 t/s | 172/247 (69.64%) |
| 65,115 | DFlash2/Vulkan | 4 | 382.54 s | 178.63 t/s | 14.19 t/s | 174/321 (54.21%) |

Against MTP on the same backend, DFlash2 reduced request time by 2.9% at 8K
and 0.9% at 65K while increasing decode throughput by 17.1% and 7.8%.
However, the MTP/ROCm requests completed in 46.78 and 361.44 seconds at those
depths. DFlash2/Vulkan was therefore 8.8% and 5.8% slower than that practical
cross-backend control despite its faster generation.

## Near-256K result

At 250,317 actual prompt tokens and 1,024 generated tokens, Vulkan's prompt
scaling changed the complete-request outcome:

| Strategy | Backend | Depth | Request | Startup | Prompt | Generate | Accepted |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| DFlash2 Q4 | Vulkan | 4 | 2,592.05 s (43m12s) | 11.18 s | 100.89 t/s | 9.23 t/s | 699/1,295 (53.98%) |
| MTP | Vulkan | 3 | 2,701.14 s (45m01s) | 10.20 s | 97.76 t/s | 7.29 t/s | 618/1,211 (51.03%) |
| MTP | ROCm | 3 | 2,966.10 s (49m26s) | 9.20 s | 87.87 t/s | 8.72 t/s | 666/1,071 (62.18%) |

DFlash2/Vulkan versus MTP/Vulkan is the attributable speculative-decoder
comparison: request time fell 4.04%, prompt throughput rose 3.21%, and decode
throughput rose 26.67%. DFlash2/Vulkan versus MTP/ROCm is the practical
candidate comparison: request time fell 12.61%, prompt throughput rose 14.81%,
and decode throughput rose 5.86%. MTP/Vulkan alone accounted for about 265 of
the 374 seconds saved relative to MTP/ROCm, which is why the larger number
must not be presented as a DFlash2-only gain.

All three completions were coherent on inspection but reached the output
length limit while still reasoning, and their response hashes differed. That
is sufficient for a throughput probe, not for a quality-equivalence claim.
The near-256K test interval produced no matching AMDGPU fault, SVM mapping
failure, GPU reset, process trap, general-protection fault, or OOM kernel
event. Every benchmark container was removed after its run.

## Promotion and retest gate

Reopen this investigation only after a newer llama.cpp DFlash2 implementation
contains a plausible change to the failing path or materially changes the
decoder. Then:

1. Use a new isolated source pin and image tag. Reclassify and apply every
   Paracetamol llama.cpp patch before building without layer cache.
2. Re-resolve the target and draft artifacts to full revisions, byte sizes,
   SHA-256 hashes, and licenses. Do not silently reuse the historical draft
   against a changed model family or architecture.
3. Repeat ROCm and Vulkan at the same actual shallow, 65K, and near-250K
   depths with MTP controls in the same image. Keep reasoning, sampling,
   context, cache types, batches, seeds, and prompt generation identical.
4. Repeat the winning draft depth with at least three seeds. Re-screen nearby
   depths if upstream changes proposal length or decoder policy. Record whole
   request time and output validity in addition to decode rate and acceptance.
5. Complete direct and gateway Chat Completions, Responses, reasoning-off and
   medium controls, a nested tool round trip, interruption cleanup, and a
   matched Pi coding-agent evaluation before catalog promotion.
6. Inspect kernel logs and peak memory. Repeat the relevant spot check on
   every practical hardware profile because a `gfx1151` Vulkan result does not
   accept ROCm or another architecture.

Do not add a managed preset while the default ROCm selection can silently
enter the failed DFlash2 path. If DFlash2 remains Vulkan-only, first design an
explicit fail-closed profile/backend compatibility rule while keeping backend,
model preset, and profile as separate dimensions. A normal catalog integration
requires either a working ROCm path or that deliberate runtime boundary. A
default change additionally requires repeated complete-request and quality
evidence; a faster decode phase or merged pull request is not enough.

## Retained local evidence and discovery sources

The raw benchmark JSON remains outside version control in the ignored
`build/dflash2-probe/build/benchmarks/` directory. The three deep records are:

```text
dflash-q4-vulkan-near-256k.json
mtp-vulkan-near-256k.json
mtp-rocm-near-256k.json
```

The same directory contains the shallow ROCm Q4/Q8 and Vulkan 8K/65K screens.
The disposable checkout and raw files are not a source-tree dependency and
may be removed by `make clean`; this record preserves the decision and the
minimum reproducible identities.

The investigation began with a
[community benchmark report](https://www.reddit.com/r/LocalLLaMA/comments/1vvncyh/i_benchmark_dflash_2_pr_build_in_llamacpp_on_qwen/).
That report used an older pull-request commit and CUDA results, so it served
only as a lead. The measurements above use the exact current-at-test llama.cpp
commit and the pinned
[DFlash2 GGUF release](https://huggingface.co/incoai/Qwen3.8-27B-DFlash2-GGUF/tree/6cb5872e2cee6b4e780a8414922350be8e42d65c).
