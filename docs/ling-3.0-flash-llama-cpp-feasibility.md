# Ling 3.0 Flash llama.cpp feasibility snapshot

This is a dated maintainer research record from 2026-08-11. It explains why
Paracetamol did not add Ling 3.0 Flash after testing two AtomicChat GGUF
quantizations on a 128 GB Strix Halo host.

This document is evidence, not a current support declaration. Inspect the
current `Containerfile`, catalog, source, upstream issues, and tests before a
retest. Hostnames, usernames, private paths, and disposable result locations
are intentionally omitted.

## Decision

Do **not** integrate Ling 3.0 Flash yet.

The model and tested GGUFs were coherent through CPU and Vulkan. The same Q4
and Q5 artifacts produced corrupted mixed-language text, leaked template
tokens, and malformed tool arguments through the tested ROCm backend on
`gfx1151`. A patched Atomic TurboQuant fork fixed the APU allocation and page
fault failures, but not numerical correctness.

A Vulkan-only integration would require a second, incompatible llama.cpp
stack for one model. That complexity is not justified while upstream
BailingMoE3 support and the ROCm correctness path are still moving. Retain
the results and retest after those boundaries converge.

Laguna XS 2.1 was tested in the same investigation and worked correctly in
Paracetamol's ordinary pinned llama.cpp image. It became the practical model to
integrate instead.

## Snapshot under test

| Component | Tested value |
| --- | --- |
| Paracetamol source | `b1587ce` before the catalog integration |
| Project ROCm | `7.14.0` |
| Normal llama.cpp | `62bf73d25c53b8161f8a22894d4f90c4aebbd7d0` |
| Atomic TurboQuant fork | `cd560939087c95b93a1f30a95603d6b079436952` from release `b10269-1.5.1` |
| Profile and architecture | Strix Halo, `gfx1151`, Radeon 8060S |
| Host memory policy | 128 GB system RAM with a 112 GiB TTM/GTT ceiling |
| Host software | Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, rootless Podman |

The project image used Paracetamol's usual rootless confinement and
unified-memory policy. Tests used one selected render node and `/dev/kfd`.
The final accepted controls left no running container and no recent GPU fault
in the kernel log.

## Immutable model inputs

The standard-format source was
[`inclusionAI/Ling-3.0-flash`](https://huggingface.co/inclusionAI/Ling-3.0-flash).
It describes a 124B-total, 5.1B-active mixture-of-experts model trained for a
262144-token context, with coding, reasoning, and tool-use capabilities.

The GGUFs came from
[`AtomicChat/Ling-3.0-flash-GGUF`](https://huggingface.co/AtomicChat/Ling-3.0-flash-GGUF/tree/253738fe190c15f329001f263f355fc1562bbe7c)
at revision `253738fe190c15f329001f263f355fc1562bbe7c`. The repository declares
MIT and requires Atomic's TurboQuant llama.cpp release rather than ordinary
upstream llama.cpp.

| Variant and shard | Bytes | SHA-256 |
| --- | ---: | --- |
| Q4 `00001-of-00002` | 44,797,227,488 | `f53e96da745bccf8f283c9f37d7739b35a26ee64f1a7ebb30558e3b4eb71e818` |
| Q4 `00002-of-00002` | 29,426,325,152 | `46a2f65af5bc9b7aacd1f0890912a4a05d897ec3ea80fb08794302009777ea32` |
| Q5 `00001-of-00002` | 44,556,536,416 | `35ba32300c5f96e954ec233d59045f586a875a94eea8236945fe32dab4b66def` |
| Q5 `00002-of-00002` | 44,884,475,424 | `a22cc19499e8f8dac50fe183899047c49ef6031d7767671ad01800bfaf1325af` |

Every local file matched the published exact size and SHA-256. The Q5
comparison therefore did not reuse a damaged or partial Q4 download.

The Atomic release archives used for controls were also hashed locally:

| Backend archive | Bytes | SHA-256 |
| --- | ---: | --- |
| `llama-turboquant-linux-x64-rocm.tar.gz` | 68,977,097 | `ee47c7104c02b3d2b335aeda1ab76d4d01e6e3e5f5ac4dc779fd7b3feb93900e` |
| `llama-turboquant-linux-x64-vulkan.tar.gz` | 31,926,551 | `9f7fd2254a6cac37dce756700d41563f81a4557ea0cc10a35758ea91c02a18f8` |

## Support boundary

Paracetamol's pinned upstream llama.cpp rejects the GGUF cleanly with unknown
architecture `bailingmoe3`. The relevant upstream work was still open in
[`llama.cpp` PR 26608](https://github.com/ggml-org/llama.cpp/pull/26608) at
the time of the experiment.

Atomic's release contained BailingMoE3 and TurboQuant support, but its ROCm
binary did not contain Paracetamol's integrated-APU host-buffer correction. A
normal memory-mapped attempt emitted SVM mapping failures and could hang. A
direct-I/O attempt instead produced a `gfx1151` page fault in
`k_get_rows_float`. These were backend integration failures, not model-quality
results.

A disposable image was therefore built from exact Atomic commit
`cd560939087c95b93a1f30a95603d6b079436952` with two existing Paracetamol
patches:

- the integrated-APU host-buffer correction; and
- the reasoning-effort budget compatibility patch used by maintained agent
  clients.

Unrelated Paracetamol Vulkan patches were omitted. The patches applied and the
fork compiled cleanly. The resulting temporary image was approximately
5.59 GB. No source or patch from that experiment was added to Paracetamol.

That patched image eliminated the SVM mapping, hang, and page-fault failure
under the normal Strix policy. Flash Attention still rejected the model's
head size, so the viable test shape used Flash Attention off and F16 K/V
cache.

Atomic's fork also renumbers custom GGUF tensor types and moves the ordinary
`Q2_0` identifier. Adopting the entire fork would therefore risk making
existing upstream GGUFs mean something different. This is a stronger
compatibility objection than carrying one fail-closed source patch.

## Fixed benchmark shape

The comparable native measurements used:

- one `gfx1151` device with full GPU offload;
- unified memory and `--load-mode none`;
- prompt processing 512 and text generation 128;
- three repetitions at depth zero;
- batch 2048 and microbatch 512;
- F16 K and V caches; and
- Flash Attention off.

| Backend and target | pp512 tokens/s | tg128 tokens/s | Correctness |
| --- | ---: | ---: | --- |
| ROCm, Q4 | 377.981073 | 27.562078 | corrupt |
| Vulkan, Q5 | 338.536248 | 33.417694 | correct |

The ROCm numbers are recorded to prevent a future throughput-only comparison
from misclassifying the path as healthy. Performance is irrelevant when the
generated tokens are wrong.

Q5 ROCm decoded one-token and short API probes at about 24.47 and 25.85
tokens/s respectively, but exhibited the same corruption as Q4. Raising the
quantization quality did not repair the backend.

## Correctness controls

The same Q5 model produced a clean exact answer and valid structured function
call on CPU at about 16.31 API decode tokens/s. The released Vulkan build
produced a clean exact answer, coherent normal text, and a valid function call
at about 33.42 native decode tokens/s in the fixed benchmark.

ROCm Q4 and Q5 instead mixed Chinese and English inside words, emitted
malformed punctuation, leaked reasoning and tool template markers, and could
consume the complete token limit while constructing invalid JSON arguments.
One retained request began with a plausible weather-tool decision, then
corrupted the city argument with closing template tags and repeated end
tokens until the 512-token ceiling.

These controls establish three useful facts:

1. the downloaded weights and chat template can produce correct output;
2. the failure is not explained by Q4 quantization alone; and
3. the tested TurboQuant ROCm path is numerically wrong on `gfx1151` even
   after its memory-boundary failures are fixed.

A Laguna XS 2.1 Q4_K_M control in Paracetamol's normal llama.cpp image also
returned exact arithmetic, clean separated response content, a valid
`get_weather({"location":"Warsaw"})` call, and a correct tool-result follow-up
on ROCm. This makes a general host, Paracetamol confinement, or API-client fault
unlikely.

## Context and memory caveats

The Ling source model claims 262144-token training. The tested Atomic GGUF
metadata advertises only 131072 tokens. No metadata override or long-context
quality claim was accepted.

The Vulkan Q5 control occupied about 89.5 GB of GTT and left roughly 39 GB of
system memory available on the test host. It fits the high-memory machine but
is not a small operational choice. A future 256K claim must include actual
deep-prompt retrieval and output-quality tests, not only successful KV-cache
allocation.

## Retest criteria

Revisit Ling only when all of the following are practical:

1. BailingMoE3 support is merged into an upstream llama.cpp revision that can
   be evaluated without replacing established GGUF tensor meanings.
2. A supported ROCm implementation returns the same exact answers and valid
   structured tool calls as CPU or Vulkan on `gfx1151`.
3. Q4 and Q5 pass a longer coherent-generation probe, not only load and
   native throughput tests.
4. The supported context is unambiguous between the model release and GGUF
   metadata, followed by deep-context retrieval and memory acceptance.
5. The candidate completes at least one maintained agent-client tool loop and
   repository task without leaked template tokens, malformed arguments, GPU
   faults, or unbounded repetition.

If only Atomic's fork remains viable, first isolate its required source
changes and prove compatibility with Paracetamol's existing upstream GGUF
catalog. Do not adopt the fork wholesale or add a Vulkan-only application
merely because the model loads.
