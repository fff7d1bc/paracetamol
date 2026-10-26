# Flash-Next Gufo optimization screen

This is a dated research record from 2026-10-26, not a new runtime preset.
The goal is useful ROCm speed without changing model bytes, matrix precision,
F16 KV, sampling policy or output quality. Gufo is inspected as source, not
installed or run as another inference engine.

**Outcome: keep production unchanged.** Exact DPP transport passes numerical
qualification but does not establish a useful isolated speedup. Reader reuse
and lookahead have too little measured work to recover. Deeper greedy DFlash2
barely helps serial decode and hurts four-client throughput in its screen.
The existing depth-three preset and fourteen-patch image remain in place.

## Comparison boundary

The control uses llama.cpp
`c6fb3b5be2209f6c12339833198190f6f6829581` with the fourteen production
patches, tree `ebac725e678b76a5a97f7c605593296b8dde1b09`.
The image is `llama-cpp-ubuntu26.04-rocm10.1-c6fb3b5-r65`, ID
`e5b8d42153d37d6dc2f59c27fc688895ff1593e04890ec7452e1829d32ce6a34`.
The target is Strix Halo `gfx1151` with 128 GB unified memory.

The regular Unsloth Flash-Next UD-Q4_K_XL and Inco DFlash2 Q8_0 artifacts
stay fixed. Greedy depth three, four slots in one 262144-token pool, F16 KV,
the direct embedding reader and ordinary F32/quantized matrix arithmetic
remain the control. Experiments are independent, not an optimization stack.
No host power, kernel, swap or memory-policy changes are made.

Language requests pass through the ordinary gateway. The probes omit the
six managed sampler fields and verify their gateway provenance and native
configuration. Numerical and compiler diagnostics are separate from inference
acceptance. Separate performance-comparison arms contain no diagnostic reader
or boundary instrumentation. Unsetting the diagnostic flag does not restore
an uninstrumented control because phase clocks and extra allocations remain.

## Exact DPP lane transport

This candidate borrows only Gufo's lane-transport idea in the existing narrow
normalization and HC normalization/injection kernels. On `gfx1151` wave32 it
uses DPP row exchange and a cross-half-wave permutation instead of the generic
shuffle. F32 bits and the descending XOR addition tree remain unchanged.
All other architectures and wave64 retain the generic path. This is not a
generic shuffle replacement. Its reviewed call sites require every lane in
each participating wave to be active.

The candidate tree is `bf31ce453df239f160f6e8c8f974bdc3aef93d22`.
Its isolated runtime image ID is
`1fee6eeeb9e33fe2c4302f06c5a516be959c0a8d8cde4bebfbf5cd7fc53fee44`.
The source carries Gufo's full MIT notice and provenance to
[`XorLane` commit `7e4621c`](https://github.com/gufo-org/gufo/commit/7e4621c37079ab1bc62c1fd07ca03c37b85831cc).

The actual ROCm compiler replaces all twenty `ds_bpermute` instructions in
the normalization translation unit and all 160 in the HC translation unit.
HC injection VGPR use falls from 83 to 73, with no spills or added LDS.
These are real instruction/resource changes, not end-to-end speed evidence.

The GPU fixture passes 10240 exact per-lane exchanges and 2638080 every-lane
reduction comparisons, including special F32 values, reconverged column tails
and whole-wave row tails. All 112 targeted ROCm operator cases pass. Populated
state restore and three clear/restores pass with NMSE zero. All 248320 F32
output logits match the control byte-for-byte, with SHA-256
`dc8863bb4457868c6cfaa0f8f891ff82ed285a58baba452aad65bbca14dd9dde`.
That is a logit count from the state diagnostic, not a 248K context length.
The separate known compact-versus-graph-disabled discrepancy is not resolved.

Four ordinary-gateway screens run in control/candidate/candidate/control order.
Each passes cold/repeated six-key retrieval, a late history edit, simultaneous
four-client generation, separate label/follow-up isolation checks, cancellation
and recovery. The candidate and control differ only in `libggml-hip.so` among
the inspected server, model, CPU and base-library files.

| Run order | Cold prefill tokens/s | Late-edit prefill tokens/s | Serial HTTP tokens/s | Four-client aggregate tokens/s | Serial accepted/proposed | Accounted verification rounds |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Control A | 698.258 | 676.219 | 23.906 | 39.369 | 259/743 | 249 |
| DPP A | 700.072 | 683.080 | 25.390 | 40.811 | 268/720 | 240 |
| DPP B | 700.847 | 664.919 | 27.088 | 38.929 | 283/677 | 227 |
| Control B | 706.697 | 623.228 | 23.952 | 39.404 | 255/757 | 253 |

Cold prefill overlaps, the late-edit control varies substantially, and
four-client throughput moves in both directions. Serial output is faster in
both candidate runs, but those runs also accept more draft tokens and need
fewer accounted rounds. Native generated time per accounted round is about
81.8 to 83.2 ms across all four runs. That ratio is a diagnostic normalization,
not a physical kernel cost. The counter omits some early replay attempts.
It cannot establish equal work or identify a causal speedup.

The DPP candidate remains **held**. The instruction/resource changes and
numerical qualification are real, but this screen does not isolate a
repeatable worthwhile end-to-end gain that warrants another carried patch.
Other managed architectures have compilation and fallback checks only, not
GPU acceptance. No Vulkan performance claim or production dispatch change
follows from these results.

Serial rate divides 512 output tokens by the two warm HTTP request walls.
Four-client aggregate divides 1024 output tokens by shared group wall time,
never sums individual rates. Per-request parallel latencies span 23.26 to
26.30 seconds across these arms. The fixed 256-token timing outputs are
intentionally truncated and are not completed-task quality scores. The
parallel fixture uses short nonthinking histories, not four long xhigh tasks.
Seeds, sampler omissions, task bytes and polling policy remain fixed.
The timing requests are byte-identical across arms, but generated reasoning
and content differ. Each arm's two warm repeats follow its same trajectory,
not two independent completed coding tasks. The serial separation warrants
the bounded activation and fixed-work follow-up below, not automatic adoption.

### Bounded activation and fixed-work follow-up

A separate diagnostic image passes thirteen gateway requests and confirms
the narrow and HC launch sites on the actual target/draft pair. It records
128-wide gated RMS, 256-wide plain RMS, HC normalization at 2560 embedding
width and the gated HC injection fusion. Its image ID is
`3ca3bc71b551288f69f44b72576b6f45a19a79b4aeff80631fd55804aa02a82f`.
The bounded host records prove selection/submission, not graph replay counts
or kernel execution frequency. This traced image is never a timing arm.

The original sixteen fixed operator graphs pass their CPU-reference oracles
and ten synchronized measurements each on both uninstrumented libraries.
A second helper covers six shapes seen in the model trace. It runs in
control/candidate/candidate/control order, with three warmups and 100
measurements per shape per arm. All six oracles and all 600 measurements
pass in every arm. The exact same executable and deterministic inputs are
used with each library. The helper SHA-256 is
`00c6601cd7b6a5fc967032337249c7d10caa695302672540a157880bd8f8e4e0`.

| Observed shape | Control A median, us | DPP A median, us | DPP B median, us | Control B median, us |
| --- | ---: | ---: | ---: | ---: |
| RMS gated, width 128, 14016 rows | 228.932 | 224.684 | 229.373 | 224.123 |
| RMS plain, width 256, 7008 rows | 102.924 | 102.905 | 108.885 | 106.641 |
| HC norm gated, width 2560, 2 tokens | 29.595 | 28.724 | 29.015 | 29.215 |
| HC norm plain, width 2560, 1 token | 24.907 | 24.186 | 24.236 | 24.636 |
| HC norm plain, width 2560, 4 tokens | 40.206 | 40.046 | 39.936 | 38.884 |
| HC inject gated, width 2560, 292 tokens | 1824.001 | 1823.540 | 1832.527 | 1825.804 |

These are full original test graphs, including unfused reference and output
plumbing, not isolated kernel latency. Common work can dilute a fusion gain.
Two small HC shapes improve by about 1.8% and 2.3% when averaging arm medians.
The larger normalization and injection shapes are effectively unchanged or
slightly slower, and the four-token HC shape moves in both directions.
This does not isolate the cause of the serial timing separation.
No additional broad model matrix or production patch is justified now.
Revisit only with a cleaner matched-target workload or a materially changed
compiler/kernel shape, not an assumption that better ISA must be faster.

## Reader reuse and prompt-row prefetch

Both ideas look reasonable in source, but measured reader work is too small
to justify their added lifetime and scheduling machinery here.

A diagnostic-only image profiles pair sorting, worker creation/join,
read/dequantization, the enclosing PLE input boundary and the existing target
completion wait. It changes neither the read algorithm nor arithmetic.
The first 32K run exposes reader records but misses part of the native logger
output. A second diagnostic uses bounded numeric stderr records and completes
the 247210-token cold retrieval plus two genuine 4K append turns.
Every answer oracle and both suffix-only cache gates pass.

| Diagnostic request | All reader gather wall | Native prompt plus generation | Optimistic gather fraction |
| --- | ---: | ---: | ---: |
| Cold 32K retrieval | 142.688 ms | 55.247 s | 0.258% |
| 32K late history edit | 133.975 ms | 56.802 s | 0.236% |
| Cold 247K retrieval | 783.987 ms | 429.483 s | 0.183% |
| First new 4K append near 251K | 114.355 ms | 12.839 s | 0.891% |
| Second new 4K append near 256K | 79.262 ms | 11.446 s | 0.692% |

The numerator includes every attributed prompt and verification gather.
These are deliberately generous fractions assuming all gather work vanishes,
not measured speedups or evidence of exposed GPU idle. Profiling itself adds
overhead, and some CPU work can overlap already queued GPU work.
Buffered `pread` byte counts are not measurements of physical SSD traffic.
Worker CPU and wall sums overlap and cannot be added into an elapsed-time
decomposition. The several-second feature wait is an already-required target
completion fence, not evidence that embedding reads take several seconds.

Gather time is not the whole PLE input boundary. That enclosing boundary,
including row preparation, gather and upload, totals 3.027 seconds in the
initial deep request, about 0.705% of native model time. The two append
fractions are about 1.35% and 1.13%. Row preparation includes hashing,
staging and prefetch work, not just the reader's pair sort. These too are
diagnostic gross ceilings, not evidence that all such work is removable.

The actual embedding table is IQ4_NL with 160 values and 90 compressed bytes
per row. A full 2048-token batch references 32768 rows across sixteen heads
and stages 20 MiB of F32 data. Representative full-batch gathers take about
5 ms. The reader is configured for 64 threads on this system. Small gathers
use fewer.

A separate bounded pair-vector reuse candidate passes 648 CPU cases across
28 actual GGML formats, eight simultaneous callers, injected short reads,
EINTR, I/O failures and thread-construction failures. Its sanitizer checks
also pass for the reader and fixture. The matching GGML library is not
sanitizer-instrumented, and this is not a ThreadSanitizer result.
Removing this allocation changes local synthetic gather time by
roughly minus 1.4% to plus 1.3%, with no consistent win. That is not a
real-model speedup, so the candidate remains held.

Persistent worker pooling is not implemented. Only part of the already-small
gather duration could be removed, while safe construction, failure draining,
bounded queues and destruction would add lasting complexity.

Cross-call prompt-row lookahead is also held. Logical and physical batches
are both 2048, so a context-local next-ubatch hint usually has no next ubatch
to prepare. Useful lookahead would need a server hint across decode calls,
exact row verification and invalidation on context edits, restore, cancellation
and slot reuse. A bounded F32 buffer is feasible, but the measured ceiling
does not justify that implementation for this workload.

## Independent DFlash2 depth screen

Gufo's adaptive dense-model policy motivates a small width check, not a
transplant of its cost table. The isolated experiment changes greedy draft
depth from three to five for the exact regular Flash-Next pair only.
Its image `fb2c1e02c193f70e34a134ec29ebb3fc7c678a149a6594812e4159cfd762ca08`
derives directly from the immutable r65 control and replaces only the
entrypoint script. Native binaries, math libraries and model files are
unchanged. An ignored controller overlay renders five through the ordinary
router path. Shipped catalog validation and container policy remain intact.
The probe inspects the live native settings rather than trusting the requested
depth. This arm contains no DPP or reader optimization.

| Greedy draft depth | Serial HTTP tokens/s | Four-client aggregate tokens/s | Parallel latency range | Serial accepted/proposed |
| --- | ---: | ---: | ---: | ---: |
| Three, control A | 23.906 | 39.369 | 24.83 to 26.00 s | 259/743 |
| Three, control B | 23.952 | 39.404 | 25.09 to 25.98 s | 255/757 |
| Five, isolated screen | 24.268 | 34.234 | 27.31 to 29.91 s | 302/1032 |

Depth five passes the same 32K retrieval, edit, isolation, cancellation and
recovery checks. It produces the exact requested timing-token counts, but
offers only about 1.4% more serial throughput than the two controls' average
and about 13.1% less four-client throughput. It proposes more draft tokens
and needs fewer accounted verification rounds without a useful net win.
This is one bounded screen, not a universal depth ranking or near-256K
acceptance of depth five. There is no reason to expand its matrix or change
the current preset on this evidence. **Keep greedy depth three.**

## What Gufo's speed claims establish

The reviewed source is
[Gufo `1c62f0a`](https://github.com/gufo-org/gufo/tree/1c62f0a9f96aec33c8128bf652eff312bdf56f90).
There is no reproduced Gufo throughput claim in this screen.

The README's 71.55 tokens/s DFlash2 example is **dense 27B**, not Flash-Next.
Its retained artifact uses a 38-token repetition prompt, 128 generated tokens,
thinking off, temperature zero and 110/110 accepted draft tokens. That native
generation rate corresponds to about 59.86 output tokens/s over the recorded
client wall time. It is not an agent-workflow expectation.
See the [dense repetition artifact](https://github.com/gufo-org/gufo/blob/1c62f0a9f96aec33c8128bf652eff312bdf56f90/docs/models/qwen3.8-27b/artifacts/multi-repetition-q4-gufo-dflash2.json).

The [Flash-Next ROCm guide](https://github.com/gufo-org/gufo/blob/1c62f0a9f96aec33c8128bf652eff312bdf56f90/docs/models/qwen3.8-flash-next/ROCM.md)
has selected warm sampled-MTP code rows above 70 tokens/s. They are neither
greedy DFlash2 nor four-client aggregate throughput. The current loader
explicitly rejects DFlash2 for Flash-Next. Dense-model adaptive width curves
are therefore not settings to copy into our model's preset.

All four recorded Flash-Next shard hashes match the managed Unsloth target,
but the internal numerical contract differs. Gufo narrows some F32 model
tensors, uses F16 or W8A8 activation paths and F16 HC intermediates, and has
its own runtime and speculative-head policies. Those differences do not prove
bad outputs. They do prevent an unchanged-arithmetic speed comparison and are
outside this screen's extraction scope. Its documented quality checks are
also not a proof of parity with this llama.cpp build or the original model.
See the [Flash-Next quality record](https://github.com/gufo-org/gufo/blob/1c62f0a9f96aec33c8128bf652eff312bdf56f90/docs/models/qwen3.8-flash-next/QUALITY.md)
and [ROCm executor source](https://github.com/gufo-org/gufo/blob/1c62f0a9f96aec33c8128bf652eff312bdf56f90/src/models/qwen38_flash_next/kernels/rocm/executor.cpp).

Gufo still offers useful hypotheses, such as pricing draft width by full-cycle
cost and actual active clients, and using exact target verification for prompt
copies. Neither establishes a tuning gain for the managed Flash-Next setup.
Do not import its dense cost tables, reduced draft head or numerical shortcuts
on the strength of a headline token rate.

## Evidence and limits

Raw private evidence, exact source deltas, diagnostic fixtures, manifests,
per-request reports and cleanup records remain under the ignored
`build/gufo-validation-20261026/` tree. The gateway wrappers initially retain
their outputs under `build/performance-sweep-20261026/` before local copying.
No prompts, user sessions, host addresses or personal paths are published.

These are model- and workload-specific Strix Halo screens. They do not
establish broad coding quality, other-hardware acceptance or performance for
different GGUF releases. No host configuration or downloaded content changes.
