# Routine upgrade runbook

Use this runbook for a periodic review of Paracetamol's upstreams and pinned
dependencies. It is the short operational entry point; the linked maintenance
guides remain authoritative for how each kind of update is performed.

The runbook has two separate operations:

- **scan** discovers and evaluates available updates without changing files,
  images, containers, or persistent data;
- **apply** updates one explicitly selected compatibility unit, validates it,
  and commits the result.

If a request says only “run the upgrade runbook,” perform **scan**. Do not
infer permission to apply every available update. Include the review date in
the report because upstream state is time-sensitive.

## Scan

### 1. Establish the current state

Read [README.md](README.md), inspect `git status --short`, and preserve any
existing user work. Derive current versions from authoritative source files,
not prose snapshots:

```bash
rg -n '^(ARG .*VERSION|ARG .*COMMIT|ARG .*UBUNTU_IMAGE)' Containerfile
rg -n 'identity.Image' internal/application/registry.go
cat containers/content_tools/requirements.txt
cat applications/comfyui/constraints.txt
```

Also inventory source patches under `applications/`. A patch is part of the
selected upstream pin and must be reviewed with it.

### 2. Research candidates

Check primary upstream sources for:

- the Ubuntu base image digest and corresponding dated snapshot tag;
- AMD ROCm/PyTorch packages and all supported device extras;
- content download tooling;
- ComfyUI, ComfyUI-GGUF, and rgthree-comfy;
- llama.cpp and DwarfStar with their native build toolchains at the project
  ROCm version; and
- important pinned application dependencies and security advisories.

Do not treat a moving branch, floating image tag, package index “latest”
label, or unreviewed resolver output as an update candidate. Resolve candidates
to the immutable commit, digest, or exact package versions that an apply
operation would use.

Models, workflows, and catalog metadata are a separate content review. Include
them only when the request asks for catalog updates; then follow
[content-catalog.md](content-catalog.md).

### 3. Review coupling and local deviations

For each candidate, identify:

- pins, image tags, constraints, notices, and documentation that move with it;
- source patches or entrypoint assumptions that touch changed upstream code;
- build targets and CPU checks required;
- GPU architectures needed for honest acceptance; and
- any licensing, network, download, persistence, or isolation change.

A patch that no longer applies is a review stop, not evidence that it can be
deleted. Remove a local patch only after identifying the equivalent upstream
change in the proposed pin or proving that its protected behavior no longer
exists. In particular, review llama.cpp's HIP APU host-buffer patch against
upstream PR 25863, its replacement discussion and the removal gate in
`upgrading.md`. Review `reasoning-controls.patch` against upstream
per-thinking-mode sampling defaults on every llama.cpp update. Native effort
parsing already belongs to upstream.

### 4. Report and recommend

Return a compact report with one row per compatibility unit:

| Unit | Current | Candidate | Recommendation | Coupling and validation |
| --- | --- | --- | --- | --- |
| Example | exact current pin | exact candidate pin | update, watch, or hold | affected images, patches, and hardware |

Use these recommendations consistently:

- **update**: worthwhile now, with an understood validation path;
- **watch**: promising, but too new, incomplete, or awaiting an upstream fix;
- **hold**: incompatible, unverifiable, or not beneficial to this project;
- **current**: no meaningful newer candidate found.

Call out security fixes separately. End with an ordered proposal that keeps
unrelated compatibility units in separate changes. A scan performs no builds
unless the request explicitly expands its scope to include them.

## Apply one compatibility unit

An apply operation needs an explicit unit such as “ROCm/PyTorch,” “llama.cpp,”
or “ComfyUI.” Do not combine convenient neighboring updates merely because
newer versions exist.

### 1. Select the detailed procedure

Use the matching guide:

| Compatibility unit | Procedure |
| --- | --- |
| Ubuntu base | [Upgrade the Ubuntu base](upgrading.md#upgrade-the-ubuntu-base) |
| ROCm and PyTorch | [Upgrade ROCm and PyTorch](upgrading.md#upgrade-rocm-and-pytorch) |
| Content download tools | [Upgrade content download tools](upgrading.md#upgrade-content-download-tools) |
| llama.cpp | [Upgrade llama.cpp](upgrading.md#upgrade-llamacpp) |
| DwarfStar | [Upgrade DwarfStar](upgrading.md#upgrade-dwarfstar) |
| ComfyUI | [Upgrade ComfyUI](upgrading.md#upgrade-comfyui) |
| Bundled ComfyUI extensions | [Upgrade bundled ComfyUI extensions](upgrading.md#upgrade-bundled-comfyui-extensions) |
| Python dependencies | [Upgrade ordinary Python dependencies](upgrading.md#upgrade-ordinary-python-dependencies) |
| Hugging Face tooling | [Upgrade Hugging Face tooling](upgrading.md#upgrade-hugging-face-tooling) |
| Models and workflows | [Content catalog maintenance](content-catalog.md) |

Before editing, run the baseline in [README.md](README.md). Record any
pre-existing failure rather than attributing it to the update.

### 2. Make the bounded change

Update the immutable source pins and their directly coupled dependency locks,
image tags, patches, notices, and documentation. Review full upstream diffs
and dependency-lock diffs. Preserve offline behavior, exact downloads,
rootless confinement, and the supported GPU target set.

During iteration, use `--no-layer-cache` to check prerequisites through their
normal layer cache and retain downloaded-package caches. Use `--no-cache` for
the final cold build required by the selected detailed procedure. Never
substitute a successful CPU startup or image build for GPU inference
acceptance.

### 3. Validate and commit

Run the applicable validation tiers from
[testing-and-release.md](testing-and-release.md), including Tier 1 in full.
Test each affected application, inspect installed versions and `pip check`,
and perform representative hardware acceptance when the change touches GPU
behavior. Update [hardware-acceptance.md](hardware-acceptance.md) only with
results actually observed on the named hardware.

If required hardware is unavailable, finish all safe checks and report the
specific deferred acceptance; do not describe the update as GPU-validated.
Commit the bounded update according to the repository commit discipline, then
report:

- old and new immutable pins;
- important upstream changes and local patch decisions;
- builds and tests completed;
- hardware acceptance completed or deferred; and
- the resulting commit.

For deferred target-hardware work, include the public, host-neutral handoff
from [hardware-acceptance.md](hardware-acceptance.md#deferred-acceptance-handoff)
rather than referring to a contributor's private machine by name.

## Stop and report

Do not force an update through any of these conditions:

- a source patch or deterministic transformation no longer matches;
- the candidate lacks an immutable source or complete dependency closure;
- AMD no longer provides one of the supported GPU targets;
- a license becomes incompatible, ambiguous, or materially different;
- the application introduces an unpinned download, telemetry, or broader
  network requirement;
- container permissions, mounts, writable paths, or device exposure would
  broaden unintentionally;
- installed dependency versions disagree with reviewed pins; or
- a required migration could overwrite or invalidate persistent user data.

Explain the condition, preserve the working state, and recommend the smallest
next investigation.

## Example requests

```text
Run the routine upgrade scan. Do not change anything.

Run the routine upgrade scan, but limit it to ROCm/PyTorch and llama.cpp.

Apply the routine upgrade runbook to llama.cpp using the candidate from the
latest scan. Review whether the HIP APU patch is still required.
```
