# Qwen3.8 DFlash2 llama.cpp Strix Halo feasibility snapshot

This maintainer research record covers the October 23 Flash-Next evaluation,
its optional-preset follow-up and the original August 23 dense-27B experiment
on Fedora 44 Strix Halo. The two architectures use different draft models.
The regular non-speculative Flash-Next remains the default.

This document is historical, machine-specific evidence rather than a current
support or performance declaration. Inspect `Containerfile`, the catalog,
source, and current hardware-acceptance records for active policy.

## Optional greedy preset follow-up (2026-10-23)

**Greedy DFlash2 is an optional choice, not the new default.** Regular
Unsloth Flash-Next remains the recommended model at xhigh. Probabilistic
drafting is not exposed. The initial measurements below still apply to its
tradeoffs. Greedy helps some single-request decode work, does not accelerate
prefill, leaves about 5 GiB less available memory and loses throughput with four
parallel clients in the short xhigh screen.

The optional IDs append `-dflash2` to the regular Unsloth and Huihui
abliterated-v2 Flash-Next IDs. Both reuse their existing target shards and one
shared Inco Q8_0 draft pinned below. They preserve the target template,
reasoning preservation, gateway-owned missing sampler defaults, normal matrix
precision, F16 KV and the 262144-token pool. They explicitly select greedy
proposals at depth three. They are accepted only on ROCm Strix Halo, not
Vulkan, RDNA 4 or Strix Point. Installation and selection are described in
the [application guide](guides/applications.md#optional-greedy-dflash2).

### Managed-path checks

The follow-up uses image `280141e-r61`, ID
`34565dee597fbdfcd482a3abdda56a9813a51b5f5ad6af46a29db5ba6ed8ef73`.
The llama.cpp commit and thirteen patches are unchanged from the initial
evaluation. The server, CLI, benchmark binaries and every regular file under
the native library directory are byte-identical to `r60`. The image change
packages the reviewed entrypoint policy, not new inference kernels.

Unlike the initial experimental arms, these checks select ordinary managed
catalog presets through the gateway without editing a generated router file.
Normal installation verifies the shared draft and reuses existing target
receipts. Inventory and client tests withhold the optional choices when the
draft is missing or unverified and preserve the ordinary model preference.
Direct-router discovery also permits installing either target family alone
with the shared draft. Missing optional content is not made mandatory, and
partial or stale target installations still fail closed.

The regular managed greedy preset also repeats the complete ten-request
short matrix successfully, including replay of its own assistant histories.
The same two object-form tool-choice fallback warnings remain. No experimental
INI hook is used in this follow-up.

Its populated 32K regression check also passes both six-key retrievals and
the changed-history answer with the same input and cache counts used below.
The fixed-256 HTTP requests take 12.580 and 9.380 seconds, with 22.077 GiB
minimum sampled available memory. These are representative managed-path
acceptance measurements, not a fresh paired `r61` speed comparison. The
earlier matched `r60` comparison is retained separately below.

### Independent Huihui checks

The unchanged Huihui target and its greedy counterpart each pass the complete
ten-request short matrix. This includes thinking off and xhigh, nested tools,
replay of their own full assistant messages, streaming and live prefix reuse.
Both retain the pre-existing object-form `tool_choice` fallback warning.
The observed sole-function round trip is not a claim of complete forced-tool
API support.

A separate ten-seed replay uses one unchanged Huihui assistant/tool history
per mode. Only the model selector changes. Both return the exact tool value
in all ten sampled answers. Native rendered prompts match byte for byte,
all 454 input tokens are evaluated without cache or truncation, and the six
samplers match the omitted-client-field gateway policy. This is one small
replayed item, not twenty independent quality tasks or a guarantee against
hallucination.

At populated 32K, both modes recover all six keys twice and correctly return
the changed value after a separate history edit. Actual inputs are 32977
tokens for xhigh retrieval and 32935 for the medium edit. The repeat evaluates
four tokens, while the edit fully reprocesses its input. Requests, controls
and native prompt hashes match between modes.

| Huihui fixed-256 measurement at 32K | No speculation | Greedy DFlash2 |
| --- | ---: | ---: |
| First request HTTP wall | 15.099 s | 13.160 s |
| Identical cached repeat HTTP wall | 11.888 s | 9.983 s |
| Minimum sampled available memory | 27.086 GiB | 22.051 GiB |

The cached repeat takes about 16% less wall time. Each timing answer is
deliberately limited to 256 tokens, including reasoning. It is not a completed
task or quality pass. These single observations do not establish a stable
speedup or show that the shared draft fits every abliterated target equally
well.

The greedy Huihui preset also passes cold and cached six-key retrieval at
247210 actual input tokens. The repeat reuses 247206 tokens and evaluates
four. Both answers are exact and untruncated. The cold native prefill takes
410.846 seconds. The separate fixed-256 timing requests have 247200 input
tokens and take 15.652 and 11.477 seconds of HTTP wall time. Minimum sampled
available memory is 15.708 GiB, above the unchanged 8 GiB guard. This bounded
four-request ceiling phase omits the history edit tested at 32K. There is no
matched Huihui no-speculation ceiling arm, so it proves this tested workload
works, not a paired near-ceiling speedup.

Native Maki 0.6.3 also completes separate regular and Huihui greedy xhigh tool
loops, each in two turns. An isolated configuration uses the ordinary gateway
export, permits only one exact synthetic Bash command and requires the real
successful tool result followed
by the exact final answer. Thinking is present, and gateway reports confirm
xhigh and the six omitted sampler fields receiving their catalog defaults.
The normal user configuration is untouched. Native Maki output and reports
do not independently prove OpenAI wire-history preservation, actual engine
sampler settings or lack of input truncation. The separate API checks above
cover those boundaries.

The actual CPU-only router starts on loopback, exposes its inventory and
stops cleanly through the normal controller. Its root remains read-only, no
GPU devices are exposed, and the server process has no permitted, effective
or bounding capabilities. All test gateways and containers are gone after
acceptance. The kernel window has no GPU fault, reset, SVM mapping failure,
process trap or OOM event. No host power or kernel settings are changed.

Private commands, raw messages, reports and native output remain under
`build/dflash-presets-20261023/`. Only sanitized results are tracked. These
checks do not retest dense 27B DFlash2 or change that historical decision.

## Flash-Next follow-up (2026-10-23)

**Keep the existing no-speculation default.** DFlash2 improves the cached
single-request fixed-output tests, but not prefill, and its benefit does not
transfer uniformly to parallel work. It also leaves about 5 GiB less available
memory on the tested 128 GB host. The source refresh is accepted separately.
This initial evaluation does not add a managed DFlash2 preset. The later
optional-preset acceptance above is a separate decision, not a default change.

The newer Flash-Next draft now runs through llama.cpp on ROCm. It no longer
hits the first-request hidden-state assertion described in its pinned model
card. This is an isolated evaluation, not a new supported or default preset.
Regular Unsloth Flash-Next retains its existing no-speculation policy.

### Source and artifact

The test uses llama.cpp `280141e6d4769b1e4424de999437cc72ffac33d6`,
ROCm 10.1 and Paracetamol's thirteen reviewed patches. The locally built image
is `llama-cpp-ubuntu26.04-rocm10.1-280141e-r60`, ID
`83cb393c11b752e948005c43b386e04d8b0813d00c23a6da5439181bd67a2f81`.
The coupled source refresh and its regression checks are recorded in
[hardware acceptance](hardware-acceptance.md).

Two upstream changes matter for this test:

- [PR 30927](https://github.com/ggml-org/llama.cpp/pull/30927), merged
  October 22, reduces Flash-Next's four hyperconnection streams to the mean
  expected by the drafter. The model card's unsupported warning predates it.
- [PR 30663](https://github.com/ggml-org/llama.cpp/pull/30663), merged
  October 23, adds probabilistic proposal sampling. It is selected with
  llama.cpp's native `--spec-draft-sampling probabilistic`. The upstream
  default remains `greedy`, and both modes are tested separately here.

The draft is the Q8_0 conversion from
[`incoai/Qwen3.8-Flash-Next-DFlash2-GGUF`](https://huggingface.co/incoai/Qwen3.8-Flash-Next-DFlash2-GGUF/tree/4f5a39be8f0a9e9a573ac9824251830a8f4096d9),
revision `4f5a39be8f0a9e9a573ac9824251830a8f4096d9`:

```text
Qwen3.8-Flash-Next-DFlash2-Q8_0.gguf
652101024 bytes
sha256 541f4d6ee230747a34d4a2c6890e6e08cc8b19c461226dc1829701a544e46e8a
```

Its own pinned publisher card declares Apache-2.0. The corresponding source
checkpoint is pinned at `a22a1744e85c17364dcc54a6a420af7135d4c7f6`.
A reviewed ignored catalog derivative passes the normal installer, full SHA
verification and receipt publication. It does not add a public model preset.

The target is the unchanged managed Unsloth UD-Q4_K_XL Flash-Next at
`c8b5954a88c2775c546b92593eda40ea041d3176`, with the same four shards
and hashes recorded in `catalog/catalog.json`. Q8_0 describes only the small
draft, not a replacement target quantization.

### Controls and short correctness screen

Every generation request goes through the ordinary gateway. A guarded
ignored helper changes only the target's generated router section before
lazy backend startup, then restores the normal file on shutdown. The real
controller, receipt-backed inventory, sampler insertion and request reports
remain in use. No special inference route or weakened content gate is added.

Except for the labelled temperature-zero control, all six sampler fields are
omitted so the gateway supplies the model's thinking or non-thinking
defaults. Reports are checked against actual native settings. Thinking uses
temperature 1, top-p 0.95, top-k 20, min-p 0.05, presence penalty 0 and
repeat penalty 1. Non-thinking uses 0.7, 0.8, 20, 0, 1.5 and 1 respectively.
The target keeps F16 K/V, normal matrix arithmetic, four slots sharing a
262144-token unified pool, 2048 batch and microbatch sizes, direct
embedding reads and no RAM prompt archive. DFlash drafts at most three
tokens per step. The platform and CPU power policies remain unchanged.

Old and refreshed no-speculation images pass the ten-request short matrix,
including thinking off and xhigh, nested tool calls with full assistant
replay, streaming and live prefix reuse. The probabilistic DFlash arm passes
the protocol checks but misses one exact tool-result answer. It returns an
invented title instead of the literal value in the tool result.

Greedy DFlash separately passes the complete ten-request matrix, including
replay of its own assistant reasoning and tool calls. Both source images and
draft modes retain existing warnings for object-form `tool_choice`, falling
back to default selection. These checks establish the observed round trip
with the sole listed function, not complete support for that API form.

To investigate the probabilistic miss, the failed request is replayed with
one byte-identical rendered prompt, ten explicit seeds from 42 through 51,
no cached input and a separate temperature-zero control. All requests contain
451 input tokens without truncation.

| Mode | Correct sampled replays | Separate temperature-zero control |
| --- | ---: | --- |
| No speculation | 10/10 | Pass |
| DFlash probabilistic draft, depth 3 | 9/10 | Pass |
| DFlash greedy draft, depth 3 | 10/10 | Pass |

The same sampled seed repeats the invented title in probabilistic mode.
This is a retained behavioral miss, not proof of a broken sampling
distribution. Speculative execution consumes randomness differently, so
identical seeds do not require identical sampled answers. The source intends
both paths to preserve target verification, rather than accepting approximate
tokens. At temperature zero the server disables probabilistic rejection, so
that control does not independently validate its stochastic branch.

These are repeated answers to one small item, not thirty independent tasks
or a general model-quality score. Greedy drafting passes this screen and is
carried forward for longer-context checks. Both modes receive a matched
32K timing screen, without discarding the earlier probabilistic miss.

### Populated 32K result

All three current-image modes pass cold, repeated and changed-history
six-key retrieval. The old-image no-speculation control passes too.
Corresponding request bodies, rendered prompts and sampler provenance match.
The xhigh retrieval has 32977 input tokens. Its repeated request reuses all
but four. A separate medium-effort changed-history request fully reprocesses
32935 tokens and returns the new value correctly.

| Measurement | No speculation | Greedy DFlash, depth 3 | Probabilistic DFlash, depth 3 |
| --- | ---: | ---: | ---: |
| Cold native prefill | 46.545 s | 46.758 s | 47.853 s |
| First fixed-256 HTTP request | 15.010 s | 12.143 s | 11.489 s |
| Identical cached fixed-256 HTTP request | 11.843 s | 8.935 s | 8.055 s |
| Minimum sampled available host memory | 26.607 GiB | 21.527 GiB | 21.463 GiB |

The first timing request reuses 30929 of 32967 input tokens and evaluates
2038. The repeat reuses 32963 and evaluates four. Every timing request emits
exactly 256 tokens, including reasoning, with an intentional `length` finish.
These forced-output probes measure fixed work, not a completed program or a
task-quality pass. Retrieval reasoning lengths vary, so their total wall
times are not used as fixed-work speed comparisons.

The cached request takes 24.6% less wall time with greedy drafting and 32.0%
less with probabilistic drafting. This is a small fixed-order screen with
one observation of each cache condition per mode, not a stable universal
speedup estimate. Prefill does not improve. The approximately 5 GiB lower
available memory matters on a 128 GB host, but is an arm-level measurement,
not an isolated accounting of draft weights, KV or feature-capture buffers.

Actual child arguments select `ROCm0` for the draft. Upstream's default
requests full eligible draft-layer offload, but these verbosity-three logs do
not independently report every layer or tensor placement. They are not proof
that every draft operator executes on the GPU.

### Near-256K ceiling result

The same three modes also pass cold and cached six-key retrieval with
247210 actual input tokens in the unchanged 262144-token allocation. The
repeat reuses 247206 tokens and evaluates four. Corresponding request bodies,
rendered prompts and sampler provenance match across modes. This bounded
ceiling screen does not repeat the medium-effort changed-history test.

| Measurement | No speculation | Greedy DFlash, depth 3 | Probabilistic DFlash, depth 3 |
| --- | ---: | ---: | ---: |
| Cold native prefill | 410.437 s | 414.092 s | 411.914 s |
| First fixed-256 HTTP request | 18.496 s | 15.489 s | 14.464 s |
| Identical cached fixed-256 HTTP request | 14.320 s | 11.468 s | 10.861 s |
| Minimum sampled available host memory | 20.117 GiB | 15.172 GiB | 15.523 GiB |

The timing requests have 247200 input tokens. The first reuses 245162 and
evaluates 2038. Its repeat reuses 247196 and evaluates four. Both deliberately
produce exactly 256 tokens, including reasoning, rather than completing a
coding task. Retrieval answers pass without input truncation, but their
variable reasoning lengths make their request walls unsuitable for a
fixed-work comparison.

The cached timing request takes 19.9% less wall time with greedy drafting
and 24.2% less with probabilistic drafting. Cold prefill remains essentially
unchanged. These are single observations per cache condition and mode in a
fixed-order screen, not a general speedup guarantee. Neither mode triggers
the 8 GiB available-memory guard, though both leave about 5 GiB less memory
than the no-speculation control.

### Four-slot protocol screen with thinking off

All three modes run four simultaneous streams, each producing exactly 256
tokens, then replay four independent histories and test cancellation. Fixed
opaque labels are the same across modes. All own-label, foreign-label and
follow-up checks pass, with actual generation observed in all four slots.
This is a short non-thinking protocol screen, not xhigh or long-context
throughput acceptance.

| Mode | Shared wall for 1024 output tokens | Aggregate tokens/s | Per-request wall range |
| --- | ---: | ---: | ---: |
| No speculation | 19.783 s | 51.762 | 19.779 to 19.782 s |
| Greedy DFlash, depth 3 | 31.335 s | 32.679 | 30.662 to 31.333 s |
| Probabilistic DFlash, depth 3 | 29.221 s | 35.044 | 28.516 to 29.219 s |

Aggregate rate is total generated tokens divided by the group's shared wall
time, not the sum of per-request rates. Continuous native-slot polling is
included symmetrically in this screen. These observations show that the
single-request gain does not automatically carry over to parallel work.
They do not establish the same result for every reasoning mode or workload.

Cancellation leaves all four native slots idle within about 0.10, 0.26 and
0.16 seconds respectively. The probabilistic arm then returns `RECOVERED.`
instead of the exact requested `RECOVERED`. Its protocol checks pass, but
that unchanged exact-output oracle fails. The punctuation miss is retained
without relabelling the arm as a full quality pass or a backend-state defect.

Two earlier no-speculation fixture attempts remain failed records. A prompt
about a private marker provokes refusals. Replacing that wording with a
synthetic label removes the refusals, but numbered labels let the model
invent adjacent worker names. Opaque independent labels make the final
foreign-marker check informative. Every mode uses that same final fixture
and unchanged exact-match predicates.

### Two- and four-client xhigh timing

A separate short-context screen measures the usual xhigh reasoning mode,
with two fixed-output rounds per mode and client count. Every request
evaluates its full 96 or 97 input tokens with zero cached tokens and emits
exactly 256 tokens, including reasoning. Request bodies, rendered prompts
and sampler provenance match across draft modes. Actual generation overlaps
in all requested slots. No completed-task quality score is assigned to these
deliberately limited outputs.

| Clients | Mode | Round 1 aggregate tokens/s | Round 2 aggregate tokens/s | Combined aggregate tokens/s | Per-request wall range |
| ---: | --- | ---: | ---: | ---: | ---: |
| 2 | No speculation | 34.509 | 34.129 | 34.318 | 14.834 to 15.000 s |
| 2 | Greedy DFlash, depth 3 | 37.243 | 36.857 | 37.049 | 13.309 to 13.890 s |
| 2 | Probabilistic DFlash, depth 3 | 36.122 | 40.763 | 38.303 | 12.318 to 14.172 s |
| 4 | No speculation | 51.961 | 51.759 | 51.860 | 19.657 to 19.783 s |
| 4 | Greedy DFlash, depth 3 | 31.165 | 37.276 | 33.948 | 27.038 to 32.855 s |
| 4 | Probabilistic DFlash, depth 3 | 43.149 | 44.686 | 43.904 | 21.928 to 23.731 s |

Each two-client round generates 512 tokens, and each four-client round
generates 1024. Combined rate divides their total output by the sum of the
two shared round walls, never by summed per-request rates. Native-slot
polling stops at the first observed full generation overlap. That initial
polling remains included in wall time, so this is not an interference-free
decode benchmark.

Two-client aggregate throughput improves about 8% with greedy drafting and
12% with probabilistic drafting. Four-client throughput instead falls about
35% and 15% respectively. The visible round variation and two observations
per condition do not establish stable rankings or a universal speedup.
These are short prompts in the normal 256K allocation, not four populated
long histories. Minimum sampled available memory is 27.869 GiB for the
four-client control and about 23.3 GiB for its draft arms.

### Initial decision and limits

The merged implementation makes ROCm DFlash2 worth revisiting, but these
results do not justify changing the regular parallel coding-agent path.
Keep the current target, F16 KV and no-speculation policy. The verified small
draft is retained for repeatable local research, not exposed as another
managed model.

This initial screen uses draft depth three, one target quantization and one Strix
Halo host. It is not exhaustive draft-depth tuning, a full Maki agentic
comparison, a Vulkan DFlash acceptance result or evidence for other GPU
architectures. Fixed-output timing includes reasoning and deliberately stops
at the token limit. It does not measure completed useful code. The recorded
sampled-answer misses are real observations, not enough to establish a
sampling-distribution defect or a general accuracy regression.

Reconsider adoption after a material implementation or draft update, with
matched gateway sampling, actual populated context and representative
parallel coding work. Retain the literal tool replay, cancellation and
memory checks rather than treating a higher native token rate as sufficient.

Exact commands, requests, rendered prompts, reports, raw responses, native
settings and failed attempts remain privately under
`build/dflash-next-20261023/`. They are disposable local evidence, not
tracked prompts, telemetry or a runtime dependency.

## Dense 27B experiment (2026-08-23)

The following sections preserve the original experiment and its then-current
policy. Its model identifiers, Pi runner, ROCm version and source patch set
are historical, not current configuration instructions or Flash-Next results.

### Decision

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

### Snapshot under test

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

### Exact model artifacts

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

### Workload and measurement rules

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

### ROCm compatibility screen

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

### Vulkan screens

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

### Near-256K result

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

### Promotion and retest gate

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

### Retained local evidence and discovery sources

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
