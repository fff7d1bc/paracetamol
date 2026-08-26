# Qwen3.8 RDNA head-dim-256 Flash Attention feasibility snapshot

This maintainer research record captures a 2026-08-26 isolated evaluation of
llama.cpp pull request
[#26419](https://github.com/ggml-org/llama.cpp/pull/26419) with Qwen3.8 27B on
the Fedora 44 Strix Halo host. The pull request selects the AMD WMMA F16 Flash
Attention kernel for attention head dimensions through 256 and bypasses LDS
staging for its K/V loads.

This document is machine-specific historical evidence rather than a current
support or performance declaration. Inspect `Containerfile`, the catalog,
source, and current hardware-acceptance records for active policy.

## Decision

Do not carry pull request #26419 as a Paracetamol patch for `gfx1151`.

The exact candidate was stable but consistently slower than the pinned
llama.cpp kernel selection. With the managed Unsloth Dynamic Q8_K_XL
Qwen3.8 27B artifact, F16 K/V cache, and ROCm, prompt processing changed by:

- -5.1% at 4K context depth;
- -21.7% at 32K; and
- -32.9% at 128K.

Generation remained within 0.05% of baseline at every depth. The candidate's
three prompt-processing samples were tightly grouped, and its complete 128K
benchmark took about 35% longer than the paired baseline. This is a
`gfx1151` performance rejection, not a correctness failure.

The candidate 252K run was stopped after about one minute and produced no
result. Running it for more than an hour could not justify a patch after the
loss increased monotonically through 128K. A complete baseline at 258,048
tokens remains recorded to describe the unmodified path, but it must not be
presented as a paired candidate comparison.

Published `gfx1201` gains from the pull request do not contradict this result.
RDNA 3.5 and RDNA 4 can prefer different attention kernels. Revisit this
decision only after the pull-request implementation materially changes or
when representative `gfx1201` hardware is available for a separate profile-
scoped evaluation. Do not broaden an RDNA 4 result to Strix Halo by
assumption.

## Snapshot under test

- Paracetamol baseline:
  `eec0f3df90360c6746051ff36ec6861f1e604f1c`
- pinned llama.cpp:
  `5d5cb4c3a4ea8769490d39a275ee49a45184774d` (`b10631`)
- candidate pull-request head:
  `d76c0046947c8b3fe92949fffeb634bbe7cc5d40`
- baseline image:
  `localhost/paracetamol:llama-cpp-ubuntu26.04-rocm7.14-5d5cb4c-r30`
- baseline image ID:
  `32d13da5104fd9ca375c8c0daa0ab4a1d221a8e221709cf09f0bb2dbed163dcf`
- isolated candidate image:
  `localhost/paracetamol-test:llama-cpp-pr26419-d76c004`
- candidate image ID:
  `3592e14127af745050182d227a5664cbe7221eb70922b112aef904ea40f89ba3`
- ROCm: 7.14.0
- host: Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, Ryzen AI Max+ 395,
  128 GB LPDDR5X-8000, Strix Halo `gfx1151`
- render node: `/dev/dri/renderD128`

The candidate added the exact 67-addition, 6-deletion pull-request diff to an
isolated checkout at the pinned llama.cpp commit. All four managed patches
applied first and remained present:

```text
hip-apu-host-buffer.patch
reasoning-controls.patch
quantized-kv-flash-attention.patch
vulkan-f16-kv-contiguize.patch
```

The extra patch then applied with `git apply --check`. The resulting image
compiled HIP and Vulkan for all four managed GPU targets, retained the exact
source-revision label plus a test-only patch label, and passed retained-binary
dynamic-library closure and CPU version checks. It used a separate image tag
and did not replace the accepted image.

## Model and benchmark conditions

Every measured condition used the receipt-verified managed preset
`qwen3.8-27b-ud-q8-k-xl` and this exact artifact:

| Field | Value |
| --- | --- |
| Repository | `unsloth/Qwen3.8-27B-GGUF` |
| Revision | `4604b899a826000505a834e623272db5b7fd62f6` |
| File | `Qwen3.8-27B-UD-Q8_K_XL.gguf` |
| Bytes | 31,457,991,680 |
| SHA-256 | `af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377` |

The native `llama-bench` comparison held the image's source pin, existing
patches, ROCm backend, model, render node, balanced host power policy, F16 K/V
cache, enabled Flash Attention, batch 2048, physical batch 512, pp512, tg128,
and three measured repetitions constant. Only pull request #26419 differed.
The host ran no other build or inference workload during either benchmark
matrix.

## Results

| Context depth | Baseline pp | Candidate pp | pp change | Baseline tg | Candidate tg | tg change |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 4,096 | 311.24 t/s | 295.45 t/s | -5.07% | 7.1696 t/s | 7.1694 t/s | -0.003% |
| 32,768 | 200.21 t/s | 156.85 t/s | -21.66% | 6.7844 t/s | 6.7873 t/s | +0.043% |
| 131,072 | 88.75 t/s | 59.58 t/s | -32.87% | 5.7493 t/s | 5.7503 t/s | +0.017% |
| 258,048 | 52.37 t/s | not run | not paired | 4.8073 t/s | not run | not paired |

The 32K candidate prompt samples were 157.392, 155.991, and 157.163 t/s.
The 128K samples were 59.665, 59.428, and 59.634 t/s, compared with baseline
samples of 89.322, 88.502, and 88.418 t/s. The regression is much larger than
run-to-run variation.

The benchmark commands ran sequentially with a fresh container and model load
at every depth. Publication timestamps therefore provide a useful complete-
command wall-time comparison for the later points:

| Context depth | Baseline command | Candidate command | Change |
| ---: | ---: | ---: | ---: |
| 32,768 | 3m22s | 3m43s | +10.4% |
| 131,072 | 16m59s | 22m58s | +35.2% |

This wall-time evidence includes model startup and depth-cache construction,
while the pp/tg rows measure only their respective benchmark phases. Both
views reject the candidate on this host.

The test interval produced no matching AMDGPU fault, SVM mapping failure, GPU
reset, process trap, general-protection fault, or OOM kernel event. All
benchmark containers were removed. Because the performance gate failed before
promotion, candidate server, reasoning, tool-call, and agent-quality
acceptance were intentionally not run. The already-accepted production image
and source checkout remained unchanged.

## Retest gate and retained evidence

Reopen the `gfx1151` investigation only after the pull request or its eventual
upstream equivalent materially changes kernel selection, LDS bypass, WMMA
tiling, or synchronization for head dimension 256. Repeat 4K, 32K, and 128K
first. Do not spend time at near-256K unless the candidate is already neutral
or positive at 128K.

Test RDNA 4 separately when `gfx1201` hardware is available. Use the same
pinned Qwen3.8 artifact and F16 cache policy, retain decode and output checks,
and scope any resulting patch or runtime selection to the architectures that
actually benefit.

The raw benchmark JSON remains outside version control on the test host:

```text
/home/piotr/tmp/paracetamol-pr26419-results/baseline-d4096.json
/home/piotr/tmp/paracetamol-pr26419-results/baseline-d32768.json
/home/piotr/tmp/paracetamol-pr26419-results/baseline-d131072.json
/home/piotr/tmp/paracetamol-pr26419-results/baseline-d258048.json
/home/piotr/tmp/paracetamol-pr26419-results/candidate-d4096.json
/home/piotr/tmp/paracetamol-pr26419-results/candidate-d32768.json
/home/piotr/tmp/paracetamol-pr26419-results/candidate-d131072.json
```

No candidate 258,048-token JSON exists. The interrupted run was excluded from
every measurement above.
