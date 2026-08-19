# Testing, release checks, and housekeeping

Validation is layered so ordinary development on a host without suitable
target GPU capacity remains useful without pretending it replaces
target-hardware acceptance.

## Validation tiers

### Tier 1: static and unit checks

Run for every change:

```bash
python3 -m compileall -q applications containers src/rocmplete tests tools
bash -n applications/comfyui/entrypoint.sh \
  applications/llama-cpp/entrypoint.sh \
  applications/dwarfstar/entrypoint.sh
python3 -m json.tool catalog/catalog.json >/dev/null
git diff --check
PYTHONPATH=src python3 -m unittest discover -s tests
```

If a shell entrypoint or JSON manifest is added, include it explicitly.

Unit tests cover catalog invariants, download command confinement, native-tree
behavior, workflow rendering, research probes, benchmark preparation, CLI
resolution, profile detection, Podman command construction, and local Markdown
links and section anchors. Add tests for behavior, not merely new lines or
parser choices.

`.github/workflows/checks.yml` runs this dependency-free surface on Python 3.12
and Python 3.14. It also exercises representative CLI dry-runs without Podman.
Hosted CI does not replace local image builds, CPU startup, provider-network
checks, or target-hardware acceptance.

### Tier 2: CLI and dry-run checks

Exercise user-visible composition:

```bash
./rocmplete --help
./rocmplete guide comfyui
./rocmplete run --help
./rocmplete images export all --output /tmp/rocmplete-images.tar --dry-run
./rocmplete doctor --help
./rocmplete content install all --dry-run
./rocmplete run comfyui --profile cpu \
  --listen 127.0.0.1 --dry-run
./rocmplete run llama-cpp server --model /path/to/model.gguf \
  --profile cpu --listen 127.0.0.1 --dry-run
./rocmplete content install llama-cpp qwen3.6 --dry-run
./rocmplete run llama-cpp server --router \
  --profile cpu --listen 127.0.0.1 --dry-run
./rocmplete run dwarfstar server --profile strix-halo --dry-run
./rocmplete agent --help
./rocmplete agent install pi --help
./rocmplete agent pi --help
./rocmplete agent pi --no-sandbox -- --help
./rocmplete agent pi -- list
./rocmplete agent pi -- install --help
./rocmplete agent pi -- update --extensions --help
./rocmplete agent maki --help
./rocmplete agent maki --no-sandbox -- --help
./rocmplete agent maki -- index src/rocmplete/cli.py
./rocmplete benchmark llama-cpp \
  --preset qwen3-0.6b-q8-0 --profile cpu --dry-run
./rocmplete benchmark llama-cpp \
  --preset qwen3-0.6b-q8-0 --compare-backends --dry-run
./rocmplete benchmark llama-cpp \
  --preset qwen3-0.6b-q8-0 --context-depth 32768 \
  --cache-type-k q8_0 --cache-type-v q8_0 --flash-attn on --dry-run
./rocmplete acceptance run --dry-run
```

For remote-import changes, also exercise one exact Hugging Face LFS file and
one exact Civitai version with `content import ... --dry-run`. Confirm that
the plan shows a full Hugging Face commit or exact Civitai version/file,
provider size and SHA-256, the intended destination, `NOASSERTION`, and the
ignored local-pack path. The dry run must not create that pack or the selected
data directory. Treat provider/network/authentication failure as an external
acceptance limitation, not a unit-test replacement.

Inspect resolved commands for:

- correct image and container name;
- a private network namespace and exactly one TCP publication on the selected
  host IP for web applications;
- read-only root;
- dropped capabilities and `no-new-privileges`;
- `--userns keep-id` and an explicit host-derived `--umask` for commands with
  a persistent `/data` bind mount;
- only intended bind mounts;
- no GPU devices in CPU mode;
- exactly `/dev/kfd` plus the complete selected render-node set in GPU mode;
- offline/network-none behavior where promised.

For agent-client sandbox changes, run real Pi and Maki bubblewrap
probes on Linux. Confirm each client starts and exits cleanly, its launch
directory and private XDG state are writable, and the real home, SSH agent,
inherited token variables, ordinary client state, and GPU devices are absent.
Confirm that the host loopback llama.cpp endpoint remains reachable. On a host
with external connectivity, resolve one external hostname from inside the
sandbox, especially when `/etc/resolv.conf` points into `/run`. Repeat with
Linuxbrew client installations because their prefix lives below `/home`, which
the sandbox otherwise hides. Confirm `AGENTS.md` loads in both clients while
unapproved project `.pi` resources do not affect Pi.

For managed Pi picker changes, also open the configured model-selection
shortcut and bare `/model`, confirm models remain in stable family groups,
and select a reasoning-capable model. Its supported reasoning selector must
open immediately. Confirm exact `/model PROVIDER/MODEL` does the same while
model cycling and session restoration remain prompt-free.

On Fedora-family hosts where `/home` links to `/var/home`, confirm that both
absolute spellings resolve to the mounted project while sibling home content
remains absent.

For image-archive changes, additionally create a tiny disposable local image,
save it as a Docker archive, inspect it through `image_archive.py`, remove and
load it, compare the image config ID, and remove it again. Do not use a full
ROCmplete export merely to exercise archive plumbing on every development
pass.

### Tier 3: image builds

Build every affected target. During ordinary iteration, force the affected
image instructions to run while retaining locally downloaded Python packages:

```bash
./rocmplete build comfyui --no-layer-cache
```

Before a release or after base/dependency changes, perform a genuinely cold
build of all targets:

```bash
./rocmplete build all --no-cache
```

The output must show one `content` tools build, one minimal `runtime` build,
one shared PyTorch `base` build, and all application builds. A cache-free
aggregate build must not reinstall a prerequisite once per application.
PyTorch application builds reference the newly tagged PyTorch base with
`--pull=never`; native applications reference the lower runtime with
`--pull=never` and must not reference the managed PyTorch base.

`--no-cache` bypasses both Podman's image-layer cache and ROCmplete's host pip
download cache. `--no-layer-cache` bypasses only the selected application's
image layers; prerequisite targets still pass through their normal layer
cache. It is suitable for repeated local build testing where downloading the
same multi-gigabyte AMD wheels adds no coverage.

Check installed dependency consistency:

```bash
for app_image in $(PYTHONPATH=src python3 -c \
  'from rocmplete.config import APPLICATIONS; print(*(item.image for item in APPLICATIONS.values() if item.shared_pytorch_base))')
do
  podman run --rm --entrypoint /opt/venv/bin/python \
    "$app_image" -m pip check
done
```

For llama.cpp, inspect the exact labels and native runtime closure:

```bash
podman image inspect CURRENT_LLAMA_IMAGE
podman run --rm --network none --read-only --cap-drop all \
  --security-opt no-new-privileges \
  --entrypoint /usr/local/bin/llama-cli CURRENT_LLAMA_IMAGE --version
```

The image must contain `llama-cli`, `llama-server`, and `llama-bench`, have no
unresolved `ldd` entries, link both `libggml-hip` and `libggml-vulkan`, and
retain rocBLAS/hipBLASLt payloads only for `gfx1150`, `gfx1151`, `gfx1200`,
and `gfx1201`. CPU startup is not GPU inference acceptance.

For DwarfStar, run `ds4`, `ds4-server`, and `ds4-bench` help with the
entrypoint overridden, inspect all three with `ldd`, and confirm that the
final image has no `ds4-agent`, `ds4-eval`, GCC build toolchain, Git checkout,
ROCm development wheel, or PyTorch package. The minimal ROCm core wheel owns
the HIP compiler driver and LLVM runtime needed by the packaged runtime.
Confirm the image label and HIP code cover the canonical four architectures.
Then run `acceptance --application dwarfstar` on each memory-capable hardware
class. Starting a source-built binary on the build host is not inference
acceptance.

For the optional DSpark path, first dry-run `run dwarfstar server --dspark`
and confirm that it selects the exact managed target/support pair in one
read-only mount. On target hardware, compare fixed temperature-zero prompts
with DSpark off and on, verify the output rather than throughput alone, and
exercise a 128K server request carrying `"temperature": 0`. Inspect kernel
logs for mapping faults, GPU resets, and process crashes. DSpark CLI mode
forces temperature zero; nonzero server sampling is outside the supported
contract.

Inspect image metadata and history when pins or licenses changed:

```bash
podman image inspect CURRENT_IMAGE
podman history CURRENT_IMAGE
```

### Tier 4: CPU-only startup

CPU mode is a startup and HTTP smoke test, not an inference test:

```bash
./rocmplete run comfyui --profile cpu \
  --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8188/ >/dev/null
./rocmplete logs comfyui
./rocmplete stop comfyui

./rocmplete run llama-cpp server --router --models-max 1 \
  --profile cpu --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/v1/models
./rocmplete stop llama-cpp
```

Use different ports if they are occupied. Confirm startup banners show `cpu`
and that containers disappear after stopping. Run this check from a shell with
`umask 0077` as well: immutable profile/downloader helpers must remain readable
to the keep-id process, `/tmp/comfy` must be writable, and persistent files
must retain the private host mask.

The llama.cpp router check requires `content install llama-cpp qwen3.6`,
but does not load a model. Confirm `qwen3.6-27b-mtp-q8-0` and
`qwen3.6-35b-a3b-mtp-ud-q8-k-xl` are present and unloaded.
The pinned upstream router may also advertise its reserved empty `default`
preset; that is not managed content and should not be selected during the
smoke test.

For DwarfStar agent-client integration, start the managed 128K DwarfStar
server, select the matching `dwarfstar/deepseek-v4-flash-0731-q2-imatrix`
provider/model in Pi and Maki, then complete one read plus function-tool round
trip in each. Confirm Pi's off and high choices and Maki's off and high paths.
Confirm the generated providers follow
`--dwarfstar-port`. Do not claim agent
compatibility from `/v1/models` or a plain text response alone.

For the maintained llama.cpp reasoning matrix, exercise each native choice
through Pi: Qwen3.6 off/on; Qwen3.8 off, low, medium, and xhigh; and Muse low,
medium, high, and xhigh. Confirm Pi omits unsupported values. For Maki
0.4.8 at or after commit `a9495e1`, capture the actual request fields for the
same matrix. Confirm Qwen3.6 maps every enabled name to its on toggle,
Qwen3.8 `high` snaps to medium, Muse off clamps to low, and adaptive follows
the selected family's managed default. Exercise new interactive sessions:
Maki commit `a9495e1` does not apply `always_thinking` in `--print` mode, so
that path cannot validate the generated default or named selector. Keep these
protocol checks separate from cross-model quality benchmarking.

After a Pi update, also verify the transport rather than trusting the selector
label: every exposed Qwen3.6 preset must use `qwen-chat-template`, and every
Qwen3.8 preset must use Pi's `openai` thinking format with off mapped to
`none`. Run one live representative of each distinct template path. Thinking
off must produce no reasoning block and must select the server's non-thinking
sampling defaults; a TUI status line saying `thinking off` is not evidence of
either behavior.

For coding-agent evaluation changes, validate the frozen inputs before using
GPU time:

```bash
./rocmplete benchmark agent --list-tasks
./rocmplete benchmark agent \
  --preset qwen3.6-27b-mtp-q8-0 \
  --thinking high --task re-align --dry-run
PYTHONPATH=src python3 -m unittest tests.test_agent_evaluation
```

Every implementation hidden test must fail on its recorded base commit, pass
on its reference commit, and grade the complete reference diff as `solved`.
Inspect the generated fixture to confirm it has exactly one Git commit, no
remote, controlled `AGENTS.md`, and no mounted hidden-test sibling. Confirm a
review task can change only `ROCMLETE_EVAL_ANSWER.md`, dependency changes are
reported and restored before grading, and recognized network commands make an
attempt unsolved. Dry runs must create no data, source mirror, server, fixture,
or result.

Target-hardware acceptance uses Pi, ROCm, one exact host, 131072 tokens, and a
fresh fixture and session per attempt. First run one easy and one safety task
for a bounded integration check, then run the frozen selection. Inspect Pi's
JSONL, the per-attempt server-log delta, ordinary and hidden test logs, build
log, patch hash, and Markdown solve rate. An unsolved task is a valid completed
measurement; infrastructure failure and interruption must still checkpoint
and clean up the exact model container. Keep machine-specific run trees and
results out of the repository.

Speculative-decoding catalog changes additionally require one single-model
dry run and router INI inspection for every affected strategy. Cover an
embedded MTP draft, a separate MTP draft, and a separate DFlash draft when
those paths change. Confirm the target and draft are both hash-verified before
startup, the draft path is mounted through the existing read-only model
partition, and no speculative arguments appear for ordinary presets. CPU
startup validates argument shape only; it is not a correctness or performance
result for speculative decoding.

Changes to the llama.cpp share report require direct and router tests. Confirm
that `status llama-cpp` reports the running image ID, source labels, resolved
profile, exact PID 1 command, and redacted authentication state. In router
mode, confirm that no model selection lists the mounted preset identifiers and
that `--model PRESET` reports its context, reasoning and sampling policy,
profile-specific cache and Flash Attention choices, and backend-specific draft
depth. Start with `auto` on target hardware so the report proves it shows the
resolved profile rather than merely echoing the request.

A context-metadata override additionally requires single-model and router
inspection for the exact `override-kv` values and disabled fitting. Hardware
acceptance must exercise prompts beyond the GGUF-declared window and compare
retrieval, output quality, memory, and speculative acceptance with the normal
metadata-backed preset. Successful allocation alone is not acceptance.

### Tier 5: GPU diagnostics

On each target host, use the finite
[target-hardware acceptance matrix](hardware-acceptance.md):

```bash
./rocmplete doctor --render-node /dev/dri/renderD128
./rocmplete run comfyui --profile EXPECTED_PROFILE \
  --render-node /dev/dri/renderD128 --listen 127.0.0.1
```

Verify:

- RX 9060 family cards report `gfx1200` and resolve `rdna4`;
- Radeon AI PRO R9700 and RX 9070 family cards report `gfx1201` and resolve
  `rdna4`;
- Strix Halo reports `gfx1151` and resolves `strix-halo`;
- Strix Point reports `gfx1150` and resolves `strix-point`;
- forcing either other profile fails;
- `auto` succeeds;
- multiple render nodes require an explicit complete selection;
- Doctor performs a tensor operation on every explicitly selected GPU and
  rejects a mixed-architecture set;
- one llama.cpp server selected on two matching cards reports managed layer
  splitting and runs a model that exercises both cards;
- one ComfyUI process selected on two matching cards completes a graph using
  built-in component-placement or multi-GPU nodes;
- device permissions work without changing host groups or broadening
  container privileges.
- enforcing SELinux hosts report `container_use_devices`, fail clearly while
  it is off, and pass a real GPU tensor operation after it is enabled without
  disabling container labels;
- hosts exposing AppArmor's unprivileged-user-namespace sysctl report whether
  it is active; a nonzero value prints the persistent opt-out, apply command,
  bubblewrap impact, and system-wide security scope without changing the host;
- Strix Halo and Strix Point diagnostics report system RAM, the active TTM
  module and ceiling, and effective GTT; undersized GTT prints the expected
  RAM-aware host recipe without changing the host. The additional KFD kernel
  warning remains specific to Strix Halo.
- an OSTree boot with rpm-ostree available receives a transactional
  `rpm-ostree kargs` TTM recipe; a conventional GRUB host receives an owned
  `/etc/default/grub.d` drop-in and `update-grub`; a conventional Fedora host
  with `grubby` receives an all-kernel boot-entry update; other conventional
  hosts retain the active-module `modprobe.d` and detected initramfs-tool
  fallback.

### Tier 6: representative inference

Run this only on suitable target hardware. For changes affecting a family,
test at least:

Begin with the bounded checkpointed smoke:

```bash
./rocmplete acceptance run
```

Keep its JSON and Markdown result. A `BLOCKED` result means generated media
still needs human review; resume the same result rather than rerunning passed
cases. Then exercise the additional matrix rows coupled to the change.

- each changed llama.cpp speculative preset against a non-speculative control,
  recording prompt processing, generation speed, peak memory, acceptance
  behavior, and output sanity on `gfx1150`, `gfx1151`, `gfx1200`, and
  `gfx1201` where the preset is practical;

- one base and one accelerated bundle;
- every affected precision;
- T2V and I2V/edit paths when both exist;
- native application and ComfyUI paths if both consume related content;
- balanced memory policy;
- conservative policy on either RDNA 3.5 APU or another constrained-memory
  case.

Record the image tag, Git commit, profile, render-node set, model bundle,
policy, and result. Managed benchmark JSON records much of this for ComfyUI.

For llama.cpp, run the same catalog preset and explicit pp/tg/repetition
tuple with both ROCm and Vulkan on all target hardware classes. Preserve the
native result JSON below `apps/llama-cpp/benchmarks`; it records the selected
backend. Do not compare a catalog-pinned result with an unhashed local-model
result.

### llama.cpp source-update acceptance

Use this finite sequence after moving `LLAMA_CPP_COMMIT` or rebasing a native
backend patch. It turns the general validation tiers into a repeatable update
gate without assuming that one contributor owns every hardware class.

1. **Host-independent checks:** run Tier 1, perform the final no-cache
   `llama-cpp` build, inspect image labels and history, check the native
   runtime closure with `ldd`, and exercise CPU CLI and router startup. Confirm
   that all four GPU targets remain in the image and that no source, model, or
   web asset was fetched at runtime.
2. **Primary target acceptance:** on one capable machine, run `doctor`, the
   bounded acceptance smoke, real CLI and router generation, both API
   endpoints, reasoning controls, speculative and non-speculative generation,
   and interruption cleanup. Then run the benchmark matrix below for every
   backend or patch path affected by the update.
3. **Cross-architecture spot checks:** on each remaining applicable hardware
   class, run `doctor`, a tiny-model GPU smoke, and the smallest representative
   benchmark that exercises the changed backend path. A profile-specific
   change does not require unrelated large-model testing, but generic HIP,
   Vulkan, device-selection, or model-loading changes do.
4. **Handoff:** record unavailable hardware as deferred rather than silently
   treating the primary machine as universal acceptance. Use the handoff
   format in [hardware-acceptance.md](hardware-acceptance.md#deferred-acceptance-handoff)
   and provide commands with every default made explicit.

Use catalog-managed models and keep the image, model, profile, render nodes,
prompt sizes, generation sizes, repetitions, batch sizes, cache types, Flash
Attention policy, and context depth identical between comparisons. The
minimum matrix is:

| Case | Purpose | Minimum comparison |
| --- | --- | --- |
| Tiny smoke | Detects basic load, offload, and generated-output failures cheaply. | ROCm and Vulkan, 32 prompt tokens, 16 generated tokens, one measured repetition. |
| Representative f16 KV | Detects ordinary dense or sparse long-context regressions. | Both backends at shallow depth and at least 32K context, 512 prompt tokens, 128 generated tokens, three repetitions. |
| Representative q8_0 KV | Exercises quantized-KV Flash Attention and its memory/performance tradeoff. | Repeat the long-context case with both K and V explicitly set to `q8_0` and Flash Attention explicitly enabled. |
| Speculative control | Separates model/runtime changes from speculative-decoding behavior. | The same managed family, prompt, and backend with MTP or DFlash enabled and disabled; record acceptance rate as well as aggregate timing. |
| Service behavior | Detects integration regressions outside `llama-bench`. | Router model selection, Chat Completions, Responses, reasoning levels, one-shot CLI exit, and clean stop or interruption. |

The exact representative model may change with the catalog. State why the
selected dense or sparse model covers the changed code, and do not substitute
a faster unrelated architecture merely because it is convenient.

Do not treat one successful tiny workflow as acceptance of every model family.

## Release checklist

ROCmplete does not currently publish prebuilt images, so “release” means a
source state from which users build locally.

### Source and documentation

- [ ] Working tree contains only intended changes.
- [ ] All new upstreams use full commits or image digests.
- [ ] Default image tags match the pins.
- [ ] No stale version or old command remains in docs or tests.
- [ ] `THIRD_PARTY_NOTICES.md` matches built and downloaded components.
- [ ] `catalog/README.md` describes new content/license classes.
- [ ] User README quick-start and focused `docs/guides/` examples are current.
- [ ] Maintainer docs still point to real files and commands.
- [ ] Local Markdown links and section anchors pass the unit-test check.

Useful searches:

```bash
rg -n 'TODO|FIXME|WIP|latest|main' \
  Containerfile applications catalog src/rocmplete docs README.md
rg -n 'localhost/rocmplete:|_COMMIT|_VERSION' .
```

`latest` or `main` may appear in explanatory prose, but must not be a stored
source revision or image base.

### Catalog and provenance

- [ ] Catalog and every tree manifest parse as JSON.
- [ ] All model revisions are full 40-character commits.
- [ ] Sizes and SHA-256 values come from pinned metadata or verified bytes.
- [ ] License URLs point to the exact relevant source/revision where possible.
- [ ] `NOASSERTION` is used for unresolved repacks/conversions.
- [ ] Required agreements are attached.
- [ ] Every bundle is in `all` and intended selector groups.
- [ ] Every workflow bundle has a pinned benchmark.
- [ ] `content install all --dry-run` counts and disk totals look intentional.

### Automated and container checks

- [ ] Static checks and full unit suite pass.
- [ ] All images build from cache.
- [ ] All images build without cache after base/dependency changes.
- [ ] `pip check` passes in each image.
- [ ] CPU HTTP smoke passes for web applications.
- [ ] Batch dry-runs are confined and reference installed paths.
- [ ] No managed test container remains:

  ```bash
  podman ps -a --filter name=rocmplete
  ```

### Hardware acceptance

- [ ] The finite [target-hardware acceptance matrix](hardware-acceptance.md)
      records current results or explicit `N/P` reasons.
- [ ] `acceptance run` passes, or its checkpointed result records reviewed
      failures and explicit profile-level `N/P` cases.
- [ ] RX 9060 family diagnostics and forced/auto profile tests pass.
- [ ] R9700 or RX 9070 family diagnostics and forced/auto profile tests pass.
- [ ] Strix Halo diagnostics and forced/auto profile tests pass.
- [ ] Strix Point diagnostics and forced/auto profile tests pass.
- [ ] Representative inference passes for affected applications/families.
- [ ] Memory and experimental kernel policies are tested if changed.
- [ ] Relevant managed benchmark completes and records expected metadata.
- [ ] A relevant `benchmark suite --dry-run` resolves the intended ordered
      bundle set without starting a container.
- [ ] Suite resume skips intact completed entries and rejects changed
      catalog/runtime inputs, rebuilt image IDs, and mismatched result files.

## Routine housekeeping

### Before working

```bash
git status --short
PYTHONPATH=src python3 -m unittest discover -s tests
podman ps -a --filter name=rocmplete
```

This distinguishes pre-existing local changes and containers from work created
in the current session.

### Periodically

- Review upstream releases and security notices for Ubuntu, ROCm/PyTorch,
  ComfyUI, ComfyUI-GGUF, rgthree-comfy, and llama.cpp.
- Review model cards and licenses at pinned revisions for later changes or
  takedowns.
- Check whether gated-model access behavior changed.
- Build without cache to expose transitive dependency drift.
- Run `pip check` in all final images.
- Run `content install all --dry-run` and compare counts/size with user
  documentation.
- Verify a rotating sample of installed bundles by full SHA-256.
- Inspect disk use:

  ```bash
  podman system df
  du -sh "${XDG_DATA_HOME:-$HOME/.local/share}/rocmplete" 2>/dev/null
  ```

- Back up user-owned persistent subtrees.

Do not automate dependency or model upgrades directly into the main branch.
Discovery may be automated; pin selection, license review, and compatibility
acceptance require maintainer judgment.

## Backup and recovery

The [persistent-data](guides/operations.md#persistent-data) and
[scoped-cleanup](guides/operations.md#scoped-cleanup) sections define what is
irreplaceable and what each cleanup scope owns. Review those paths and back up
user input, output, state, custom models, workflows, and benchmark results
before any destructive release housekeeping. Catalog content and generated
caches are reproducible, although replacing them may be expensive.

For a safe rebuild:

1. Stop applications.
2. Back up user-owned data.
3. Build new image tags.
4. Start in CPU mode.
5. Run GPU diagnostics.
6. Test existing content without forcing workflow replacement.
7. Only then remove obsolete images.

## Failure guide

### Build-time patch no longer matches

The upstream source changed. Diff the old and new commits, understand the new
behavior, and rewrite the strict patch. Do not weaken `replace_once` or skip
the patch.

### `pip check` fails

The selected pins are not a consistent environment. Fix requirements or
constraints; do not add `--no-deps` merely to complete the build.

### Catalog refuses to load

Read the exact loader error. Common causes are a short revision, invalid
SHA-256, unsafe path, duplicate destination, unknown selector, missing
agreement, or a workflow bundle without a benchmark.

### Setup reports size or hash mismatch

ROCmplete will not overwrite an installed file. Determine whether it is:

- user content at a managed destination;
- an old pinned model revision;
- an incomplete manual download;
- upstream content that changed unexpectedly despite the same revision.

Move the file aside explicitly, verify provenance again, and rerun
`content install TARGET`. A remote mutation at an immutable-looking revision
is a security/provenance event and should not be normalized by updating the
hash without investigation.

An exact-size staging file that fails SHA-256 is different: ROCmplete preserves
it beside the expected path with an `.invalid-*` suffix. Retrying can then
download or reuse a verified replacement without deleting other resumable
staging. Inspect the quarantined file if the mismatch may indicate remote or
storage corruption.

### Hugging Face returns 401 or 403

Check repository visibility, gating, account acceptance, and `HF_TOKEN`.
License acceptance in ROCmplete does not grant upstream account access, and an
HF token does not replace `--accept-license`.

### Workflow source hash mismatch

The image's installed workflow-template package does not match the catalog.
Check the package version, built image tag, source resource, and constraints.
Do not update the hash until the actual source revision and license are
understood.

If the workflow-template package is reported missing, confirm the failing
image reference in the error. Workflow extraction must use the managed ComfyUI
image, not the content-tools image.

### Rendered workflow hash mismatch

The renderer, source graph, provenance fields, or JSON serialization changed.
Inspect the rendered diff. Repin only after verifying the graph remains the
intended workflow.

### Container already exists

Use:

```bash
./rocmplete logs APPLICATION
./rocmplete stop APPLICATION
```

If normal stop cannot recover an abandoned managed container, inspect it with
Podman before using `./rocmplete cleanup containers` and confirming its plan.
The cleanup plan must include labelled transient benchmark, acceptance,
diagnostic, shell, and downloader containers as well as application
containers. It must never select an unlabelled container merely because its
name begins with a broad project-like prefix; only exact known transient names
and the generated downloader prefix are exceptions.
Cleanup never removes persistent data unless the explicit `data` scope is
selected and its plan is confirmed.

### Profile mismatch or unsupported architecture

Confirm the selected render-node set with `doctor`. A forced profile is
an assertion, not a compatibility override. Do not map an untested
architecture to the nearest known profile merely to let startup continue.
