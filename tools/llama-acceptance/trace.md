# Qwen4Exp dispatch and physical-span collection, v1

This is a diagnostic procedure, not a timing arm or an assertion that hardware
has passed. It preserves the ordinary gateway, managed models, sampling,
entrypoint, graphs, F16 KV, four slots, shared 262144-token pool and
batch/microbatch 2048. It adds no normal engine option and requires no extra
device, capability, security relaxation, profiler installation or host change.
The separate missing GPU poisoned-reset fixture remains BLOCKED; this trace
does not implement or qualify it.

The reviewed source is upstream `ded522f8304554317e7ff8f88698dbbbca5425aa`,
patched tree `20d5c279af454b8bb9ba78fb7a8f4a0e93ded53c`. The owning host file
`src/models/qwen4exp.cpp` must hash to
`18b3e67959d253d63e518274fc5bfbd851a326207988652c909e8e761fcc3919` before
either change. Refreshing these guards requires reviewing the matcher,
physical layout and dispatch names, not automatically replacing hashes.
`prepare-trace.json` records the production recipe, fragment, wrapper and
generated recipe hashes. Commit the tracked protocol with its code changes;
retain source/dirty identity, tool hash, exact commands and outputs privately.

## Prepare, without running a workload

Build the checker as in the [kit bootstrap](README.md#bootstrap-from-a-clone).
These commands only generate a fresh recipe; they do not call Podman or probe
a GPU. Prepare a normal-source diagnostic and the deliberately narrowed host
control separately:

```bash
"$CHECKER" prepare-trace --out build/trace-normal-recipe
"$CHECKER" prepare-trace --control qsa-host-span --out build/trace-control-recipe
```

Before exporting either arm, create fresh owner-only evidence envelopes.
Do not rely on the checkout or `build/` directory being private. These commands
refuse existing destinations (including symlinks); on failure stop and choose
fresh paths consistently throughout the procedure. Do not chmod existing
evidence or delete it to make a destination reusable.

<!-- trace-export-create -->
```bash
(
  set -eu
  umask 077
  mkdir -m 0700 build/trace-normal-export
  mkdir -m 0700 build/trace-control-export
)
```

For each recipe, build its builder, extract its actual instrumented source
tree into its new envelope and then build the runtime. For the normal-source arm:

```bash
podman build --pull=never --layers --target llama-acceptance-trace-builder \
  -f build/trace-normal-recipe/Containerfile \
  -t localhost/paracetamol-experiment:trace-normal-builder .
TRACE_BUILDER_ID=$(podman image inspect --format '{{.Id}}' \
  localhost/paracetamol-experiment:trace-normal-builder)
EXPORT_ID=$(podman create --pull=never --network=none --read-only \
  --cap-drop=ALL --security-opt no-new-privileges \
  --entrypoint /bin/false "$TRACE_BUILDER_ID")
podman container inspect "$EXPORT_ID"
podman cp "$EXPORT_ID":/opt/trace/source-tree.txt build/trace-normal-export/source-tree.txt
podman rm "$EXPORT_ID"
TRACE_TREE=$(cat build/trace-normal-export/source-tree.txt)
podman build --pull=never --layers --target llama-acceptance-trace \
  --build-arg "ACCEPTANCE_TRACE_TREE=$TRACE_TREE" \
  -f build/trace-normal-recipe/Containerfile \
  -t localhost/paracetamol-experiment:trace-normal .
TRACE_IMAGE_ID=$(podman image inspect --format '{{.Id}}' \
  localhost/paracetamol-experiment:trace-normal)
```

Check each command before proceeding; remove only that exact newly created
unstarted export container. Repeat with distinct `trace-control-*` destinations
and tags for the second recipe. Do not reuse the normal arm's tree. Export
`/opt/trace/evidence` from each final image with the same exact-container
procedure. Retain both image IDs, recipe manifests and source diffs. The
builder verifies that only `libllama` changed; all other installed bytes,
including HIP kernels, remain unchanged. The runtime then replaces only the
server launcher with the tracked profiler wrapper, retaining its original
binary at `/opt/trace/llama-server`. The normal entrypoint stays untouched.

The pinned Linux `rocm-sdk-core==10.1.0` wheel supplies
`_rocm_sdk_core/bin/rocprofv3`, `lib/librocprofiler-sdk.so.1` and the tool
library under `lib/rocprofiler-sdk/`. Its console entry point exposes
`/opt/venv/bin/rocprofv3`; this does not require the devel extra or an added
profiler package. This was checked against the exact version's wheel directory
and the upstream 10.1 packaging's [core artifact filter](https://github.com/ROCm/TheRock/blob/b324869fb3116353ac7eec5e6926aa3d452ac987/build_tools/build_python_packages.py)
and [console entry points](https://github.com/ROCm/TheRock/blob/b324869fb3116353ac7eec5e6926aa3d452ac987/build_tools/packaging/python/templates/rocm-sdk-core/setup.py).
The recipe requires that already-installed launcher and saves its
executable version/help. A missing tool, unsupported option, unresolved
library, build failure or changed source contract is BLOCKED, not permission
to install a different ROCm SDK or relax confinement. AMD documents the
selected [kernel-trace option](https://rocm.docs.amd.com/projects/rocprofiler-sdk/en/docs-10.1.0/how-to/using-rocprofv3.html)
and [output controls](https://rocm.docs.amd.com/projects/rocprofiler-sdk/en/docs-10.1.0/how-to/rocprofv3-io-options.html).
CSV is used for kernel-dispatch evidence, not optional hipFILE/rocSHMEM domains;
the docs warn that direct CSV may omit those domains. Installed help and actual
child trace qualification still take precedence over presumed availability.

Signal handling follows the pinned Linux launch chain: the core console
trampoline uses `os.execv`; the single-pass [rocprofv3 script](https://github.com/ROCm/rocm-systems/blob/7da43026e4f2ef7600433c38c820732f7a205d22/projects/rocprofiler-sdk/source/bin/rocprofv3.py)
defaults to `use_execv=True` and replaces itself with the application via
`os.execvpe`. Multipass/counter collection is not requested. The tracked shell
forwards INT/TERM/HUP unchanged to that exact PID, waits for its exit and saves
the status. Host-only mock tests cover SIGINT/SIGTERM delivery, application
flush and nonzero exit preservation; they do not qualify the actual profiler.
Recheck this launch contract when updating the modular ROCm package.

## Collect through the ordinary gateway

Use the [isolated image namespace procedure](README.md#fixed-diagnostic-controls)
to build a separate controller, tag the inspected `TRACE_IMAGE_ID` only in that
namespace, and start its ordinary gateway. Use a distinct namespace/binary for
each arm; never retag production images, replace the normal binary, change
product/state identity or stop somebody else's gateway. Use exactly the
documented gateway command with reviewed profile/render node, loopback and
statistics disabled. Run arms sequentially on capable `gfx1151` hardware.

Before starting, record the exact two image IDs, controller/fixture hashes,
model and receipts, selected render node, same-boot identity and expected
policy. Preserve the normal memory/kernel guards and stop conditions. First
pass the normal-image correctness gates; do not use a trace to excuse an
output failure. Warm the selected child normally and identify its exact
gateway-owned container. Then run a fresh diagnostic `prefill` arm:

```bash
"$CHECKER" check --trace --gateway-url "$GATEWAY_URL" \
  --container-id "$CONTAINER_ID" --image-id "$TRACE_IMAGE_ID" \
  --data-dir "$DATA_DIR" --model "$MODEL" --suite prefill \
  --prefill-regression-percent "$PREFILL_TOLERANCE" \
  --out build/trace-normal-prefill
```

Use regular Flash-Next plain `MODEL` first; repeat separately for its managed
`-dflash2` identity when the affected path requires it. Repeat the second arm
with `--control qsa-host-span`, its exact trace image and fresh output path.
The checker keeps ordinary gateway defaults and correlated reports; do not
add temperature-zero requests or a new sampler for tracing. The predeclared
tolerance remains required by `prefill`, but traced timings cannot decide the
performance gate. `--trace` evidence is diagnostic REVIEW, never source-update
approval. A normal-image repeated comparison remains mandatory.

For decode-only activation investigation, `--trace --suite short` is a bounded
diagnostic subset, not physical-span or complete source-update coverage.
Keep full raw worker traces rather than filtering collection by a kernel name.

Save a native process snapshot before shutdown. Select the current arm's
already-created envelope (`build/trace-control-export` for the control):

```bash
TRACE_EXPORT_DIR=build/trace-normal-export
```

Process arguments and backend logs can contain private paths or request data.
Use a private creation mask for the redirections themselves, even inside the
private envelope; refuse overwriting an existing snapshot:

<!-- trace-export-snapshot -->
```bash
(
  set -euC
  umask 077
  podman top "$CONTAINER_ID" pid hpid args > "$TRACE_EXPORT_DIR/processes.txt"
  podman logs "$CONTAINER_ID" > "$TRACE_EXPORT_DIR/backend.log" 2>&1
)
```

Record exact model-worker PID and host PID (plus process start identity) and
the wrapper's `ACCEPTANCE_TRACE_DIRECTORY=/data/acceptance-trace-v1.XXXXXXXX`
line. Do not identify the worker from a stale PID or a router-only trace.
After the requests complete and slots idle, stop only the gateway you started
through the isolated controller's normal `stop gateway`. Retain its terminal
output, including backend shutdown and profiler errors. This gives the
profiler a graceful flush; the container is normally auto-removed. Its private
`/data` output survives at the already-mounted
`$DATA_DIR/apps/llama-cpp/acceptance-trace-v1.XXXXXXXX`. No extra writable
mount is introduced. Copy only that exact observed directory into the arm's
private `build/` evidence and hash every copied file. Do not glob/select the
latest directory. Preserve failed/partial collections too. Trace directories
are mode 0700 under umask 077 and may contain private invocation paths.
After archiving and verifying hashes, removal of that exact newly created
trace directory is an explicit operator cleanup; never delete other app data.

The wrapper stores profiler version, argv, exit status and per-PID raw CSVs.
Require a complete, nonempty kernel trace belonging to the actual model
worker, a clean flush/exit and no hardware/profiler warnings. Child injection
and graph replay tracing must be qualified on the actual selected tuple.
Router-only records, a missing worker CSV, partial output, bus-lock/driver
faults or an interrupted flush leave activation BLOCKED. Do not alter host
policy or disable graphs to obtain a passing collection.

## Read the evidence, not just the process exit

Use a CSV-aware reader (Go `encoding/csv`, or a spreadsheet) for exact kernel
names, per-PID dispatch IDs, agent/queue IDs, start/end timestamps and grid/
workgroup dimensions. Do not split CSV on commas: demangled templates contain
commas. Keep the full original names and rows. Match the worker PID to the
saved native process snapshot; a PID in the filename alone is insufficient.
Count only valid dispatch rows (end greater than start), by full name and
phase. Counts are observations, not fixed across plain/DFlash, warmup or
different graph reuse. No expected count may be inferred from a zero exit.

The current source launch sites imply these demangled base names. Template
arguments and namespace decoration may surround them; retain those details
instead of matching unrelated substring names:

| Path | Required observations when exercised |
| --- | --- |
| QSA prefill (`qsa-prefill.cu`) | `qsa_pack_k`, `qsa_pack_v`, `qsa3_rows_kernel`, `qsa3_merge_kernel`, `qsa3_attn_kernel`; nonzero worker dispatches during real prefill, not only startup. |
| Short-query QSA (`qsa-decode.cu`) | `qsa_decode_unique_ids`, `qsa_decode_merge` and the eligible `qsa_decode_wmma_partial` or `qsa_decode_partial` branch. |
| Convolution (`gdn-conv.cu`) | `gdn_concat_tail` and `gdn_conv_direct_kernel<TT>` for matched multi-token graphs; a numerical/matcher PASS with no actual direct-kernel rows does not close activation. |
| Single-token fused decode | `gdn_decode_fused_cuda<128, 4>` followed by `gdn_decode_norm_cuda<128>` when its matcher is eligible; multi-token speculative verification need not select this path. |
| Tiled prefill GDN | `gated_delta_net_tiled_cuda<128, 8, 8, 16, ...>` in the eligible multi-token range. |
| Other scoped pipeline paths | When a changed path is exercised, retain `rms_rows_f32<...>`, `qwen_hc_norm_kernel<...>`, `qwen_hc_norm_inject_kernel<...>`, `ple_concat_tail`/`ple_conv_kernel<...>`, `mm_ids_helper_512_10`, `qwen_moe_reduction_v4`, or `qwen_q8_matvec<...>` rows as applicable. Absence is not automatically failure for a workload that does not meet that path's source guards. |

The host `ACCEPTANCE_QSA_SPAN` line records layer, query tokens, batch-index
count, physical
`cache_k->ne[2]`, V dimensions, selected-ID count and `selected`/`compact`
decisions at the owning graph boundary. These are not logical request token
counts, slot occupancy or configured capacity. Graph construction also occurs
during reservation, and cached graphs can execute without a fresh host line.
At this exact source, `llama_batch_allocr::ubatch_reserve` leaves
`data->batch_idxs` empty while `ubatch_add` copies the real request indices.
Exclude `batch_indices=0` reservation/unknown graphs from executed-span claims;
require a request-built graph with populated indices and matching work evidence.
Therefore the line alone proves neither request execution nor kernel launch.
Separate reservation/warmup from active request logs and require actual native
prompt-work reports and corresponding worker dispatch rows. If the records
cannot attribute a span to executed request work rather than reservation,
record physical-span coverage BLOCKED; do not infer it from a 262144 setting.

## Rejected host-span control

The control changes only the owning host ceiling from 262144 to 262140.
It keeps the same HIP implementation, precision and workload. To qualify the
regression detector, actual conditioned request work must reach an observed
physical K span in `(262140, 262144]`. The normal-source arm must show
`selected=1 compact=1` at that span and actual QSA prefill dispatch. The
matched control must show `selected=0 compact=0`, loss of those QSA prefill
dispatches for the affected work, and the existing normal-image prefill
campaign must reject the deliberately degraded control using its predeclared
oracle/tolerance. Generic dense attention can remain correct; a numerical
failure is not required for this performance control.

If conditioning never reaches that span, only reserve graphs show it, both
arms use the same fast path, or the timing control is unexpectedly accepted,
retain the evidence as unqualified/BLOCKED or REVIEW. Do not lower the ceiling
again after observing results, change prompts/policy, select favorable runs,
weaken thresholds or count a trace overhead slowdown as control rejection.
Use the separately prepared uninstrumented `prepare-control qsa-host-span`
image for repeated timing rejection, not this trace image.

The dispatch and span observations explain whether the intended path ran;
they do not prove numerical equivalence, poison clearing, full-model quality,
near-limit history correctness, cross-architecture acceptance or a speedup.
