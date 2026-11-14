# Historical bounded coding-quality screen

This is the retained November 2026 experiment protocol, not an active runner
or a fresh-checkout acceptance dependency. Its executable kit, exact fixtures,
toolchain manifests and raw results live in ignored private experiment
artifacts. Commands below are historical instructions for owners of those
artifacts, not runnable setup for a new contributor.

Use [llama.cpp maintenance](../llama-cpp-maintenance.md) for current source
acceptance. Preserve this record's attempt identities, hidden-oracle rules,
sandbox limits and limitations when interpreting old evidence.

Tier 1 checks the controller and trusted fixtures without inference. Native
operator/state tests check engine behavior; a generated program's hidden-test
result checks one model/request/runtime combination. None replaces the others.
The Pi runner was retired on 2026-10-16: `evaluations/coding/` preserves frozen
historical inputs, not an active runner or a Maki replacement. See the
[archived evaluation record](coding-agent-evaluation.md). The maintained
`tools/gufo-recheck/` checks three literal/tool cases, not coding quality.

The bounded screen retained for this experiment was an ignored experiment under
`build/refresh-20261113-mmq-compat/`, not a fresh-checkout dependency. Its entry
points are `acceptance/PLAN.md`, `acceptance/run-gateway.sh --phase coding`,
and `acceptance/probe/coding.go`. The separate `acceptance-repair/README.md`
and `acceptance-fresh-seeds/README.md` describe distinct follow-ups. Read the
actual plans and scripts before reuse; their frozen model/image restrictions
may intentionally reject a newer candidate. Do not bypass those guards.

With the retained kit and an absolute candidate checkout, local preflight is:

```bash
coding_work="$PWD/build/refresh-20261113-mmq-compat/acceptance"
(cd "$coding_work" && sha256sum --check --strict INPUTS.sha256)
bash "$coding_work/coding/self-test.sh" --source-only
bash "$coding_work/check.sh" /absolute/candidate-checkout
"$coding_work/gateway-probe" --help
```

`check.sh` compiles/vets trusted helpers against that checkout and runs inherited
fixture tests; it is not model acceptance. Verify current controller `--help`,
compiler version, source/input manifests, full image ID, image labels and
controller SHA-256 before target execution. Use the exact argument lists in
the retained plan, not remembered flags. Prepare and verify the private Go
toolchain with `coding/prepare-toolchain.sh` and `coding/toolchain-identity.sh`
as that plan requires; never mount or relabel the system toolchain. First run
`coding/self-test.sh --image-id FULL_IMAGE_ID` to validate trusted solutions in
the identical sandbox. Infrastructure failure blocks model classification.

The first-attempt screen uses two independent fresh user prompts: Python
`coalesce_ranges(ranges)` merges overlapping or adjacent inclusive integer
intervals from an iterable into sorted tuples, rejects reversed intervals,
and preserves input. Go `MergeSortedUnique(streams [][]int) []int` merges
sorted streams into fresh ascending unique storage, returns a non-nil empty
slice for empty input, and handles all `int` values without overflow.
Contracts cover empty/duplicate/negative/extreme values and ownership;
unchanged hidden tests include deterministic edge and 120 property cases per
language. The Go prompt requests O(N log k), but the tests do not prove that
bound. Requests use `xhigh`, seed 42, cache disabled, gateway-owned sampling,
and output ceilings of 8,192 Python / 16,384 Go tokens. Require natural EOS
and exactly one bounded JSON `code` string before executing generated code.

Execute it only in the retained rootless CPU-only, no-network, read-only,
capability-free, no-new-privileges sandbox: 2 GiB, two CPUs, 128 PIDs,
120-second lifetime and a 30-second Go test timeout. These are untrusted-code
limits, not inference deadlines. Require exact image/owner/CID cleanup, and
retain the gateway's memory/kernel guards and ordinary backend lifecycle.
Run arms sequentially, with no unrelated workload stopped to make room.

Keep three result classes distinct. The original first attempt remains its
own result. One-turn repair preserves the complete original request/assistant
object and appends bounded actual verifier feedback, without diagnosis or a
suggested implementation. The declared fresh-seed expansion changes only Go
seed to 1234 and 2026, with no feedback/history, no new tasks, retries or budget
increase. A later pass never replaces an earlier miss. Do not leak hidden
tests into prompts, weaken an oracle, cherry-pick seeds or raise a budget after
observing a result. New protocols need a separately declared fixture identity.

Retain exact prompts, hidden-test hashes, toolchain identity, requests/replies,
code, verifier logs, finish/token evidence, runtime/sampler reports, source and
image identities, guards and cleanup under ignored `build/`. Classify quality,
output-limit, protocol and infrastructure failures separately. One miss is not
proof of an engine defect or a statistically reliable quality difference.
The final 2026-11-14 performance-recovery matrix did not rerun coding; it must
not be described as fresh coding acceptance. See the dated
[hardware record](hardware-acceptance-history.md#strix-halo-hip-mmq-compatibility-refresh-2026-11-13).

If ignored artifacts are gone, recover their complete hash-verified fixture
and toolchain manifests from retained private evidence before claiming exact
reproduction. Otherwise reconstruct contracts, independent edge/property and
ownership oracles, sandbox controls and predeclared attempts as a new fixture
version. Prose task descriptions alone cannot reproduce the original bytes.
Keep hidden answers and private host identifiers out of tracked documentation.
