# Original public toy coding fixtures, version 1

These two fixtures were written from scratch for this developer acceptance
tool. They contain no private project, checkout, client conversation or archived
coding-evaluation task. Their subjects are textbook pure algorithms, not changes
to product code. Similar algorithm names in an older experiment do not make this
a reproduction: prompts, harnesses, trusted implementations and oracles here
have a new identity, `paracetamol.toy-coding.v1`.

Only the task prompt is sent to the model. Harnesses, generated test inputs,
trusted implementations and expected results are public reviewer material, but
are not appended to inference requests. Expected answers are computed by the
Go controller and never mounted into the grading container. Candidate stdout
is parsed and compared externally; printing a test-suite PASS marker cannot
pass grading. This is functional assessment of generated code, not an
adversarial anti-cheating system: candidate code can inspect its execution
environment, and same-process ownership checks are not a security proof.

The Go oracle independently flattens/sorts/deduplicates; the trusted solution
uses a heap. The Python oracle computes overlap connectivity to a fixed point;
the trusted solution sorts and folds. Deterministic edge
and pseudorandom tests include signed-64-bit extrema, duplicates, empty inputs,
nested/touching intervals and ownership checks. Self-test runs trusted answers
and deliberately broken controls in the same confinement used for candidates.
It is not inference and establishes no model-quality claim.

The prompts request appropriate asymptotic complexity, but this bounded grader
checks functional behavior and ownership only. It neither proves nor grades
time/space complexity; the execution ceiling is a sandbox safety limit, not an
asymptotic-performance test.

Grading uses a separately and explicitly built CPU-only image, never a managed
application image or a host toolchain mount. The recipe pins the production
Ubuntu index digest, Go 1.26.8 archive and SHA-256 from
https://go.dev/dl/?mode=json&include=all, and Python 3.14.4 Ubuntu package
versions from https://packages.ubuntu.com/resolute/python3.14 (reviewed
2026-11-15). Other distro dependencies resolve from signed Ubuntu repositories;
the complete installed package manifest is retained in the image. The recipe
does not promise byte-identical package archives. Pin expiry fails the build;
do not loosen a package/version or substitute a host installation.

All executions require a locally built full immutable image ID, rootless
Podman, a read-only root, no network, no capabilities, no-new-privileges, no
GPU/host devices, bounded tmpfs, 2 GiB memory, two CPUs, 128 PIDs and a
120-second lifetime. Only a private staged input directory is mounted read-only,
without relabeling the checkout or system toolchains. SELinux or confinement
failures remain infrastructure failures. Input, raw bounded output, image
inspection, exit status and host-oracle result stay in create-once directories
under ignored build/. Exact CID/image/name/owner checks precede cleanup.

One-shot misses, repairs and independent fresh seeds are separate attempt
identities owned by the campaign. A miss is not a deterministic engine defect,
and a later success must not replace an earlier failure.
