# Qwen3.8 Flash-Next direct embedding reads on Strix Halo

## Summary

This September 13, 2026 screen found a useful memory saving and a modest
high-context prefill improvement, not a general fix for ordinary ROCm
allocation. It retains the existing **Unsloth Dynamic Q4_K_XL** model, F16
KV cache and 262144-token context ceiling.

Direct embedding reads freed about 27 GiB of available host memory without
meaningfully improving prefill or decode by themselves. Using some of that
headroom for a 2048-token microbatch improved 32K prompt throughput by 41.5%.
At 247K, prefill fell from 34m57s to 30m48s, saving about 4m08s, with at least
27.68 GiB still available. Generation did not improve, and replacing the
tail of a cached long prompt made the first continuation slower.

The ordinary unified-memory allocation setting still produced corrupt prose,
wrong tool arguments and HTTP 500s. The reader does not fix that failure.
This is a useful experimental ROCm path, but not a completed managed
integration. Production remains on `8172e65-r36`, and Flash-Next remains
Vulkan-only in the managed inventory, without MTP.

## What changed

The candidate is [llama.cpp PR 28136](https://github.com/ggml-org/llama.cpp/pull/28136)
at commit `c6a9e5c9ae6d6a551217f75c9a04b2e8b1aa62dd`. It adds
`--lazy-mode on-direct`, which gathers the requested embedding rows on the
CPU using buffered `pread`, then supplies the resulting F32 rows to the
graph. It uses the existing dequantizers, rather than inventing a lower
precision format. This avoids accessing the large table through the graph's
ordinary lazy-mmap gather path.

The name does not mean Linux `O_DIRECT`. The implementation opens a buffered
descriptor and requests `POSIX_FADV_RANDOM`. Linux page cache can serve these
reads. We did not flush global caches, so the results are not a cold-SSD
benchmark or proof of performance with a small page cache.

The tested table has 320,001,536 rows of 90 bytes, about 26.8 GiB. Logs confirm
64 read workers and a CPU model-buffer reduction from 28,110.09 to 644.14 MiB.
The 27,465.95 MiB table remains mapped, but its rows are supplied by the
reader. A mapping's size is not the same as its resident physical footprint.
The ROCm model allocation stays at 78,056.46 MiB.

The patch also contains a Gemma4 path. This experiment exercises Flash-Next
only and does not establish Gemma4 acceptance.

## Matched conditions

The starting checkout was Paracetamol
`01492e62986bdab408b5c46195635b540f4be805`. Aion was the 128 GB Framework
Desktop with Ryzen AI Max+ 395, LPDDR5X-8000 and `gfx1151`, running Fedora 44
and kernel `7.1.7-200.fc44.x86_64`. The kernel remained unchanged, with roughly
34 days of uptime at the start. No reboot, GPU reset, power-policy or memory
limit change was made. Platform profile remained balanced and CPU EPP
remained `balance_performance`.

The control was the current `8172e65-r36` llama.cpp image, ID
`a034d9f0fe848d7d21e1a0facd00c46f70aebc09635714a21ae4c91cdb3f6196`.
The candidate was built from that exact builder layer and runtime, retaining
llama.cpp `8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f`, ROCm 10.0 and all five
existing Paracetamol patches. Its image ID was
`560547a290290fcd5b8ab36954e6277bea4a52768c99fe34af0eab3fe4979a24`.
CPU, HIP and Vulkan shared-library hashes were identical across the images.
Only the obsolete llama-bench help context needed rebasing in the candidate
patch. The reader implementation was not changed.

Both images used the existing verified four-shard
`qwen3.8-flash-next-125b-a6b-ud-q4-k-xl` artifact from Unsloth revision
`c8b5954a88c2775c546b92593eda40ea041d3176`, totaling 111,334,654,784 bytes.
This is the pinned **Unsloth Dynamic Quant**, not an arbitrary Q4 conversion.
No new model download or lower-precision KV cache was involved.

The main comparison retained these settings in both arms.

| Setting | Value |
| --- | --- |
| Context ceiling and slots | 262144 tokens, one slot |
| K/V precision | F16 for both |
| Flash Attention | On |
| Batch and microbatch | 2048 and 512 |
| Speculation | None |
| Allocation environment | `GGML_CUDA_ENABLE_UNIFIED_MEMORY` absent |
| Main model loading | `--load-mode none` |
| Embedding placement | `--override-tensor per_layer_token_embd.weight=CPU` |
| Host weight buffers | `--no-host` |
| Automatic fitting | `--fit off` |
| RAM prompt archive | `--cache-ram 0` |
| Template and default sampler policy | Existing managed Flash-Next policies |
| Timed requests | Medium reasoning, temperature zero, seed 42 |
| Safety floor | Stop below 4 GiB sampled host available RAM |

The control used `--lazy-mode off`. The candidate used `on-direct`. An extra
short candidate run with the reader disabled checked that simply carrying
the patch did not disturb the resident path.

The allocation settings repeat the coherent experimental path in the
[September 11 memory-policy screen](hardware-acceptance.md#flash-next-rocm-memory-policy-screen).
They are not the normal managed ROCm policy. A read-only test entrypoint
explicitly removed the unified-memory variable. Setting it to `0` would not
do that, since upstream checks whether the variable exists.

The normal gateway was stopped during measurement. Test servers retained
Paracetamol's rootless, read-only, capability-free confinement and published
only a private loopback port. No other inference or build ran alongside
the timed requests. The resident control ran before the direct-reader case
in the main paired comparisons. The microbatch follow-ups ran afterward.
Run order was not randomized.

## Workload and results

The input was a frozen public Paracetamol source archive from `01492e6`,
containing 143 Go and Markdown files. The assembled text was 1,268,201 bytes
with SHA-256
`fd8ef9463952604f8a497b0d99b66d2e50d63394cf39e8e48d00f855f49b2b6b`.
Six exact markers were distributed through tokenizer-sized slices of this
varied text. It was not padded by repeating one sentence.

Short cases used two uncached requests per input size and image.

| Actual input tokens | Resident prompt tokens/s | Direct-reader prompt tokens/s |
| --- | --- | --- |
| 3,667 | 295.10 | 294.40 |
| 32,167 | 260.35 | 261.22 |

The 32K throughput difference is 0.3%. Sampled available memory stayed
above 8.44 and 35.20 GiB respectively over the complete short suites.
This supports a useful memory improvement, not a meaningful prefill speedup.

Both long cases recovered all six keys from 247,165 input tokens, with no
prefix reuse for the initial request. Three fixed 256-token continuations
then reused the large prefix. These are single paired runs, not repeated
population estimates.

| Measure | Resident control | Direct reader |
| --- | --- | --- |
| Prompt processing | 2,096.668 s | 2,107.802 s |
| Prompt tokens/s | 117.88 | 117.26 |
| Retrieval HTTP time | 2,136.427 s | 2,149.638 s |
| Three continuations, total HTTP time | 161.514 s | 165.859 s |
| Continuation output tokens / total HTTP time | 4.76 tokens/s | 4.63 tokens/s |
| Aggregate backend-timed continuation decode | 5.02 tokens/s | 4.88 tokens/s |
| Minimum sampled available RAM, complete long suite | 8.04 GiB | 35.06 GiB |
| Observed startup to ready | 63.06 s | 34.78 s |

The retrieval answers matched exactly, but their reasoning differed and
used 199 versus 204 output tokens. The fixed continuations also differed.
Both complete long suites passed, including fresh requests, tools, prefix
reuse and cancellation afterward. Neither encountered a kernel warning.
The faster observed startup is not a controlled cold-loading benchmark.

### Using the headroom for a larger microbatch

With the direct reader retained, the follow-up raised the physical
microbatch from 512 to 2048, then to 4096. The logical batch remained 2048
for the first change and increased to 4096 for the latter. Context stayed
at 262144, with the same model, F16 KV, allocation policy and requests.

| Microbatch | 32K prompt tokens/s | Mean retrieval HTTP time | Minimum available RAM |
| --- | --- | --- | --- |
| 512 | 261.22 | 136.32 s | 35.20 GiB |
| 2048 | 369.57 | 100.24 s | 28.89 GiB |
| 4096 | 383.30 | 97.64 s | 20.21 GiB |

Both new settings passed their prose, nested-tool, streaming, cache and
cancellation checks, and both repeated retrievals returned all six keys.
The 2048 setting increased prompt throughput by 41.5% and reduced complete
retrieval time by 26.5% relative to the direct reader at 512. This is a
microbatch benefit enabled by the extra headroom, not a reader-only speedup.

4096 added just 3.7% more prompt throughput while allocating another
7224 MiB of GPU compute memory and 1596 MiB of host compute memory. Its
swap use briefly grew by 0.87 GiB, mostly released when the test container
stopped. There was no kernel warning or guard failure, but 2048 was the more
balanced choice for the final long-context test.

The 2048 follow-up passed the complete 247K sequence with at least 27.68 GiB
available and no new swap growth. Its retrieval, including reasoning, was
byte-identical to the original resident control. It processed the prompt in
1848.348 seconds at 133.72 tokens/s, and completed the HTTP request in
1888.682 seconds. Against the resident control, that saves 248.32 seconds
of prefill, an 11.8% time reduction or 13.4% throughput increase. Against
the direct reader at 512, it saves 259.45 seconds of prefill.

Generation did not get faster. The three fixed 256-token continuations took
180.015 seconds over HTTP, or 4.27 output tokens/s, compared with 161.514
seconds and 4.76 tokens/s in the resident control. Aggregate backend-timed
decode was 4.98 versus 5.02 tokens/s. The first continuation replaced the
tail of the original prompt. It reused 245,117 rather than 246,649 tokens,
requiring 2046 rather than 514 tokens of prompt work. That first request
took 77.539 rather than 58.377 seconds. The two subsequent repeats took
51.236 and 51.240 seconds. This is an observed prefix-rewind cost, not a
claim that every normal append-only conversation gets slower. The pinned
[server checkpoint logic](https://github.com/ggml-org/llama.cpp/blob/8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f/tools/server/server-context.cpp)
places end-of-prompt checkpoints relative to the physical microbatch size.

The short prefill gain therefore does not translate into a blanket 42%
speedup for coding agents. Fresh long prompts improve less, and edits or
retries that rewind a hybrid model's cached prefix have their own cost.

## Correctness and limits

The short control and direct-reader cases passed reasoning off/low/medium/
xhigh, nested tool calls and their replies, streaming, Responses, preserved
reasoning history, effective sampler defaults and explicit overrides, prefix
reuse, and cancellation followed by a fresh request. The patched resident
control also passed its memory and 4K retrieval checks.

A native reader fixture passed 50 reference and truncated-file checks across
F32, F16, Q8_0, IQ1_S and IQ4_NL. It covered unaligned file offsets, empty and
boundary-sized gathers, duplicate and out-of-order rows, and error handling
in serial and parallel reads. Returned F32 values matched the existing CPU
dequantizers bit for bit. The first fixture launch failed to resolve a
builder library. Setting the fixture's `LD_LIBRARY_PATH` fixed the launch,
without a code change. Dependency closure and `pip check` passed in both
runtime images.

These are execution and protocol checks, not a coding-quality score. Both
images' short Go explanation repeated the same false claim about Go lacking
preemptive multitasking. Long fixed-output continuations were deliberately
truncated at 256 tokens and must not be graded as finished implementations.
One long request per image cannot establish a stable performance percentage.

No scheduler, sparse-attention, MTP or custom ROCm-runtime patch was added.
No proprietary inference engine was downloaded or executed. Actual hardware
testing was limited to this `gfx1151` host.

## Integration boundary

The final allocation control kept the direct reader, 512-token microbatch
and every other diagnostic setting, but restored the presence of
`GGML_CUDA_ENABLE_UNIFIED_MEMORY`. It reproduced corrupt prose, failed
multi-turn recall, incorrect nested tool arguments and HTTP 500s. Eight
checks failed. A trivial streaming sentinel still passed, showing why a
healthy endpoint and one short answer are insufficient acceptance.
There was no kernel warning in that failure window. These observations are
consistent with the allocation-sensitive reports in
[issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797), without
proving a particular driver or coherency mechanism.

The successful experimental recipe does not by itself establish safe router
or coding-agent integration. Normal router children inherit their parent's
allocation environment. A future integration must make this policy explicit
for the selected model, preserve other presets' accepted policies, and test
real router/client traffic and the chosen RAM prompt-cache policy. It must
not globally unset the variable for every APU model.

The direct reader plus a 2048 microbatch is worth a focused integration pass.
The memory saving is substantial and the populated long-context test passed.
It is not a reason to import the entire fork, enable unfinished MTP, replace
the GGUF or declare future Qwen4 models accepted before testing them.

## Evidence and recovery

Raw commands, image identities, patches, probes, request/response JSON,
memory samples and logs are retained on both test hosts under
`~/.local/share/paracetamol/apps/acceptance/results/20260913-flash-next-direct-reader/`.
The archive's `evidence-manifest.md` maps the cases and preserves the failed
controls as well as the passing results.

The candidate passed CPU-only empty lazy-router startup and inventory
checks. Its first launch hit an occupied test port, so the retry used an
ephemeral loopback port without disturbing the existing service. An
auxiliary tiny Qwen 0.6B probe missed its exact-answer sentinel with a
coherent response. That check is recorded as a failure. The real default
Q8 MTP model then passed prose, multi-turn, nested-tool, streaming and
thinking-off recovery checks on the unchanged production image.

Tier 1 checks passed. There were no kernel warnings from the start of the
GPU matrix through final recovery, including during the deliberately
failing allocation control. This does not turn that corrupt-output case
into a passing result.

All experiment containers were removed. Only the temporary candidate image
tag was deleted, without pruning other images or build caches. The original
gateway was restored on loopback and its existing LAN address, with no
backend eagerly loaded. The kernel and host memory/power policies remained
unchanged.
