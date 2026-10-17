# Testing, release checks, and housekeeping

Validation is layered so ordinary development on a host without suitable
target GPU capacity remains useful without pretending it replaces
target-hardware acceptance.

## Validation tiers

### Tier 1: static and unit checks

Run for every change:

```bash
make check
make test
make static
PYTHONPYCACHEPREFIX=/tmp/paracetamol-pycache python3 -m compileall -q applications containers evaluations tests
bash -n applications/comfyui/entrypoint.sh \
  applications/llama-cpp/entrypoint.sh
find catalog -type f -name '*.json' -print0 | \
  xargs -0 -n1 python3 -m json.tool >/dev/null
git diff --check
```

If a shell entrypoint or JSON manifest is added, include it explicitly.

Package-local Go tests cover catalog invariants, download command confinement,
workflow rendering, benchmark preparation, CLI resolution, profile detection,
Podman command construction, persistent verification, and client-config exports.
Add tests for behavior and failure paths, not merely new lines or parser
choices.

Gateway statistics tests use temporary SQLite files and fixture HTTP responses.
They cover UTC boundaries, retention, restart and shutdown draining, streaming
and JSON usage, missing observations, queue overflow, corrupt stores, file
permissions, and private-data sentinels in the database and WAL. Run the gateway
race tests when changing the writer or shutdown lifecycle. Check both normal
and static builds after updating the SQLite driver. No inference is needed to
validate accounting.

CI explicitly uses umask `0022`. Statistics fixtures must create a dedicated
private database directory rather than assume `t.TempDir()` itself is mode
`0700`. Negative path tests must start with a valid private parent so they
exercise the intended file or schema rejection instead of failing early on
directory permissions. Streaming HTTP tests must synchronize with handler
completion before asserting final request accounting, not just receipt of
the last flushed response bytes.

The small `tests/` Python suite exercises retained container-side policy at
the source boundary: profile validation, resumable downloader confinement,
and ComfyUI Manager patch fail-closed behavior. `make test` runs it after the
Go suite.

`.github/workflows/checks.yml` runs the Go 1.26 checks, static build, the
container-owned Python and shell checks, catalog validation, and representative
CLI dry-runs without Podman.
Hosted CI does not replace local image builds, CPU startup, provider-network
checks, or target-hardware acceptance.

Native Maki compatibility has an optional installed-client check. The normal
suite uses renderer and HTTP fixtures and needs no installed client.

```bash
PARACETAMOL_TEST_MAKI=/absolute/path/to/maki make test
```

It downloads generated TOML and Lua into temporary XDG directories, checks the
model listing, and performs a simulated tool round trip with native Maki.
It checks reasoning, authenticated and keyless access, replay and the absence
of sampler overrides without a GPU or real credentials. It does not touch
normal client state or prove macOS execution.

Embedded gateway page checks cover exact anonymous routes, protected data,
read-only behavior, empty inventory and separate HTTP/client reasoning defaults.
Real-browser acceptance is optional and uses an installed Chromium and
Puppeteer module. Neither is a build or runtime dependency. For example, with
browser tooling already available locally,

```bash
PARACETAMOL_TEST_BROWSER=/absolute/path/to/chrome \
PARACETAMOL_TEST_PUPPETEER=/absolute/path/to/puppeteer-core/lib/puppeteer/puppeteer-core.js \
PARACETAMOL_TEST_SCREENSHOTS="$PWD/build/web-acceptance" \
make test GO_PACKAGES=./internal/gateway
```

This starts fixture HTTP servers, never inference. It checks mobile layout,
configuration previews/downloads, model filtering, HTML-safe rendering,
clipboard fallback, failed refresh and the instructions-only auth state.
The Statistics tab also covers lazy reads, date-range races, historical model
IDs, incomplete counts, missing versus zero observations, and disabled or
unavailable storage.
Screenshots and downloaded fixtures stay under `build/`. Manually inspect
the images after layout changes. Browser automation is not a substitute for
checking native client configuration or target GPU inference.

For a Go or Makefile change, also force the ordinary checkout binary through
its atomic publication path rather than relying on an older incremental
artifact:

```bash
make -B build
```

The project tests exercise both build recipes with a fake compiler and failed
file operations. A failure or interruption must return a nonzero status,
preserve the previous executable and remove the temporary output. Only a
successful, non-empty build may replace the executable.

CI uses Go 1.26.9. The `go 1.26.0` directive in `go.mod` is the language
compatibility floor, not a recommendation to run the original 1.26.0
toolchain. Keep host toolchains on a supported security patch release through
the host's package or configuration management. Normal Make targets use the
installed toolchain and do not download an update automatically.

The optional native-Linux race pass is appropriate after changes to process,
signal, cleanup, checkpointing, HTTP, or other concurrent boundaries:

```bash
make race
```

For llama.cpp concurrency changes, verify the running backend's slot count,
KV layout and total token pool. Use explicit worker counts, distinct request
markers and repeated turns to catch state leakage. Confirm overlapping
generation rather than merely successful requests queued through one slot.
Measure aggregate generated tokens divided by total wall time and report
per-request latency separately. Include an overflow request and long-context
retrieval without mistaking configured capacity for populated-context coverage.

Normal builds use CGO and require a C compiler so the host binary follows
glibc and NSS name resolution. The race target uses the same native boundary.
`make static` is the explicit CGO-disabled build. It remains in Tier 1 as a
separate artifact check, but it cannot resolve names supplied only by NSS
modules such as mDNS.

### Tier 2: CLI and dry-run checks

Exercise user-visible composition:

```bash
./paracetamol --help
./paracetamol guide comfyui
./paracetamol run --help
./paracetamol images export all --output /tmp/paracetamol-images.tar --dry-run
./paracetamol doctor --help
./paracetamol content install all --dry-run
./paracetamol run comfyui --profile cpu \
  --listen 127.0.0.1 --dry-run
./paracetamol run llama-cpp server --model /path/to/model.gguf \
  --profile cpu --listen 127.0.0.1 --dry-run
./paracetamol content install llama-cpp qwen3.8 --dry-run
./paracetamol run llama-cpp server --router \
  --profile cpu --listen 127.0.0.1 --dry-run
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --profile cpu --dry-run
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --compare-backends --dry-run
./paracetamol benchmark llama-cpp throughput \
  --preset qwen-qwen3-0.6b-q8-0 --context-depth 32768 \
  --cache-type-k q8_0 --cache-type-v q8_0 --flash-attn on --dry-run
./paracetamol acceptance --dry-run
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

For image-archive changes, additionally create a tiny disposable local image,
save it as a Docker archive, inspect it through `images import --dry-run`, remove and
load it, compare the image config ID, and remove it again. Do not use a full
Paracetamol export merely to exercise archive plumbing on every development
pass.

### Tier 3: image builds

Build every affected target. During ordinary iteration, force the affected
image instructions to run while retaining locally downloaded Python packages:

```bash
./paracetamol build comfyui --no-layer-cache
```

Before a release or after base/dependency changes, perform a genuinely cold
build of all targets:

```bash
./paracetamol build all --no-cache
```

The output must show one `content` tools build, one minimal `runtime` build,
one shared PyTorch `base` build, and all application builds. A cache-free
aggregate build must not reinstall a prerequisite once per application.
PyTorch application builds reference the newly tagged PyTorch base with
`--pull=never`; native applications reference the lower runtime with
`--pull=never` and must not reference the managed PyTorch base.

`--no-cache` bypasses both Podman's image-layer cache and Paracetamol's host pip
download cache. `--no-layer-cache` bypasses only the selected application's
image layers; prerequisite targets still pass through their normal layer
cache. It is suitable for repeated local build testing where downloading the
same multi-gigabyte AMD wheels adds no coverage.

Check installed dependency consistency. Read the current image references from
`internal/application/registry.go`; for every Python application image run:

```bash
podman run --rm --entrypoint /opt/venv/bin/python CURRENT_COMFY_IMAGE \
  -m pip check
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

Inspect image metadata and history when pins or licenses changed:

```bash
podman image inspect CURRENT_IMAGE
podman history CURRENT_IMAGE
```

### Tier 4: CPU-only startup

CPU mode is a startup and HTTP smoke test, not an inference test:

```bash
./paracetamol run comfyui --profile cpu \
  --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8188/ >/dev/null
./paracetamol logs comfyui
./paracetamol stop comfyui

./paracetamol run llama-cpp server --router --models-max 1 \
  --profile cpu --listen 127.0.0.1 --detach
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/v1/models
./paracetamol stop llama-cpp
```

Use different ports if they are occupied. Confirm startup banners show `cpu`
and that containers disappear after stopping. Run this check from a shell with
`umask 0077` as well: immutable profile/downloader helpers must remain readable
to the keep-id process, `/tmp/comfy` must be writable, and persistent files
must retain the private host mask.

The llama.cpp router check requires `content install llama-cpp qwen3.8`,
but does not load a model. Confirm `unsloth-qwen3.8-27b-mtp-ud-q8-k-xl` and
`unsloth-qwen3.8-27b-ud-q8-k-xl` are present and unloaded.
The pinned upstream router may also advertise its reserved empty `default`
preset; that is not managed content and should not be selected during the
smoke test.

For gateway changes, first run the unit and race tests for `internal/gateway`,
then start `run gateway --profile cpu`. Confirm omitted application selection
discovers only built applications with a compatible verified model, startup
loads no backend, `/v1/models` contains only the receipt-verified frozen
snapshot, `status gateway` reports `unloaded`, and Ctrl-C removes an exact
backend container, joins its log follower, prints a clean stop, and exits zero
if one was started. With an exact non-loopback `--listen`, confirm both
`127.0.0.1:PORT` and the selected address serve the same frozen inventory and
that shutdown closes both; an occupied required address must close any listener
opened earlier and fail startup. Confirm `-a llama-cpp` and
`--application llama-cpp` produce the same strict inventory and the default
llama.cpp loaded-model limit is one.
Also exercise `stop gateway` and `stop all` through the private local control
socket, first unloaded and then with a backend resident. Both commands must
wait for gateway cleanup, leave no gateway listener or private backend, and
remain idempotent. `stop gateway` must work without invoking Podman; `stop all`
must drain the gateway before removing normal application containers.
GPU acceptance must additionally cover lazy llama.cpp start, prefixed backend
output, a streamed request, client cancellation, FIFO draining, and a
router model switch. Confirm `status gateway` derives llama.cpp child residency
from a read-only private `/models` probe without `reload`, filters foreign
models and router arguments, and reports bounded fixed diagnostics on malformed
or unavailable responses. Send native Maki and generic OpenAI-compatible
reasoning and sampler payloads, then use `status gateway --requests N` to verify that client
and gateway-default values, selected policy/mode, wait/upstream/total timing,
lifetime/model aggregates, validated
session grouping, and available input/output/cached/reasoning tokens are
credible while prompts, tools, authorization, response content, invalid
session values, and forwarded peer headers are absent. Confirm the private
Paracetamol session header is not forwarded upstream. Exercise normal JSON,
fragmented SSE, client disconnect,
and concurrent backend/controller logs; correlation IDs must match start and
finish/reject lines and every physical line must retain one complete
`gateway |` or `llama-cpp |` prefix.

For sampling-policy changes, test every catalog-backed Qwen mode with missing,
null, partial, and explicit-zero sampler fields. Invalid or conflicting
thinking controls must return HTTP 400 before allocation. Unknown fields,
tools, messages, and large integer values must survive normalization. Models
without a policy must remain byte-preserving. Inspect llama.cpp's verbose
generation settings on real requests, not only gateway logs. Direct benchmarks
must send and record the same catalog tuple now that engine startup carries
no sampling patch or option.

For llama.cpp cancellation, cover both streaming and non-streaming requests
through the native router and through the gateway. Wait until a child slot
has actually generated tokens, disconnect that client, and observe the slot
returning to idle through the private native `/slots?model=MODEL_ID` endpoint.
Keep the request's output limit high enough that natural completion cannot
masquerade as successful cancellation. Record the disconnect-to-idle time and
then check a normal follow-up request. A successful follow-up alone is not
proof that the abandoned work stopped, especially with several slots. Do not
expose this diagnostic route on the public gateway or publish the child port
outside loopback for the test.

First-output timing must ignore role-only and usage-only SSE chunks and remain
unavailable for non-streaming responses. Check text, reasoning and tool-call
streams, plus fragmented, malformed and oversized events. Cache-reuse totals
must use paired input/cache observations, not all input counts divided by a
partially reported cache count. Verify status reads only selected GPU sensors,
preserves unavailable values, reports host RAM rather than estimated model
memory, and labels APU SoC power as including the CPU. No sensor sampling may
start a backend or run in the inference proxy path.

With optional gateway authentication, test every route with missing, wrong,
duplicate and correct Bearer headers on both loopback and publication
listeners. Rejected requests must not read bodies, sample resources, query
model residency or start a backend. Confirm credentials do not reach backend
requests, logs, status or process arguments. Metadata clients must refuse
redirects, distinguish HTTP 401 from a wrong service, and bound response reads.
Check server/client key-file precedence independently, private file modes,
symlink/FIFO/empty/malformed rejection and key rotation by restart. Run native
Maki against an authenticated endpoint using temporary XDG state and
verify actual inference headers, not only generated configuration. Anonymous
operation must remain unchanged when no key is configured.

For a backend-restricted llama.cpp preset, verify direct startup automatically
selects its declared backend only when `--backend` was omitted. An explicit
incompatible choice and a managed benchmark must fail before container start.
Confirm an incompatible router and gateway omit the preset, while the declared
backend advertises and serves it. Hardware acceptance must include coherent
multi-turn output and a complete function-tool round trip. Treat a successful
load, synthetic `llama-bench` result, or short retrieval key as capacity or
performance evidence only.

For profile-scoped model allocation, also test missing/ambiguous host topology
and the independent container-side profile check. A mixed router must retain
its ordinary parent allocation policy while parsing the exceptional preset.
Load an ordinary model, switch to the exceptional model, then back again and
inspect actual child options, output and speculation. Run the build-time
`test-native-policy.cpp` checks and a non-root CPU startup through the real
entrypoint. Do not bypass bundled helper permissions by testing only the
native binary. A populated near-limit request, live-prefix reuse, cancellation
and reload are separate gates from reserving a large context at startup.

For host-configuration changes, load the complete generated example and
partial files with every table omitted in turn. Confirm unknown keys,
duplicates, oversized files, non-regular paths, invalid scalar types, and an
explicitly selected missing file fail closed. Exercise `-c` before and after a
command, preserve arguments after `--`, confirm CLI and environment precedence,
and verify `config init` creates mode `0600` once without replacing an existing
file. `--help` must remain available without loading configuration.

For native Maki integration, cover both configuration export routes. Check the
frozen inventory, tool-eligibility filtering, endpoint derivation, explicit
base-URL override, no-store responses and authentication. Requests must not
start a backend, fetch a supplied URL or export credentials. Forwarded headers
must not change the exported authority.

Use separately installed Maki 0.6.3 or newer for the optional transport fixture
above. Qwen3.8 supports off, low, medium and xhigh. Unsupported high snaps to
medium, and Muse off clamps to low. Adaptive follows each model's exported
preference. Regular Flash-Next defaults to xhigh while Huihui and dense Qwen3.8
remain medium. HTTP calls without reasoning retain the catalog medium fallback.
Use Maki's native `always_thinking` Lua setting in print-mode tests. Its hidden
`--thinking` flag is accepted but ignored in the tested release.

On an available inference host, download the export to temporary native client
state and complete a real file-read and shell-tool round trip over the LAN
hostname. Check off and xhigh on regular Flash-Next and one other model family.
Require reasoning history to survive the tool-result request. Inspect actual
gateway request metadata and model output.
Run a native macOS smoke separately when macOS is available. Linux fixtures
do not establish DNS, permissions, keychain or runtime behavior on a Mac.

The former Pi coding-task runner is archived. Do not alter its frozen inputs
or relabel its old results as native Maki measurements. See the
[historical evaluation record](coding-agent-evaluation.md).

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
that `--model PRESET` reports its context, reasoning and sampling ownership,
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
./paracetamol doctor --render-node /dev/dri/renderD128
./paracetamol run comfyui --profile EXPECTED_PROFILE \
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
./paracetamol acceptance
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

For the quantized on-device state-copy correction, run upstream's
`test-llama-context` with `--test '^(baseline|cp_d|cp_d_s|ps)$'` on a small managed
model. Repeat with both cache types set to `f16`, `q8_0` and `q4_0`. Require
four passed cases and zero skipped cases for each run. The `ps` case checks
that partial checkpoint size does not grow with tokens for a non-SWA cache.
Also check real MTP checkpoint reuse because a plain tiny-model cache does
not exercise the target/draft combination. The `baseline` case
is a prerequisite, not an optional control. Filtering it out can produce a
successful exit with the device-copy test skipped. This optional-cache test
does not change the managed presets' F16 policy.

Compile `applications/llama-cpp/test-topk-moe-lifetime.cpp` in the matching
builder when changing the HIP top-k MoE boundary patch. Run it on the selected
GPU with the same confinement as the state fixture below. It needs no model
and checks allocation-lifetime callbacks for eight terminal or partial-suffix
graphs. Zero callbacks is a failure, not a passing skipped fusion. Follow it
with upstream's `test-backend-ops -b ROCm0 -o TOPK_MOE` numerical cases.

#### Fast Flash-Next state screen

Before a large llama.cpp refresh matrix, run
`applications/llama-cpp/test-qwen4exp-state.cpp` with `--state-smoke`.
It prefills 4096 deterministic tokens, decodes two more, saves sequence state
and compares the next token's full logits after ordinary restore and three
clear-and-restore cycles. It uses no sampler, speculative decoder, harness,
HTTP service or causal-mode transition. The default without `--state-smoke`
retains the causal-transition regression test.

Compile the fixture in the matching llama.cpp builder, against that build's
headers and libraries. Do not reuse a fixture executable across different
patch stacks. `common_params` has no stable ABI. Keep the resulting executable
below the checkout's ignored `build/` tree and bind it read-only into the
corresponding runtime image. Use the normal rootless, read-only, network-none
GPU confinement and only the selected render node.

Inside that runtime, the direct-reader invocation is

```sh
env -u GGML_CUDA_ENABLE_UNIFIED_MEMORY /opt/probe/test-qwen4exp-state \
  --state-smoke --model /models/FIRST_FLASH_NEXT_SHARD.gguf \
  --device ROCm0 --gpu-layers 999 --flash-attn on \
  --ctx-size 8192 --batch-size 2048 --ubatch-size 2048 \
  --cache-type-k f16 --cache-type-v f16 --load-mode none \
  --lazy-mode on-direct --override-tensor per_layer_token_embd.weight=CPU \
  --no-host --fit off
```

Set `PARACETAMOL_QWEN4EXP_QSA_KERNEL=1` and
`PARACETAMOL_QWEN4EXP_QSA_GRAPH=1` when testing the accepted compact-graph
stack. Leave them unset for upstream attention. The fixture prints those
switches and the graph-disable overrides rather than requiring downstream
features, so it can also test unpatched upstream.

For an unpatched control, replace `--lazy-mode on-direct` with
`--lazy-mode off` and check available memory first. This makes the embedding
table resident and is not the managed long-context policy. On the October 6
Strix Halo screen it reached about 109 GiB of used host memory at 8K context.
Do not substitute mmap for this control without a separate residency check.

Any nonfinite logit, top-1 mismatch or normalized squared error above `1e-6`
fails the screen. Load time and test time are printed separately. A passing
screen is permission to continue acceptance, not evidence of long-context
quality, speculative correctness or a production-ready source update.

For a separate cross-build logit comparison, repeat the unchanged baseline
in independent processes first. A fixed sampler seed does not establish
bit-identical inference across processes. Keep the same teacher-forced
tokens and compare full-vocabulary vectors, including a real-source input
rather than only repeated synthetic tokens. If the baseline itself varies,
record those controls before attributing a mismatch to a candidate. Do not
relax the within-process save/restore gate above. Isolated operator tests
against the unfused GPU path and CPU reference remain necessary, and a
passing retrieval answer alone is not a numerical-equivalence test.

## Release checklist

Paracetamol does not currently publish prebuilt images, so “release” means a
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
  Containerfile applications catalog internal docs README.md
rg -n 'localhost/paracetamol:|_COMMIT|_VERSION' .
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
  podman ps -a --filter name=paracetamol
  ```

### Hardware acceptance

- [ ] The finite [target-hardware acceptance matrix](hardware-acceptance.md)
      records current results or explicit `N/P` reasons.
- [ ] `acceptance` passes, or its checkpointed result records reviewed
      failures and explicit profile-level `N/P` cases.
- [ ] RX 9060 family diagnostics and forced/auto profile tests pass.
- [ ] R9700 or RX 9070 family diagnostics and forced/auto profile tests pass.
- [ ] Strix Halo diagnostics and forced/auto profile tests pass.
- [ ] Strix Point diagnostics and forced/auto profile tests pass.
- [ ] Representative inference passes for affected applications/families.
- [ ] Memory and experimental kernel policies are tested if changed.
- [ ] Relevant managed benchmark completes and records expected metadata.
- [ ] A relevant `benchmark comfyui suite --dry-run` resolves the intended ordered
      bundle set without starting a container.
- [ ] Suite resume skips intact completed entries and rejects changed
      catalog/runtime inputs, rebuilt image IDs, and mismatched result files.

## Routine housekeeping

### Before working

```bash
git status --short
make test
podman ps -a --filter name=paracetamol
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
  du -sh "${XDG_DATA_HOME:-$HOME/.local/share}/paracetamol" 2>/dev/null
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

Paracetamol will not overwrite an installed file. Determine whether it is:

- user content at a managed destination;
- an old pinned model revision;
- an incomplete manual download;
- upstream content that changed unexpectedly despite the same revision.

Move the file aside explicitly, verify provenance again, and rerun
`content install TARGET`. A remote mutation at an immutable-looking revision
is a security/provenance event and should not be normalized by updating the
hash without investigation.

An exact-size staging file that fails SHA-256 is different: Paracetamol preserves
it beside the expected path with an `.invalid-*` suffix. Retrying can then
download or reuse a verified replacement without deleting other resumable
staging. Inspect the quarantined file if the mismatch may indicate remote or
storage corruption.

### Hugging Face returns 401 or 403

Check repository visibility, gating, account acceptance, and `HF_TOKEN`.
License acceptance in Paracetamol does not grant upstream account access, and an
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
./paracetamol logs APPLICATION
./paracetamol stop APPLICATION
```

If normal stop cannot recover an abandoned managed container, inspect it with
Podman before using `./paracetamol cleanup containers` and confirming its plan.
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
