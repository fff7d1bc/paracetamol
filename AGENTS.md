# ROCmplete Agent Notes

## Project purpose

ROCmplete bootstraps locally built, rootless ROCm application containers and
verified persistent content for AMD `gfx1150` (Strix Point), `gfx1151` (Strix
Halo), `gfx1200` (Radeon RX 9060 family), and `gfx1201` (Radeon AI PRO R9700
and Radeon RX 9070 family). It currently manages ComfyUI, llama.cpp, and an
experimental high-memory DwarfStar path for DeepSeek V4 Flash.

The project does not publish or depend on prebuilt ROCmplete application
images. A successful build or CPU startup is not evidence that GPU inference
works on any target hardware class.

## Start here

Read `docs/README.md` before a non-trivial change. It routes maintenance work
to the architecture, upgrading, catalog, extension, and testing guides.

Source files and tests are authoritative. When a change affects a count,
version snapshot, command, or behavior described in prose, update that prose
in the same change. Important ownership boundaries are:

- `Containerfile`: content tools, base image, ROCm/PyTorch versions,
  application commits, and final image stages.
- `containers/content_tools/requirements.txt`: complete pinned content-tools
  dependency set.
- `src/rocmplete/config.py`: image tags, ports, container names, profiles, and
  the application registry, capabilities, and runtime defaults.
- `src/rocmplete/hardware_profiles.py`: canonical GPU profile and architecture
  identities.
- `src/rocmplete/cli_parser.py`: public command tree, usage examples, and
  parser defaults.
- `src/rocmplete/cli.py`: command validation, orchestration, and human-facing
  output.
- `src/rocmplete/project.py`: repository-root discovery for source-tree
  resources and build context.
- `src/rocmplete/ui.py`: TTY detection and semantic terminal styling.
- `src/rocmplete/build.py`: local image build command construction.
- `src/rocmplete/runtime/`: constrained application Podman commands.
- `src/rocmplete/layout.py`: host application/content/staging partitions.
- `src/rocmplete/content_verification.py`: durable managed-content verification
  receipts and filesystem-identity invalidation.
- `src/rocmplete/podman.py`: the host process boundary.
- `src/rocmplete/image_archive.py`: validated offline transfer of managed
  build outputs.
- `src/rocmplete/catalog.py` and `catalog/`: catalog schema, pinned content,
  relationships, and benchmark resources.
- `src/rocmplete/recipes.py`: small runnable application recipes shared by
  guided content installation and application guides, including recipe bundle
  validation.
- `src/rocmplete/bundles.py`: download staging, verification, and installation.
- `src/rocmplete/remote_import.py`: allowlisted remote metadata resolution,
  destination inference, and verified ignored local-pack generation.
- `tools/*_probe.py`: read-only source, archive, and workflow catalog research.
- `src/rocmplete/workflows.py`: deterministic workflow transformation and
  provenance.
- `src/rocmplete/benchmark.py`: benchmark preparation, execution, results,
  suite resume, and cleanup.
- `src/rocmplete/llama_benchmark.py`: native llama-bench result capture,
  metadata, atomic writes, and cleanup.
- `bin/rocmplete`: PATH-friendly delegation to the checkout launcher.
- `src/rocmplete/agent_models.py` and `src/rocmplete/agent_sandbox.py`: shared
  agent-client model policy and bubblewrap boundary.
- `bin/opencode`, `bin/pi`, `bin/omp`, `bin/maki`,
  `src/rocmplete/opencode.py`, `src/rocmplete/pi_agent.py`,
  `src/rocmplete/omp_agent.py`, and `src/rocmplete/maki_agent.py`: runtime
  client launch and local model-catalog generation below the public `agent`
  command group.
- `containers/common/profile.py` and application entrypoints: container-side
  profile enforcement and application policy.
- `applications/<application>/`: application-owned dependency pins,
  entrypoints, and strict, reviewable deviations from pinned upstream sources.
- `tests/`: enforced behavior and expected command shapes.

Keep `README.md` and `docs/guides/` user-facing. `docs/README.md` is the shared
documentation index, while the other pages directly below `docs/` are
maintainer-facing. Do not make temporary analysis or project history into a
durable source of truth.

## Behavior to preserve

Treat these as architectural invariants unless the task explicitly redesigns
them:

- Application images are built locally from immutable base, source, and
  dependency pins. Separate applications have separate final images. PyTorch
  applications use the explicitly tagged, locally built ROCm/PyTorch
  prerequisite and never pull it. PyTorch and native images both derive from
  the explicitly tagged minimal ROCm runtime; native applications do not
  inherit the unrelated PyTorch payload.
- Every managed GPU application follows the single project `ROCM_VERSION`.
  Native build tooling does not permit a second ROCm release. Upgrade the
  shared runtime, PyTorch base, application tags, and native toolchains
  together, then accept the resulting tuple on the applicable hardware
  classes.
- Managed containers are rootless, read-only, capability-free, and use
  `no-new-privileges`. Each application receives only its own writable
  `/data`; managed content is mounted separately and read-only below
  `/content`; temporary writes use explicit `tmpfs` mounts.
- GPU runs expose `/dev/kfd` and only the exact selected render-node set. CPU
  mode exposes neither. Multiple render nodes are never guessed, and an
  application must explicitly declare support before one workload may receive
  more than one.
- Forced profiles are checked against the architecture PyTorch sees.
  Python profile identities come from `src/rocmplete/hardware_profiles.py`;
  Containerfile target lists and shell-only entrypoint mappings must agree
  with it.
- Web applications use private rootless networking and publish exactly one
  TCP port. The default host address is `127.0.0.1`. An explicit non-loopback
  address has no authentication and must remain visibly warned about.
- Downloads use full Hugging Face revisions or exact Civitai model-version
  IDs, plus exact byte sizes and SHA-256 hashes. Applications must not
  silently fetch unpinned model content.
- Managed content is runtime-ready only with a current verification receipt.
  Missing or stale receipts require a normal installer pass; runtime gates do
  not silently hash large files.
- Remote imports remain one-file, allowlisted, schema-valid content packs and
  reuse the normal installer; they never become an arbitrary URL downloader.
- Missing or ambiguous license information remains `NOASSERTION` and requires
  explicit acknowledgment. Do not infer a repack's license from its upstream
  base model.
- Curated workflows derive from pinned licensed sources and retain source,
  rendered-hash, and license provenance. Existing differing workflows are
  user modifications unless replacement is explicitly forced.
- Source patches, workflow renderers, catalog validation, and benchmark
  transformations fail closed when upstream structure or pinned bytes change.
- Cleanup is scoped to ROCmplete-owned resources. Never introduce a general
  Podman or system prune.

Use project terminology precisely:

- a **profile** selects hardware behavior;
- an **application** is an independently built image or service;
- a command **mode** is an operation such as llama.cpp `server` or `cli`;
- a **bundle variant** selects model precision or behavior;
- a workflow **renderer** deterministically adapts one pinned source graph.

Keep these dimensions orthogonal instead of encoding their cross-product in
top-level commands or profile names.

For llama.cpp, keep guided content recipes organized by model family. A
recipe may install multiple reviewed variants from the same family when they
form one practical selection, as `qwen3.6` does. Give unrelated families
separate recipes even when they share a use case; do not group them under
subjective role names such as `agents`, `assistants`, or `coding-models`.
Precision, MTP, and other advanced controls may remain exact bundles when they
would only clutter the guided menu.

## Working agreements

### Current project phase: public pre-release

ROCmplete is public but has not yet promised stable interfaces. Backward
compatibility is a design consideration, not a release constraint. Avoid
arbitrary interface churn, but do not preserve an awkward command, state
layout, module boundary, schema, image structure, or internal API merely
because it already exists. Breaking changes must be deliberate, coherent, and
documented for current users.

When the existing structure obstructs a feature, creates conceptual overlap,
or makes the system harder to maintain, actively consider a coherent refactor
or breaking redesign, including changes with a large blast radius. Prefer
making the whole design fit together now over accumulating compatibility
layers before a stable release. Reassess this policy before declaring stable
interfaces or when this section is removed. This does not relax the project's
mutation safety, verification, licensing, isolation, or testing requirements.

- Preserve existing user changes and untracked files. Inspect
  `git status --short` before editing and keep unrelated work out of the diff.
- Prefer the smallest direct design that satisfies the current need. Do not
  add speculative layers, flags, configuration, applications, or dependency
  machinery for possible future use.
- Fix a shared invariant at its owning boundary rather than duplicating
  defensive checks at callers.
- Preserve Python 3.12 compatibility. The host launcher intentionally uses the
  standard library; add a host dependency only for a concrete need that cannot
  be met cleanly by existing facilities.
- Container dependencies must be pinned and consistent. Do not loosen pins,
  add `--no-deps`, or weaken `pip check` merely to make an image build.
- New build-context inputs must be added to the `.containerignore` allowlist.
- Keep application, profile, mode, bundle, and renderer changes in their
  documented extension points. Do not expose an upstream command surface
  wholesale.
- Update user documentation when commands, flags, defaults, state locations,
  exit behavior, or requirements change. Update maintainer docs when ownership
  or maintenance procedure changes.

## Commit discipline

A feature, bug fix, or other requested file-changing delivery is not complete
until its intended changes have been committed. Create one or more focused,
independently reviewable commits before reporting completion; do not wait for
the user to ask for a commit. Read-only reviews and brainstorming remain
non-mutating and must not create an empty or unrelated commit.

Before every commit:

- inspect `git status --short` and the complete working-tree diff, including
  every untracked file intended for the commit;
- run `git diff --check`, focused tests, and the relevant broader checks;
- stage only explicit intended paths, never `git add .` or `git add -A`;
- inspect the complete staged diff and `git diff --cached --name-only`; and
- keep unrelated user changes and untracked files out of the commit.

After every commit, inspect `git show --stat HEAD` and `git status --short`.
Do not amend, squash, rebase, or otherwise rewrite commits unless the user
explicitly requests it.

## CLI, subprocess, and mutation safety

- Keep parsing and subcommand structure explicit and covered by focused tests.
  Usage text, copyable examples, defaults, environment precedence, and README
  examples must agree.
- Use explicit subprocess argument lists, never shell strings containing
  user-controlled values. Prefer structured external output over parsing
  human-oriented output.
- Do not impose arbitrary deadlines on builds, downloads, inference, or other
  intentionally long operations. Bounded probes and local HTTP checks should
  have appropriate timeouts and controlled errors.
- Keep filesystem, process, clock, network, and device boundaries small enough
  to substitute in tests.
- Validate the complete content or cleanup plan before mutation when
  practical. Refuse unsafe paths, collisions, unexpected file types, size/hash
  mismatches, and ambiguous device selection before destructive work.
- Preserve resumable staging and atomic installation. A partial download must
  not look installed; installed bytes must be validated before staging is
  moved or removed.
- Local mirror reuse must remain size- and SHA-256-gated. Never move a
  candidate outside the validated mirror root, allow mirror/data overlap, or
  remove failed or interrupted staging.
- Destructive persistent-data operations require an explicit scope and
  confirmation. Preserve irreplaceable `custom-loras`, `input`, `output`,
  `state`, `user`, and benchmark data unless the selected operation clearly
  owns them.
- Make child-container, temporary-cache, and benchmark cleanup explicit.
  Preserve `finally`-style cleanup and suite checkpointing when failures or
  interruptions occur.
- Keep dry runs honest: use the real planning and validation paths, start no
  workload, download nothing, delete nothing, and do not create the persistent
  data directory.
- When partial success is possible, report what completed, what remains, and
  whether retrying resumes or repeats the work.

ROCmplete's CLI is currently human-oriented. Do not silently introduce a
machine-readable stdout contract or move established output between stdout and
stderr as incidental cleanup; define and test such a contract deliberately.

## Testing and verification

Test behavior and failure paths in proportion to risk. Prefer existing
`unittest` style, temporary directories, small fixtures, and fakes at network,
Podman, process, clock, and filesystem boundaries.

For every change, run the applicable Tier 1 checks:

```sh
python3 -m compileall -q applications containers src/rocmplete tests tools
bash -n applications/comfyui/entrypoint.sh \
  applications/llama-cpp/entrypoint.sh \
  applications/dwarfstar/entrypoint.sh
python3 -m json.tool catalog/catalog.json >/dev/null
PYTHONPATH=src python3 -m unittest discover -s tests
git diff --check
```

Also apply the relevant higher tier:

- CLI changes: inspect `--help`, incomplete-command guidance, and affected
  CPU/dry-run command composition.
- Catalog or workflow changes: load the full catalog and run
  `./rocmplete content install all --dry-run`; check counts, sizes, licenses,
  relationships, hashes, and rendered graphs.
- Container, dependency, patch, or entrypoint changes: build every affected
  target and run `pip check`; build all targets without cache for shared-base
  or release-sensitive changes.
- Web application changes: perform CPU-only startup on loopback and confirm
  clean stop/removal. CPU mode is not inference acceptance.
- Runtime security changes: assert read-only root, dropped capabilities,
  exact mounts, networking, device exposure, and CPU device absence.
- Benchmark changes: use deterministic dry-runs first, preserve result/suite
  compatibility rules, and test cleanup and resume failure paths.
- Profile, ROCm, PyTorch, memory-policy, or inference changes: perform hardware
  acceptance on `gfx1150`, `gfx1151`, `gfx1200`, and `gfx1201` as applicable.

Ordinary development checks on a host without suitable target GPU capacity
must not run AI inference. Podman image builds, unit tests, catalog checks, CPU
startup, and non-inference diagnostics remain appropriate. Distinguish code
failures from unavailable hardware, host permissions, external services, or
gated content.

## Performance and maintenance rationale

Measure before optimizing. Compare the same image, catalog pins, profile,
render-node set, seeds, run count, cache mode, and runtime policy. Keep
benchmark inputs deterministic and machine-specific results out of catalog
source.
Performance acceptance belongs on representative target hardware.

For concurrent llama.cpp server measurements, make the client worker count
explicit and inspect the running server's slot and context settings. Report
aggregate generated tokens divided by total wall time, lines or requests per
second, and per-request latency. Do not sum per-request token rates. Validate
output quality and truncation because a throughput improvement is not useful
when it changes the workload's result.

During non-trivial changes, keep concise rationale near code where a plausible
refactor could break hidden behavior. Important hotspots include:

- duplicated host/container profile validation;
- exact-match upstream patching;
- Podman confinement argument ordering and ownership;
- content-addressed tree links and regular-file refusal;
- resumable staging, atomic moves, and approximate progress accounting;
- workflow source/rendered hashes and provenance fields;
- benchmark signature inputs, incremental suite state, and cleanup.

Comments should explain intent, coupling, operational limits, or revisit
conditions—not restate code or invent history.
