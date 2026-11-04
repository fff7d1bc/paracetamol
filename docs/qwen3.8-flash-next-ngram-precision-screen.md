# Qwen3.8 Flash-Next n-gram precision screen

## Summary

Keep the existing IQ4_NL n-gram table for now. This November 4, 2026 screen
found that a Q8_0 replacement works with ROCm and greedy DFlash2, but it was
not a consistent upgrade.

At about 100K input tokens, prefill was effectively unchanged. Cached
single-client decode improved by about 8%, while the small four-client
fixture was variable and favored the original table overall. Both variants
passed three of five bounded coding tasks, with different failures. Q8 did
not establish the quality improvement needed to justify switching.

No catalog, model default, engine patch, sampler policy or client setting
changed. The original model files remain intact. This is an n-gram-only
experiment with the regular **Unsloth Dynamic UD-Q4_K_XL** model, not a
whole-model Q8 conversion or a new main-weight quant release.

## What changed

Only `per_layer_token_embd.weight` changed. Its shape remains
`160 × 320001536`. All other tensor payloads, tokenizer data and model
metadata were verified unchanged, apart from the table's type and the
following tensor offsets needed to store its larger payload.

| Property | Original | Private candidate |
| --- | --- | --- |
| Non-ngram weights | Unsloth UD-Q4_K_XL | Byte-identical |
| N-gram format | IQ4_NL | Q8_0 |
| Encoded bytes per 160-value row | 90 | 170 |
| N-gram tensor bytes | 28,800,138,240 | 54,400,261,120 |
| Complete four-shard model bytes | 111,334,654,784 | 136,934,777,664 |

The candidate adds about 25.6 GB on disk. It does not require the complete
larger table to become GPU-resident. The existing
[direct reader](qwen3.8-flash-next-direct-reader-feasibility.md) reads the
needed rows, dequantizes them on the CPU and supplies the same-sized F32
rows to the graph. A larger encoded row can still affect storage traffic and
page-cache behavior.

The original model is pinned to Unsloth revision
`c8b5954a88c2775c546b92593eda40ea041d3176`. The replacement table comes from
the same repository at
`d96201194f103c906994f283f504209dc8d2a10c`, using
`engram/Qwen3.8-Flash-Next-engram-Q8_0.gguf_file`. Its file size is
54,400,261,312 bytes and its SHA-256 is
`e3d76a2747c1fe53e3420d1bb7fd9593aafd03035fee4926317db80ad5c5296a`.
The [publisher's pinned repack instructions](https://huggingface.co/unsloth/Qwen3.8-Flash-Next-GGUF/blob/d96201194f103c906994f283f504209dc8d2a10c/engram/README.md)
describe the separate table files.

The current IQ4_NL payload also reconstructs the publisher's standalone
IQ4_NL file exactly, including its published SHA-256. This verifies that the
control table matches that repack. No newer main weights were substituted.

The private derivative was constructed without editing any managed original.
The unchanged shards were copied with copy-on-write, and the mixed shard was
rewritten and independently reread. Every unchanged tensor and the donated
table passed payload-hash comparison. The rewritten shard has SHA-256
`7d8b4597b27356c8645fd41078979b382b1cd1233cd388fda0326f5b28038b43`.
It is a locally derived file, not a published Hugging Face artifact. The donor
retains `NOASSERTION` licensing metadata and was downloaded with explicit
operator acknowledgment.

## Matched runtime

The acceptance hardware was a 128 GB Strix Halo system using `gfx1151` and
ROCm. Both variants used the same locally built `740251a-r71` image, ID
`9ea0d1277fb74bad75f5c22b093209dcf715abd24d759c090934ac760e7344d7`,
from llama.cpp `740251a920abf92b7744d9c0518ca157db0db579` and Paracetamol
`531427089c3bfb98c21418dbb27c0e9fdc345daf`.

Both retained F16 K/V, ordinary quantized/F32 matrix arithmetic, greedy
DFlash2 depth three, four unified slots and a 262144-token total pool.
The same BF16 vision projector remained loaded. No host configuration,
power policy, swap setting or operating-system cache policy changed.

All generation went through the ordinary gateway. Clients omitted the managed
sampler fields, and request reports verified the gateway's mode-dependent
defaults. A private controller overlay redirected only the selected native
model path for the candidate. The original catalog, receipt checks and
sampling-policy source remained unchanged. A separately hashed derivation
manifest bound the candidate bytes before startup.

## Performance

Fresh backend processes ran in original, Q8, Q8, original order. Each used
the same varied-text corpus with six retrieval markers. The first request
contained 98,497 input tokens with no reused prompt prefix, followed by a true
4096-token reference append and two cached, fixed-256-token xhigh requests.

| Measurement | Original IQ4_NL | Q8_0 table | Q8 change |
| --- | ---: | ---: | ---: |
| Uncached-prefix native prefill, tokens/s | 655.8 | 659.7 | +0.6% |
| True-append native evaluation, tokens/s | 582.0 | 574.6 | -1.3% |
| Cached xhigh output / HTTP wall, tokens/s | 20.71 | 22.35 | +7.9% |
| Four short requests, aggregate output / wall, tokens/s | 41.35 | 38.16 | -7.7% |

These values pool generated or evaluated tokens over the corresponding
summed durations across the two runs. The parallel denominator is shared
wall time, never the sum of individual request rates.

Prefill variation was larger than the difference between variants. Cached
serial output ranged from 20.57 to 20.85 tokens/s for the original and from
21.67 to 23.07 for Q8. Pooled draft acceptance was about 30.4% and 36.5%
respectively. The sampled completions differed, so this does not isolate a
general reader or dequantizer speed improvement.

The parallel fixture used four distinct short, nonthinking requests, each
generating 256 tokens. All four slots were observed generating concurrently.
It is not four populated 100K histories, and its rate should not be compared
with the serial xhigh row as a concurrency-scaling measurement. One matched
parallel pair was nearly tied, while the other favored the original by about
15%. The pooled result warrants caution, not a universal 8% regression claim.
Individual request latencies ranged from 22.35 to 25.84 seconds for the
original and from 23.23 to 27.72 seconds for Q8.

Both variants recovered all six markers and passed history, tool, streaming,
cancellation and recovery checks. Available RAM was about 23.2 GiB after
loading and 18.2–18.5 GiB after populating the text history. The continuous
minimum remained above 18.1 GiB, against an 8 GiB stop floor. No new GPU,
SVM, kernel or OOM faults appeared. These are shared-memory pressure
observations, not separate VRAM and RAM allocations to add together.

Host file caches were not flushed. Uncached-prefix prefill is not a cold-SSD
measurement or proof that the two table sizes have equal page-cache needs.

## Bounded coding results

Five synthetic Go tasks used fresh histories, medium effort, seed `17041`
and an 8192-token ceiling covering reasoning and final code. Both variants
received identical prompts requesting plain Go source. Hidden tests ran
only after inference stopped, in read-only, network-disabled CPU containers
with resource limits. No repairs or retries were counted.

| Task | Original IQ4_NL | Q8_0 table |
| --- | --- | --- |
| Strict byte-range normalization | Pass | Pass |
| UTF-8 edits in original byte coordinates | Token limit, no code | Returned code, failed tests |
| TTL/LRU cache simulation | Pass | Pass |
| Escaped configuration parser | Pass | Token limit, no code |
| Lexicographically minimal dependency plan | Token limit, no code | Pass |
| Natural-stop functional passes | 3/5 | 3/5 |

Q8's UTF-8 implementation panicked on a valid insertion at the end of a
string. This was a generated-code defect, not a GPU or inference-engine
fault. Answers that exhausted the token allowance without code have no
functional accuracy result. They are not counted as incorrect implementations.

The five attempts took 16m57s of total HTTP response wall time for the
original and 14m18s for Q8. Those totals include truncated attempts and the
failed implementation. They do not mean Q8 completed the same five jobs
faster.

Initial original-model xhigh pilots requested JSON-wrapped code and exhausted
4096- and 12288-token allowances on reasoning without an answer. They were
excluded. The medium/plain-source protocol and both seeds were frozen before
any candidate coding results. The predeclared confirmation stage required an
initial functional improvement without losing an original pass. The tied
score and parser completion regression did not meet that gate, so the second
seed was not run.

## Decision and limits

The Q8 table is technically viable with the tested ROCm/DFlash2 path. It is
not established as a better default. It uses more disk space, shows a mixed
concurrency result and does not improve the bounded functional score.
Higher table precision alone is not evidence of better coding reliability.

This screen does not qualify populated near-256K context, Q8 image/history
behavior, completed xhigh coding tasks, other quant publishers, Huihui models
or other GPU architectures. An optional long image-history control exhausted
its allowance in the unchanged original and remains incomplete, not a Q8
failure. The existing production
[vision acceptance](qwen3.8-flash-next-vision-feasibility.md) is unchanged.

The normal controller was never replaced. The generated router configuration
was restored to its verified original bytes, and no test services remain
running. Experimental inputs and private derivation evidence remain available
for a future investigation. Raw requests, responses, checksums, native
settings, memory samples and grading logs stay in ignored
`build/ngram-screen-20261104/`, `build/ngram-perf-20261104/` and
`build/ngram-quality-20261104/`, not in the public record.
