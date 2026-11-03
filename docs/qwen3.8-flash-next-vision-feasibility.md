# Flash-Next vision feasibility

## Decision

Keep vision enabled on the existing regular Unsloth and Huihui Flash-Next
presets, including their DFlash2 variants. The measured unused overhead is
about 1.1–1.3 GB with no convincing text-speed regression. Actual image use
retains about 2.3–2.5 GB above the text-only controls in the 100K screen.
No extra model IDs or opt-in switch are added.

Existing installations must rerun their bundle's installer, restart the
gateway and refresh Maki's provider export. Verified target shards are reused.
Dense Qwen remains text-only. See the [application guide](guides/applications.md#llamacpp)
for setup and the [gateway contract](gateway.md) for inline-image admission.

## What was measured

The screen uses a 128 GB Strix Halo system, ROCm 10.1 and llama.cpp
`740251a920abf92b7744d9c0518ca157db0db579` with Paracetamol's existing patches.
The target is the regular Unsloth Qwen3.8 Flash-Next UD-Q4_K_XL. Its weights,
chat template, gateway sampling defaults and ordinary quantized/F32 matrix
paths stay unchanged. The server uses F16 K/V, a shared 262144-token pool,
four slots and greedy DFlash2 depth three. No host power, memory or kernel
setting changes are part of this experiment.

The matching BF16 projector is 907542944 bytes. Its file size is not a
runtime-memory estimate. It is GPU-offloaded by the normal selected-device
policy. The corresponding Huihui conversion has a separate pinned projector.
Neither projector conversion includes license text at the tested revision,
so both retain unverified-license treatment and require acknowledgment.

## Unused vision overhead

Fresh-process A–B–B–A compares text-only startup with projector-enabled
startup. All four arms pass the same approximately 98K-token retrieval,
4096-token true append, cached generation and natural-answer recovery checks.
Each idle or populated memory observation is the median of fifteen samples
taken one second apart.

| Observation | Paired mean added memory |
| --- | ---: |
| Loaded model, before the long text prompt | 1.13 GB |
| Populated long text history, no image submitted | 1.28 GB |

These are differences in host `MemTotal - MemAvailable`, not exact process
residency. On a unified-memory APU, adding VRAM, GTT, RSS and host pressure
would double-count shared allocations. Kernel and filesystem activity can
also affect this measurement.

Cold native prefill is 665.5 tokens/s for the combined controls and
666.4 tokens/s with vision loaded. Cached fixed-output generation is
21.91 versus 22.30 tokens/s, respectively. The repaired candidate measures
22.04 tokens/s in its separate 100K text check. The bounded comparison finds
no convincing unused-text penalty. It does not establish identical performance
on every prompt or GPU.
Fixed-output timing uses total generated tokens divided by summed HTTP wall
time. It is not a completed-task quality score.

## DFlash2 compatibility repair

The existing image fails on its first DFlash2 image request with HTTP 500
and `failed to process speculative batch`. The same projector and target
work without speculation, including an image-history follow-up. This
reproduces [upstream issue 27408](https://github.com/ggml-org/llama.cpp/issues/27408).

An image occupies many embedding rows but advances a smaller number of
M-RoPE positions. The target sees the image, while DFlash skips its embedding
features. The next text injection then encounters a hole in the draft cache.
Already-merged image handling and actual-position fixes do not close that
hole at this pin.

The private candidate evaluates
[draft PR 30220](https://github.com/ggml-org/llama.cpp/pull/30220), pinned at
`73647e8ade6fb4293d221c4b92aafb7e82976418`. It fills forward draft-position
holes with zero-feature injections before processing real features. Target
logits and greedy verification remain unchanged. Draft conditioning changes,
so acceptance can fall and large reused-prefix gaps can add work. This is
not a claim of bit-identical seeded output or universal image correctness.
The generic consecutive-position guard remains intact.

The candidate changes only the installed `libllama-common` payload relative
to the control. Target, numerical, ROCm and Vulkan libraries are byte-identical.
The source-built candidate passes fifty native policy tests, six serializer
tests and nine CPU CTests before GPU testing.

## Image and cache observations

At approximately 100K context, six natural-answer checks pass through the
ordinary gateway with the candidate. They cover a synthetic 1080p image,
identical-image history replay, a text follow-up, replacing the earlier image,
following that changed branch and returning to the earlier text-only branch.
Complete assistant messages, including reasoning, are retained in replay.
Expected image labels and colors are not disclosed in the text prompt.

The first image takes 7.77 seconds with 103954 cached tokens from 106036 input
tokens. It generates 61 tokens, with 40 accepted from 60 draft tokens. The
same-image follow-up takes 2.50 seconds and retains 106096 cached tokens.
Changing the image correctly changes the answer and invalidates its image
portion without throwing away the long common text prefix.

Retained host pressure after this image is about 2.29 GB above the text-only
controls, rising to about 2.48 GB after the remaining image/text branches.
These histories contain additional tokens and are not perfectly equal-sized
memory comparisons. They remain below the requested conservative 3 GB budget.
Image encoding has its own transient allocations and latency. A resident
projector does not make actual image processing free.

The near-full-context repeat starts with 235520 corpus tokens and reaches
243507 input tokens on its first image. All five text and six image/history
checks pass without truncation. At least 12.70 GB remains available in its
continuous one-second sampling. Retained image/text pressure grows about
1.46 GB above that same run's already projector-enabled populated text state.
This is populated near-256K coverage, not a fully populated 262144-token input
or a promise that many simultaneous long histories fit.

Four explicit concurrent short requests also pass, with two different images
and two independent text markers. Native metadata observes all four slots
generating simultaneously. Full-history follow-ups, image-request
cancellation, native return to idle and image/text recovery pass. This is a
functional isolation check, not a concurrency throughput benchmark.

Short image and history checks pass for regular and Huihui plain targets on
Vulkan. Huihui's own projector also passes with both plain and DFlash2 ROCm
targets. Each publisher retains its own pinned companion and verification
receipt. These six supported model/backend combinations are not a claim of
acceptance on every supported GPU architecture.

The normal source-built `740251a-r71` image passes the concurrent image/text
and cancellation/recovery checks again. Installed numerical libraries are
byte-identical to `r70`; the changed common library is byte-identical to the
tested private candidate despite the two locally qualified source comments.
Native Maki 0.6.5 then uses the genuine gateway-generated configuration,
calls its real `view_image` tool and correctly reads the synthetic image at
xhigh in two turns. Gateway reports confirm streaming completion and all six
gateway-owned sampling defaults. Maki resizes this 1920×1080 image to
1568×882. This benign check uses disposable XDG state, not an OS filesystem
sandbox or a claim that provider configuration confines local image reads.

No guarantee is made for arbitrary image dimensions, many-image histories,
other hardware classes or every possible cache/checkpoint sequence. These
label/color/shape fixtures test functionality and state reuse, not general
visual reasoning or OCR accuracy. More images can consume additional memory
and encoding time. The 3 GB decision concerns the measured retained overhead,
not a hard per-request allocation limit. In the 100K image screen, transient
host pressure reaches about 3.40 GB above the populated text-only controls.

## Provenance and evidence

Production uses `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm10.1-740251a-r71`,
image ID `9ea0d1277fb74bad75f5c22b093209dcf715abd24d759c090934ac760e7344d7`.
The [patch ledger](upgrading.md#llamacpp-downstream-patch-ledger) records the
comment-qualified source hash, immutable upstream PR commit and removal gate.
The managed catalog pins both projectors' complete revisions, sizes, hashes
and unresolved license warnings.

Private raw fixtures, requests, responses and measurements remain below the
ignored `build/vision-screen-20261103/` evidence directory. Controls are
`control-a1/` and `control-a2/`, original projector-enabled text repeats are
`vision-b1/` and `vision-b2-text/`, and repaired long runs are `gap-100k/`
and `gap-230k/`. Functional and publisher/backend checks are `extra/`,
`huihui-rocm/`, `regular-vulkan/` and `huihui-vulkan/`. The normal image and
native-client check is `production-r71/`. Raw image bytes, session content
and machine identifiers are not part of the tracked record.

The final image also passes CPU-only tiny-model startup on loopback, with
no GPU devices, a read-only root and zero effective process capabilities.
The health check succeeds and the managed stop removes its exact container.
No CPU generation is requested.
