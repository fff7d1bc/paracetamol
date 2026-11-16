# Maintainer acceptance kit qualification

The public clone can rebuild the runner and grader without old experiment
files. Runtime qualification was not uniformly green. Both model variants
missed values in near-limit retrieval checks, and the bounded Go task did not
produce final code. These outcomes stay visible rather than being converted
into an engine-upgrade approval.

## Scope

The qualification on 2026-11-15 and 2026-11-16 checked that the tracked
[maintainer kit](../../tools/llama-acceptance/README.md) can be rebuilt without
private experiment runners. The implementation was reviewed independently
before commit `b866d7ec35c5acc1be6f323e4fa54a47929d3312`.
The reviewed lifecycle, initialization and failure-classification refinement
is commit `7ce5349dcde8cb579a265beea249dd52c78f44a7`.

This is a tooling qualification, not a new engine upgrade, a model leaderboard
or full hardware acceptance. Normal application defaults, model content and
llama.cpp patches were unchanged. A passing selected suite cannot replace
missing source-update gates.

The Go and Python tasks were newly authored textbook algorithms. Their
prompts, reference implementations, independent oracles and execution
harnesses were not copied from private projects or the archived Pi tasks.
The recovered thinking-off replay contains only an invented record/tool
request. Its bounded provenance is recorded beside the
[scenarios](../../tools/llama-acceptance/internal/scenarios/PROVENANCE.md) and
[coding fixtures](../../tools/llama-acceptance/internal/coding/PROVENANCE.md).

## Rebuild and grading checks

A literal independent Git clone started with no `build/` directory or reused
module/build caches. It passed:

- `make llama-acceptance` and `self-test --source-only`.
- `make check`, `make test` and `make static`.
- Forced controller/helper builds and all four preparation operations, without
  starting inference or downloading model content.

The same bootstrap and preparation checks were repeated successfully in a
new clean clone of the final refinement commit. Its helper also ran all
eight real sandbox self-tests using an explicitly identified local grading
image built from the unchanged tracked recipe.

The first clone was deeply nested under another checkout's ignored build
tree. Its helper built and source checks passed, but existing gateway-control
tests exceeded Linux's pathname Unix-socket length limit. A second independent
clone at a shorter path passed the full checks without skips or source
changes. This was a path-environment limitation, not a dependency on old
experiment files.

The original checkout also passed Tier 1 checks and the complete helper
package race-detector pass. A fake-Podman cancellation test was corrected to
wait until its fake container had started before canceling. A fixed 500 ms
deadline had tested metadata interruption under race instrumentation instead
of the intended created-container cleanup.

The pinned CPU grader image built successfully. Actual rootless sandbox
self-tests passed both reference solutions and rejected all six deliberately
broken implementations, with 71 cases per task. These checks ran locally and
on the inference host. They validate the grader and its cleanup, not model
quality. Generated-code execution stayed CPU-only, networkless and bounded.

## Runtime tuple

Production-path checks used a dedicated ordinary loopback gateway with its
normal sampling-default policy. The selected regular Unsloth Flash-Next
UD-Q4_K_XL model and greedy depth-three DFlash2 variant used current managed
receipts, the tracked Qwen template, F16 K/V, four slots, a 262144-token shared
pool and batch/microbatch 2048. Hardware was `gfx1151`, with ROCm 10.1.0 and
kernel `7.2.15-300.fc45.x86_64`. Existing firmware and CPU EPP profiles were
`performance`; the campaign did not change them.

The normal engine source was
`ded522f8304554317e7ff8f88698dbbbca5425aa`, patched tree
`20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c`. Private evidence retains exact
image, helper, fixture, content and template hashes, complete synthetic
requests/responses, correlated reports and native slot observations. It does
not rely on the gateway's bounded report retention surviving indefinitely.

The available-RAM and current-boot kernel-journal guards remained enabled.
No swap, kernel, firmware, device permissions or system configuration was
changed to make a test pass.

## Observed results

The regular DFlash2 variant passed all 12 short cases, comprising six exact
thinking-off replay controls, low/medium/xhigh effort, omitted-effort defaults
and nested JSON/streaming tool round trips. These are bounded protocol and
answer checks, not broad model-quality equivalence.

The first populated-cache prefill run completed in about 11 minutes. All six
conditioning requests passed, and the final native snapshot established
261877 resident slot-prompt tokens. Cold 32K work measured 32903 newly
processed tokens in 44372.45 ms. The final cold 16K request measured 16035
tokens in 21685.777 ms, but failed its exact answer oracle.

That failure remains FAIL. The prompt requested records 0, 147, 295, 442 and
589. The fourth returned value belonged to record 443 instead of 442; the
other four matched. An independent review checked all 590 prompt records and
the footer against the generator and found no prompt/oracle error. The
response stopped naturally after 60 output tokens with no cached input. This
establishes a retrieval miss in this trial, not an engine defect or a
baseline-to-candidate regression. The timings are observations from that
failed-quality run, not an accepted performance comparison.

The v1 DFlash2 near-limit history run passed cold retrieval at 247161 input
tokens with no cached input, then passed cache-enabled population, exact
repeat and isolated edit. The first appended turn requested four different
original-record values plus a marker. Three values and the marker matched;
the fourth was `VALUE_172363825` instead of `VALUE_172435096`. Its actual
request contained the expected literal. The report recorded 247882 input
tokens with 247782 cached, but prefix reuse did not make the answer correct.
The failure was retained, and append two and the old trailing lifecycle cases
were not reached. These partial passes do not qualify the complete history
gate or attribute the miss to speculation.

The plain model passed its cold retrieval, population and repeat, then failed
the isolated edit. It returned `VALUE_154316424` where the actual prompt
contained `VALUE_154332262`, while correctly honoring the edited final value.
The report recorded 247161 input tokens, 245113 cached and 2048 newly
processed, with natural completion. It did not reach either append. The
different failures in these two trials do not establish a DFlash2 regression
or distinguish model behavior from an inference defect.

That separation motivated result schema v2. Long retrieval remains the
`history` suite, while the existing idle-slot, worker, cancellation and media
checks have an independently runnable `lifecycle` suite. Both remain
mandatory in `all`. Oracles, seeds, budgets, context depth and coding tasks
were not weakened. Historical v1 evidence remains unchanged and cannot be
relabelled as complete v2 coverage.

An early standalone lifecycle snapshot passed its 14 cases on the already-used
plain child. The same snapshot BLOCKED before inference on a fresh DFlash2
child because unused native slots omitted occupancy counters. This exposed a
runner-preparation gap, not evidence of a model failure. The final v2
procedure adds four explicit marker requests and then requires measured
counters in all four slots. Its required lifecycle coverage is 18 cases and
21 requests. The earlier snapshots are retained but cannot satisfy the
expanded coverage contract. Prefill uses the same versioned preparation;
the older v1 timing observations do not qualify the new preparation path.

Both final lifecycle runs passed all 18 cases and 21 requests. They covered
explicit slot preparation, resident history preservation/reactivation,
four independently checked worker markers, verified nonstreaming and SSE
cancellation followed by native slot-idle/recovery checks, and synthetic
red/repeated-red/blue/text media histories. The DFlash2 run began with a fresh
child, demonstrating the initialization fix on previously unused slots.
Worker markers establish isolation, not overlapping-generation throughput.

The matching fail-fast tool snapshot also passed all 12 short cases on each
normal model variant before the isolated MMQ control. Strict scenario
truncation remains FAIL, while coding-specific output ceilings remain REVIEW.

An early coding run was BLOCKED by a gateway connection EOF after the
foreground SSH session was lost. Its successful grader controls and failed
request remain retained. Subsequent diagnostic gateways wrote directly to
private log files rather than depending on that SSH output pipe.

The completed DFlash2 coding suite remained REVIEW. All eight sandbox
controls passed. The Go first attempt and fresh seeds 1234 and 2026 each
used the fixed 8192-token completion budget on reasoning without final code.
The one predeclared repair stopped naturally after 90 tokens, also without
final code. Python passed its first attempt against all 71 cases. That Python
success does not erase the four Go misses. This persistent bounded Go failure
makes this baseline screen non-discriminating for that task; it is not an
engine-update verdict or a reason to increase budgets retrospectively.

## Native preparation and remaining limits

The matching-source diagnostic image built all five custom probes and both
optional upstream test binaries. All seven selected installed library hashes
matched the normal runtime image. Compilation and hash agreement do not mean
that every native GPU or full-model state gate was executed.

Actual weight-free GPU runs on the normal image then passed all eight top-k
lifetime cases with positive valid dependency callbacks, followed by all
forty convolution cases and 160 rounds with required ROCm matcher checks,
bitexact marked/unmarked outputs and unowned-state checks. These ran with
normal QSA policy flags, unified-memory mode unset and no model work active.
Filtered current-boot kernel observations found no new fault pattern. The
506 custom numerical cases listed in the kit were independently enumerated
from source, not executed as a complete matrix in this campaign.

Both fixed source-control images built successfully in isolated namespaces.
Their manifests preserve the exact single-file mutations and verify that
unselected installed payloads remain unchanged. The MMQ control restores the
corrected upstream activation-sum arithmetic in the locally optimized stack;
it is not a claim that the upstream arithmetic is mathematically wrong. The
QSA control deliberately lowers the owning host matcher's span ceiling, not
an exact reconstruction of an older release.

The isolated MMQ plain-model control was rejected on its first replay. It
stopped naturally after 22 output tokens but returned
`ALDER_937261\n</think>\n\nALDER_937261`, violating the unchanged single-answer
oracle. Fail-fast preserved the result and issued no later short cases.
The normal plain-model image passed the byte-identical request, whose SHA-256
was `0feb5d8cce1a9f3cc737b17bd818a1932d025c300cf31ac987e0570f410e059e`.
Both used the same helper snapshot, model content and gateway policy. The
control's actual patched tree was
`7f55eced0cf2027fa6ba2a0ddebbc8b8bb1ed77b`, with only the selected library
payload replaced. This validates rejection of the known compatibility
regression, not an arithmetic defect upstream or complete source-update
acceptance. No DFlash2 MMQ run was needed after that rejection.

The QSA timing-control campaign and a counterbalanced source-update
comparison were deferred. A failed baseline answer and one timing observation
per condition do not qualify that performance gate. Other GPU classes,
full-model native state checks and a complete new-engine acceptance matrix
are not supplied by this tooling record.

The fresh-maintainer review also identified a code-coverage gap, not merely
unavailable hardware. The tracked recurrent-reset fixture executes CPU
graphs only. The required GPU-backed poisoned-reset gate has no qualified
fixture yet, and the full-model save/restore probe tests different behavior.
The campaign checklist now makes that absence explicit. Implementing it
requires reviewed GPU state ownership, host-only metadata input handling and
rejection of CPU arithmetic fallback, not just renaming the CPU backend.

Both dedicated gateways stopped cleanly at the end. No test containers or
test-port listener remained, and the normal image identity was unchanged.
Experimental images remain in the scoped developer cache; no general image
prune or persistent-content deletion was performed.

Keep raw private artifacts under ignored `build/` or an operator-managed
backup. The tracked runner and fixtures survive deletion of that directory;
historical raw results do not. Follow the
[current acceptance procedure](../testing-and-release.md#llamacpp-source-update-acceptance)
for a later update, not this dated coverage list.
