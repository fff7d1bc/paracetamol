# Gufo developer recheck

## Developer-only recheck kit

`tools/gufo-recheck/` is an explicitly opted-in experiment, not dormant Gufo
application support. It is absent from normal commands, application discovery,
`build all`, image exports, model lists and Maki configuration. There is no
configuration switch that promotes it into a managed backend.

The kit preserves only the pinned source-build fragment, the thin external
gateway transport and three frozen literal/tool regressions. It does not
launch or stop containers, download models, change host settings, or recreate
the full coding, long-context and concurrency benchmark matrix. Ordinary unit
checks use fake HTTP responses and never invoke inference.

### Prepare a source build

On the Linux development checkout:

```bash
make gufo-recheck
CHECKER=./build/linux-amd64/bin/gufo-recheck
"$CHECKER" prepare --out build/gufo-recheck-source-CANDIDATE
./paracetamol build runtime
podman build --pull=never --layers --target gufo-recheck \
  -f build/gufo-recheck-source-CANDIDATE/Containerfile \
  -t localhost/paracetamol-experiment:gufo-CANDIDATE .
```

Replace `CANDIDATE` with a unique run label. `make gufo-recheck` builds only
the developer executable. `prepare` performs no build or network operation.
It appends the narrow embedded fragment to the **current** production
Containerfile, inheriting its native ROCm SDK and minimal runtime. It refuses
changed stage contracts or conflicting pins rather than maintaining a second
copy of production. The generated recipe and hashed `prepare.json` are
create-once, private files below the ignored `build/` directory.

The default Gufo source is the tested `2c97b72` pin, not a moving upstream
branch. For a new candidate, deliberately update the full commit in
`prepare.go` and both matching `GUFO_COMMIT` declarations in the fragment,
review its license and numerical/API changes, rebuild the helper, then
prepare a new directory. Do not override build arguments without recording
the actual resulting image and source identity. Distro package names in the
optional fragment also need review after Ubuntu changes.

The fragment builds only `gfx1151`, includes the upstream MIT license, and
checks binary loading, help and `pip check` without GPU devices. These checks
do not qualify GPU inference. Keep builds separate from inference timings.

### Launch the backend explicitly

Start the source-built image manually on a free acceptance host. Use the
normal rootless, read-only, capability-free confinement with
`no-new-privileges`, writable `/tmp` tmpfs, private networking, and only
`/dev/kfd` plus the exact selected render node. Publish only
`127.0.0.1:18556:8080`. Mount each verified target shard and the existing BF16
projector individually and read-only, not their parent directory.

The baseline Gufo arguments used by the [engine screen](../../docs/research/qwen3.8-flash-next-gufo-optimization-screen.md#2026-11-05-source-built-engine-recheck) are:

```text
serve --host 0.0.0.0 --port 8080 --sessions 1 llm
  --model /content/unsloth-Qwen3.8-Flash-Next-UD-Q4_K_XL-00001-of-00004.gguf
  --mmproj /content/unsloth-Qwen3.8-Flash-Next-mmproj-BF16.gguf
  --served-model-name unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl
  --context 262144 --think on --reasoning-effort xhigh
  --preserve-thinking on --cache-ram-bytes 8589934592
  --speculative off
```

For the separate MTP arm, mount the verified sidecar pinned in the
[engine comparison](../../docs/research/qwen3.8-flash-next-gufo-optimization-screen.md#matched-conditions) and replace
`--speculative off` with `--speculative mtp --mtp-model /content/MTP.gguf
--draft-tokens 7 --min-draft-tokens 1`. Do not substitute a DFlash2 draft.
The kit does not fetch the sidecar or acknowledge its unverified license on
the operator's behalf. Reuse verified bytes or use the normal installer in
an isolated experiment data root with the exact revision, size and hash.

Before each run, retain the actual image inspection, full source commit,
target/projector/sidecar identities, native arguments, hardware and memory
observations alongside its private results. Reuse the same target bytes in
the control. The proxy verifies host inventory receipts, **not** what an
external engine actually mounted. A supplied model name or result label is
not an attestation of its weights or math.

### Run the quality gate

In one terminal, wrap that already-running backend:

```bash
"$CHECKER" proxy --experimental \
  --upstream http://127.0.0.1:18556 \
  --listen 127.0.0.1:18557 \
  --data-dir /ABSOLUTE/PARACETAMOL_DATA
```

The wrapper accepts only the receipt-backed regular Flash-Next identity and
one request at a time. It reuses actual gateway sampling, proxying, reporting
and cancellation. Its `application=llama-cpp` metadata denotes catalog policy
provenance, not the experimental engine. UI, client configuration and other
production routes are blocked. Both listeners must be numeric loopback
addresses. The wrapper owns no backend lifecycle.

In another terminal, set `CHECKER` again and run:

```bash
CHECKER=./build/linux-amd64/bin/gufo-recheck
"$CHECKER" check --experimental \
  --gateway-url http://127.0.0.1:18557/v1 \
  --label 'Gufo CANDIDATE AR' \
  --out build/gufo-recheck-CANDIDATE-ar
```

Run MTP separately with a fresh output directory and accurate label. For a
separate production-gateway control, `check` also permits
`--model unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl-dflash2`. The wrapper
itself deliberately does not expose that alias as experimental MTP.

The frozen requests use `xhigh`, seed 42 and omitted samplers. The checker
refuses sampling, reasoning, context or F16 policy drift, verifies the
gateway marker, correlates typed request reports, and checks the six default
values and their gateway provenance. Request-file hashes are retained and
guarded by tests. Input changes require a deliberate fixture-version review,
not editing the prompt until a candidate passes.

Each run preserves full requests and raw JSON/SSE replies, typed reports,
oracle/protocol errors and a summary in a new private directory. Backend
error envelopes, missing completion evidence and truncated streams fail the
gate. Existing directories are never overwritten. There is no inference
deadline. Cancellation preserves completed evidence and marks the run failed.
Metadata polling alone is bounded. Exit zero means all three narrow cases
pass, exit two means a quality or protocol miss, and other nonzero exits
mean an execution or setup failure.

Passing this screen is permission to consider further tests, not permission
to enable Gufo. Revisit practical coding, long context, concurrency, internal
precision and client behavior before any separate integration decision.
Stop the proxy with Ctrl-C and clean up only the exact backend container the
operator started. Neither the proxy nor the checker stops that container.
