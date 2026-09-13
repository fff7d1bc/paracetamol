# Qwen3.8 tiled GDN prefill on Strix Halo

## Summary

This September 13, 2026 screen found a small useful prompt-processing gain
from one open-source HIP kernel. It did not reproduce the headline 1.2K
tokens/s result, and it did not make token generation faster.

With the existing Unsloth Dynamic Q8_K_XL Qwen3.8 27B and MTP, prompt
throughput increased about 10% at 3,897 input tokens and 8.6% at 33,047.
At 247,627 tokens, a single paired run saved about 74 seconds of prefill,
falling from 30m28s to 29m14s. Generation timing was essentially unchanged.
The paired long retrieval and fixed continuation outputs were identical,
including reasoning and speculative acceptance counts.
End-to-end coding sessions that spend most of their time generating tokens
will benefit less than these prompt-throughput percentages suggest.

Flash-Next gained about 5.2% and 3.6% at the same two short input lengths,
and 2.6% at 247,627 tokens. This was an experimental ROCm allocation policy, not
the managed Vulkan path. It does not justify enabling ordinary ROCm for
Flash-Next or changing model precision.

The cleaned kernel is included in the `8172e65-r36` image, restricted to
Strix Halo. No configuration change is needed. Model artifacts, F16 KV,
sampling, MTP depth and managed backend choices remain unchanged.

## What was isolated

The starting point was the community discussion of
[Flash-Next prefill on Strix Halo](https://www.reddit.com/r/LocalLLaMA/comments/1weobt6/qwen38_flash_next_now_at_12k_ts_prefill_on_strix/).
No proprietary engine was downloaded or executed. Source review identified
one independently separable change in
[Piotr Wilkin's MIT-licensed llama.cpp fork](https://github.com/pwilkin/llama.cpp/commit/964c6f2f0b66008243883298f3f535bc99f5998c).
The tested source commit was `964c6f2f0b66008243883298f3f535bc99f5998c`.

Its tiled gated-delta-net kernel stages 16 input tokens in shared memory
and computes several state columns per warp. It uses F32 arithmetic and
AMD lane reductions. This is not a new weight quantization, lower-precision
KV cache, sparse approximation, or speculative-decoding method. The previous
kernel already retains recurrent state in registers, so that alone is not
the distinction.

The matching shape also occurs in Qwen3.8 27B. Dispatch requires 48 value
heads, a 128-element state dimension, one sequence and between 16 and 32768
tokens in that operation. That last bound describes the microbatch, not the
conversation's context ceiling. Single-token decode remains on the previous
kernel. Recurrent snapshot handling needs regression coverage because MTP uses it.

The full fork and associated work involve other changes. The
[author's write-up](https://pwilkin.github.io/strix-halo/) describes a wider
optimization sequence. The independent
[RDNA port handover](https://github.com/stew675/llama-cpp-rdna-boosts/blob/main/wip/iq4nl-prefill/HANDOVER-2026-09-12-iq4nl-weight-gemm-port.md)
also distinguishes uniform IQ4_NL expert weights from mixed quantizations.
Those matrix paths are not a drop-in speed claim for our existing Q4_K_XL
artifact. They were not imported or tested in this screen. Neither were
custom HIP runtime or command-submission changes, host IOMMU changes, power
tuning, new model files, or unfinished matrix kernels.

## Matched conditions

The host was Aion, a Framework Desktop with Ryzen AI Max+ 395, 128 GB
LPDDR5X-8000 and `gfx1151`, running Fedora 44. Both images used the same
ROCm 10.0 runtime, `/dev/dri/renderD128`, balanced platform profile and
`balance_performance` CPU EPP. No other inference or build ran alongside
timing. The original gateway was stopped for the comparison.

The control was the current locally built image
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r35`, ID
`66cb660ee864929a5981ec6adf17c0ade7ddedc9610afe4734949ac5b96e3eb7`.
Both images used llama.cpp source
`8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f` and its four existing downstream
patches. The experimental image changed only `libggml-hip.so.0.23.0` and
diagnostic image labels. The server, server implementation, model library
and Vulkan backend remained byte-identical. The candidate image ID was
`54728609fc357a6290cc6dfd3534839784a6acfda679f63b9181e0935650bbf8`.

All server cases used a 262144-token ceiling, one slot, F16 K/V, Flash
Attention, batch 2048 and microbatch 512. RAM prompt archives were disabled
for these experiments. The requests explicitly controlled whether the live
prefix cache was reused. Model loading completed before request timing.

| Model | Exact managed variant | Loading and speculation |
| --- | --- | --- |
| Qwen3.8 27B | `qwen3.8-27b-mtp-ud-q8-k-xl` | Ordinary resident ROCm policy, unified-memory environment variable present, MTP depth 3 |
| Qwen3.8 Flash-Next 125B-A6B | `qwen3.8-flash-next-125b-a6b-ud-q4-k-xl` | Experimental resident ROCm policy, unified-memory environment variable absent, CPU ngram embedding, `--no-host`, no MTP |

Both are the existing verified **Unsloth Dynamic Quants**, not generic Q8
or Q4 conversions. Qwen27B came from revision
`4604b899a826000505a834e623272db5b7fd62f6`, artifact SHA-256
`af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377`.
The four Flash-Next shards came from revision
`c8b5954a88c2775c546b92593eda40ea041d3176`. Commands and verification-gated
artifact identities are retained with the evidence. No downloads or model
replacements were needed.

The Flash-Next allocation recipe repeats the coherent experimental case in
the [September 11 screen](hardware-acceptance.md#flash-next-rocm-memory-policy-screen).
It is not a fix for the corrupt ordinary ROCm configuration. Setting the
unified-memory variable to `0` would not make it absent. A test-only
entrypoint removed it explicitly. Runs sampled host available memory once
per second and would stop below 4 GiB.

## Prompt-processing results

Short cases used three sequential uncached requests per image and length,
with temperature zero, seed 42 and thinking off. All returned the requested
key exactly. Values below are mean backend-reported prompt tokens/s.

| Model | Actual input tokens | Control | Tiled GDN | Change |
| --- | --- | --- | --- | --- |
| Qwen27B Q8 MTP | 3,897 | 311.38 | 343.01 | +10.2% |
| Qwen27B Q8 MTP | 33,047 | 275.01 | 298.76 | +8.6% |
| Flash-Next Q4 | 3,897 | 358.27 | 376.75 | +5.2% |
| Flash-Next Q4 | 33,047 | 305.60 | 316.59 | +3.6% |

Qwen27B's 33K mean prefill fell from 120.168 to 110.614 seconds.
Flash-Next's fell from 108.137 to 104.383 seconds. Complete HTTP time fell
by essentially the same amount. These are modest isolated gains, not a
reproduction or disproof of the much more extensively modified fork.

### Populated near-256K Qwen27B Q8 MTP

Each image processed one 247,627-token synthetic archive from an uncached
prefix and returned six keys distributed across it. These requests used
medium reasoning, temperature zero and seed 42. The candidate ran before
the control, reversing the short screen's order.

| Measure | Control | Tiled GDN |
| --- | --- | --- |
| Prompt processing | 1827.850 s | 1754.104 s |
| Prompt tokens/s | 135.47 | 141.17 |
| Complete retrieval HTTP time | 1844.520 s | 1770.778 s |
| Three cached 256-token continuations, total HTTP time | 97.144 s | 96.989 s |
| Continuation output tokens / total HTTP time | 7.91 tokens/s | 7.92 tokens/s |

That is 4.2% higher prompt throughput or 4.0% less prefill time. Both images
returned all six keys correctly. Retrieval text, reasoning, and the three
continuation choices matched byte for byte. The continuation requests had
495 accepted tokens out of 795 drafted in both images. Their output windows
contained only reasoning and were deliberately truncated at 256 tokens.
They are not completed-code quality scores. Both fresh unrelated requests
afterward returned exactly `AFTER_LONG_OK`.

The short fixed-generation control also found no meaningful decode change.
Three uncached 256-token code continuations took 49.422 seconds over HTTP
before and 49.365 after. Their choices matched byte for byte, with 528 of
700 drafted tokens accepted in each image.

### Populated near-256K Flash-Next Q4

The same retrieval and continuation sequence passed on both images using
the experimental ROCm allocation recipe. Each initial request processed
247,627 prompt tokens with no cached prefix. The candidate again ran first.

| Measure | Control | Tiled GDN |
| --- | --- | --- |
| Prompt processing | 1908.096 s | 1860.482 s |
| Prompt tokens/s | 129.78 | 133.10 |
| Complete retrieval HTTP time | 1940.091 s | 1892.776 s |
| Three cached 256-token continuations, total HTTP time | 159.296 s | 160.705 s |
| Continuation output tokens / total HTTP time | 4.82 tokens/s | 4.78 tokens/s |

The patch saved 47.615 seconds of prefill, a 2.6% throughput increase or
2.5% less prefill time. Retrieval text and reasoning matched byte for byte,
with all six keys correct. The truncated code continuations differed in
their comments, despite identical reasoning. Their HTTP time was 0.9%
longer with the patch, not a generation-speed improvement or a code-quality
comparison. Both post-long fresh requests passed. Sampled host available
memory stayed above 8.27 GiB in both cases, and no new kernel warnings
appeared during the paired long-context matrix.

## Correctness and limitations

The kernel and all existing HIP code compiled for the four current build
targets. Actual inference and operator acceptance were limited to `gfx1151`.
Both the control and prototype passed 75 native GDN and cache-fusion cases
against the CPU reference. This included 34 added cases covering 15/16/17
and 31/32-token boundaries, larger tails, permuted layouts, grouped heads
and one or four recurrent snapshots.

Direct Q8 MTP checks passed for reasoning controls, effective sampling
defaults and overrides, nested tools, streaming, Responses, reasoning
history, prefix caching and cancellation followed by a fresh request.
Flash-Next's corresponding short protocol, retrieval and cancellation
checks passed too. A Go explanation contained the same factual mistake in
both images, which is a reminder that coherent output is not a factual
accuracy grade.

These synthetic tasks test execution, output continuity and performance.
They do not replace hidden-graded coding tasks, establish universal numeric
equivalence, or prove a result on other GPUs. Short timings have three
repetitions. The long comparison has one request per image, so its precise
percentage should not be treated as a stable guarantee.

## Production acceptance

The normal `build llama-cpp --no-config --no-layer-cache` path produced
`localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.0-8172e65-r36`, image ID
`a034d9f0fe848d7d21e1a0facd00c46f70aebc09635714a21ae4c91cdb3f6196`.
Installed Ubuntu package versions matched the control exactly. The compared
server, server implementation, model library and Vulkan backend remained
byte-identical. The final HIP library SHA-256 was
`b145125427767aca8214ddf1c8c13029e9f2c95b023481e1ff40cc6a4159b73b`.

The final image passed all 75 CPU-reference numerical cases, dependency
closure, `pip check`, CPU-only lazy-router startup and the tiny Qwen3 GPU
acceptance case. The test executable came from the already compiled matching
source and fixtures. It ran against the final image's GPU, CPU and ROCm
libraries, not the prototype builder's libraries. The accepted filter is
`GATED_DELTA_NET,GATED_DELTA_NET_CACHE_FUSION`. Earlier exact-only and invalid
regex-filter attempts are retained in the evidence, not counted as complete
coverage.

The cleaned image repeated the Q8 MTP protocol, sampler, history, cache and
cancellation checks. Its three-repeat means were 343.34 prompt tokens/s at
3,897 tokens and 298.73 at 33,047, matching the prototype. Three fixed
generation requests took 49.376 seconds over HTTP, with outputs and MTP
counts identical to the control. Both dense Qwen3.6 27B aliases passed the
same functional checks. The real router passed Q8 MTP, Q8 non-MTP and Q4 MTP
tool checks, plus simultaneous default Q8 requests. That last check tested
correctness, not parallel throughput.

The populated near-256K timings above belong to the prototype comparison.
The production adaptation changes only the dispatch gate, diagnostic logging,
include spelling and test fixtures, not the kernel arithmetic. Its acceptance
repeated numerical, short-performance and functional checks rather than
another complete long matrix.

Two x86 split-lock warnings named a Python process during the final hardware
diagnostic step. The checks passed, and no AMDGPU fault was recorded. These
warnings were outside the timed matrix, whose kernel journal remained clean.

## Maintenance boundary

The production adaptation restricts dispatch to exact `gfx1151`, removes
the fork's static debug counter, retains the existing source include, and
carries the numerical cases with the kernel patch. It introduces no user
flag or model-specific sampler behavior. Other architectures and unmatched
shapes keep the existing dispatch.

On a future llama.cpp or ROCm upgrade, apply the patch strictly, run the
native CPU-reference GDN tests on Strix Halo, and repeat real Q8 MTP and
populated-context checks. Remove it when the pinned upstream kernel offers
equivalent correctness and performance. Do not broaden the device gate on
the strength of a successful compilation.

Commands, source adaptations, image identities, response JSON, native case
logs and failed diagnostic attempts are retained on both acceptance and
development hosts below
`~/.local/share/paracetamol/apps/acceptance/results/20260913-tiled-gdn/`.
The archive excludes model files, helper binaries and build caches. Temporary
test containers and the three experiment image tags were removed without
pruning existing build caches. The production and rollback image tags remain.
The original gateway was restored, with its backend left unloaded.
