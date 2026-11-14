# Strix llama.cpp reverse-ablation screen

This is a dated research record from 2026-11-14, not a managed application,
production upgrade or recommendation to adopt the full fork. The investigation
asks which Strix-fork changes can survive unchanged Paracetamol correctness
gates before any performance comparison or selective extraction.

**Current outcome: hold the full-fork performance matrix.** The freshly
reproduced production control passes all six frozen nonthinking replay cases;
the full fork passes none. A coarse removal of reviewed precision-changing
families also passes none. Disabling fusion on that coarse control recovers
five cases, but seed 7 still fails. This is an unresolved replay failure in this setup,
not proof of an incorrect mathematical operation or a general model-quality
verdict. No fork speed claim follows. Separately extracted Q4 scheduling
candidates pass bounded correctness checks. The widening-only gain reverses in
confirmation, while prefetch has no independent confirmation. No source patch
or runtime setting is adopted.

## Comparison boundary

Physical testing covers Strix Halo `gfx1151` with ROCm 10.1 only. All model
requests use the ordinary gateway and the same receipt-verified regular Unsloth
Flash-Next UD-Q4_K_XL files. The plain preset disables speculation. Projector,
chat template, F16 K/V, four configured slots in a shared 262144-token pool,
batch 2048, microbatch 2048 and the direct embedding-reader policy stay fixed.
Configured context and slot capacity do not establish populated long-context
or concurrent-generation acceptance.

The accepted production control is upstream
`ded522f8304554317e7ff8f88698dbbbca5425aa`, patched tree
`aeeb3f45e00deae964a7949da69f1389fa98176a`, and locally built
`llama-cpp-ubuntu26.04-rocm10.1-ded522f-r77`, image ID
`0de2f71f6181e4fe61f1b03c3d2dc69adc532b860394d972d10e5065b78ffdaa`.
Its earlier acceptance and limitations remain in the
[prefill recovery record](hardware-acceptance-history.md#strix-halo-flash-next-prefill-recovery-2026-11-14).

The exploratory full-fork source is
[`c495f90f55e647f9a5b6820b503be6d39c74baa7`](https://github.com/halo-box/strix-llama.cpp/commit/c495f90f55e647f9a5b6820b503be6d39c74baa7),
with unmodified fork tree `62e1f3053c6276d0b61e4c666d1df33082bfe9b3`.
The tested image adds only the existing process-allocation startup glue, rather
than selectively importing the production patch stack. The build uses the
current r77 ROCm SDK and runtime, with HIP compilation limited to `gfx1151` for
this exploratory image. It is not a normal all-target managed build.

The fork's recorded upstream content base is
`86ea01d05ec237f89b78b41c8c1ee0f908141ac7`, not the
production `ded522f` source. A shared compiler and runtime do not make this a
same-base engine comparison. The full fork also retains its own internal
precision choices despite unchanged model bytes and F16 KV. Neither the source
identity nor a successful build establishes inference acceptance on any GPU.

No production image, controller selection, model installation, host power,
kernel or memory policy is changed for this investigation. Other managed GPU
classes and Vulkan remain untested by this record.

## Correctness gates before timing

The first model gate is the existing frozen six-case nonthinking replay:
five managed-sampler cases with seeds 42, 1234, 2026, 7 and 99, plus its existing
explicit temperature-zero control. Requests retain the original output budget,
natural EOS, exact answer oracle, cache policy and sampling provenance.
Changing a sampler or stripping an unwanted output tag is not a passing retry.
The unchanged answer predicate ignores surrounding whitespace, not embedded
tags, duplicated values or explanations. These are oracle passes, not a claim
that every successful response has byte-identical whitespace.

The gateway's response, native verbose response and rendered prompt are retained
independently. The rendered native prompt SHA-256 matches the existing fixture:
`7f18f20c01fec10f8b85200851e7e5ab44f4808decba0488158b4b1a86a2da7d`.
This rules out a different rendered prompt in these observed cases; it does not
establish equal intermediate numerical execution across engines.

All arms retain the rootless ordinary-gateway lifecycle, exact selected render
node, read-only content and container confinement. The live 8 GiB available-memory
floor, kernel/GPU/OOM checks, exact owned-process cancellation and gateway-owned
backend cleanup remain mandatory. A guard stop, hardware fault or cleanup leak
would fail the arm rather than justify a smaller context or weaker gate.

## Completed replay observations

The following are fresh plain-target observations from this investigation,
not inherited passes or a DFlash acceptance result.

| Arm | Frozen-oracle passes | Observation |
| --- | ---: | --- |
| Production r77 | 6/6 | Fresh accepted control reproduces the frozen answers. |
| Full fork | 0/6 | Extra closing reasoning tag and duplicated answer value, including the temperature-zero case. |
| Coarse precision-family removal, C0 | 0/6 | Failure persists. One response includes a closing `</result>` tag and self-correction variant. |
| C0 plus fusion disabled only | 5/6 | Seed 7 still fails. Recovery is incomplete. |
| C0 plus graphs disabled only | 0/6 | Same class of symptoms as C0. |

C0 disables the reviewed family comprising MMB/hidden-state marks, F16 graph
casts, F16 chunked GDN and the separate compensated-BF16 MMA fallback, the HC16
shortcut and the dead-`xn` path. It is a coarse combined control, not proof that
every possible precision-changing fork path is absent or that its remaining source
is equivalent to production. The fusion-only and graphs-only controls each
start from C0; they are not a cumulative disable-everything stack.

Native verbose output agrees with the API-visible failures. The observed bad
answers stop at natural EOS rather than merely exhausting their output limit.
No hardware fault, memory-guard stop or cleanup fault occurs in these completed
arms. Those observations distinguish this failure from transport rewriting,
truncation and the monitored infrastructure failure classes; they do not prove
the correctness of any kernel or identify the first divergent operation.

The partial fusion-disabled recovery makes interactions worth investigating,
but it does not identify a single defective fusion. Graph disabling alone does
not recover the replay. A passing subset of seeds is not an accepted control,
and a deterministic temperature-zero failure is not an independent arithmetic
reference.

## Relation to the earlier HIP MMQ investigation

The similar duplicated-answer symptom must not be treated as proof of the same
cause. The full fork already carries the older MMQ activation-sum behavior and
the older scalar behavior. Production r77 carries the older HIP MMQ sum through
its temporary compatibility patch but retains the corrected scalar behavior.
The engine stacks and source bases also differ.

The earlier matched investigation associated a replay transition with upstream
`8fc0376` on its own frozen stack. This experiment does not show that the same
introduction or mechanism explains the fork failure, nor that reverting that
commit is an appropriate fork fix. The
[earlier numerical investigation](hardware-acceptance-history.md#internal-numerical-mechanism)
already separates output-trajectory sensitivity from demonstrated incorrect
arithmetic. Its unchanged compatibility-patch removal gates remain in force.

## Performance results and limits

The full fork fails before the 32K gate. Its planned prefill, decode, concurrency,
DFlash and populated-context performance matrix is therefore held. No timing
ratio, speedup estimate or quality-equivalent fork performance claim is reported.

A separate production-only screen changes logical batch from 2048 to 8192
while keeping microbatch 2048. The run order is 2048, 8192, 8192, 2048, with a
fresh process and cold KV for each arm. Every request processes 32,937 uncached
input tokens, returns the same six values in 62 output tokens and passes its
natural-completion and runtime-policy checks. Model loading precedes timing;
the filesystem cache is not deliberately emptied.

| Logical batch / microbatch | Native prefill observations | Aggregate prefill rate |
| --- | --- | ---: |
| 2048 / 2048 | 746.7 and 766.6 tokens/s | 756.5 tokens/s |
| 8192 / 2048 | 755.1 and 755.1 tokens/s | 755.1 tokens/s |

Aggregate rates divide total input tokens by total native prompt time.
Summed reported request wall time, including decode and report polling, is
92.70 versus 92.91 seconds. This bounded screen provides no reason to change
the 2048 default. It is not a concurrency,
sustained decode or full-fork comparison.

The original 2048/2048 policy allows at most one full-sized microbatch per
prompt-processing call in this single-request screen. The fork's
asynchronous PLE reader cannot look ahead into the next call, so its intended
next-microbatch overlap needs a larger logical batch. That interaction remains
untested in the fork because its preceding correctness gate fails. The
production-only result neither accepts nor disproves that mechanism.

### Independent J48 and activation-prefetch screen

Source review identified a coupled change worth testing directly on accepted
r77, without the fork's arithmetic or scheduler. It widens the existing Q4_K
expert tile from J32 to J48 and enables activation-Y register prefetch for that
cell. It does not prefetch Q4 weights through a new dequantizer. The current
pipeline already contains the ordinary Q4 weight-loader branch.

Three controls keep the accepted folded seam traversal and convolution fix:
original J32, J48 without prefetch, and J48 with prefetch. Selection occurs
before J-dependent padding and descriptor production. It is restricted to the
hinted `gfx1151` Q4_K 512-expert/10-used shape at exactly 2048 tokens, without
precision overrides, matrix fusion or fallback layouts. Other types and partial
microbatches keep the original path. Quantizers, scales, auxiliary sums and
per-output K traversal remain unchanged.

All three source variants compile with the four configured HIP targets and
Vulkan. On physical `gfx1151`, ten activated comparisons independently rebuild
the original and wider routing descriptors and launch all three paths on the
same tensors. Outputs are finite and bit-identical. All nineteen cases retain
the independent CPU reference and unchanged `5e-4` NMSE limit. These include
actual projection dimensions, multiple K chunks, broadcast and selected-expert
layouts, skewed expert counts and partial expert-row tails. Nine refusal cases
confirm that unsupported shapes, types, hints and precision overrides decline
the new selection. The unchanged 26-case MoE suite also passes.

Both normal candidate images pass the original six-case model replay. The
diagnostic image used for numerical comparisons is never used for timing.
Its first launch stopped before GPU work because copied input metadata was
root-only. A separately identified image
corrects only those metadata permissions, verifies unchanged binary/library
digests and retains every original numerical gate and the failed evidence.

The first unprofiled timing block uses the order J32, J48, J48+prefetch,
J48+prefetch, J48, J32. A separate confirmation block compares the simpler J48
change against J32 in ABBA order. All ten requests retain the same frozen
32,937-token uncached prompt, correct six-value answer, 62 output tokens,
natural EOS and runtime/sampler checks.

| Block | Variant | Observed prefill tokens/s | Aggregate tokens/s |
| --- | --- | --- | ---: |
| Initial | J32 baseline | 734.7, 753.1 | 743.8 |
| Initial | J48, no prefetch | 754.6, 765.1 | 759.8 |
| Initial | J48 with prefetch | 747.1, 778.9 | 762.7 |
| Confirmation | J32 baseline | 752.3, 744.1 | 748.2 |
| Confirmation | J48, no prefetch | 726.2, 748.9 | 737.4 |

Widening alone initially gains about 2.2%, then loses about 1.4% in confirmation.
Pooling its four observations against the four matched baseline observations
leaves about 0.3%. Prefetch adds only about 0.4% over widening in the initial
block and has no independent confirmation. These samples do not establish a
repeatable adoption benefit or prove that every possible workload is slower.
Keep the accepted J32 path rather than selecting the best observed rate.

Separate profiler overlays preserve native binary and library bytes. Both
actual model-child traces contain 1,380 calls to the intended folded Q4_K/J48
kernel, with the correct prefetch flag and zero calls to the opposite variant.
Thus this is not merely a source change that never activates. Traces cover the
child lifetime, including initialization, READY warmup, the request and decode.
Their timings are excluded from the release-performance comparison.

No hardware, memory-guard or cleanup fault occurs in these completed candidate
arms. Populated near-256K, concurrency, coding and full DFlash acceptance are
not run for the candidates because the bounded timing screen does not justify
promotion. Their passing numerical and short replay checks do not replace
those later requirements.

## Reverse-ablation and extraction method

The [maintenance procedure](../llama-cpp-maintenance.md#investigate-a-regression)
and [patchset rationale](../architecture.md#upstream-base-and-broken-out-patches)
remain the reusable workflow. This screen adds two practical lessons.

Remove dependency-valid groups, including their producers, consumers, dtype
marks and dispatch guards. Disabling one advertised switch need not remove a
hardcoded backend path. Conversely, deleting a precision family can also lose
its associated fusion and scheduling, so the timing delta would not be a pure
precision attribution. Independent reversals and cumulative removals answer
different questions.

When the full fork fails its first gate, do not use its speed as an accepted
reference. A source-identified mechanism can still be evaluated independently
on the accepted base, with numerical references, actual kernel activation,
unchanged model oracles and counterbalanced timing. That is what the J48
follow-up does. A correct kernel and a worthwhile end-to-end optimization are
separate findings.

The current C0/fusion/graph observations support investigating interactions,
not adopting a wholesale fork or declaring a global model-quality regression.
The baseline, actual artifact policy and failed replay must remain available
alongside every reduction.

## Evidence and resumption boundary

Private immutable kits, source/build identities, requests, native responses,
reports, policy snapshots, guard observations and cleanup results remain under
ignored `build/strix-reverse-ablation-20261114/`. Separate runtime and policy kits
preserve the original 2048/2048 screen and new 8192/2048 experiment without
rewriting earlier evidence. Private machine identifiers, addresses, paths,
raw prompts and logs are not published in this record.

This bounded screen is complete, without a production change. Revisit it when
a fork correction clears the original replay or a new, specific mechanism
justifies another experiment. The full-fork performance difference remains
unmeasured here, not explained away by the negative J48 result.

Before resuming, inspect the retained manifests and ownership state rather
than inferring the newest image or process from a tag or an old PID. If evidence
is unavailable, state that limitation and create a new versioned fixture. Do
not claim an exact replay from this prose alone. The retained kits are not a
supported engine or new public command surface. This screen left the
then-current `ded522f-r77` production image unchanged. The later
[pool/reset follow-up](hardware-acceptance-history.md#strix-halo-flash-next-pool-and-reset-corrections-2026-11-14)
has separate source changes and acceptance evidence.
