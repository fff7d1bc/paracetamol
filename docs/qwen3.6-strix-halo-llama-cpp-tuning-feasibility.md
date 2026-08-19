# Qwen3.6 llama.cpp Strix Halo tuning feasibility snapshot

This maintainer research record began on 2026-08-13 and was extended on
2026-08-14. It evaluates a small set of current llama.cpp tuning and template
claims against ROCmplete's managed Qwen3.6 models on Strix Halo. It records
which settings reproduced, which did not, and what remains to be accepted
before changing managed defaults.

This document is evidence, not a current support or performance declaration.
Always inspect `Containerfile`, `catalog/catalog.json`, source, and tests for
the current pins and policies. Hostnames, usernames, and private paths are
intentionally omitted.

## Conclusions

Two settings merited a focused integration follow-up for the dense
`qwen3.6-27b-mtp-q8-0` preset:

1. Increase MTP draft depth from two to three. This improved shallow
   generation by 6.5% and 37K-context generation by 3.0% without changing the
   target model, sampling, or output length.
2. Evaluate Q8 K/V cache with Flash Attention as managed policy for this
   preset. Combined with depth three, it was 10.9% faster than the current
   depth-two/F16 policy at 37K context, improved 94K prefill by 5.1%, and
   passed an exact three-needle retrieval check at 94K.

Neither result generalizes into a global llama.cpp default. Q8 K/V was neutral
for the hybrid `qwen3.6-35b-a3b-mtp-ud-q8-k-xl` preset, which should retain
F16 K/V based on this evidence. The current ROCm backend also remained the
right default for Qwen35-A3B.

The following proposed optimizations did not reproduce and should not be
integrated:

- forcing one server slot for a single active request;
- setting MTP minimum draft probability to `0.5` or `0.75`;
- switching Qwen35-A3B MTP from ROCm to Vulkan; or
- applying Q8 K/V cache to Qwen35-A3B as a family-wide policy.

Qwen's preserved-reasoning recommendation worked mechanically but did not
show a quality difference in the small two-turn control. It remains a
separate harness-level quality and total-token-efficiency question, not a
demonstrated inference-speed optimization.

The initial measurement study changed no runtime or catalog default. The
subsequent accepted integration is recorded below.

## Integration acceptance

Commit `9fa54a5` integrated the two Qwen27 findings without generalizing them:

- the preset now verifies up to three MTP draft tokens on every profile; and
- only Strix Halo selects Flash Attention on with symmetric Q8_0 target K/V.

The speculative draft cache remains F16. Qwen35-A3B, Strix Point, and RDNA 4
retain their previous cache and Flash Attention defaults. The catalog rejects
a quantized K/V declaration unless the same profile explicitly enables Flash
Attention, and direct and router startup derive equivalent llama.cpp policy.

The exact candidate bytes were built as
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r19`, image ID
`d4b7065b465a85efbfc5ff0aa10895283bdc5e79b2aae5b528f9f1b6e9647147`, on
the Fedora 44 Strix Halo host. Package verification passed. Direct ROCm
startup at the preset's default 262144 context resolved
`--spec-draft-n-max 3`, `--cache-type-k q8_0`, `--cache-type-v q8_0`, and
`--flash-attn on`. It returned exact bounded content while accepting 124 of
135 MTP proposals. Router startup generated the same model policy at 262144
context and returned exact content while accepting 125 of 132 proposals. A
131072-context Vulkan control also loaded the same policy and returned exact
content, accepting 114 of 117 proposals. All containers stopped cleanly, and
the kernel recorded no GPU fault, reset, or device loss.

The agent-quality run that accompanied this acceptance was retired on
2026-08-17 after the managed template, sampling ownership, and harness policy
changed. The direct same-image measurements above remain the attributable
performance evidence.

## Snapshot under test

- ROCmplete llama.cpp image:
  `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r18`
- llama.cpp commit: `62bf73d25c53b8161f8a22894d4f90c4aebbd7d0`
- ROCm: 7.14
- profile and architecture: Strix Halo, `gfx1151`, Radeon 8060S
- host software: Fedora Linux 44, kernel `7.1.7-200.fc44.x86_64`, rootless
  Podman 5.8.4
- memory policy: 128 GB system RAM with a 112 GiB TTM/GTT ceiling
- managed context: 131072 tokens
- target models: the verified managed Q8 Qwen3.6 27B MTP artifact and managed
  UD-Q8-K-XL Qwen3.6 35B-A3B MTP artifact

The normal ROCmplete rootless confinement, exact device exposure, unified
memory policy, and `--load-mode none` policy remained active. The test left no
managed container running.

Unless a case says otherwise, the server used its normal 2048 logical batch,
512 physical batch, F16 K/V cache, automatic Flash Attention, and this fixed
historical sampler:

```text
temperature=0.6 top_p=0.95 top_k=20 min_p=0
presence_penalty=0 repeat_penalty=1
```

This sampling matched the model author's precise-coding guidance and was held
fixed rather than treated as another performance variable. It is not the
current managed default: current Qwen3.6 presets select mode-aware dense or
sparse sampling in the server.
Q8 cases changed the target cache with `--cache-type-k q8_0`,
`--cache-type-v q8_0`, and `--flash-attn on`; the separate MTP draft cache
types remained at their F16 defaults.

## Workloads and measurement rules

The tests called the OpenAI-compatible Chat Completions endpoint directly so
harness orchestration could not confound the server settings.

The shallow workload used an 83-token Go engineering prompt and requested 768
tokens. Each Qwen27 candidate used seeds 42, 43, and 44. Reported shallow
generation rates are the arithmetic mean of the three server timing records.
The first run populated a reusable prompt prefix; generation remained the
comparison of interest.

The deep workload generated a deterministic synthetic Go source prompt of
37138 tokens and requested 512 tokens with seed 42. Fresh servers prevented
prompt-cache reuse between F16 and Q8 cases.

The retrieval workload generated a deterministic 94443-token Go source with
three unrelated hexadecimal values buried near the beginning, middle, and end.
Thinking was disabled and the response was constrained to those three values.
Both cache types received identical fresh prompts.

The 512- and 768-token performance requests often exhausted their output
budget while still reasoning. They are timing and speculative-acceptance
workloads, not standalone answer-quality evidence. The retrieval and
preserved-reasoning controls supplied the limited correctness evidence.

## Qwen3.6 27B MTP results

### Server slot count was neutral

The managed server left `--parallel` automatic and initialized four slots with
unified K/V. With one active request, MTP depth two averaged 15.75 generated
tokens/s. An otherwise identical `--parallel 1` server averaged 15.73
tokens/s. Draft counts, accepted tokens, and responses were effectively
identical for the same seeds.

This does not reproduce older reports of a large recurrent-MTP penalty merely
from initialized idle slots. Concurrent load remains a different workload,
but there is no basis here for reducing ROCmplete's server capacity globally.

### MTP depth three won

With one slot and no draft probability threshold:

- depth two averaged 15.73 generated tokens/s;
- depth three averaged 16.78 generated tokens/s, a 6.7% increase over that
  control and 6.5% over the normal four-slot baseline; and
- depth-three individual results were 16.14, 17.70, and 16.49 tokens/s.

At 37138 prompt tokens with F16 K/V, depth two generated at 15.31 tokens/s and
depth three at 15.76 tokens/s. The advantage narrowed to 3.0% but did not
reverse at this depth.

Depth three generated more speculative candidates and had a lower raw
acceptance ratio. The higher final throughput demonstrates why acceptance
percentage alone is not an optimization target.

### Draft probability thresholds lost

At depth three, a minimum draft probability of `0.5` averaged 15.24 tokens/s.
A threshold of `0.75` averaged 14.03 tokens/s. The no-threshold control
averaged 16.78 tokens/s.

The `0.75` case raised aggregate reported acceptance to approximately 91%,
but suppressed enough useful draft opportunities to reduce final throughput
by 16.4%. ROCmplete should retain llama.cpp's zero threshold for this preset.

### Q8 K/V helped the dense model at depth

At shallow context, depth three with Q8 K/V and Flash Attention averaged
17.04 tokens/s, compared with 16.78 for F16. This small difference by itself
would not justify policy.

At 37138 prompt tokens:

- F16 depth two: 233.70 prompt tokens/s and 15.31 generated tokens/s;
- F16 depth three: 234.49 prompt tokens/s and 15.76 generated tokens/s; and
- Q8 depth three: 241.72 prompt tokens/s and 17.02 generated tokens/s.

The candidate Q8/depth-three combination was therefore 10.9% faster at
generation than the current F16/depth-two shape in this deep workload. Against
depth-three F16 alone, Q8 improved prefill by 3.1% and generation by 7.9%.

At 94443 prompt tokens, Q8 processed 169.38 prompt tokens/s versus 161.23 for
F16, a 5.1% increase. Both returned the three buried values exactly:

```text
ALPHA-73=8f2c19a7
MIDDLE-1517=c4e881bd
OMEGA-2864=71da50ef
```

The 94K completion contained only 45 deterministic tokens, so its 18.67 versus
18.18 tokens/s generation result is not treated as a stable throughput claim.
The exact retrieval pass is useful evidence, but it is not a substitute for a
coding-agent task and multi-turn repository-recall evaluation before making
Q8 K/V the managed default.

## Qwen3.6 35B-A3B controls

The managed Qwen35-A3B MTP preset already uses the winning draft depth of
three.

At 37138 prompt tokens with ROCm:

- normal four-slot F16: 625.55 prompt tokens/s and 64.56 generated tokens/s;
- one-slot F16: 620.86 prompt tokens/s and 64.80 generated tokens/s; and
- normal four-slot Q8: 628.80 prompt tokens/s and 64.01 generated tokens/s.

Those differences are noise-sized. Neither one slot nor Q8 K/V improves this
hybrid MoE model enough to justify changing its policy.

The backend comparison also contradicted the older external result that
motivated the check. At shallow context, two ROCm runs averaged 59.98 generated
tokens/s and two Vulkan runs averaged 53.10. At 37138 tokens, Vulkan prefill
was slightly faster, 637.69 versus 625.55 tokens/s, but generation fell to
50.80 versus ROCm's 64.56 tokens/s. ROCm was 27.1% faster for the long-context
generation phase and remains the appropriate managed default.

## Preserved reasoning control

The official Qwen3.6 card recommends preserving thinking for agent scenarios.
ROCmplete's managed template and pinned llama.cpp already support the relevant
`--reasoning-preserve` behavior, but the Qwen presets do not currently enable
it.

A two-turn control explicitly passed the first assistant response, including
`reasoning_content`, back into the second request. Without preservation, the
second prompt contained 622 tokens. With preservation, it contained 1664,
confirming that approximately 1000 tokens of prior reasoning remained in the
history. Both variants correctly identified the generation-counter invariant
that prevents an invalidated in-flight cache load from publishing stale data
and proposed comparable APIs.

Preservation added roughly 3.2 seconds of second-turn prefill in this small
case and showed no obvious quality difference. Its possible value is avoiding
redundant reasoning or inconsistent decisions across longer tool loops. That
must be measured separately in Pi and Maki because clients may
serialize assistant reasoning differently.

## 2026-08-14 chat-template A/B

A later community report proposed a substantially rewritten Qwen template for
Qwen3.5, Qwen3.6, and Qwen3.8. The exact candidate inspected was Froggeric v22
at revision `9f14778c92c3b5ed3e0738085694c0d3452802dd`, 19,262 bytes with
SHA-256
`398edf5b5bb802fb6b9c9a8dba670d09f2aaeef6fdcaa0b2ca307265f59f78dc`.
Its published 28-case check uses Python Jinja rendering and substring
assertions; it is useful template coverage but is not llama.cpp inference,
tokenizer, cache, or coding-agent acceptance.

All four managed Qwen3.6 GGUFs contained the same 8,057-byte Unsloth-modified
template, SHA-256
`55d4931433fe502b794226ee7f4d206a6bdd436ac9f80eb7d8ebb4c639f9ea0c`.
That baseline already accepts normalized OpenAI string-form tool arguments and
supports llama.cpp's optional reasoning-preservation control. It has two
observable history defects: every system or developer message after its first
two leading instructions is silently omitted, and an assistant tool call with
empty reasoning can be replayed with a closed empty `<think>` block.

Four immutable render candidates were frozen for the probe:

- the embedded baseline above;
- a narrow 8,343-byte candidate, SHA-256
  `0e3bb87f2256e05fe08f0c5be96e276c4755113a187749c97462adeda67e745f`,
  which fixes those two defects and included an unused raw-string argument
  fallback;
- exact v22; and
- a 19,263-byte v22-neutral control, SHA-256
  `d7bbe015f7e5ff45e5c229763d6b3ef4c61a4abf147724c7b2ee35ad021e0bd5`,
  which changes only v22's implicit reasoning-effort default from `xhigh` to
  `medium`.

The exact pinned llama.cpp `/apply-template` and `/tokenize` endpoints rendered
identical histories against the managed Qwen3.6 27B tokenizer. Baseline,
narrow, and v22-neutral retained normalized object and JSON-string tool
arguments. Only the narrow and v22 candidates retained later system and third
leading instructions, and both avoided the empty historical reasoning block.
The narrow candidate kept the baseline 25-token completed-turn history and
reduced the tool-continuation probe from 309 to 305 tokens. V22-neutral
preserved old reasoning by default, expanded the same tool continuation to 416
tokens, and expanded the completed-turn history to 34 tokens. Exact v22 also
injects an `xhigh` instruction when the client supplies no template-level
effort value, rewrites the tool protocol, adds heuristic tool-error warnings,
and supports prompt-time truncation. Those are policy changes, not required
compatibility fixes. ROCmplete's current server patch maps top-level effort to
bounded generation but does not pass that value into template kwargs, so exact
v22 would silently select its own `xhigh` prompt for ordinary clients.

A deterministic two-turn Qwen3.6 27B MTP cache probe then compared the same
first response and follow-up. Baseline and narrow were effectively identical:
the second request contained 128 prompt tokens, reused 101, and completed in
6.168 and 6.164 seconds respectively. V22-neutral retained the first turn's
reasoning, so the follow-up contained 645 prompt tokens and reused 616, but it
still took 6.749 seconds. Its repeat took 6.352 seconds versus about 5.69 for
the other candidates, and its MTP-accepted decode rate fell from roughly 23.35
to 20.81 tokens/s on that changed context. Preserving more cached tokens did
not make the total request cheaper.

The stochastic agent-review comparison that accompanied this template audit
was retired on 2026-08-17 and is not part of the selection evidence retained
here.

The selected managed template is the narrow behavioral correction without the
unused raw-string fallback. llama.cpp normalized both tested OpenAI argument
forms before rendering, while emitting a raw JSON object directly inside the
XML tool protocol would not be a sound fallback. The final 8,215-byte
`qwen3.6.jinja` has SHA-256
`ea69920311f2efccf6343675490b27bd22d03787ebb8ccaf6e9101bfeba72898`.
It changes neither default reasoning preservation nor tool instructions. This
keeps the compatibility gain independently reviewable and avoids importing
v22's unrelated agent policy.

The final integration image was
`localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-62bf73d-r20`, image ID
`ad6d419895b89e050c5e813e6cb0e2ed82261d2887bea27b0a92734fd1774992`.
Its installed template matched the final hash above and `pip check` reported
no broken requirements. Direct ROCm startup on `gfx1151` rendered a later
developer message, omitted the closed empty historical reasoning block, and
completed a structured `record_value` call with argument `TEMPLATE_42` plus a
tool-result continuation returning exact content `FINAL_TEMPLATE_OK`. The
first and second requests used 321 and 358 total tokens; the continuation
reused 288 prompt tokens.

Router startup generated 19 installed model sections. All four Qwen3.6
sections selected the exact managed template. The router loaded the dense
non-MTP control on demand, retained a later developer message, and returned
exact bounded content `ROUTER_TEMPLATE_OK`. Direct and router containers
stopped cleanly, and the recent kernel journal contained no matching AMD GPU
fault, reset, timeout, or device-loss event. This accepts the final wiring and
protocol behavior on Strix Halo; it does not turn the single Pi A/B repetition
into a performance claim.

## Deferred candidates

The host-level `amd_iommu=off` proposal was not tested. It changes device
isolation, virtualization, and possibly NPU behavior and therefore requires an
explicit dedicated-host policy decision and reboot rather than an inference
preset experiment.

The open native BF16 Flash Attention work was also not imported. Revisit it
after upstream review and merge, then perform the normal llama.cpp patch
reconciliation and target-hardware matrix.

MTP plus `ngram-mod` remains an experimental workload-specific candidate for
repetitive edits. It was not tested because the simpler MTP depth and cache
changes already produced an interpretable result, while multiple speculative
strategies would require a catalog and entrypoint policy extension.

## Remaining follow-up

Keep remaining follow-up changes separate so their evidence remains
attributable:

1. Repeat exact retrieval and coding-agent acceptance on `gfx1150` before
   extending the Q8 K/V policy to Strix Point. Do the same independently for
   either RDNA 4 architecture before extending it there.
2. Evaluate Qwen preserved reasoning separately with multi-turn tool loops and
   compare correctness, total prompt tokens, generated reasoning tokens, and
   wall time. Do not infer benefit from per-token speed.
3. Re-run the focused cases after a llama.cpp pin, ROCm release, Qwen template,
   MTP implementation, or Strix kernel change. These results are coupled to
   the tested tuple.

## External leads evaluated

- [Qwen3.6 27B model card](https://huggingface.co/Qwen/Qwen3.6-27B)
- [llama.cpp speculative decoding documentation](https://github.com/ggml-org/llama.cpp/blob/master/docs/speculative.md)
- [llama.cpp Qwen MTP implementation PR](https://github.com/ggml-org/llama.cpp/pull/22673)
- [Strix Halo MTP benchmark](https://calebcoffie.com/blog/benchmarking-llama-cpp-mtp-on-strix-halo)
- [Community Qwen agent configuration](https://www.reddit.com/r/LocalLLM/comments/1uydqr8/i_made_claude_code_test_every_single_variant_of/)
- [Qwen3.8 MTP or DFlash discussion that prompted the scan](https://www.reddit.com/r/LocalLLaMA/comments/1vmqduk/qwen_38_27b_mtp_or_dflash/)
- [Community fixed-template report](https://www.reddit.com/r/LocalLLaMA/comments/1vnm7le/fixed_jinja_chat_template_for_qwen_35_36_and_the/)
- [Froggeric fixed-template repository at the inspected revision](https://huggingface.co/froggeric/Qwen-Fixed-Chat-Templates/tree/9f14778c92c3b5ed3e0738085694c0d3452802dd)
- [Strix Halo Qwen backend and slot discussion](https://github.com/ggml-org/llama.cpp/discussions/20856)
- [Strix Halo quantized K/V measurements](https://thefrontierlab.ai/strix-halo-quantized-kv-cache-verified/)
- [Open native BF16 Flash Attention PR](https://github.com/ggml-org/llama.cpp/pull/26856)
