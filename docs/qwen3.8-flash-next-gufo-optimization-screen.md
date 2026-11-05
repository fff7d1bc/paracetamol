# Flash-Next Gufo optimization screen

This is a dated research record from 2026-10-26, not a new runtime preset.
The goal is useful ROCm speed without changing model bytes, matrix precision,
F16 KV, sampling policy or output quality. Gufo is inspected as source, not
installed or run as another inference engine.

That scope belongs to the original extraction work. The separate
[2026-11-05 engine recheck](#2026-11-05-source-built-engine-recheck) below
actually builds and runs newer Gufo. It also leaves production unchanged.
The [developer recheck kit](#developer-only-recheck-kit) preserves the build
recipe and regression checks without adding Gufo as an application.

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
No private prompts, user sessions, host addresses or personal paths are published.

These are model- and workload-specific Strix Halo screens. They do not
establish broad coding quality, other-hardware acceptance or performance for
different GGUF releases. No host configuration or downloaded content changes.

## 2026-11-05 source-built engine recheck

**Keep Gufo out of the managed applications for now.** The speed advantage is
real, including at long context. The tested MTP path also fails a harmless
exact-output task that the llama.cpp and Gufo non-MTP controls pass. That
failure reproduces after a fresh process start. This does not meet the
requirement for confidence without known task regressions.

This is not a finding that every Gufo mode is broken or that MTP alone causes
the failures. Non-MTP Gufo passes its nine-case 32K screen. Neither that smoke
nor a fast coding result establishes broad quality parity.

### Matched conditions

The control is the production llama.cpp source pin
`740251a920abf92b7744d9c0518ca157db0db579`, image
`llama-cpp-ubuntu26.04-rocm10.1-740251a-r71`, ID
`9ea0d1277fb74bad75f5c22b093209dcf715abd24d759c090934ac760e7344d7`.
It uses greedy DFlash2 at depth three and four slots sharing one 262144-token
pool. This recheck measures one active client, not four-client throughput.

Gufo is built from
[`2c97b72`](https://github.com/gufo-org/gufo/tree/2c97b72d8671ddd0c74ae7d517b8cef4ebb78871),
after v0.30.0, against the same project ROCm 10.1 SDK and minimal runtime.
Its image ID is
`6924fb309226a0d989d57134ec5f77eae53d198cedeba7b2c70cc70da1506ce0`.
No prebuilt Gufo image is used. The ROCr runtime is the unmodified project
wheel, not Gufo's Nix queue-memory-patched runtime. Gufo uses one 262144-token
session, an 8 GiB RAM checkpoint budget and no persistent disk cache.
Separate arms use no speculation or its shared-Q8_0 MTP sidecar with adaptive
draft length one through seven. Gufo does not support Flash-Next DFlash2.
The experimental sidecar is
`MTP/mtp-Qwen3.8-Flash-Next-shared-Q8_0.gguf` from Unsloth revision
`38bb39ee97821de2c9009abb7e93950eec396e66`, size 2786568256 bytes and SHA-256
`5ff54097406a905cf3a724c709124ceb0e3e10235ee862298969e91c96fa96e6`.

Both engines receive the same managed Unsloth UD-Q4_K_XL target shards and
BF16 vision sidecar, with F16 KV. Only text is tested. Hardware is 128 GB
Strix Halo `gfx1151` on Fedora 45 with kernel 7.2.15. Host settings, power
profile, swap policy and model bytes remain unchanged. Builds and inference
do not overlap. The containers remain rootless, read-only, capability-free,
restricted to the selected GPU devices and published only on loopback.

Every generation passes through the production gateway or an isolated
transport using the actual gateway sampler, proxy and reporting code. The
probe explicitly requests `xhigh` and seed 42, omits the six managed sampler
fields, and verifies their resolved values and gateway provenance. Gufo's
native default `min_p=0` therefore does not replace the managed `min_p=0.05`.
Tool schemas stay present and identical across the entire coding exchange.
Assistant reasoning and tool results are replayed. The task goals and inputs
match, but compiled Gufo rendering and managed Jinja rendering are not claimed
to produce identical token streams.

### Measured results

The context labels describe frozen archive lengths. Request framing adds a
few hundred tokens. The near-256K cold request has 247241 input tokens and
its appended turn reaches about 254K, safely inside the 262144 ceiling.

| Measurement | llama.cpp with DFlash2 | Gufo with MTP |
| --- | ---: | ---: |
| 32K cold native prefill, tokens/s | 716 | 1678 |
| 128K cold native prefill, tokens/s | 601 | 1583 |
| Near-256K cold native prefill, tokens/s | 572 | 1502 |
| 32K warm 256-token HTTP output rate, tokens/s | 30.25 | 49.96 |
| 128K cold retrieval request wall, seconds | 229.16 | 88.72 |
| Near-256K cold retrieval request wall, seconds | 444.31 | 170.36 |
| Three-task coding suite wall, seconds | 518.88 | 271.41 |
| Coding tasks completed and graded successfully | 2/3 | 2/3 |

Non-MTP Gufo separately reaches 1753 native prefill tokens/s and 29.08 warm
HTTP output tokens/s at 32K. Its faster cold path does not by itself provide
the large decode gain. Some Gufo MTP retrieval responses approach 78 native
decode tokens/s at 128K. That is not a general workflow rate or the matched
fixed-length result in the table.

All engines recover the six distributed values in their tested cold,
repeated and appended histories. Gufo MTP passes those checks at 32K, 128K
and near 256K. At near 256K its repeated request takes 6.36 seconds and its
6144-token history extension takes 10.81 seconds, versus 11.55 and 21.38
seconds for llama.cpp. Cache counters are inspected rather than assuming a
sequential turn is warm. Gufo also restores intermediate checkpoints for the
32K late-edit case, so its gain there includes avoiding prefill work, not
just faster arithmetic.

The coding tasks are small synthetic Go functions covering wrapped EINTR
retry, exact JSON numeric literal extraction and interval merging. The same
hidden tests run in network- and filesystem-isolated graders. Both engines
complete retry and merging in one submission. Both exhaust the same
8192-token budget on numeric extraction without submitting code. The suite
wall therefore includes an incomplete task, not three completed jobs.
Grading time is recorded separately. Fixed 256-token outputs are deliberately
truncated throughput fixtures, not completed-task quality scores.

### Failures that block qualification

The tool-marker checks were prompted by the linked
[quality discussion](https://www.reddit.com/r/StrixHalo/comments/1wwz2io/qwen_38_flash_next_does_gufo_make_more_errors/).
The historical tool-parser fixes are already included in the tested pin.
Their presence does not substitute for acceptance of this version.

- With a tool available, a request to echo a harmless literal XML string
  succeeds on llama.cpp and non-MTP Gufo. Gufo MTP instead ends after 30
  reasoning tokens with an empty visible answer. Its reasoning calls the
  requested literal a prompt-injection attack. The exact failure repeats
  after a fresh MTP process start.
- A request to print a literal marker and call `read` exactly once exhausts
  4096 tokens with repeated calls on Gufo MTP in both buffered and streaming
  responses. A fresh diagnostic replay produces 76 parsed calls. This is
  **not a clean Gufo-only regression**. The 32K llama.cpp arm also misses the
  same oracle, returning three calls and no requested literal. Non-MTP Gufo
  passes it. All initial failures remain in the evidence.

Diagnostic replays retain Gufo's pre-parser generated text as well as API
responses and the effective sampling values. The XML response contains only
the refusal-like reasoning, not a correct answer removed by the gateway.
The repeated native tool-call blocks also occur in generated text. They are
not invented by the gateway. Reasoning-token counters alone cannot locate
the parser boundary because Gufo also accepts a canonical tool header as an
implicit end to reasoning. Traced runs are excluded from timing comparisons.

This establishes candidate task failures, not their numerical or sampler
root cause. It does not establish an error-rate ranking from one seed or
prove MTP causality. Successful coding and long-context retrieval do not
erase these separate misses under the confidence requirement.
The engines have their own sampling and automatic tool-grammar
implementations. Matching request parameters does not make those
implementations identical.

### Numerical and capacity limits

Same GGUF files and F16 KV do not make the numerical implementations
equivalent. Current Gufo still narrows some F32 tensors and uses F16 HC and
quantized activation paths, with upstream fast-math compilation. See its
[device-model loader](https://github.com/gufo-org/gufo/blob/2c97b72d8671ddd0c74ae7d517b8cef4ebb78871/src/models/qwen38_flash_next/kernels/rocm/device_model.cpp).
Those choices do not prove worse answers. They do rule out describing this
as unchanged-arithmetic speed for the existing llama.cpp model path.

Gufo's native phase rates exclude separate cache restoration, checkpoint
capture, queueing and HTTP work. Client wall time and task completion remain
the practical comparison. Output lengths and seeded trajectories can differ
between engines and speculation modes.

Gufo also allocates context per session rather than borrowing from one
shared token pool. Four 262144-token MTP sessions add about 20.59 GiB of core
state over one, before other per-session allowances. This is derived from
[`SessionBytes`](https://github.com/gufo-org/gufo/blob/2c97b72d8671ddd0c74ae7d517b8cef4ebb78871/src/models/qwen38_flash_next/kernels/rocm/executor.cpp#L1624),
not a measured four-session footprint or an OOM result. A four-by-64K setup
would not preserve the single-session 256K capability. The concurrency
matrix is not expanded after the quality gate fails.

### Evidence and restoration

Private fixtures, grader logs, source-build recipe, request reports, raw
diagnostic traces and memory records are retained under the ignored
`build/gufo-recheck-20261105/` tree. The first coding run is retained only as
calibration because it removed tool definitions on the final turn. The
reported version-two runs fix that prompt/cache confound before comparing
engines and distinguish passing code from successful final completion.

The memory guard never intervenes. No new GPU fault or OOM appears in the
inspected kernel journal. Experimental containers, guards and proxy
processes are stopped and removed where applicable. The test host returns
to its original idle state with no host reconfiguration. The experimental
image and verified MTP sidecar remain available for a later investigation.
No managed application, default, content pin or client configuration changes.

## Developer-only recheck kit

`tools/gufo-recheck/` is an explicitly opted-in experiment, not dormant Gufo
application support. It is absent from normal commands, application discovery,
`build all`, image exports, model lists and Maki configuration. There is no
configuration switch that promotes it into a managed backend.

The kit preserves only the pinned source-build fragment, the thin external
gateway transport and three frozen literal/tool regressions. It does not
launch or stop containers, download models, change host settings, or recreate
the full coding, long-context and concurrency benchmark matrix. Ordinary unit
checks use fake HTTP responses and never invoke inference.

### Prepare a source build

On the Linux development checkout:

```bash
make gufo-recheck
CHECKER=./build/linux-amd64/bin/gufo-recheck
"$CHECKER" prepare --out build/gufo-recheck-source-CANDIDATE
./paracetamol build runtime
podman build --pull=never --layers --target gufo-recheck \
  -f build/gufo-recheck-source-CANDIDATE/Containerfile \
  -t localhost/paracetamol-experiment:gufo-CANDIDATE .
```

Replace `CANDIDATE` with a unique run label. `make gufo-recheck` builds only
the developer executable. `prepare` performs no build or network operation.
It appends the narrow embedded fragment to the **current** production
Containerfile, inheriting its native ROCm SDK and minimal runtime. It refuses
changed stage contracts or conflicting pins rather than maintaining a second
copy of production. The generated recipe and hashed `prepare.json` are
create-once, private files below the ignored `build/` directory.

The default Gufo source is the tested `2c97b72` pin, not a moving upstream
branch. For a new candidate, deliberately update the full commit in
`prepare.go` and both matching `GUFO_COMMIT` declarations in the fragment,
review its license and numerical/API changes, rebuild the helper, then
prepare a new directory. Do not override build arguments without recording
the actual resulting image and source identity. Distro package names in the
optional fragment also need review after Ubuntu changes.

The fragment builds only `gfx1151`, includes the upstream MIT license, and
checks binary loading, help and `pip check` without GPU devices. These checks
do not qualify GPU inference. Keep builds separate from inference timings.

### Launch the backend explicitly

Start the source-built image manually on a free acceptance host. Use the
normal rootless, read-only, capability-free confinement with
`no-new-privileges`, writable `/tmp` tmpfs, private networking, and only
`/dev/kfd` plus the exact selected render node. Publish only
`127.0.0.1:18556:8080`. Mount each verified target shard and the existing BF16
projector individually and read-only, not their parent directory.

The baseline Gufo arguments used by this screen are:

```text
serve --host 0.0.0.0 --port 8080 --sessions 1 llm
  --model /content/unsloth-Qwen3.8-Flash-Next-UD-Q4_K_XL-00001-of-00004.gguf
  --mmproj /content/unsloth-Qwen3.8-Flash-Next-mmproj-BF16.gguf
  --served-model-name unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl
  --context 262144 --think on --reasoning-effort xhigh
  --preserve-thinking on --cache-ram-bytes 8589934592
  --speculative off
```

For the separate MTP arm, mount the verified sidecar pinned above and replace
`--speculative off` with `--speculative mtp --mtp-model /content/MTP.gguf
--draft-tokens 7 --min-draft-tokens 1`. Do not substitute a DFlash2 draft.
The kit does not fetch the sidecar or acknowledge its unverified license on
the operator's behalf. Reuse verified bytes or use the normal installer in
an isolated experiment data root with the exact revision, size and hash.

Before each run, retain the actual image inspection, full source commit,
target/projector/sidecar identities, native arguments, hardware and memory
observations alongside its private results. Reuse the same target bytes in
the control. The proxy verifies host inventory receipts, **not** what an
external engine actually mounted. A supplied model name or result label is
not an attestation of its weights or math.

### Run the quality gate

In one terminal, wrap that already-running backend:

```bash
"$CHECKER" proxy --experimental \
  --upstream http://127.0.0.1:18556 \
  --listen 127.0.0.1:18557 \
  --data-dir /ABSOLUTE/PARACETAMOL_DATA
```

The wrapper accepts only the receipt-backed regular Flash-Next identity and
one request at a time. It reuses actual gateway sampling, proxying, reporting
and cancellation. Its `application=llama-cpp` metadata denotes catalog policy
provenance, not the experimental engine. UI, client configuration and other
production routes are blocked. Both listeners must be numeric loopback
addresses. The wrapper owns no backend lifecycle.

In another terminal, set `CHECKER` again and run:

```bash
CHECKER=./build/linux-amd64/bin/gufo-recheck
"$CHECKER" check --experimental \
  --gateway-url http://127.0.0.1:18557/v1 \
  --label 'Gufo CANDIDATE AR' \
  --out build/gufo-recheck-CANDIDATE-ar
```

Run MTP separately with a fresh output directory and accurate label. For a
separate production-gateway control, `check` also permits
`--model unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2`. The wrapper
itself deliberately does not expose that alias as experimental MTP.

The frozen requests use `xhigh`, seed 42 and omitted samplers. The checker
refuses sampling, reasoning, context or F16 policy drift, verifies the
gateway marker, correlates typed request reports, and checks the six default
values and their gateway provenance. Request-file hashes are retained and
guarded by tests. Input changes require a deliberate fixture-version review,
not editing the prompt until a candidate passes.

Each run preserves full requests and raw JSON/SSE replies, typed reports,
oracle/protocol errors and a summary in a new private directory. Backend
error envelopes, missing completion evidence and truncated streams fail the
gate. Existing directories are never overwritten. There is no inference
deadline. Cancellation preserves completed evidence and marks the run failed.
Metadata polling alone is bounded. Exit zero means all three narrow cases
pass, exit two means a quality or protocol miss, and other nonzero exits
mean an execution or setup failure.

Passing this screen is permission to consider further tests, not permission
to enable Gufo. Revisit practical coding, long context, concurrency, internal
precision and client behavior before any separate integration decision.
Stop the proxy with Ctrl-C and clean up only the exact backend container the
operator started. Neither the proxy nor the checker stops that container.
