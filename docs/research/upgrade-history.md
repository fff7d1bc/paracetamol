# Historical upgrade decisions

This archive preserves the dated upgrade decisions extracted from `docs/upgrading.md`
on 2026-11-14. Source identities, results, failed candidates, attribution and
precision limitations are retained. Present-tense statements and instructions
below describe their recorded source state, not today's supported configuration.
Retired templates, models, clients and commands are not restored by this record.
References below to a current patch ledger or contracts now route to the
[live patch contracts](../upgrading.md#current-patch-contracts), not a second
maintained ledger in this archive.

Use the [current upgrade guide](../upgrading.md), [llama.cpp maintenance guide](../llama-cpp-maintenance.md)
and source pins for current procedures, patch contracts and acceptance status.
Private or ignored evidence is not a fresh-checkout dependency; its absence
prevents an exact replay claim from prose alone.

## Foundation refresh (2026-11-13)

The Ubuntu-only refresh selects dated snapshot `resolute-20261105.1`, OCI
index `sha256:66b8084a0d45bd4877992bd38e04b51bfa99bd57293364cb6391fb674771846e`.
Managed image revisions advance to runtime `r6`, PyTorch base `r9`, ComfyUI
`0.28.0-r15`, llama.cpp `740251a-r72`, and content-tools `huggingface2.4-r3`.
ROCm 10.1.0, the AMD PyTorch tuple, application sources, all fifteen native
patches, content-tools' fifteen-package lock, catalog and runtime policies
remain unchanged. This is separate from later application or dependency
updates.

All five cold image builds, installed dependency and device-payload checks,
native dependency closure, confined ComfyUI CPU startup and Strix Halo
diagnostics/gateway checks pass. Python remains 3.14.4, avoiding AMD
torchvision's excluded 3.14.1 interpreter. The twenty compared native
binaries/libraries match `r71` byte for byte; the small paired screen does not
establish a foundation-caused inference speed change. See the
[bounded acceptance record](hardware-acceptance-history.md#strix-halo-ubuntu-foundation-refresh-2026-11-13)
for scope and deferred hardware checks.

## Flash-Next pool and reset corrections (2026-11-14)

The `ded522f-r78` follow-up adds three reviewed graph/layout patches without
changing the upstream pin, model bytes, precision or runtime defaults. The
[acceptance record](hardware-acceptance-history.md#strix-halo-flash-next-pool-and-reset-corrections-2026-11-14)
separates component results from the combined 100K comparison, native reset
controls, populated histories and the inconclusive one-shot Go screen. The
stack at that point had eighteen patches. Exact scopes and removal conditions
are maintained in the [live patch contracts](../upgrading.md#current-patch-contracts).

## Flash-Next prefill recovery (2026-11-14)

The `ded522f-r77` follow-up keeps the upstream pin, fifteen-patch stack,
temporary HIP activation-sum compatibility policy and runtime defaults. It
changes two parts of `hip-qwen4exp-pipeline.patch`, without introducing a new
switch, packed routing format or reduced-precision matrix path.

Small-MoE MMQ scheduling traverses the original upstream seam-and-tail
descriptors in output-row-first order, folding multiple descriptors into
each block. This preserves tile arithmetic and the producer's bytes. It is
limited to hinted `gfx1151` 512-expert/10-used shapes, Q4_K/Q5_1 at width 32
and Q8_0 at width 48, with existing precision and fusion exclusions. Empty
seams must continue traversal rather than return before later expert tails.
A block-wide barrier protects shared storage between iterations. The
original routing fixture remains unchanged, while eight maintained matrix
cases add multiple K chunks and the actual projection dimensions.

The convolution matcher now recognizes upstream's overlapping rollback
windows and `SET_ROWS` store. The old matcher rejected that changed graph
and silently used the correct but slower full-concat path. The adaptation
retains the store, rejects unknown readers and aliasing, and preserves input
lifetimes. Its dedicated fixture checks current graph geometry, all rollback
slots and unowned cells, matcher eligibility after allocation, and exact
agreement with the unmarked path. Ordinary model traces are needed as well
to show that production kernels actually run.

Descriptor packing and a dedicated outer kernel were tested separately but
did not improve the selected scheduling path, so neither is carried. The
fork's BF16/F16 internal matrix changes remain excluded. See the
[hardware record](hardware-acceptance-history.md#strix-halo-flash-next-prefill-recovery-2026-11-14)
for measurements, combined-image acceptance and coverage limits.

The later [reverse-ablation and Q4 scheduling screen](strix-llama-reverse-ablation.md)
leaves this baseline unchanged. It records the full fork's failed replay gate
and a numerically passing J48 follow-up without a repeatable timing win.

## llama.cpp HIP MMQ compatibility refresh (2026-11-13)

The follow-up selects frozen upstream
`ded522f8304554317e7ff8f88698dbbbca5425aa`, 293 commits after `740251a`,
in image `ded522f-r76`. The fifteen-patch source tree is
`6235aa1dff65d4866165647499e02d67d8b06d51`. Five patches need source/API
rebases. Nine retained patches keep their executable changes. Upstream
`e56f006ed6b416ae8b7ccb5243b204743f09df1e` replaces the former quantized-KV
state-copy patch, and a temporary HIP MMQ compatibility patch brings the
carried count back to fifteen. The failed candidate below remains historical
evidence, not an accepted intermediate release.

This refresh is accepted on 2026-11-14 with the documented prefill cost of
about 19% in matched 32K screens. The original regression and bounded runtime
gates pass, including two successful fresh coding attempts after a retained
one-shot failure. This is not a claim of universal quality equivalence.
Batch and microbatch remain 2048. This `r76` acceptance kept performance
recovery separate. The later [prefill recovery](#flash-next-prefill-recovery-2026-11-14)
is accepted in `r77`; it is no longer pending work on this image.
See the [acceptance record](hardware-acceptance-history.md#strix-halo-hip-mmq-compatibility-refresh-2026-11-13)
for exact results and limits.

The compatibility patch retains the pre-`8fc0376` floating-input auxiliary
sum only in `quantize_mmq_q8_1`, for normal and scatter instantiations.
Its `GGML_USE_HIP` guard applies to every HIP device, not only Strix Halo or
Qwen4exp. Quantized activation bytes and scales stay unchanged. The scalar
Q8_1 quantizer, newer few-row producer and non-HIP paths retain upstream's
corrected behavior. F16 KV, model files, matrix precision, samplers and
runtime settings are unchanged. There is no new switch or output filter.

This is a temporary compatibility mitigation for an observed local
execution-path regression, not a mathematical correction or evidence that
upstream is wrong. Remove it only when the corrected HIP sum passes the
unchanged replay, speculative and non-speculative gateway checks, numerical
and state tests, populated long context and applicable hardware acceptance.
Do not weaken exact-output or numerical gates to remove it. Results and
coverage limits belong in the [hardware acceptance record](../hardware-acceptance.md).

The pipeline rebase consumes upstream's seam-and-tail tile descriptors in
place of the former private MoE tile representation. Existing shape guards,
tile-width policy and invalid-expert checks remain. The routing fixture
independently checks row coverage, maps, tiles and guards across 1,344 cases.
Precision guards follow the current public API and still decline explicit
overrides. The HC fixture uses the supported shape-bound F32 SGEMM reference,
retaining F32 accumulation and its strict `1e-10` NMSE gates.

## Historical held llama.cpp source refresh (2026-11-13)

The following records the earlier hold and its investigation. Statements
about retaining `740251a-r72` describe that decision at the time. The later
compatibility refresh above is a separate candidate and acceptance decision.

HOLD upstream `951d465f5c7c16fb486ff45bb14d99984e68c6db` and the private
`951d465-r74` image. The proposed fourteen-patch stack rebases five patches
and replaces quantized-KV state-copy code with its equivalent upstream fix.
Its build, operator and state checks pass, including the candidate's
F16/Q8_0/Q4_0 device-copy removal gate, but the unchanged nonthinking replay
fails through the ordinary gateway with both DFlash2 and the plain target.
Passing lower-level checks does not override that acceptance failure.

Production remains `740251a-r72` with all fifteen existing patches. Only the
separate Ubuntu foundation and content-tools dependency units are selected;
no engine source, default, sampler, client filter, forced EOS behavior or
reverse-Q8 production patch is introduced. Remaining candidate long-context,
performance and vision gates are stopped, not accepted. See the
[bounded hold record](hardware-acceptance-history.md#strix-halo-llamacpp-source-refresh-hold-2026-11-13).

The [upstream November 12 reply](https://github.com/ggml-org/llama.cpp/pull/31247#issuecomment-6825182003)
reports clean corrected NVIDIA replay and improved agreement with its
non-activation-quantized reference. The local three-patch, 8K, one-slot,
non-speculative corrected/reversed pair subsequently passes six chat cases
in each arm, but is not pristine upstream. Raw continuation and
teacher-forced prefill follow different execution histories; their bounded
near-EOS divergence is not a numerical-accuracy, KLD or broad-quality result.
Later pristine corrected and exact-reverse-only arms at the same 8K/one-slot
scale both pass all six cases using non-mmap general loading and explicit
lazy CPU embedding reads. The earlier forced-mmap loading block is not a
permanent upstream-inference block. In the full fourteen-patch binary at
8K/one slot, the same replay passes 6/6 with both QSA environment switches
absent and only 1/6 after enabling both. The graph/pipeline-only control also
gives 1/6; the attention-only control gives 5/6 with a different extra tag.
The binary, native arguments, prompt and samplers are unchanged within this
grid. A local fast-path execution sensitivity therefore reproduces the
symptom without DFlash2 or production-sized context. Neither a six-control
normalization/recurrent disable nor a twelve-control combined disable makes
all cases clean. These results do not yet identify a faulty individual
optimization or prove reduced numerical accuracy. The subsequent half-split
narrows the repeated-answer sensitivity to the three normalization controls,
not the recurrent/convolution half. Individual controls then implicate HC
injection rather than narrow normalization. A diagnostic source control
declines only runtime HC injection fusion while retaining early projection
graph ordering and allocator dependencies. It removes the repeated-answer
symptom but leaves the separate extra-tag case. This narrows the execution
path, not a demonstrated arithmetic defect or a production fix. Keep the
source hold until the original configuration is resolved and the remaining
acceptance gates pass.

A deterministic independent HC fixture subsequently passes the unchanged
strict numerical gates with scheduler and persistent allocations. Actual
kernel traces verify gated injection activation. Its F32 fused projection
is closer to a separate scalar FP64 dot reference on this synthetic input
than the ordinary projection, not worse. No faulty HC arithmetic is
demonstrated; do not turn a replay-changing fallback into a claimed accuracy
repair. Full-model reference and execution-history questions remain open.

The later matched historical pair identifies upstream `8fc0376` as the
introducing source change on the local execution path. The accepted full
old stack passes 6/6 at the same small native policy, old plus only that
commit reproduces the held candidate's exact six responses and 1/6 score,
and the full candidate with only that commit reversed returns to 6/6.
Only each image's physical HIP library changes. The new auxiliary sum
matches the Q8_1 format and CPU reference, so this is not proof that the
upstream correction is wrong. Separate hunk controls retain the failure
after reversing only the scalar quantizer change, but pass 6/6 after
reversing only the matrix quantizer change while keeping the scalar
correction. The source hold remains. See
[matched historical attribution](hardware-acceptance-history.md#matched-historical-source-attribution)
for the causal controls, limitations and separate extra-tag case.

A later fault-preserving actual-input capture passes exact routed Q5_1
quantizer checks and an independently bounded affine-consumer reference.
The captured consumer's absolute error is `1.66e-9`; the corrected formula is
also closer to the original-input dot for that one scalar. This supports
format-correct numerical sensitivity on the existing optimized path, not a
claim that the replay regression is harmless or every local operation is
correct. Do not promote a source or arithmetic reversal merely because it
cleans up this fixture. Keep the accepted production pin until a proposed
adaptation passes the unchanged replay and broader acceptance.

The [internal-mechanism follow-up](hardware-acceptance-history.md#internal-numerical-mechanism)
now separates the auxiliary-value effect from original quantizer code
generation. A same-binary post-selector preserves the original 1/6 failure,
including every retained raw-logit record, when it copies the existing sum
bits unchanged. Selecting only the old sum bits gives 6/6 with the original
producer and separate consumer objects byte-identical. A different, matched
HC projection cut validates all 1,168 outputs against independent F32-tree
and higher-precision references, yet reverses natural EOS preference between
two locally valid reductions. Checkpoint readback and its explicit sync are
not necessary under the observed prompt split. This establishes concrete
numerical-trajectory sensitivity, not a wrong-operation finding, universal
accuracy claim or accepted repair. Production remains on the accepted pin.

## Content-tools refreshes (2026-11-03 and 2026-11-13)

The November 3 refresh advances `huggingface-hub` from 1.27.0 to 2.4.0 with
its complete fifteen-package lock. The transport now uses HTTPX2/HTTPCore2
and operating-system certificate trust through `truststore`, following the
[upstream migration](https://huggingface.co/docs/huggingface_hub/en/concepts/migration_v2).
The cold content-tools image passes `pip check`. An actual normal installer
run downloads the catalog-pinned Qwen3 0.6B Q8_0 artifact, verifies all
639446688 bytes against its SHA-256 and publishes a reusable verification
receipt in disposable storage. TLS verification remains enabled, and no
credentials or existing managed content are used. Authenticated Civitai
transport is not covered by that check; its separate HTTPS helper is unchanged.

The November 13 follow-up keeps Hub 2.4.0 and twelve other lock entries
unchanged, advancing only Click to 8.5.1 and Filelock to 4.2.10 in content-tools
`r4`. These fix upstream interrupt/help handling and a contended lock-wait
deadline, respectively. The cold image passes `pip check`, exact installed-lock
inspection and download-CLI checks. The normal installer passes a fresh pinned
Qwen3 0.6B download, full hash verification, receipt publication and reuse in
disposable storage. Dry runs remain non-mutating. Authenticated Civitai
transport is not requalified by this small public-Hugging-Face check.

## Precision-preserving ROCm and Gufo screens

### Precision-preserving ROCm follow-up (2026-11-03)

The routing follow-up keeps upstream `740251a` and all fourteen patches.
The pipeline patch adds the two invalid-expert checks from
[Strix PR 227](https://github.com/halo-box/strix-llama.cpp/pull/227), frozen at
`575da5169e2fe0c587cbdced32f41174a7316ff2`. Both the histogram and scatter
passes reject IDs outside `[0, 512)` before indexing shared counters. Valid
routing and the serial prefix scan stay unchanged. This is hardening against
invalid input, not evidence that normal model routing was corrupt.

`test-qwen4exp-routing.cpp` checks the mechanically extracted production
helper against independent expert bounds, row correspondence and buffer
guards. It covers 192 cases, including negative and oversized IDs, padded
strides, both map directions and empty valid sets. The build script refuses
the pre-fix helper before these invalid-ID tests can run. For this fixture,
the ordinary image build runs only CPU oracle self-checks and deliberate
checker corruptions.
Actual HIP helper execution remains a separate hardware gate.

Three isolated performance extractions remain outside production:

- The parallel integer prefix scan from
  [Strix PR 213](https://github.com/halo-box/strix-llama.cpp/pull/213), frozen
  at `6fa2219fb02f2a3a96873d5410e5638d8b072951`, saves about 1.8 microseconds
  in the smaller routing fixtures, but the ordinary gateway screen establishes
  no useful end-to-end gain. Retrieval, history edits and concurrent isolation
  pass. Draft acceptance and generated reasoning differ, so the single paired
  screen does not establish a general performance regression either. Keep the
  simpler serial scan.
- Stable QSA row compaction from that same proposal, using the earlier
  `ff64766de0a7632ac30acff9b71a5fa7d7d1b2df` implementation, passes the
  extracted GPU integer oracles, but its cost depends on selected width and
  ordering. It helps ascending keys with holes. At width 2560, shuffled and
  synthetic shuffled/hidden rows become substantially slower in repeated ABBA
  preprocessing measurements. Width 2051 mostly overlaps outside the
  favorable hole case. This is component evidence, not a full-model
  regression or a populated 256K-context benchmark. Reject the isolated
  extraction rather than adding shared-memory and fallback work to an
  unordered top-k path without a demonstrated workload benefit.
- GLU/MMQ input-quantizer fusion from
  [upstream PR 30489](https://github.com/ggml-org/llama.cpp/pull/30489), frozen
  at `730f0425773c0ece8fbdd5e73b80b749e6bf81b4`, keeps the selected pin's
  Q8_1 correction and F32 GLU arithmetic. The isolated extraction passes
  old-versus-new quantized-buffer comparisons, operator checks and explicit
  same-graph capture/replay controls. A diagnostic trace also proves actual
  managed-model activation. Its gateway screen gives no convincing cold
  prefill or four-worker throughput benefit. Serial decode is higher, but
  draft acceptance differs and fresh control runs also vary. This does not
  establish a repeatable fusion speedup. Leave it out rather than carrying a
  new fusion/lifetime surface or experimental switch for that result.

The [hardware record](hardware-acceptance-history.md#strix-halo-routing-hardening-and-performance-screen-2026-11-03)
owns the measurements and final bounds-only acceptance. None of these screens
changes the GGUF, F16 KV cache, internal matrix precision or default runtime
policy.

### Precision-preserving ROCm PR screen (2026-10-26)

Four isolated candidates pass the numerical and ordinary-gateway checks on
Strix Halo (`gfx1151`) but remain held for value. None establishes
a repeatable, worthwhile ROCm Flash-Next gain with the managed model bytes,
F16 KV, greedy DFlash2 depth three, four slots and shared 262144-token pool.
The [hardware screen](hardware-acceptance-history.md#strix-halo-precision-preserving-pr-screen-2026-10-26)
owns the paired measurements and qualification limits.

- The direct-K extraction from
  [Strix PR 213](https://github.com/halo-box/strix-llama.cpp/pull/213), frozen at
  `07b5e10c4df960ceb52b5327e659b7cbcdb2c9c1`, reads the same F16 K bits while
  retaining packed V. It changes neither KV precision nor selected keys.
  Its deep cold input is identical to control, while generated reasoning
  changes the subsequent append histories. Neither phase supplies a compelling
  gain. The append pair does not establish a precise causal speedup or
  regression. Revisit only with equivalent fixed work and
  a repeatable gain that warrants the extra kernel machinery.
- The shape-key graph cache from
  [PR 31004](https://github.com/ggml-org/llama.cpp/pull/31004), frozen at
  `296e8fb7088b00a003fac612971d4f7f2f3ec8da`, retains chunked graph ownership,
  the `gfx1151` recapture correction and allocator invalidation. Serial and
  four-worker controls show overlapping timings and changed draft acceptance,
  not a reliable aggregate-throughput gain. Keep it outside production until
  capture counts and counterbalanced wall-time results justify its cache policy.
- The shared output view from
  [PR 30196](https://github.com/ggml-org/llama.cpp/pull/30196), frozen at
  `0715571e1077206f5f0a89edf3cc71bfe913f29c`, synchronizes speculative batch
  outputs once while preserving ordinary scalar sampling and its timer boundary.
  One screen leaves serial throughput effectively unchanged and reduces
  four-worker throughput. That supplies no adoption case. Do not compare its
  timings directly with different fixture versions or infer a general regression
  from one screen.

A separate occupancy-only extraction from the same Strix PR changes only the
QSA kernel's compiler hint. The matching compiler lowers VGPR use from 210 to
192 without spills. Forty numerical cases and exact model-logit checks pass.
Its initial 2.67% deep cold-prefill gain fails to repeat in reverse order,
with 601.671 versus 602.190 tokens/s. Short-context results also overlap or
vary in both directions. Keep the hint outside production rather than treating
lower register use as proof of a worthwhile workload gain.

The broad scan also covered QSA, GDN, MoE, allocation, graphs, sampling, cache
and server work rather than only HIP-titled proposals. Vulkan-only or
NVIDIA-only speedups do not qualify as ROCm gains. RAM-prompt-restore fixes
do not activate with the managed `--cache-ram 0` policy. Mixed-precision
shortcuts, new quantization and private runtime replacements remain excluded.
The per-size graph arenas in
[PR 31156](https://github.com/ggml-org/llama.cpp/pull/31156) need separate
memory-cost and scheduler-lifetime review, not an unmeasured combined stack.

Potential follow-ups are narrower qualification tasks, not adoption promises:

- [PR 31084](https://github.com/ggml-org/llama.cpp/pull/31084) makes threshold-tie
  top-k selection deterministic. This changes which tied IDs are selected, not
  merely their order, so downstream canonical sorting is not an equivalent fix.
  Treat it as a correctness screen with independent selected-key oracles.
- [PR 30311](https://github.com/ggml-org/llama.cpp/pull/30311) now has head
  `0360fd1025c068d2d938f66d8418c4ae2f17bf3f`, including relocated-idle snapshot
  bookkeeping absent from the historical hold. Rerun that hold's exact-zero
  continuation oracle and report discrepancy magnitude rather than assuming it
  is the unchanged proposal or a demonstrated performance improvement.
- [PR 30489](https://github.com/ggml-org/llama.cpp/pull/30489) fuses GLU into the
  existing Q8_1 activation quantizer, without adding another quantization step.
  Profile actual activation first, then qualify arithmetic, precision overrides
  and allocation lifetimes before spending time on another carried fusion.

### Gufo-inspired Flash-Next screen (2026-10-26)

The separate [Gufo optimization screen](qwen3.8-flash-next-gufo-optimization-screen.md)
records exact DPP lane transport, reader reuse and prompt-row lookahead.
No Gufo engine, private runtime, reduced-precision math or adaptive draft
controller is imported. Reader profiling at 32K and near 256K finds no exposed
bottleneck justifying persistent workers or cross-call prefetch machinery.
The DPP numerical gates pass, but the measured serial improvement accompanies
different draft acceptance and is not isolated from it. The candidate remains
held rather than joining the production patch ledger.
The independent greedy DFlash2 depth-five screen also supplies no adoption
case. Serial throughput barely changes and four-client throughput is lower.
Keep depth three instead of copying Gufo's different-model adaptive widths.

The later [developer-only Gufo recheck kit](qwen3.8-flash-next-gufo-optimization-screen.md#developer-only-recheck-kit)
preserves an explicitly gated source-build recipe and frozen failures. Its
source pin is not a managed application dependency and need not advance in
an ordinary llama.cpp refresh. A deliberate Gufo candidate updates the full
pin in `tools/gufo-recheck/prepare.go` and both matching fragment declarations,
then prepares a fresh build against the current production ROCm inputs.
Run its cheap regression gate before expanding performance work. A passing
screen never automatically enables an application or client model.


## Earlier llama.cpp packaging and source decisions

References below to the current patch ledger route to the
[live patch contracts](../upgrading.md#current-patch-contracts). Template and
model policies in this history are retained at their original point in time,
including instructions for components that were subsequently retired.

#### Historical multimodal packaging (2026-11-03)

The `740251a-r71` packaging follow-up retains the fourteen-patch `r70` stack
and adds the DFlash multimodal-position correction as patch fifteen. It freezes
[upstream PR 30220](https://github.com/ggml-org/llama.cpp/pull/30220) at
`73647e8ade6fb4293d221c4b92aafb7e82976418`; the normalized patch SHA-256 is
`3e66828bebe8f9ceb5a5a7b786572cbc05fd9632537c592b20f0b6a7fb487150`.
The local file qualifies only the two source-comment lines claiming unchanged
output correctness: target encoding and verification stay unchanged, but draft
conditioning and acceptance change. No executable code or global cache guard
is adapted. Its SHA-256 is
`c299f24df7c580ccbe013224dbba50c6adc64eaf513afff918c9f5d8cebce91d`.
Applied after `server-speculative-metrics.patch`, it takes the full native tree
from `593631c59c69782aec7f81c7459005c0c59c1842` to
`3cd21570a1bf02003a1710327d4093a3150b6f85` without moving upstream. The exact
upstream-comment form has tree `c7a67a78d2830b10227be78c984904246f4a9970`;
do not conflate these source identities.

This fills the missing logical draft-cache positions after M-RoPE image
chunks; embedding-token counts are not position counts. The selected pin
already contains upstream's pinned-image skip and `pos_next()` drafting fix;
neither alone fills this gap. The proposal remains unmerged, includes no
upstream regression test, and its author identifies remaining cross-request
and slot-gap limitations. Keep those limits visible when qualifying a new
tuple. Source correction and image construction do not establish hardware
acceptance, resolution bounds, model quality, or distribution equivalence.
Target-hardware results belong in `hardware-acceptance.md`.


#### Earlier source refresh records

The November 3 refresh selects frozen upstream
[`740251a920abf92b7744d9c0518ca157db0db579`](https://github.com/ggml-org/llama.cpp/commit/740251a920abf92b7744d9c0518ca157db0db579),
181 commits after `c6fb3b5`, in image `740251a-r69`. All fourteen patches
remain. Eleven retain unchanged code, three need semantic rebases, and the
reporting patch receives updated source/tree provenance. No removal gate is met.

Upstream [PR 29510](https://github.com/ggml-org/llama.cpp/pull/29510) uses
flash-attention `src[5]` for shared-cache row lists. Selected QSA IDs now use
`src[6]`, with explicit mutual-exclusion guards. Keep the Qwen4exp full-cache
causal/sequence mask as a real dependency and use that same layout policy when
checking graph reuse. Do not substitute compressed row-list indices for
full-cache cell IDs. The new generic row-list accelerator path is NVIDIA-only
at this pin; HIP backend probing retains its ordinary non-row-list GPU path.

The HC output-struct refactor in
[PR 31142](https://github.com/ggml-org/llama.cpp/pull/31142) retains the raw
F32 injection projection followed by one explicit scale. The pipeline patch
also excludes upstream fused MoE gate inputs from its private small tiles.
The graph rebase normalizes fallback mask shapes using the full token count,
not an already stream-divided dimension. This prevents double-splitting a 4-D
non-unified mask. Already-canonical masks retain their tensor identity instead
of receiving per-layer identity views. The scheduler keys backend input copies
by tensor pointer, so distinct views of one shared host mask become separate
GPU copies. The initial rebase exposed about 11 GiB of extra target compute
reservation with the 256K pool. Reshape only when stream dimensions differ,
preserving the same mask values and dependencies.
`test-qwen4exp-multistream.cpp` forces the nonfused two-stream branch in a
bounded hardware fixture. It changes only that disposable context's policy,
not a production default. Preserve shape, consumer, alias and lifetime guards
rather than broadening matchers to make an update pass.

The source range includes
[DFlash feature device copies](https://github.com/ggml-org/llama.cpp/commit/2cbca389f515df455412dafa07e2b1359cc72a6d),
[graph-eviction protection](https://github.com/ggml-org/llama.cpp/commit/00f113eceb2758158582d56f98bf476b2fe3ed32).
These are upstream changes, not new downstream optimizations or independent
speedup claims. Model files, templates, gateway samplers, F16 KV, the ordinary
quantized/F32 matrix precision policy, greedy DFlash2 depth three and four
shared slots stay fixed. This does not promise bit-identical outputs across
upstream revisions.

The checked head `28b812d` is held, seven commits ahead of the selected pin.
Its [Q8_1 activation correction change](https://github.com/ggml-org/llama.cpp/commit/8fc0376721e9662d498a5706d7ee8e7574ce99f4)
is intended to align the correction sum with quantized inputs and CPU behavior.
In the matched Flash-Next thinking-off tool replay, however, that candidate
adds a closing thinking tag and repeats the correct value. Both old-source
images pass the short matrix. In a separate DFlash2 replay, five seeds and
temperature zero all miss the exact-output oracle at that head, while the
old-production and selected-pin controls pass all six. Rendered prompts and
native sampling settings match. Replacing only the physical HIP library with
the previous correction calculation restores all six checks, with other
native payloads unchanged.
This isolates the observed output difference, not a general model-quality
loss or proof that the intended numerical correction is wrong. Select the
immediately preceding upstream commit rather than carrying a permanent reverse
patch or hiding generated text in the gateway. Revisit with the same replay
fixture and broader task-quality evidence before advancing past this boundary.

The October 26 `c6fb3b5-r65` image added that server-only reporting patch to
the same source pin, making fourteen patches. It changes no inference or
sampling behavior. The build applies it after the existing native build and
rebuilds the server, retaining the compiled GPU kernels. A matching-source CPU
fixture checks absent, zero, positive and large unsigned wire values. Gateway
reports preserve missing versus observed zero without adding durable
statistics or payload storage.

The October 26 refresh selects frozen upstream
`c6fb3b5be2209f6c12339833198190f6f6829581`, 58 commits after `280141e`,
in image `c6fb3b5-r64`. Nine patches apply unchanged and four are rebased.
The thirteen downstream protections remain. Upstream
[PR 31053](https://github.com/ggml-org/llama.cpp/pull/31053) now fixes the
legacy-pool chunk-vector diagnostic, so that downstream hunk is removed.
The separate `gfx1151` executable-recapture workaround is still required.

Two upstream graph changes require semantic review rather than a context-only
patch refresh. [PR 30965](https://github.com/ggml-org/llama.cpp/pull/30965)
builds QSA selection without reading the dense causal mask. The rebase keeps
selected IDs and the visibility mask as real dependencies of the downstream
selected-key path, including its graph-disabled control.
[PR 31028](https://github.com/ggml-org/llama.cpp/pull/31028)
adds optional merged tensor forms and moves HC injection scaling before its
callback. The pipeline rebase keeps the raw F32 projection followed by one
separate scale, preserving the unchanged decode matcher's topology. Merged
tensor forms retain the upstream path when the scoped matchers decline them.
The same patch follows upstream's new MMQ fusion arguments and excludes
fused gate inputs from the private prefetch and small-tile dispatch.
Upstream [PR 31086](https://github.com/ggml-org/llama.cpp/pull/31086) changes
softplus to `log1pf(expf(x))`. The decode fusion follows that same expression.
Its exact-equivalence tests caught the old `logf(1 + expf(x))` calculation.
Neither the oracle nor the matcher guards are relaxed to accept it.
Existing Unsloth model bytes, F16 KV, matrix precision, templates, samplers,
four shared slots and the `r63` full-span correction remain unchanged.

The newly published
[CVE-2026-116096](https://github.com/CVEProject/cvelistV5/blob/main/cves/2026/116xxx/CVE-2026-116096.json)
concerns a malformed local Pocket-TTS projection file. Its cited state-object
mitigation, `0a1ef309cdbb9f2024f58a38fb4c42352c5db569`, is already an ancestor
of the previous pin. It turns that unchecked state write into a release-active
assertion and abort, not acceptance of arbitrary projection files. This
refresh must not be described as newly fixing a remote chat-request RCE.

The new range does add HTTP-input resource limits through grammar recursion
[PR 29755](https://github.com/ggml-org/llama.cpp/pull/29755), nested JSON
[PR 30242](https://github.com/ggml-org/llama.cpp/pull/30242), broader recursive
guards [PR 31153](https://github.com/ggml-org/llama.cpp/pull/31153), and DRY
sequence breakers [PR 31035](https://github.com/ggml-org/llama.cpp/pull/31035).
The RPC RDMA bounds correction also enters upstream, but managed builds
continue to set `GGML_RPC=OFF`. No new inference feature or fork optimization
is enabled by this source refresh.

The October 23 refresh selects frozen upstream
`280141e6d4769b1e4424de999437cc72ffac33d6`, 28 commits after `e197cef`,
in image `280141e-r60`. Twelve of thirteen patches apply unchanged. Upstream's
chunked GPU graphs from [PR 30804](https://github.com/ggml-org/llama.cpp/pull/30804)
require the recapture correction at each changed chunk rather than the old
single executable. Unchanged replay and other GPU architectures retain their
upstream paths. The same patch corrects an unpatched upstream HIP build
failure in the legacy-pool diagnostic, replacing its removed `instance`
member with a nonempty `instances` check. This still counts captured graph
keys, not chunks, and does not change allocation or invalidation policy.

This pin includes Flash-Next's DFlash layer-input stream reduction from
[PR 30927](https://github.com/ggml-org/llama.cpp/pull/30927) and probabilistic
DFlash2 drafting from [PR 30663](https://github.com/ggml-org/llama.cpp/pull/30663).
The latter requires explicit `--spec-draft-sampling probabilistic` to exercise
its new path. Its default remains greedy. These merges do not enable
speculation in the managed Flash-Next preset or establish ROCm DFlash2
acceptance. Models, templates, sampling policies, F16 KV and matrix precision
are unchanged. No patch removal gate is met, and the MIT license is unchanged.
The [dated DFlash2 follow-up](qwen3.8-dflash2-strix-halo-feasibility.md#flash-next-follow-up-2026-10-23)
records the independent draft evaluation rather than treating an upstream
merge as hardware or task-quality acceptance.

The subsequent `280141e-r61` image keeps that source pin and all thirteen
patches. It packages explicit greedy DFlash proposals and the accepted
streamed Flash-Next exception at depth three on ROCm Strix Halo. All native
binaries and regular files under `/usr/local/lib` are byte-identical to
`r60`. Separate optional presets reuse the regular and Huihui targets plus
one pinned Inco Q8_0 draft. The ordinary non-speculative default, F16 KV,
matrix precision and model-aware gateway samplers are unchanged. See the
[managed-preset follow-up](qwen3.8-dflash2-strix-halo-feasibility.md#optional-greedy-preset-follow-up-2026-10-23)
for the independent checks and limitations. The unrestricted direct
speculative-depth sweep intentionally rejects these streamed presets.

The later `280141e-r62` packaging removes Muse's unused chat template and
entrypoint allowlist entry. The three native executable hashes and selected
native-library hashes still match `r61`, with dependency closure and
`pip check` passing. Regular Flash-Next greedy DFlash2 becomes the guided
recipe launch and exported Maki default at xhigh after field testing. Plain
regular Flash-Next keeps adaptive xhigh as the first fallback. Raw HTTP
reasoning and sampler defaults do not change. Muse and dense Qwen MTP
aliases are retired without deleting existing GGUFs. The
[dense DFlash2 retest](qwen3.8-dflash2-strix-halo-feasibility.md#dense-27b-retest-2026-10-23)
is a separate acceptance decision, not an upstream source or math change.

The `280141e-r63` selected-key prefill correction keeps that source pin and
the same thirteen patches. A unified cache's padded physical span can reach
262144 rows while an individual request uses much less context. Previously
the 262140-row prefill limit then selected the slow fallback. Zero visibility
now marks padded descriptors, leaving all 16 bits available for real block
indices, including the final block at `0xFFFF`. Both dispatch gates cover the
full 262144-row pool. `QSA_END_BLOCK` operator cases check that boundary,
masked and maskless selection, duplicate and invalid IDs, and poisoned
padding against an independent numerical oracle. The correction changes no
KV precision, model bytes, sampling policy, or matrix arithmetic.

The November 7 shared-pool investigation reviews the Strix fork at
`62a3f414447eb8487294c34678ddc8974be7c7f9`. Its merged
[PR 215](https://github.com/halo-box/strix-llama.cpp/pull/215)
widens compact-QSA block descriptors and raises the operator bound to 1048576
rows. That is a different change from the accepted 262140-to-262144 correction
above. A private width-only extraction removes the fallback beyond 262144
without changing F16 KV, matrix arithmetic or the model's session window.
A separate local fused-indexer score-strip prototype tests the fork's memory
lifetime idea without importing its different indexer or RoPE machinery.
Neither is adopted because the 327680-token concurrent workload still reaches
the 8 GiB memory guard. The [capacity record](hardware-acceptance-history.md#320-ki-token-follow-up-and-fork-extraction)
preserves the successful checks and the uncompleted acceptance boundaries.

The same sweep does not select the fork's GDN consumer-view, input-sync,
compact visibility or incremental-QSA changes. Some require fork-only graph
and cache machinery, while the available evidence does not establish a useful
gain for the managed four-slot DFlash2 path. Do not treat their merge status
as a reason to import them or add a global synchronization barrier. Existing
precision-preserving production patches and the normal image remain unchanged.

The October 22 refresh selects frozen upstream
`e197cef4554d466e0becac20e453a355daba8e5d`, 63 commits after `dd002b9`,
in image `e197cef-r58`. Thirteen patches remain. Twelve apply unchanged;
the compact Qwen4exp graph patch needs a semantic rebase despite applying
cleanly. Upstream's new fused dense selection-mask operation from
[PR 30640](https://github.com/ggml-org/llama.cpp/pull/30640), merged as
`d454a204ef2b63af357744809c05af897611a72e`, returns before constructing
the selected IDs used by the accepted Strix Halo attention kernels.
Allow that early return only when selected-key attention is disabled.
Both the compact kernel-on/graph-on path and kernel-on/graph-off control
must retain selected-ID generation. Other supported layouts keep upstream's
new mask operation. No matrix, residual or KV precision policy changes.

The router disconnect fix is now upstream through
[PR 29707](https://github.com/ggml-org/llama.cpp/pull/29707), merged as
`4e201388c319fc9f747fbe6a666793e2dc2a6d8e`. Its separate source patch is
retired. The merge contains both child-connection closure and the single
response-wait deadline, but deliberately omits its timing-sensitive test.
Retain streaming and non-streaming router/gateway disconnect-to-child-idle
checks in source-update acceptance, rather than treating a successful
follow-up request as proof that abandoned work stopped.

The Qwen output-projection reshape from
[PR 30823](https://github.com/ggml-org/llama.cpp/pull/30823), held as an
optional patch in the October 20 screen below, is now upstream at
`839a572a4cdecdb80d211297b8e7d7323a6e99d2`. It enters with the source
refresh, not as another carried patch. Recheck dense/MTP and concurrent
Flash-Next requests. Upstream's legacy-pool graph invalidation is not the
same correction as the retained `gfx1151` executable-recapture workaround.
Keep its populated restore/clear numerical gate. PR 30311 remains unchanged
and held outside the selected stack. The MIT license is unchanged.

The [October 22 acceptance record](hardware-acceptance-history.md#strix-halo-llamacpp-source-refresh-2026-10-22)
accepts the cold production image on `gfx1151`. It covers matching-source
numerical checks, ordinary-gateway sampling and cancellation, native Maki,
paired 32K/near-256K retrieval and bounded older-model/Vulkan compatibility.
No material Flash-Next regression is observed in that small paired control.
The existing off-mode tool-output formatting miss remains documented rather
than counted as an exact-output pass. Other GPU architectures are not accepted
by this screen.

The October 20 refresh selected the frozen upstream commit
`dd002b91f583af0b80140f9a140dc04856bfc2e0`, 59 commits after `623c163`.
The image is `dd002b9-r57`. All fourteen existing patches remain. Twelve
apply unchanged; `router-disconnect.patch` follows a new import, and
`quantized-kv-flash-attention.patch` follows the upstream inclusive full-tile
loop condition. Both rebases change context only. The build explicitly sets
`LLAMA_CONNECT=OFF`: the new upstream helper can download a prebuilt binary
from a floating release, outside the managed locally built application surface.
RPC and remote UI assets remain disabled.

Upstream now contains the post-only HC gate fusion from
[PR 30602](https://github.com/ggml-org/llama.cpp/pull/30602), merged as
`ff5a23376725e5a889fa0bfb987b7541dc304da0`. The downstream matcher still
runs first on eligible `gfx1151` graphs, retaining the larger normalization
and injection fusion and its accepted F32 reduction ordering. A merged
sub-operation does not satisfy that patch's removal gate. Neither this merge
nor the other upstream changes establish equivalent behavior and the required
hardware evidence for removing the host-buffer, graph-recapture, router,
reader or quantized-state-copy protections. No patch removal gate is met.

Two proposals were screened separately rather than folded into the October 20 refresh:

- The recurrent rollback guard from
  [PR 30311](https://github.com/ggml-org/llama.cpp/pull/30311), frozen at
  `af3b1c2769945670f8e972d1aaa5ee919d3be96c`, is **held**. It fixes four
  unsafe model-free CPU metadata cases, and the actual-model GPU rollback
  case passes. The idle-sequence continuation still fails the exact-zero
  logit oracle. That failure does not print its magnitude or top-token
  comparison, so this zero-tolerance failure alone neither proves model
  corruption nor isolates a root cause. The guard is not carried. Default
  non-MTP Flash-Next uses zero rollback snapshots and does not exercise the
  new snapshot bookkeeping.
- The multi-sequence output-projection reshape from
  [PR 30823](https://github.com/ggml-org/llama.cpp/pull/30823), frozen at
  `71a6482a3ac183034c4e475ff2545d46f2516cc8`, passes the screened correctness
  and activation gates, including populated state checks, but was **held** at that screen.
  Two short fixed-order warmed long-context concurrency comparisons show
  roughly 1–3% gains, with unchanged single-request rates and no compelling
  fresh-prefill improvement. This limited screen does not establish a broader
  performance gain or justify another independently carried patch. The
  production stack and numerical defaults remain unchanged.

The [October 20 hardware record](hardware-acceptance-history.md#strix-halo-llamacpp-refresh-and-isolated-pr-screens-2026-10-20)
separates the upstream refresh from both held proposals and records the
paired measurements and acceptance limits.

Compile every native fixture against its exact candidate source and libraries,
including fixtures whose API appears unchanged. The retained native
Flash-Next state fixture uses synthetic tokens; its exact restore checks do
not establish natural-text retrieval. Real-text HTTP retrieval, history edits,
tools and client round trips remain separate acceptance gates.

The October 16 update pins `623c16393c7d53a37df8bb69c6ea8ed6e5171f62`,
37 upstream commits after `e36df60`, in image `r53`. Twelve patches apply
unchanged. `hip-qwen4exp-pipeline.patch` and `hip-qwen4exp-decode.patch` are
rebased for upstream's vectorized
F32 RMS normalization from
[PR 29720](https://github.com/ggml-org/llama.cpp/pull/29720). The decode,
HC and narrow-row fusions now reproduce the aligned float4 reduction tree.
Misaligned tensors and unsupported row shapes retain upstream's fallback.
Exact fused/reference checks remain exact, with added alignment and row-width
cases. A clean patch application did not catch this arithmetic coupling, but
the native operator tests did. Review reduction order as well as types on
future normalization updates.

Upstream also includes the previously deferred pooled-input view reuse from
[PR 30613](https://github.com/ggml-org/llama.cpp/pull/30613), Vulkan device
initialization serialization, bounded pinned-host allocation, and server
slot/draft-state fixes. This is not permission to remove the APU host-buffer,
graph-recapture, router-disconnect or KV-state-copy patches. Their removal
gates remain independent.

Two separately screened optimizations remain outside this update:

- The activation-copy change from
  [PR 30493](https://github.com/ggml-org/llama.cpp/pull/30493), head
  `4373bb620120b5fe1b811e1c4a202db9d82fa586`, lengthens Flash-Next's
  32K prefill by about 9.4% without improving decode. Do not carry it on
  the strength of results from another model or GPU.
- An isolated F16 attention kernel from
  [Strix fork PR 172](https://github.com/halo-box/strix-llama.cpp/pull/172),
  head `1ea2d675d7977c47e3c6952b3fc2ac376ef17b93`, shortens dense Qwen27B's
  32K prefill by about 10% in the initial screen, but shows no meaningful
  Flash-Next gain. Its roughly 1200-line kernel and changed attention
  evaluation order require more qualification than that result warrants.
  It is not a production-accepted patch. No fork-wide changes or private
  HIP runtime were imported.

The [October 16 hardware record](hardware-acceptance-history.md#strix-halo-llamacpp-refresh-and-optimization-screens-2026-10-16)
contains the paired Flash-Next timings and numerical limitations.

The October 15 decode follow-up keeps the `e36df604703dec1b6ce82f214698725475a57035`
source pin and adds a fourteenth patch in image `r52`. The GDN implementation
derives from [Strix fork PR 106](https://github.com/halo-box/strix-llama.cpp/pull/106)
at `9fbe5308fabbc79a53ddee5eadfad74ae0e4710e`. Its older recurrent reduction
is replaced with the selected upstream pin's eight-lane F32 ordering. Only
the reviewed single-token Qwen4exp geometry is eligible. There is no private
HIP runtime, asynchronous embedding reader or lower-precision matrix path.

The HC change adapts [upstream PR
30602](https://github.com/ggml-org/llama.cpp/pull/30602) at
`681ae40b5688343dd1a2c6c44b903ba8d5e5ac68` to the existing downstream
norm/injection fusion. Applying its post-only fusion unchanged loses that
larger fusion and slows prefill on the acceptance host. Preserve this ordering
when rebasing. The [hardware
record](hardware-acceptance-history.md#strix-halo-flash-next-decode-follow-up-2026-10-15)
separates the independent candidate screens from the combined result.

At that October 15 screen, the pooled-input view change in
[PR 30613](https://github.com/ggml-org/llama.cpp/pull/30613), tested at
`870e2b09de9a2e9f61d1b679a053baaff2a2ca9f`, was not carried. Its measured gain
is too small to justify another patch. The same applies to the parallel HC
pre-mix scheduling idea from fork commit
`389d446be5fd2babf97d2d98b3cdf39c4742a5fc`. These are deferred for value,
not classified as broken. The pooled-input change is now supplied upstream
by the October 16 pin. The HC scheduling idea remains deferred.

The October 15 follow-up pins `e36df604703dec1b6ce82f214698725475a57035`,
16 upstream commits after the October 14 refresh, in image `r51`. All thirteen
remaining patches apply unchanged. Upstream now owns the partial-checkpoint
correction from [PR 30514](https://github.com/ggml-org/llama.cpp/pull/30514)
and Vulkan KV contiguization from
[PR 27703](https://github.com/ggml-org/llama.cpp/pull/27703).

The dedicated Vulkan shader, its source patch and environment override are
removed. The merged implementation reuses bit-preserving copy shaders,
supports F16 and BF16 scratch, and selects eligible AMD devices except RDNA 4.
Its minimum query batch is 256, compared with 64 in the former downstream
path. This is upstream dispatch policy, not a newly enabled Paracetamol
tuning switch. Keep cross-architecture acceptance distinct from the local
Strix Halo measurements. The container runtime report moves to schema 2 and
drops the obsolete local-fix boolean. Rebuild the image with the matching
controller before using `status llama-cpp`.

The [October 15 hardware record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-upstream-follow-up-2026-10-15)
records the checkpoint-memory measurements, both backend comparisons and
the hardware acceptance limits.

The separate recurrent-state-copy experiment, [PR
30139](https://github.com/ggml-org/llama.cpp/pull/30139) at
`10a57dd921c19152bc9121e1ff5aafba32169843`, is **not carried**. It passes the
bounded state and 4K/32K request screens, with identical request and
response-choice objects across eighteen paired requests. On the acceptance
host, shallow dense Q8 non-MTP decode improves from 7.222 to 7.290 tokens/s,
about 0.95%. The default dense Q8 MTP HTTP control changes from 15.60 to 15.51
tokens/s. Flash-Next changes from 21.53 to 21.79 tokens/s, while its 32K
prefill takes about 4.2% longer. These small, mixed results do not justify
another carried graph-state patch. The in-place shortcut also excludes
speculative rollback snapshots. Revisit it when upstream merges or the evidence
changes, not as a known-bad proposal or an accepted long-context optimization.

The October 14 refresh pinned
`23deec025587a58469bdf0120313ff88a1ddcc7b`, 345 upstream commits after
`2090f60f0b60b67b3bcc96573c6e68a0c33f30c7`, and used image `r50`.
That fourteen-patch stack replaced the earlier partial rebase. Its
[screen record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-refresh-screen-2026-10-14)
remains useful evidence of why a successful short request was insufficient.
The [final-image record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-upstream-update-2026-10-14)
keeps the performance qualification explicit. The default dense Q8 MTP
control is effectively unchanged, as is Flash-Next's cold 247K request.
Flash-Next's warm decode is about 12.3% slower in the paired screen. This is
a maintenance and correctness update, not a universal speed improvement.

Upstream now owns Qwen4exp pooled-key state and visibility filtering through
[PR 29751](https://github.com/ggml-org/llama.cpp/pull/29751), with reduced
indexer workspace from [PR 29825](https://github.com/ggml-org/llama.cpp/pull/29825).
The private prepared-key cache, prefix eligibility machinery and selection
fusion are retired. The replacement compact patch only removes redundant
mask materialization around upstream's selection. The former generic QSA
gather is also retired. ROCm uses the retained selected-key kernels, while
Vulkan uses its upstream sparse-attention path from
[PR 28105](https://github.com/ggml-org/llama.cpp/pull/28105), reached through
the new graph's selected-width hint. These are behavior replacements, not
claims that the old proposals themselves were merged.

Canonical ID ordering is required with upstream's unordered top-k output.
Keep the negative-control order tests, independent mask-intersection tests,
and populated restore checks when rebasing these kernels. The scoped Q8
prefetch and small MoE tiles now live in the pipeline patch. Their private
instantiations explicitly retain Q8 activation precision and honor upstream
precision overrides. No retired BF16/F16 shadow-matrix mode is restored.

`applications/llama-cpp/test-qwen4exp-state.cpp` remains the hardware state
gate. It populates more than 4K tokens, decodes, switches causal mode and
compares full logits after restore and clear. Run its
[fast screen](../testing-and-release.md#fast-flash-next-state-screen) before the
larger matrix, then the full fixture. Compile against the matching patched
source and libraries and enable both QSA kernel and graph switches. There is
no private QSA cache switch or cache-lifecycle test to maintain now.

Sampling policy remains at the Go gateway boundary, moved there in image
`r49`. It applies the catalog's per-thinking-mode defaults and preserves
explicit request values. Direct engine endpoints intentionally use native
defaults, so direct benchmarks send their selected catalog tuple explicitly.
Verify the gateway's resolver against native parsing and templates for off,
omitted controls, nested booleans and named effort levels. Router cancellation
acceptance must observe the child slot becoming idle, not merely a successful
follow-up request.

The reviewed upstream pin includes the
[F16 attention race fix](https://github.com/ggml-org/llama.cpp/pull/30103),
[RDNA 3.5 MMQ tile tuning](https://github.com/ggml-org/llama.cpp/pull/28195),
[chunked GDN kernel](https://github.com/ggml-org/llama.cpp/pull/29353) and
[Vulkan AMD iGPU checkpoint-read fix](https://github.com/ggml-org/llama.cpp/pull/30049).
Their presence alone is not evidence that the entire new build is faster.
The GDN implementation uses compensated BF16 terms with
F32 state, rather than the retired private BF16 shadow-weight mode. It still
requires numerical and model-level acceptance.

The additional open PRs selected on October 14 are the small quantized-state
copy and successful-stream connection reuse corrections in the ledger.
Other reviewed proposals remain deferred.
[HIP sparse attention](https://github.com/ggml-org/llama.cpp/pull/30370), at
`1ad19e56872d60cbee26c442a449bee7f0ba8fa9`, dispatches on GCN, not Strix Halo.
The [Vulkan KV layout proposal](https://github.com/ggml-org/llama.cpp/pull/27703),
reviewed at `3fdbb7132e8ed37fa785e5c74b870b5b01cf969a`, subsequently merged
and is included in the October 15 follow-up above.
[MMQ copy vectorization](https://github.com/ggml-org/llama.cpp/pull/30493)
and [vectorized RMS norm](https://github.com/ggml-org/llama.cpp/pull/29720)
remain watch candidates rather than established end-to-end Strix Halo wins.

The Strix fork was also reviewed at
`deb5e2f50f79b812f5b957321c7b511c054043e0`. Its allocation-boundary fix is
retained above. Newer mixed-matrix paths, incremental-cache machinery and
Flash-Next MTP-specific fusions are not imported. A decode HC-mix fusion at
`389d446be5fd2babf97d2d98b3cdf39c4742a5fc` is a possible separate experiment,
not a measured gain for this update. Its supporting kernels and numerical
assumptions need their own review rather than copying the dispatch change.

The October 6 isolation follow-up changes the interpretation of the refresh
hold. Unpatched `d7a695ef679138c13d86359b84c1731d36213d32` passes with
resident embeddings. Adding only the explicit reader reproduces the logit
failure, while recreating HIP executable graphs on recapture clears the
bounded test without disabling ordinary replay. This is a reader/runtime
interaction, not evidence that MTP or every new upstream build is broken.
The resulting narrowed workaround is now listed in the patch ledger. The
[isolation record](hardware-acceptance-history.md#fedora-44-strix-halo-flash-next-state-isolation-2026-10-06)
preserves the original controls and limits of that diagnosis.

The upstream review also distinguishes closed proposals from merged fixes.
[PR 25863](https://github.com/ggml-org/llama.cpp/pull/25863) and
[PR 28136](https://github.com/ggml-org/llama.cpp/pull/28136) were closed without
merging. The newer direct-reader proposal,
[PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030), was also closed
without merging. Its replacement,
[PR 29599](https://github.com/ggml-org/llama.cpp/pull/29599), provides
mmap prefetching, not the buffered explicit-read policy accepted on this host.
[QSA gather](https://github.com/ggml-org/llama.cpp/pull/28213) is closed without
merging, while [pooled QSA keys](https://github.com/ggml-org/llama.cpp/pull/28699)
remains open. These statuses were checked on October 14. Neither a closed
proposal nor a similarly named upstream feature is grounds to remove a guard
without checking equivalent behavior. The newer Flash-Next MTP merge is
covered below and does not enable MTP in the managed preset.
The [current refresh acceptance record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-upstream-update-2026-10-14)
contains the paired ROCm/Vulkan measurements, populated-context checks and
hardware-coverage limits.

The October 4 refresh attempt at `46847e61582097979f539595d893d83d8e1d1af1`
was **held** and is not the current production pin. Upstream replaced the QSA
cache/indexer implementation and reduced indexer-score workspace. The
rebased candidate failed the populated clear-and-restore logit gate even
with upstream attention. Model-free operator tests and short API success
are insufficient to accept it. The
[native-engine trial record](hardware-acceptance-history.md#fedora-44-strix-halo-native-engine-trials-2026-10-04)
preserves the failure, diagnostics and private source archive.
Do not drop downstream safeguards or silently disable all graphs
to make this refresh pass.

The October 6 retry at `6753a033f058fbf778d282556ed9b16c78de7c71`
was also **held**. It included the newer pooled-cache scatter and graph-shape
fixes, but the eight-patch screening build still changed logits after
clear-and-restore with HIP graphs enabled. Disabling graphs passed the
fixture and remains a diagnostic, not an accepted workaround. See the
[retry record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-refresh-retry-2026-10-06).

Flash-Next MTP is now upstream through
[PR 29761](https://github.com/ggml-org/llama.cpp/pull/29761), merged on
October 1. It supersedes the closed, unmerged PR 28243. At Unsloth revision
`766911a6b7369840a91dbcd95f9f997acaab6cd6`, all four managed UD-Q4_K_XL
target shards still have their existing sizes and hashes. Only the three
self-contained MTP files changed, with a duplicate Q8 head also added at the
repository root. The shared heads did not change. Do not follow the stale
MTP README's fork-only requirement or shared-head recommendation as evidence
of current mainline compatibility.

The candidate self-contained Q8 head has a corrected draft-layer compression
ratio in its GGUF metadata. Header inspection is not inference acceptance.
The source refresh does not accept that draft or enable Flash-Next MTP.
It still needs a separate content review, populated-context correctness and
paired performance test. The existing target GGUFs need no new download.

#### Earlier extraction provenance

The following `r40` through `r48` notes describe earlier implementations and
their evidence. The [live patch ledger](../upgrading.md#current-patch-contracts)
is authoritative. In particular,
the private prepared-key cache, prefix checks and selection controls described
below were removed in `r50` and must not be added to current deployments.

The `r44` selection patch builds on the four earlier extractions. The graph
already selected whole blocks, but its ordinary gather, sort, cast and
validity operations still ran separately. The new kernel combines those
operations without changing which blocks or tail cells are selected. It
recognizes the model's sliced-tail view/reshape as well as direct and
scheduler-copied inputs. A model marker, exact shapes, input/output types,
reader checks and physical-range guards constrain dispatch. Unsupported
graphs use the existing operations. All exposed intermediate outputs remain
materialized and live through the fusion.

`PARACETAMOL_QWEN4EXP_QSA_SELECTION=0` inside the container disables both
the GPU fusion and the equivalent CPU singleton check for a same-image
control. The CPU check still rejects shared, foreign, gapped or non-scalar
prefixes. It adds no persistent state. The broader QSA graph policy remains
the prerequisite, and there is no change to model pins, F16 KV, F32 residuals,
matrix arithmetic, sampling or context limits.

The downstream BF16/F16 matrix experiment introduced in `r45` was removed
entirely in `r46`, including its kernels, flag and config setting. The small
quality screen did not show a regression, but it did not establish broad
quality parity either. The supported image retains the precision-preserving
extractions with no user opt-in. Remove `--qwen4exp-math` from any saved
commands and `qwen4exp_math` from `[gateway.llama-cpp]` if you copied either
from the experimental version. Both are rejected rather than ignored.
The retired measurements in
[hardware acceptance](hardware-acceptance-history.md#fedora-44-strix-halo-retired-mixed-arithmetic-experiment-2026-09-30)
are historical evidence, not performance claims for the current image.

The `r43` pipeline patch builds on those three extractions. It adapts direct
GDN and PLE convolution, HC_POST normalization and injection, narrow RMS/gate fusion,
compact expert tiles, expert-ID histogram routing, vectorized weighted
reduction, grouped Q8 decode projections and register-prefetched Q8 matrix
tiles from the same pinned MIT Strix fork. Every fast path requires Qwen4exp
markers or hints and exact `gfx1151`. Unsupported geometry or extra readers
keep the upstream path. Fusion matching protects aliases and allocation
lifetimes. Normalization and expert reductions retain their original F32 sum
order. The fused injection projection uses a tested parallel F32 reduction,
not a lower-precision matrix path. Explicit matrix source precision takes
precedence over the custom Q8 paths.

The patch is `hip-qwen4exp-pipeline.patch`. Its removal gate is an upstream
equivalent retaining operator and fallback checks, populated long-context
retrieval, state restore and interruption, and older-model/backend acceptance.
The Q8 prefetch escape hatch is `PARACETAMOL_QWEN4EXP_MMQ_PREFETCH=0` inside
the container. `PARACETAMOL_QWEN4EXP_HC_INJECT=0` disables the injection
fusion and its graph reordering. The broader graph opt-out disables the pipeline by
withholding model hints and fusion dispatch. Do not fold BF16/F16 matrix
experiments, shadow weights or reduced-precision residuals into this patch.

The `r42` prepared-key cache keeps the first two extractions intact. It adopts
the fork's reuse idea with a smaller local state contract, not its physical-cell
cache or fast-prefill machinery. Only a verified append of one token reuses
keys. The last complete block and tail sentinel are refreshed with the same
ordered F32 operations. Prefills keep their original graph without repeated
copies into derived storage. The first decode then prepares all keys normally
and seeds the cache. The cache is excluded from saved sessions and invalidated on
restore. It adds about 384 MiB at the current Flash-Next 256K ceiling, with no
change to the GGUF, F16 KV, residual precision, samplers or context limit.

The September 29 `r41` follow-up keeps the first selected-key patch intact.
It adds a narrowly scoped extraction from the same pinned Strix fork, not
another engine or a replacement source tree. Compact selection uses a budget
of complete blocks plus the partial tail, matching the model reference rather
than taking a fixed number of independently expanded token scores. F16 K/V,
stored GGUF weights, F32 residuals, sampling and allocation policy are unchanged.
That `r41` extraction excludes the fork's BF16 matrix/residual paths, persistent
pooled-key cache and global kernel tuning. See the [hardware record](hardware-acceptance-history.md#fedora-44-strix-halo-qwen4exp-graph-optimizations-2026-09-29)
for measured gains and the remaining performance gap.
The compact graph also requires at most 262140 padded KV cells because the
prefill kernel reserves a 16-bit block sentinel. A larger padded cache keeps
the ordinary graph rather than overflowing that representation.

The September 29 `r40` image keeps the llama.cpp pin and model presets fixed.
It adds only the gfx1151 HIP selected-key attention path for Qwen4exp and
does not import the fork's broader indexer, cache, or model-loading changes.
The scorer-strip variant was measured separately and discarded because its
129K result was indistinguishable from the kernel-only result. Recheck this
patch against upstream QSA development during the next llama.cpp refresh.

The September 25 `r39` image keeps llama.cpp pinned at `7ab4ee7` and adds
only the QSA gather patch. No model, quantization, sampling, context or MTP
default changes. The matched Strix Halo test uses the same image with the
gather path enabled and disabled; see the
[hardware record](hardware-acceptance-history.md#fedora-44-strix-halo-flash-next-qsa-gather-2026-09-25).
The larger direct-reader rewrite in
[PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030) was then under
active review and changed lazy-read semantics beyond this model. The pooled
QSA key cache in [PR 28699](https://github.com/ggml-org/llama.cpp/pull/28699)
changes persistent inference state. Neither is included in `r39`, and
Flash-Next MTP remains disabled.

The September 22 update to
`7ab4ee7baad2d920464cbacfad4f4b07cf111fd2`, packaging revision `r38`, reviews
186 upstream commits. Five patches apply unchanged. The reasoning patch
only moves past new server-process includes, and the tiled GDN test hunk
only follows an upstream test-class rename. Neither changes its protected
behavior, and all seven patches remain.
The [Strix Halo acceptance record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-7ab4ee7-update-2026-09-22)
includes the paired ROCm/Vulkan results, the small Q8 long-prefill slowdown,
Flash-Next gains, client checks and deferred hardware coverage.

The range includes Muse's first-tool-call grammar fix in
[PR 29242](https://github.com/ggml-org/llama.cpp/pull/29242), RDNA 3.5 MoE tile
selection in [PR 28935](https://github.com/ggml-org/llama.cpp/pull/28935),
MTP graph reuse in [PR 28549](https://github.com/ggml-org/llama.cpp/pull/28549),
and Qwen4exp hyper-connection and normalization fusions. Sparse Flash
Attention is enabled for Qwen4exp, with a new Vulkan implementation. The
CUDA sparse kernel still explicitly excludes HIP, so its advertised gain
must not be attributed to ROCm. Server child-process monitoring and the
shared JSON-schema representation also change. Recheck router switching,
shutdown, nested tool arguments and managed sampling rather than treating
this as a kernels-only update. This pin is preparation for new models, not
acceptance of unreleased Qwen4 artifacts.

The September 22 external review leaves these proposals outside the pin:

- [PR 29030](https://github.com/ggml-org/llama.cpp/pull/29030) rewrites the
  direct-reader proposal already carried from PR 28136. Its ownership and
  parallel-read design remain under review. A replacement must retain the
  existing EOF, dequantization and per-process policy checks.
- [PR 28243](https://github.com/ggml-org/llama.cpp/pull/28243) is the newer
  Flash-Next MTP proposal. Detached draft loading still has outstanding
  review discussion. Target-model acceptance does not enable this draft path.
- [PR 28699](https://github.com/ggml-org/llama.cpp/pull/28699) caches QSA
  pooled keys. It changes persistent inference state and needs isolated
  rollback, prefix-reuse and long-context qualification.
- [PR 28613](https://github.com/ggml-org/llama.cpp/pull/28613) changes RDNA
  3.5 small-batch thresholds, but reviewers report shape-dependent
  regressions. Do not apply its benchmark gains to every model.
- [PR 26419](https://github.com/ggml-org/llama.cpp/pull/26419) now limits
  its head-dimension-256 dispatch change to RDNA 4. That is not a new Strix
  Halo optimization or evidence that its earlier regression disappeared.

The host-buffer PR 25863 was closed without merging. Upstream points to the
conservative integrated-device rollback in PR 28604, already in our previous
pin, and the still-draft scheduler work in PR 27311. This does not satisfy
the ledger's cross-APU removal gate.

The `r37` packaging revision adds the two model-scoped patches without moving
the source pin. `test-native-policy.cpp` runs during image building without a
GPU or model weights. It checks server/CLI preset isolation and 50 direct-reader
reference/EOF cases. CPU startup must also exercise the non-root managed
entrypoint, not only the native binary, so bundled helper permissions are
covered. Flash-Next's ROCm policy is restricted to Strix Halo. Other models'
allocation and reader settings remain unchanged. The [managed integration
follow-up](qwen3.8-flash-next-direct-reader-feasibility.md#managed-rocm-integration)
supersedes the historical Vulkan-only restriction described below.

The September 13 [isolated tiled GDN screen](qwen3.8-tiled-gdn-strix-halo-feasibility.md)
adds one patch without changing the source pin, ROCm, model artifacts or
Flash-Next's managed Vulkan-only policy. Numerical regression cases live in
the same patch. Enable upstream `LLAMA_BUILD_TESTS`, build
`test-backend-ops`, and run `test -b ROCm0 -o
GATED_DELTA_NET,GATED_DELTA_NET_CACHE_FUSION` against the actual ROCm device
when requalifying it. Check the executed case count as well as the exit
status. A normal production build does not run those GPU tests.

The 2026-09-11 update to `8172e6577ac2b35de1ec1e5d1c0aaad6c4a2129f`
retains the host-buffer, reasoning-controls and HIP quantized-KV patches
unchanged. The Vulkan F16 patch needs only a rebase of its surrounding shader
table context. None of the four protected behaviors has an accepted upstream
replacement. The 32-commit range includes RDNA Flash Attention tuning in
[PR 28102](https://github.com/ggml-org/llama.cpp/pull/28102), removal of an
unused indexer V allocation in
[PR 28330](https://github.com/ggml-org/llama.cpp/pull/28330), and Vulkan
matrix, MoE and argsort fixes. Speculative-decoding position handling and MTP
cache filtering also change, so retained speculative presets need real
server and router regression checks. Open Flash-Next MTP, gather and pooled
indexer-cache proposals are not included in this pin.
The [September 11 memory-policy screen](hardware-acceptance-history.md#flash-next-rocm-memory-policy-screen)
also distinguishes the failing normal Flash-Next ROCm path from a coherent
resident-loading experiment. It does not remove the managed Vulkan-only
restriction or change other presets' allocation policy.

The 2026-09-09 update to `6d9c82ea2bb34e277c0664b8dd3434bfb4dcfb27`
rebases the reasoning, quantized-KV, and Vulkan-contiguization patches. The
HIP quantized tile calls now pass upstream's new sparse-attention argument
explicitly as false. The host-buffer patch still applies unchanged. Upstream
[PR 28604](https://github.com/ggml-org/llama.cpp/pull/28604) conservatively
disables the internal integrated-device flag again, which also prevents the
unsafe direct host-buffer computation. The local guard and pinned-allocation
handling remain in place pending the full cross-APU removal gate.

Upstream also enables reasoning-history preservation by default. Both direct
launches and router presets now explicitly apply the catalog's boolean policy
instead of inheriting that default. Qwen3.6 still drops earlier reasoning,
while Qwen3.8 and Muse keep it. This is distinct from the request's thinking
level and from sampling defaults.
The [Strix Halo acceptance record](hardware-acceptance-history.md#fedora-44-strix-halo-llamacpp-6d9c82e-update-2026-09-09)
includes the fixed ROCm/Vulkan comparison, default Q8 MTP control, API and
client checks, and deferred architecture handoff. The observed Vulkan
Q8_0-cache prefill regression is recorded separately from the F16 improvement.

The 2026-08-14 update from llama.cpp commit `62bf73d` to release `b10430`,
commit `4c1a0af`, initially classified all four then-current patches as
**rebased**. None was replaced upstream. A subsequent native-reasoning audit
replaced `reasoning-effort-budget.patch` with `reasoning-controls.patch`; the
other three retain that update classification. The 81-commit range changed
adjacent server, speculative-decoding, and backend code, including upstream
Vulkan TQ2 support, but did not provide the protected host-buffer, reasoning
transport, quantized-KV, or f16-contiguization behavior. Exact range comparison
and fail-closed patch application passed before the retained patches were
exercised on `gfx1151`. The host-buffer and reasoning changes still track the
open upstream pull requests linked in the ledger. Re-run the removal gates
rather than carrying this classification forward to a later pin by assumption.

The 2026-08-17 update from release `b10430`, commit `4c1a0af`, to release
`b10453`, commit `3cb7ffb`, classified the host-buffer, quantized-KV, and f16
contiguization patches as **unchanged** and the reasoning patch as **rebased
and reduced**. Upstream now parses top-level `reasoning_effort` and its
template-capability layer aliases both native effort and strength names. A
follow-up removed the remaining Maki numeric-budget decoder rather than
guessing native labels from output-window-dependent percentages. The patch
now owns only data-driven sampling defaults; Muse's unsupported off-to-low
fallback is scoped to its reviewed template. The 23-commit range also includes
a quadratic Jinja rendering fix and a server queue redesign; exercise
structured tools, reasoning variants, router concurrency, and latency rather
than treating a successful build as acceptance.

The 2026-08-26 update from release `b10453`, commit `3cb7ffb`, to release
`b10631`, commit `5d5cb4c`, classified `hip-apu-host-buffer.patch` as
**unchanged**, `reasoning-controls.patch` and
`vulkan-f16-kv-contiguize.patch` as **rebased**, and
`quantized-kv-flash-attention.patch` as **partly replaced upstream and
reduced**. Upstream commit `dc72703fc` now provides the Vulkan q8_0
dequantize-and-transpose path, with its own dense-layout, buffer-range,
coopmat2, and Intel gates. Paracetamol removed those duplicate Vulkan hunks
and retains only the distinct HIP tile q8_0/q4_0 path. Upstream commit
`60addddf3` corrects HIP unified-memory reporting but does not replace the
integrated-GPU host-buffer safety patch. The source still has no data-driven
per-reasoning-mode sampling defaults, so the reasoning patch remains and now
uses the bounded `common_json` request API. The f16 Vulkan path now extends
upstream's shared transpose scratch implementation and remains opt-in on
`gfx1151`.

The 2026-08-29 update from release `b10631`, commit `5d5cb4c`, to commit
`c9ca51c1f6b18427cde490c7c7eba11d87a96b2d` retained all four patches
**unchanged**. Each exact patch applied to a clean source tree and the complete
ROCm and Vulkan image build passed. The selected source contains upstream
Qwen4exp target-model support from
[pull request 27742](https://github.com/ggml-org/llama.cpp/pull/27742), which
is required for Qwen3.8 Flash-Next. Its
[separate MTP work](https://github.com/ggml-org/llama.cpp/pull/27836) remains
outside upstream, so the managed Flash-Next preset is intentionally
non-speculative. Recheck that boundary before adding an MTP alias rather than
assuming architecture support also covers the draft heads.

The 2026-09-01 update from commit `c9ca51c` to commit
`0eadefebd3f8f92a86d634a0e5b8fffc9dc792c0` retained all four patches
**unchanged**. Each exact patch applied cleanly to the selected 48-commit
range. Upstream renamed `--tensor-read-lazy` to `--lazy-mode`, so the managed
direct-model and router configuration emit the new spelling while preserving
the same lazy token-embedding policy. This range adds Qwen4exp indexer-head
prompt-processing work, ROCm long-row radix top-k, Strix Halo Vulkan batched
inference tuning, and recurrent-state rollback for Qwen3.8 Flash-Next. The
rollback support removes a target-side speculative-decoding bottleneck, but
does not add a draft-model loader. Flash-Next MTP remains in
[draft pull request 27836](https://github.com/ggml-org/llama.cpp/pull/27836)
and is not part of this pin. Keep base-model ROCm acceptance and MTP acceptance
as separate gates.

At the September 1 pin, Flash-Next generation on `gfx1151` was accepted only
through Vulkan. Ordinary ROCm allocation loads the same shards and can return
short retrieval keys, but meaningful responses become malformed multilingual
text at shallow context.
The failure survives resident and lazy loading, deterministic sampling, one
server slot, the GGUF and managed templates, a clean GPU reset, ROCm 7.14 and
10.0 images, and a build without Paracetamol's llama.cpp patches. Vulkan is
coherent with the same image, GGUF, prompt, and preset. This agrees with
[upstream issue 27797](https://github.com/ggml-org/llama.cpp/issues/27797) and
the independent [Unsloth ROCm report](https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/discussions/38).
The later `r37` model-scoped allocation and reader policy has its own
acceptance gate. It does not fix the ordinary ROCm allocation path.
Do not use successful loading, synthetic throughput, or a short-key
retrieval as evidence that the ROCm output path is fixed.

An isolated same-day evaluation rejected open pull request
[#26419](https://github.com/ggml-org/llama.cpp/pull/26419) as a downstream
patch for `gfx1151`. Its head-dimension-256 AMD WMMA Flash Attention path
reduced Qwen3.8 Q8 F16-KV prompt throughput by 5.1% at 4K, 21.7% at 32K, and
32.9% at 128K while leaving decode unchanged. Do not infer Strix Halo benefit
from the pull request's RDNA 4 results. The exact candidate, controls, early-
stop decision, and retest gate are preserved in the
[feasibility snapshot](qwen3.8-rdna-head-dim-256-flash-attention-strix-halo-feasibility.md).

The bundled `muse-glimmer-atem.jinja` derives from Meta's 9,992-byte template
at base-model revision `a4e59da52a7bc87ae7251dd5545c0dd437c44b68`, SHA-256
`cfc67e5f349f37690dfd31ed1f18bc4442a9dd32fe39a648f993cb4eb3cae678`.
Paracetamol's 10,221-byte reviewed derivative has SHA-256
`7802290a21155a7966436866dc34c06293719eeccdace751ded893cb752f39b1`.
It retains Meta's duplicate-directive correction and adds one scoped policy:
because Muse cannot disable reasoning, `enable_thinking=false` renders native
low instead of changing another model through a server-wide fallback. Meta
later repacked the official GGUFs at
revision `43c7eadd41352a299ea8e0a36b3157978dd63596` with this fixed template and
canonical Q4_K filenames. The dynamic target and DFlash candidate at that
revision retained their tensor inventories and byte-identical tensor payloads;
the only changed GGUF metadata key was the chat-template value. Paracetamol
therefore did not replace the behavior-equivalent target and draft merely to
acquire the embedded copy. On upgrades, compare the base-model template, GGUF
metadata, and tensor payload independently, then repeat direct and router
rendering plus a complete structured tool round trip before changing or
removing the override.

Qwen3.6 and its dedicated template were retired on 2026-10-16. Historical
acceptance records remain available, but there is no current Qwen3.6 policy
to rebase or preserve.

The bundled `qwen3.8.jinja` is an Apache-2.0 adaptation of Qwen's
byte-identical official templates at Qwen3.8-27B revision
`1d4bf0f2ff6012fd82039f2fa52739d0dd7c60c0` and Qwen3.8-Flash-Next revision
`de4b8e4d43b917e7706784d8bb445c9af86a3540`. The source template has SHA-256
`c3cf9e34abf4f9e36c2d72165aa9c132d3e2a725b6c2586aaa3a8af9d7a81041`;
the managed template has SHA-256
`eec13618752e4cc957e65efca4c052aac43683bfae884f07299022a1322d0e9a`.
The only prompt-policy change is the omitted-effort fallback from upstream
`xhigh` to native `medium`, with the validation message updated to identify the
managed default. Qwen3.8 supports native low, medium, and xhigh effort plus a
separate off toggle; it does not support high. On an upgrade, compare both the
base-model and selected GGUF templates, render all three levels plus off, and
complete a multi-turn tool exchange before changing the override or default.
The Qwen3.8 presets reference the catalog's `qwen3.8-27b` sampling policy:
thinking requests default to temperature 1.0, top-p 0.95, top-k 20, min-p 0,
presence penalty 0, and repeat penalty 1; off defaults to temperature 0.7,
top-p 0.8, top-k 20, min-p 0, presence penalty 1.5, and repeat penalty 1.
Recheck both official tuples and explicit-request override precedence whenever
the model card, template, catalog policy, or server request parser changes.
Flash-Next uses its separate `qwen3.8-flash-next` policy. It changes thinking
min-p from 0 to 0.05 while retaining the other Qwen3.8 thinking and
non-thinking defaults. Recheck the two families independently.

Huihui's Unsloth-derived abliterated v2 uses that same Flash-Next policy and
template. Keep its preset policy identical to regular Flash-Next apart from
the artifact and bundle identity. Compare every GGUF header and tensor layout,
then run reasoning, tools, history, sampling and long-context checks on the
actual modified weights. The regular model's successful tests do not establish
the derivative's quality. Harness preference for regular Flash-Next at xhigh
is separate from the template's medium fallback and must not leak into raw
HTTP requests that omit reasoning.

The 2026-08-14 community-template candidate at
`froggeric/Qwen-Fixed-Chat-Templates` revision
`9f14778c92c3b5ed3e0738085694c0d3452802dd` has a 19,262-byte
`chat_template.jinja` with SHA-256
`398edf5b5bb802fb6b9c9a8dba670d09f2aaeef6fdcaa0b2ca307265f59f78dc`.
Its model card claims broader local-engine, history, role, tool-argument, and
agent-loop fixes, but the template also introduces control tags, failure
escalation, payload truncation, and other prompt policy. Treat it as a pinned
experimental A/B input rather than an in-place repair or an upstream source of
truth. Reproduce a concrete failure with the managed official adaptation
before evaluating it, and change only the template in that comparison.

The bundled `qwen3-0.6b.jinja` is byte-for-byte the `chat_template` value from
Qwen base-model revision `7e4ae267688d671ddfca3122e4528ee980cf3234`, SHA-256
`a55ee1b1660128b7098723e0abcd92caa0788061051c62d51cbe87d9cf1974d8`.
The unchanged official GGUF predates its safer handling of non-string message
content, reasoning values, and tool responses. This remains a small smoke
model rather than a managed coding agent. On an upgrade, render both ordinary
text messages and the hardened non-string history before removing the
override.

The September 22 content audit found no changed bytes in the selected Qwen
GGUFs, including all four Qwen3.6 artifacts, both Qwen3.8 Dynamic Quants and
all four Flash-Next shards. The selected Gemma, KAT, translation, Shisa and
DeepSeek target/support files are also unchanged. Muse's current filenames
still point to the already-reviewed template-only repack described above.
Its base template and the official Qwen3.8 templates have not changed.
Keep the existing immutable artifact revisions and managed overrides rather
than invalidating receipts for newer repository metadata alone.


## Historical dense Qwen DFlash2 retest trigger

The [2026-08-23 Qwen3.8 DFlash2 Strix Halo
snapshot](qwen3.8-dflash2-strix-halo-feasibility.md) deliberately left the
then-open DFlash2 implementation out of the source pin and catalog. Its
Q4_K_M draft produced a useful near-256K Vulkan result, but ROCm draft
acceptance collapsed with both Q4_K_M and Q8_0. A future llama.cpp update that
merges or materially changes DFlash2 is therefore a retest trigger, not
permission to integrate it. Follow the snapshot's backend, context, quality,
and fail-closed promotion gate before adding a managed preset.
