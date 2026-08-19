# Qwen3.8 27B Unsloth Dynamic Q4 versus Q8

[Documentation index](../README.md)

> **Historical artifact notice (2026-08-19):** this comparison tested the
> earlier 17,923,394,624-byte Q4 preview at revision
> `4604b899a826000505a834e623272db5b7fd62f6`. ROCmplete now pins Unsloth's
> newer 17,559,178,144-byte Dynamic v3 Q4 at revision
> `27af057ecb382ddfea5d12837360a8980560e3ed`. Every Q4 speed, memory, and
> quality result on this page remains evidence for the exact older SHA-256
> recorded below; it has not been relabeled as a result for the current
> artifact. The current pin's focused update acceptance is recorded in the
> [hardware acceptance log](../hardware-acceptance.md).

This page compares ROCmplete's two managed Qwen3.8 27B MTP presets on real
coding-agent work. It is intended to answer the practical questions behind
the quantization names: is the smaller Q4 useful, is Q8 clearly smarter, and
is `xhigh` reasoning worth its cost?

## TL;DR

> **These results apply only to the pinned Unsloth Dynamic GGUFs**
> `Qwen3.8-27B-UD-Q4_K_XL.gguf` and
> `Qwen3.8-27B-UD-Q8_K_XL.gguf`. They are not a universal Q4-versus-Q8 result.
> Another publisher, conversion, quantization recipe, importance matrix,
> runtime, template, or sampling policy can change both quality and speed.

> **Why the `xhigh` pass compares Q4 at 128K with Q8 at 256K:** the split was
> deliberate, not an attempt to handicap Q4. It approximates the memory and
> model-choice side of a real deployment dilemma: run Q4 at 128K on one fast
> 32 GiB Radeon AI PRO R9700-class discrete GPU, or keep Q8 at 256K on a
> 128 GB Strix Halo system whose larger memory can hold the full configuration.
> Both models were actually measured on the same Strix Halo host, so this
> comparison does **not** measure R9700-versus-Strix-Halo hardware speed. It
> tests the usefulness of the two configurations intended for those hardware
> envelopes; dedicated R9700 acceptance remains pending.

- **The Unsloth Dynamic Q4_K_XL was good enough to be a serious coding-agent
  model in this test.** In the original matched-64K medium pass, Q4 and Q8 each
  strictly solved three of five first attempts and left code that passed every
  grading gate on four of five. Q4 decoded 27.5% faster and used 15.7% less
  total wall time.
- **Q8 was not uniformly much better.** It had no measured first-pass quality
  advantage at medium. At `xhigh`, however, Q8 was markedly more reliable on
  the first attempt: four strict solves out of five, versus two for Q4. Q4
  solved all three misses when each was retried once, so this was not a clean
  inability to do the work.
- **Medium remains the sensible default.** Compared with the medium pass,
  `xhigh` produced 33.6% more output and took 30.8% more wall time on Q4; on
  Q8 it produced 50.5% more output and took 41.3% more wall time. Those are
  directional comparisons rather than a controlled effort-only A/B because
  the context ceilings also differed.
- **Use Q4 at 128K when model size, working memory, and decoding speed matter.**
  Its GGUF is 16.69 GiB instead of Q8's 29.30 GiB. The 128K Q4 server occupied
  30.83 GB while idle and 31.73-31.82 GB during the controlled agent runs on
  Strix Halo. A dedicated 32 GiB RDNA 4 card still needs hardware acceptance;
  these unified-memory measurements do not guarantee that fit.
- **Use Q8 at medium when its larger footprint is comfortable and first-pass
  conservatism matters more than speed.** Select `xhigh` for a particularly
  difficult task or a targeted retry, not as an automatic default.

The practical conclusion is not that Q4 equals Q8. Five tasks and a few
targeted retries cannot establish equivalence. The useful conclusion is that
this particular Dynamic Q4 is not obviously "dumb" or untrustworthy: it kept
the same medium first-pass score, was substantially faster, and eventually
solved every task in the suite. Q8's clearest advantage appeared in `xhigh`
first-pass reliability.

## Which preset should I choose?

| Need | Suggested starting point | Why |
| --- | --- | --- |
| Normal coding-agent work on a high-memory host | `qwen3.8-27b-mtp-ud-q8-k-xl`, medium | ROCmplete's conservative managed default and the stronger `xhigh` first-pass result |
| Faster responses or a smaller memory budget | `qwen3.8-27b-mtp-ud-q4-k-xl`, medium | Strong medium result, 16.69 GiB model, and 27.5% higher generation rate in this agent pass |
| A difficult task that medium missed | Retry the chosen quant at `xhigh` | `xhigh` helped some hard cases, but greatly increased token use and latency |
| A 32 GiB discrete GPU | Q4 at its 128K default, pending local acceptance | The Strix Halo working set was below 32 GB in decimal units, but a discrete `gfx1201` result is still required |
| A definitive choice for your own work | Run both against representative repositories | This suite was small, stochastic, Go-only, and specific to the pinned ROCmplete stack |

ROCmplete keeps Q8 as the managed-client default. The evidence here makes Q4
a credible alternative rather than proving it should replace Q8 for every
user.

## Primary first-pass results

The primary comparison gave every model and effort condition exactly one
attempt at each of five hidden-graded tasks. A **strict solve** means the agent
finished within the 45-minute cap and its working tree passed the ordinary
tests, hidden tests, and build gate. A **code pass** means those gates passed
even when the agent failed to stop before the timeout. Counting both matters:
a correct patch that never returns control is useful quality evidence, but it
is still an operational failure.

| Effort and quant | Context | Strict solves | Code passes | Timeouts | No-edit exits | Output tokens | Generation | Total wall time |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Medium Q4 | 64K | 3/5 | 4/5 | 1 | 0 | 88,163 | 17.825 t/s | 1:44:43 |
| Medium Q8 | 64K | 3/5 | 4/5 | 1 | 0 | 84,332 | 13.985 t/s | 2:04:13 |
| `xhigh` Q4 | 128K | 2/5 | 3/5 | 1 | 2 | 117,829 | 16.735 t/s | 2:17:00 |
| `xhigh` Q8 | 256K | 4/5 | 5/5 | 1 | 0 | 126,920 | 13.670 t/s | 2:55:31 |

`Generation` is the weighted server decoding rate: total generated tokens
divided by total measured generation time. It is not a sum or average of
per-request token rates. `Total wall time` includes prompt processing, tool
calls, tests, and agent overhead across all five attempts.

The direct quant comparisons are clear within each effort pass:

- At medium, Q4 decoded 27.5% faster than Q8 and completed the whole pass in
  15.7% less wall time, with the same strict and code-pass counts.
- At `xhigh`, Q4 decoded 22.4% faster. Q8 took 28.1% more total wall time, but
  delivered two more strict first-attempt solves and two more code passes.
- Raw decoding speed does not guarantee a faster task. On the two easiest
  `xhigh` tasks, Q8 generated fewer tokens and finished slightly sooner despite
  decoding more slowly.

The effort comparison needs more care. Medium used a forced 64K ceiling for
both quants because it began before Q4's 128K default was accepted. The later
`xhigh` pass used each preset's current default: 128K for Q4 and 256K for Q8.
That deliberate split represented the R9700-class and high-capacity Strix
Halo deployment envelopes described above. The increased wall time and output
strongly show the practical cost of the tested `xhigh` configurations, but
they cannot isolate reasoning effort from context and run-to-run randomness.

## What happened on each task?

All five tasks came from four real Go codebases. The named public projects were
`reencode`, `spinherd`, and `taskaffctl`; two tasks came from a fourth codebase
that is intentionally unnamed here. The suite covered overwrite/path aliasing,
symlink-boundary safety, cancellation propagation, block-device identity, and
CPU-topology diagnostics. It did not cover Python, JavaScript, Rust, frontend
work, prose, broad greenfield design, or multilingual use.

The table below shows strict first-pass outcomes and elapsed wall time. "Code
passed; timeout" means every grading gate passed but Pi did not finish before
45 minutes.

| Task | Medium Q4, 64K | Medium Q8, 64K | Xhigh Q4, 128K | Xhigh Q8, 256K |
| --- | --- | --- | --- | --- |
| Overwrite/path aliasing | Code passed; timeout (45:00) | Solved (15:49) | Solved (32:28) | Solved (30:16) |
| Symlink-boundary safety | Solved (5:24) | Solved (7:07) | Solved (18:29) | Solved (16:58) |
| Cancellation propagation | Solved (16:55) | Solved (20:36) | Code passed; timeout (45:00) | Code passed; timeout (45:00) |
| Block-device identity | Hidden-test miss (18:59) | Code passed; timeout (45:00) | No edit (21:34) | Solved (43:57) |
| CPU-topology diagnostics | Solved (18:25) | Hidden-test miss (35:41) | No edit (19:29) | Solved (39:20) |

This is why neither the token rate nor the aggregate solve count tells the
whole story. Medium Q4 was much faster on the simple symlink task, but timed
out after already producing a correct overwrite fix. Xhigh Q8 was more
reliable on the two system-adjacent tasks, but spent nearly 40-44 minutes on
each. Both `xhigh` quants produced a correct cancellation patch and then kept
working until the cap.

## What the targeted retries changed

Retries were run only for selected misses, so they are diagnostic evidence,
not another balanced score to add to the first-pass table.

At medium and 64K:

- Q4 solved the overwrite task on its second attempt in 13:54. Its second
  block-device attempt made no edit.
- Q8 solved the CPU-topology task on its second attempt in 18:10. Its second
  block-device attempt timed out without making an edit.

At `xhigh`:

- Q4 solved cancellation in 41:31, block-device identity in 39:42, and CPU
  topology in 44:38 when each first-pass miss was retried once. It therefore
  produced a strict solution for all five tasks within at most two attempts.
- Q8's cancellation retry again produced code that passed all gates and again
  hit the 45-minute cap. This reproduced a stopping failure twice; it was not
  evidence that Q8 could not find the code change.

The retries reduce confidence in a simple "Q4 cannot solve these tasks"
interpretation. They do not erase Q8's better `xhigh` first-pass result. In an
interactive workflow, needing to notice and rerun a no-edit response is a real
cost.

## Why Q4 now defaults to 128K

One hard medium-effort task was also used for a small Q4 context screen. Each
context received two independent attempts:

| Q4 context | Strict solves | Code passes | Compaction observation | Server memory on Strix Halo |
| --- | ---: | ---: | --- | --- |
| 64K | 0/2 | 0/2 | One no-edit run compacted near the ceiling | 25.98 GB idle |
| 128K | 2/2 | 2/2 | No compaction | 30.83 GB idle; 31.73-31.82 GB active |
| 256K | 1/2 | 1/2 | No compaction | 40.52 GB idle |

This tiny screen does not prove that 128K improves model intelligence or that
it is always better than 256K. It showed that 64K could obstruct a realistic
agent trajectory and that 128K removed the observed pressure without the
large 256K working set. That evidence supports ROCmplete's reviewed 128K Q4
default.

The largest live prompt observed in the `xhigh` runs was about 100.4K tokens.
Nothing in this suite demonstrated a need for 256K, but five tasks cannot rule
out longer real sessions. The Q8 preset therefore retains its native 256K
default.

## Exact scope of the result

The comparison ran on 2026-08-17 and 2026-08-18 with this pinned stack:

| Component | Tested value |
| --- | --- |
| Host | Fedora Linux 44, Ryzen AI Max+ 395, 128 GB LPDDR5X-8000, Strix Halo `gfx1151` |
| ROCmplete source | `80f2e2f3d6bf6406c0f2ef3f9d195e8bb93cad6c` |
| llama.cpp | `3cb7ffb1a1f612d5e4a46244ae5a3c77ad934a70` |
| ROCm | 7.14.0 |
| Container | `localhost/rocmplete:llama-cpp-ubuntu26.04-rocm7.14-3cb7ffb-r29` |
| Agent harness | Pi 0.84.2, network-disabled sandbox, one fresh server per attempt |
| Inference | ROCm backend, embedded MTP heads, draft depth 3 |
| Reasoning sampling | Temperature 1.0, top-p 0.95, top-k 20, min-p 0, presence penalty 0, repeat penalty 1 |
| Attempt limits | 45 minutes per task; 16,384 generated tokens per model turn |

The exact model inputs were:

| Managed preset | Exact Unsloth artifact | Size | SHA-256 |
| --- | --- | ---: | --- |
| `qwen3.8-27b-mtp-ud-q4-k-xl` | `Qwen3.8-27B-UD-Q4_K_XL.gguf` | 17,923,394,624 bytes (16.69 GiB) | `bee238bbeb3dc0a34bde4d0dedbaee1f98c009e8bb4226f03070054c12fb1372` |
| `qwen3.8-27b-mtp-ud-q8-k-xl` | `Qwen3.8-27B-UD-Q8_K_XL.gguf` | 31,457,991,680 bytes (29.30 GiB) | `af36ecb6b5db1407953345b746c14ac93f0657dda413910b4348683a2d990377` |

Both files came from `unsloth/Qwen3.8-27B-GGUF` at revision
`4604b899a826000505a834e623272db5b7fd62f6`. Both used ROCmplete's same
reviewed Qwen3.8 chat template and server-side thinking sampling policy. The
medium pass held context at 64K, so quantization was the intended model
difference. The `xhigh` pass deliberately exercised each preset's current
default, so both quantization and context differed.

The controller did not fix a generation seed. Five first attempts per
condition are too few for confidence intervals or claims of statistical
significance. The hidden grader prevents self-reported success from deciding
the outcome, but it does not make the task sample broad or universal.

## What this result does not establish

- It does not compare generic Q4 with generic Q8. Even another Unsloth Q4 or
  another Qwen3.8 conversion may behave differently.
- It does not establish equivalent quality. Matching three of five strict
  medium solves is encouraging, not a proof of parity.
- It does not show that `xhigh` itself caused every difference. Context and
  stochastic generation changed between passes.
- It does not measure non-MTP presets, Vulkan, concurrent serving, vision,
  non-thinking mode, low effort, or another client harness.
- It does not demonstrate discrete 32 GiB GPU acceptance. The memory figures
  came from a unified-memory Strix Halo host.
- It does not generalize beyond the five Go repair tasks. Users should test
  their own task distribution before making an expensive deployment choice.

Within those limits, the evidence is actionable: start at medium, choose the
Unsloth Dynamic Q4 when its speed and footprint matter, retain Q8 when its
larger working set is acceptable and first-pass robustness is the priority,
and reserve `xhigh` for work that justifies its much longer trajectories.
