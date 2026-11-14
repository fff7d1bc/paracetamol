# llama.cpp maintenance

Use this guide to resume a source refresh, investigate a regression, or plan
an isolated optimization. It does not depend on an earlier conversation.
The [routine upgrade runbook](routine-upgrade-runbook.md) defines scan versus
apply, [upgrading](upgrading.md#upgrade-llamacpp) owns dependency and patch
policy, and [source-update acceptance](testing-and-release.md#llamacpp-source-update-acceptance)
owns the required checks.

The [upstream-base and patchset design](architecture.md#upstream-base-and-broken-out-patches)
explains why local changes stay separate and how that supports regression isolation.

## Establish the starting point

Inspect the checkout before building or contacting an inference host:

```bash
git status --short
git log -8 --oneline
rg -n '^(ARG .*VERSION|ARG .*COMMIT|ARG .*UBUNTU_IMAGE)' Containerfile
rg -n 'llama-cpp-ubuntu' internal/application/registry.go
./paracetamol run gateway --help
./paracetamol status gateway --help
```

Source pins, patch application order and image identity come from the
Containerfile and registry, not an old report's wording. Read the
[patch contracts](upgrading.md#current-patch-contracts) and the latest
applicable [hardware record](hardware-acceptance.md). Dated holds and failed
candidates remain evidence, not instructions to restore an old production
pin. Preserve uncommitted work and inspect it before attributing a failure to
the committed source.

Before runtime work, establish which host and render node are available,
whether another workload is active, and who owns its gateway and containers.
A previous session's permission or recorded PID is not evidence that a
process is still yours. Inspect image IDs, labels and process start times.
Do not restart user workloads, prune resources or change host power, swap,
kernel or driver policy as incidental test preparation.

### Accepted baseline and open issues

The accepted 2026-11-14 state consists of two separate changes:

- `6253784` refreshes upstream to `ded522f` with a temporary HIP MMQ
  activation-sum compatibility patch. The
  [refresh record](hardware-acceptance.md#strix-halo-hip-mmq-compatibility-refresh-2026-11-13)
  includes the original replay regression and the coding screen.
- `4d68612` delivers `ded522f-r77`, restoring the convolution fast path and
  improving small-MoE scheduling without reduced precision. The
  [prefill recovery record](hardware-acceptance.md#strix-halo-flash-next-prefill-recovery-2026-11-14)
  identifies the final source tree, image, paired measurements and limits.

Physical acceptance covers `gfx1151` only. The `gfx1150`, `gfx1200` and
`gfx1201` targets remain deferred, including the HIP MMQ workaround's effects
on those devices. Compiling all targets does not establish GPU acceptance.

This is a dated baseline, not a substitute for inspecting the current files.
The roughly 31% prefill gain is against the slower refreshed `r76` image,
not another 31% over the pre-refresh build. The result broadly recovers the
earlier performance. It does not establish a threefold gain or sustained
decode improvement.

The following distinctions matter for the next investigation:

| Area | Established result | What remains open |
| --- | --- | --- |
| HIP MMQ sums | Corrected upstream sums change the numerical trajectory on the local optimized path. The temporary compatibility patch restores the frozen replay. | No incorrect upstream arithmetic was demonstrated. Removing the workaround requires its unchanged removal gates, not a cleaner-looking source diff. |
| Convolution fallback | Upstream's overlapping rollback windows and `SET_ROWS` stopped matching the carried optimization. The adapted matcher restores the fast path and preserves stored state. | Future graph changes need matcher, lifetime and observed kernel-activation checks, even if the patch still applies. |
| MoE scheduling | Folded traversal of upstream descriptors improves the measured prefill workload, including its Q5_1 tensors. | Packing and an extra outer kernel did not justify adoption. The later [J48/activation-prefetch screen](strix-llama-reverse-ablation.md) passes bounded numerical and replay checks but does not establish a repeatable speed gain. The full-fork performance comparison is held on its replay failure. |
| Vision warnings | The bounded image/history cases pass. Known recurrent position warnings match the accepted baseline. | The original `r77` wrapper failed its image-ID classifier. A separately reviewed classification accepts only that exact image and warning pattern. This is not a general warning exemption. |

Mixed BF16/F16 internal matrix paths, alternative quantizers and wholesale
fork adoption are outside this precision-preserving baseline. A successful
compatibility workaround is not automatically a numerical accuracy fix.
The [numerical investigation](hardware-acceptance.md#internal-numerical-mechanism)
records the controls behind that distinction.

## Freeze a comparison before running it

Give baseline and candidate separate immutable image IDs and retain their
patched source-tree identities. A tag, branch name or upstream commit alone
does not identify a locally patched binary. Compile native fixtures against
each matching source and library set. Diagnostic binaries must have separate
identities from the final normal image.

Record the following before a timed run:

- Target and draft artifact revisions and hashes, template, backend, profile
  and selected render node.
- Actual native arguments, relevant environment switches, context pool,
  slot count, cache precision, batch and microbatch, vision and speculation.
- Prompt and oracle hashes, reasoning level, sampler values and their
  provenance, seed, output budget, cache state and worker count.
- Kernel/driver and power-policy observations, competing work, memory guard,
  stop conditions and exact cleanup ownership.

Regular Unsloth Flash-Next is the primary acceptance model. Keep the managed
greedy DFlash2 variant and a plain control. Dense Qwen27B normally needs only
a short reply and tool exchange unless the changed path warrants more.
ROCm is the primary performance target. Vulkan remains a correctness
fallback, not a reason to carry Vulkan-only tuning in this workflow.

The accepted `r77` policy uses F16 KV, four slots sharing a 262144-token pool,
batch/microbatch 2048 and greedy DFlash2 depth three. Inspect actual startup
and per-model settings rather than assuming these values survived a rebase.
Do not change model bytes, precision, context or samplers while measuring an
engine change. Screen microbatch sizes separately and record logical batch
size too. The previous 1024/2048/4096 screen favored 2048 for its tested image,
not every future build.

## Exercise the production request path

Use the ordinary gateway for model-level acceptance. Let it supply missing
samplers, test explicit overrides separately, and save the request's effective
policy. Client xhigh defaults are distinct from the raw API's omitted-effort
fallback. Direct engine probes are useful diagnostic controls, but must send
the complete intended sampling tuple themselves. A fixed seed does not repair
a sampler mismatch.

Capture `X-Paracetamol-Request-ID` and retrieve the corresponding
`/paracetamol/v1/requests/REQUEST_ID` report while it is retained. The
[gateway report contract](gateway.md) defines the metadata and privacy
boundary. Reports are bounded, in-memory observations, not a durable replay
database. They do not contain prompts or responses. A private test runner
must save its own synthetic inputs, outputs and reports before shutdown or
retention eviction. Never turn production traffic logging on to compensate.

Run the [source-update checks](testing-and-release.md#llamacpp-source-update-acceptance)
in increasing cost order. Start with exact-source native checks and a short
replay before filling a near-limit context. Include reasoning, nested tools,
streaming and both cancellation paths. Cancellation must make the actual
child slot idle, not merely allow another request to succeed.

Long-context acceptance must populate the cache, then test an exact repeat,
an edited value and true append turns that preserve the complete assistant
messages, including reasoning. A large context allocation alone is not such
a test. Distinguish cache reuse from newly evaluated tokens. If an edit also
changes reasoning mode or another prompt input, do not call its full
reprocessing an isolated cache-rewind regression.

For generated-code checks, use the
[bounded coding-quality protocol](testing-and-release.md#bounded-coding-quality-screen).
It separates first attempts, repairs and fresh repetitions. Host unit tests
and archived Pi evaluations do not replace that check. The `r77` recovery
did not rerun the earlier `r76` coding screen.

## Investigate a regression

First classify what failed. An HTTP error, malformed tool call, truncated
answer, wrong program, nonfinite tensor, slower prefill and log-classifier
rejection are different observations. Preserve the first failing artifacts
and unchanged oracle before attempting a fix.

1. Reproduce with a matched accepted baseline and candidate. Compare rendered
   prompts and effective settings before blaming the model or kernel. Record
   baseline variability across fresh processes where relevant.
2. Reduce one dimension at a time. Plain versus speculative generation,
   one slot versus several, shallow versus populated context, and individual
   optimization controls answer different questions. Keep the original
   failing request alongside every reduction.
3. Distinguish upstream from local interaction. Try pristine upstream when
   capacity and loading policy permit. Label minimal patched controls
   honestly. A successful build, or a loading failure under a different
   memory policy, is not an upstream inference verdict.
4. Bisect source changes or patch groups using the smallest reproducible
   case. Preserve the failing and passing image identities. A reversal that
   changes the answer identifies a trigger, not necessarily faulty math.
5. Before interpreting instrumentation, prove the instrumented control still
   reproduces the original result. Capturing tensors, adding synchronization,
   disabling graphs or changing allocation can remove the failure itself.
6. Test the implicated operation against an independent reference. Report
   error magnitude, finiteness, ownership and state preservation. Passing
   operator checks do not override a failing model replay, and differing
   model output does not by itself prove incorrect arithmetic.
7. Rebuild and qualify the final normal image. An isolated kernel, diagnostic
   overlay or individually passing patch set is not the combined delivery.

For a performance regression, profile the unchanged workload after correctness
checks. Inspect whether the intended kernels actually launch. A matcher can
decline safely and still cause a large slowdown. Matcher eligibility tests
and numerical equality alone cannot detect that loss of activation. Keep
profiled runs separate from throughput comparisons.

Use counterbalanced baseline/candidate order. Record prompt-processing time,
generation time, end-to-end wall time, actual cached/new tokens and draft
acceptance separately. Changed answer length or speculative acceptance can
explain timing differences. For concurrent runs use total generated tokens
divided by total wall time, not a sum of per-request rates.

Stop on a hardware fault, memory guard, state corruption or reproducible
quality failure. Do not lower the guard, relax an oracle or hide output to
finish the matrix. If the baseline no longer reproduces, or the next useful
control needs unavailable hardware or permission, save the narrowed result
and state exactly what evidence is missing. Avoid an open-ended parameter
sweep without a remaining hypothesis.

## Preserve enough to resume

Keep the public record and private evidence separate:

- Commit source changes, maintenance rationale, host-neutral acceptance
  summaries, unresolved questions and links to earlier findings.
- Keep raw requests, responses, reports, logs, compiler output and generated
  runners under an ignored `build/` experiment directory. Record hashes and
  commands relative to that directory. Never copy private hostnames, home
  paths, addresses, credentials or project content into tracked examples.

At an interruption boundary, leave a short experiment note containing the
accepted baseline, candidate identities, tested hypothesis, completed and
failed checks, untested changes, exact artifact paths, ownership/cleanup
state and the next discriminating check. Separate observations from suspected
causes. Mark completed experiments closed so a new session does not restart
them from an obsolete plan.

Session loss does not normally delete these files, but `build/` is not a
backup and `make clean` removes it. A fresh clone has no private runners or
results. Preserve important evidence in an operator-managed backup before
cleanup. If it is missing, document that limitation and reconstruct a new,
versioned fixture from the test contract. Do not claim an exact replay of a
lost experiment or treat a Markdown summary as its raw evidence.

Finish with a focused commit and explicit deployment status. Record whether
the selected image was merely built or actually installed, verify its ID and
the matching controller, and retain a recoverable previous state. Leave only
the requested services running. Update the accepted hardware record and this
guide's dated baseline when a later delivery supersedes it.
