# Native inference gateway

Paracetamol's gateway is the normal shared API entry point for managed text
inference. It presents verified llama.cpp models on one port,
loads no model at startup, and owns one mutually exclusive GPU resource pool.
It is native Go code in the host control-plane binary. It does not embed
llama-swap, run a gateway container, or receive the Podman socket.

Native clients do not need a Paracetamol installation. The gateway serves
Maki example configuration at
`GET /paracetamol/v1/clients/maki/providers.toml` and
`GET /paracetamol/v1/clients/maki/init.lua`.
See [remote Maki setup](guides/remote-maki.md) for installation and refreshes.
Both routes use the normal gateway authentication policy, disable caching,
and consult only the frozen inventory without loading a backend. No eligible
coding model returns HTTP 409. Invalid export parameters return HTTP 400.
The optional `base_url` query parameter accepts one credential-free HTTP(S)
API URL ending in `/v1`. Otherwise the validated request authority supplies
the address. Forwarded headers are ignored.

## Browser landing page

Open the gateway's root address, such as `http://gateway.example:7455/`.
Replace `gateway.example` in these examples with your server's real address.
The embedded dark page has four tabs.

- **Overview** shows the OpenAI-compatible base URL and links to setup and models.
- **Configuration** contains native Maki setup,
  configuration previews and downloads, and instructions for other clients.
  Fresh setup writes directly to `~/.config/maki` (or `$XDG_CONFIG_HOME/maki`),
  without temporary directories. Both downloads must succeed before either
  file is written. Existing configuration files are never replaced.
- **Models** groups the frozen inventory by family, with search, exact IDs,
  publisher labels, configured context, residency and expandable reasoning
  details. It distinguishes Maki's preferred reasoning from the HTTP fallback.
- **Statistics** shows local usage totals for today, seven days or thirty days,
  a daily token chart and a per-model breakdown. It reads only when opened,
  refreshed or given a different date range. It never starts a backend.

Refresh is manual. A failed refresh marks retained information as stale.
Pages reserve scrollbar space to avoid horizontal shifts. Expandable sections
use Show/Hide labels and support keyboard activation.
The last-check timestamp uses ISO 8601 in UTC (`Z`), independent of the browser's locale.
There is no model loading, switching, shutdown, request-history view or
background polling. Publisher and family names are presentation labels,
not licensing assertions. Unknown names keep their exact model ID.

`GET /paracetamol/v1/ui` is the page's protected read-only projection of the
frozen inventory and residency snapshot. It accepts no query parameters and
contains no host paths, request records, credentials or resource telemetry.
Its shape is internal to the embedded page, not a new client API contract.
Use `/v1/models` and the documented status endpoint for other clients.

Only `GET` and `HEAD` on `/`, `/assets/gateway.css` and `/assets/gateway.js`
are anonymous. They contain generic instructions and no server inventory.
When authentication is enabled, the browser shows authenticated curl commands
instead of a login form. It never accepts, stores or forwards a Bearer key.
Page data and configuration exports remain protected.

The browser uses its own origin for copyable commands. Configuration requests
pass that origin plus `/v1` as `base_url`, including through HTTPS proxies.
Direct curl exports retain their existing request-authority behavior.
The page has no external scripts, fonts or assets. HTML, CSS and JavaScript
are embedded in the Go binary, with no frontend runtime or build step.

## Start the gateway

Start every currently runnable text application in the foreground without
extra selection:

```bash
./paracetamol run gateway
```

Automatic discovery considers llama.cpp and includes the
application only when its image is built and it contributes at least one
compatible receipt-verified model. Any explicit `-a` or `--application`
options replace discovery and are strict: a missing image or model fails
startup. To select llama.cpp explicitly:

```bash
./paracetamol run gateway -a llama-cpp
```

## Configuration

Every gateway setting is optional. The normal XDG configuration path is
`${XDG_CONFIG_HOME:-$HOME/.config}/paracetamol/config.toml`; create a complete
runnable file without replacing existing configuration with:

```bash
./paracetamol config init
```

The tracked `config.example.toml` shows the same schema without choosing a
machine-specific storage path.

Empty `applications` and `render_nodes` arrays retain automatic discovery:

```toml
[gateway]
applications = []
profile = "auto"
render_nodes = []
# Loopback remains available when this selects another exact address.
listen = "127.0.0.1"
port = 7455
startup_timeout = "30m"

[gateway.client]
# Used by the controller's `status gateway` command, not native clients.
url = "http://127.0.0.1:7455/v1"

[gateway.llama-cpp]
backend = "rocm"
models_max = 1
slots = 4
```

Select another file globally either before or after the command:

```bash
./paracetamol -c configs/gateway.toml run gateway
./paracetamol run gateway --config configs/gateway.toml
```

Explicit command flags win over corresponding environment variables where
defined; both win over the selected configuration, which wins over built-in
defaults. A command-line `-a` list replaces `gateway.applications`; it does not
append to it. Use `--no-config` to bypass the optional XDG file. An explicitly
selected missing file, an unknown key, or a malformed value is an error.
Configuration exposes the reviewed typed gateway surface rather than arbitrary
upstream llama.cpp arguments; security-relaxing `--unconfined` remains
command-line-only.

Gateway clients resolve their endpoint in this order: `--gateway-url`,
`PARACETAMOL_GATEWAY_URL`, `[gateway.client].url`, then the built-in loopback
default. The client URL is independent of `[gateway].listen` and
`[gateway].port`. This lets one checkout hold a client profile for another
host without changing how a local gateway would publish itself. URLs must use
HTTP or HTTPS, contain no credentials, query, or fragment, and end in `/v1`.

The configuration reader intentionally implements only the forms used by this
schema: one-line quoted strings, decimal integers, one-line string arrays,
comments, and the documented table headers. `config init` and
`config.example.toml` emit that supported subset. Multiline strings, multiline
arrays, dotted assignments, and other general TOML features are rejected.

One command-line exception keeps an ad hoc port change local. An explicit
`--port` without an explicit `--listen` selects `127.0.0.1` for that invocation,
even when configuration or the environment normally publishes another
address. Supplying both options applies that address and port. A port selected
only by configuration or the environment does not suppress the resolved
listen address.

The compact startup card shows the local endpoint, any additional publication,
selected profile and render nodes, applications, loaded configuration path,
verified model count, short inventory fingerprint, and the copyable local
status command. The gateway always remains available to managed local clients
at `http://127.0.0.1:PORT/v1`. A concrete non-loopback `--listen` adds that exact
address on the same port; `0.0.0.0` uses one wildcard socket, which already
includes IPv4 loopback. IPv6 uses a separate `tcp6` listener alongside IPv4
loopback rather than relying on host dual-stack policy. Opening every required
listener is all-or-nothing.

There is no daemon or detach mode. The foreground process owns listener and
backend cleanup. Ctrl-C, `stop gateway`, and `stop all` stop accepting work on
every address, drain active HTTP requests, remove its exact private backend
container, print a successful stop, and exit cleanly. The stop commands use a
private same-user Unix socket, never the published HTTP API. Its location is
`$XDG_RUNTIME_DIR/paracetamol/gateway.sock`, or
`$HOME/.local/share/paracetamol/run/gateway.sock` when the runtime directory is
unset. One gateway may run per user. A gateway started by an older build has no
control socket and still needs Ctrl-C or SIGTERM. A non-loopback `--listen` is
visibly warned about. Without a configured key it is an unauthenticated
trusted-LAN publication. With a key, the warning still identifies unencrypted
HTTP.

## Optional authentication

Authentication is off unless a server key file is explicitly configured. When
enabled, all API routes require `Authorization: Bearer KEY`, including health,
model inventory, page data, configuration exports and status. Only the exact
generic landing-page and asset routes described above are anonymous.
Loopback and LAN listeners enforce the same rule.
Missing, incorrect or duplicate authorization headers receive HTTP 401 before
body parsing, resource inspection or backend scheduling. There are no user
accounts, roles, remote key-management endpoints or anonymous health bypass.

Create a private random key outside the checkout. For example, on the server,
choose an unused file and run these commands. Shell noclobber prevents replacing
an existing key.

```bash
mkdir -p "$HOME/.config/paracetamol"
(umask 077; set -C; openssl rand -hex 32 > "$HOME/.config/paracetamol/gateway.key")
```

The file must be regular, not a symlink, and inaccessible to group/other users
(`chmod 600`). It contains one 32 to 512 character token using letters,
digits or `-._~+/=`, with an optional final newline. An empty, missing, public
or malformed configured file is an error, never a fallback to anonymous access.
Generate random bytes, not a memorable password.

Configure the GPU host with its absolute path, or use
`run gateway --api-key-file /absolute/path/to/gateway.key`.

```toml
[gateway]
api_key_file = "/home/USER/.config/paracetamol/gateway.key"
```

Securely copy the same key to each authorized client host, then configure its
client section for `status gateway`. It can point to the same local file as
the server section. Native Maki has its own credential configuration described
in the [client guide](guides/remote-maki.md#authentication).

```toml
[gateway.client]
url = "http://gateway.example:7455/v1"
api_key_file = "/home/USER/.config/paracetamol/gateway.key"
```

`status gateway` also accepts `--gateway-api-key-file PATH`. Server precedence
is the command flag,
`PARACETAMOL_GATEWAY_API_KEY_FILE`, then `[gateway].api_key_file`. Client
precedence is its command flag, `PARACETAMOL_GATEWAY_CLIENT_API_KEY_FILE`, then
`[gateway.client].api_key_file`. All paths must be absolute. Client credentials
never implicitly inherit the server key because the selected client URL can
point to another host. There is no command-line flag containing the key itself.

Generic HTTP clients use their ordinary API-key/Bearer setting. For curl, this
example passes the header over stdin so the key does not appear in process
arguments.

```bash
printf 'Authorization: Bearer %s\n' "$(< "$HOME/.config/paracetamol/gateway.key")" |
  curl --fail --header @- http://gateway.example:7455/v1/models
```

The server reads its key once at startup and retains only a SHA-256 digest for
constant-time comparison. Restart it to rotate or disable the key. Native
clients own their credential storage. The Maki export contains only the
environment-variable name and, when authentication is disabled, a dummy
credential. It never contains the gateway key. Client tools may be able to
read their own credentials, so this is not a secret boundary against the
agent itself.

Keys are not printed in startup/status output, request logs or the request
ledger. Inventory and status probes refuse redirects and validate the gateway
marker. Authorization headers are consumed at the public gateway and stripped
before proxying to either backend. The startup card and status show only
whether authentication is enabled.

A Bearer key restricts access but does not encrypt it. Use HTTPS through a
trusted reverse proxy or an encrypted network/tunnel for untrusted links.
Keep the firewall rules. Gateway authentication does not protect direct
application servers or the loopback-only backend ports from local processes.

## Frozen inventory

Startup derives a snapshot from the selected applications, the built-in model
catalog, and current verification receipts. It does not hash large model files
in the runtime gate. An incomplete, unverified, architecture-incompatible, or
unselected model is absent from `GET /v1/models`. Every selected application
must contribute at least one schedulable model or startup fails.

A catalog backend can also be restricted to a concrete hardware profile.
Flash-Next's ROCm streaming path is restricted to Strix Halo. Automatic
selection resolves the exact render nodes through KFD topology, and the
container independently checks the detected profile. An unknown profile does
not advertise a restricted model. Vulkan remains available as the explicit
Flash-Next fallback.

The snapshot remains fixed for the process lifetime. Installing, replacing,
or verifying content does not change a running gateway: restart it. This makes
client configuration and routing deterministic and prevents a model appearing
halfway through a session. The versioned status response includes an inventory
fingerprint for the exact snapshot.

## Scheduling and lifecycle

llama.cpp models share one private router allocation. The first Chat
Completions request starts the required application in a constrained,
Paracetamol-owned container with an ephemeral loopback host port. That port is
never the public interface. Once started, the container's stdout and stderr are
followed in the foreground terminal with a `llama-cpp |`
prefix. The log follower changes with the allocation and is joined during
backend stop, so it cannot outlive the gateway-owned container.

Gateway controller and HTTP lines use the `gateway |` prefix. Each accepted
Chat Completions request receives an opaque, restart-safe correlation ID and
matching start/finish lines; an early rejection receives one reject line. Backend and
controller prefixes are emitted as complete physical lines, so concurrent
output cannot splice one prefix into another.

Clients may additionally correlate the requests belonging to one coding task
with a UUID-shaped session identifier. Generic callers send
`X-Paracetamol-Session-ID`; the gateway consumes that private header rather
than forwarding it. It also recognizes `X-Session-Affinity` and `X-Session-ID`
from compatible clients. Invalid values are ignored, and the gateway never
fingerprints messages or other request content to infer a session.

Requests run concurrently through the one llama.cpp allocation, subject to
the backend's own slots. During startup, requests wait in a bounded FIFO queue
until the backend passes its readiness check. The gateway fills
missing sampling fields only for models with a reviewed catalog policy, as
described below. Messages, tools, and unknown fields retain their values.
SSE responses stream without aggregation. After a successful upstream SSE
response begins, the gateway adds a content-free comment every 15 seconds at
an event boundary. These keepalives exceed one byte per second on average so
clients with low-speed timeouts can tolerate long prompt processing. They do
not count as generated output, usage, or completion, and do not prove backend
progress. Client cancellation still stops the request. A partial upstream event
is forwarded unchanged and never interrupted by a comment. Keepalives do not
cover gateway startup, model loading, or slot queues before upstream headers;
HTTP errors and non-streaming JSON responses retain their original behavior.

The body limit is 16 MiB and the waiting queue holds at most 64 requests.
Client cancellation removes a queued request or propagates to the upstream
request. A proxy transport failure marks the allocation failed so it is
drained and recycled rather than silently reused.

The llama.cpp router keeps one model child loaded by default. Its
`--models-max` limit counts loaded children; it does not predict whether their
combined weights and contexts fit memory. Increase it only for a model set
known to fit. At the count limit, the pinned router queues a new model request
until it can evict an idle least-recently-used child; it does not evict a busy
child.

Request concurrency is a separate setting. Each loaded llama.cpp model has
four request slots by default. Use `--slots N` or
`[gateway.llama-cpp].slots = N` to change it. A fifth request waits for a slot
without loading another copy of the model.

With multiple slots, llama.cpp shares the preset's KV token pool. A 256K
preset is **256K total across active conversations**, not four independent
256K allocations and not a fixed 64K limit per conversation. One conversation
can use nearly the whole pool when the others are idle. Long simultaneous
conversations must fit together, and running out of KV space can cause
reprocessing or a backend error. More slots improve aggregate throughput on
some workloads but do not promise faster individual responses. Use `--slots 1`
when one long conversation should have the server to itself.

Per-sequence state and work buffers can still grow with the slot count,
especially for recurrent and speculative models. Four slots reduce this
overhead compared with eight, but are not a memory or performance acceptance
claim for every model and GPU. Use a smaller value when memory headroom
matters. Existing config files containing `slots = 8` retain that explicit
override. Change it to four or omit the key to adopt the default.

The startup banner, gateway model details, and `status llama-cpp` show the
configured request-slot count separately from the loaded-model limit. Direct
`run llama-cpp server` supports the same `--slots` flag and default.

Keep `--models-max 1` for Flash-Next on a 128 GB Strix Halo host. Its improved
memory headroom does not mean it can coexist with the dense Q8 model. Switching
between them uses the existing idle-child eviction. Flash-Next's allocation
opt-out is local to its child process, so switching back does not alter the
dense model's policy.

## Model-aware sampling defaults

The gateway is the shared sampling-policy boundary for Maki, curl, and
other HTTP clients. Each reviewed Qwen preset references a catalog policy.
There is no model-name guessing and no universal Qwen tuple applied to other
families. Models without a policy keep their original request bytes.

For a policy-bearing model, the gateway resolves thinking from the request,
or from the model's managed default when no control is sent. It then fills
only missing or JSON-null `temperature`, `top_p`, `top_k`, `min_p`,
`presence_penalty`, and `repeat_penalty` fields declared by that policy.
Explicit values win independently, including zero. Invalid explicit sampler
values are left for the engine to reject, not repaired by the gateway.

Use top-level `"reasoning_effort":"none"` for off, or a native level listed
by the model. Qwen3.8 also accepts the
`chat_template_kwargs.enable_thinking`
boolean and supported nested `reasoning_effort` values.
No client-specific workaround is involved.

The exported Maki startup snippet selects explicit `xhigh` for new sessions,
including Huihui. Choosing adaptive instead still maps regular Flash-Next to
xhigh and Huihui or dense Qwen to medium. These are client preferences, not
gateway overrides. The catalog and shared template still use medium when a
raw HTTP request omits reasoning. All four Flash-Next presets share one
sampling policy and template.

Malformed or conflicting thinking controls return HTTP 400 before a backend
starts. For example, `reasoning_effort: medium` combined with
`enable_thinking: false` is ambiguous and is rejected. Put `none` at the top
level, not inside `chat_template_kwargs.reasoning_effort`. Duplicate fields
in the request or its reasoning-control objects are also rejected. The
gateway does not rewrite reasoning levels or templates.

The existing reviewed tuples are preserved. Flash-Next on llama.cpp uses
thinking min-p 0.05. Moving policy ownership does not change a model's
default reasoning level.

`status gateway --requests N` shows the selected policy and thinking mode,
plus whether each sampler came from the client or the gateway. The startup
inventory fingerprint includes policy values. Restart the gateway after a
catalog policy change. No engine rebuild is needed for a sampler-only change.

Direct engine endpoints bypass this policy. They use native engine defaults
unless the caller supplies settings.
The old llama.cpp sampling patch has been removed, with no compatibility
flag. Use the gateway for shared managed defaults. Direct benchmark runners
send and record the catalog tuple explicitly for their fixed reasoning mode.

## Ownership and security

Backends retain the normal rootless, read-only, capability-free runtime
policy, exact GPU device set, application data mount, and read-only content
mount. They publish only an ephemeral `127.0.0.1` port. The gateway never
guesses render nodes.

The backend publication is explicitly
`127.0.0.1::CONTAINER_PORT`, where the empty host-port field asks Podman for
an available port. The gateway discovers that assignment and connects through
host loopback. It never connects to a container IP. A LAN peer cannot reach
the ephemeral mapping by scanning the host because it is not bound to a LAN
address, even when the public gateway is. The port number remains discoverable
to local processes through the gateway log, Podman, or the host socket table.
The current trust boundary therefore includes the gateway host and its local
processes. Gateway authentication restricts access to the public endpoint. It
does not create an authorization boundary against untrusted local code. That
threat model would require independently protecting the backend transport.

Before opening the public listener, startup verifies rootless Podman, every
selected image, device policy, the receipt-backed content snapshot, and
container collisions. A stale container may be reclaimed only when its exact
name and managed/application/`gateway-backend` labels all match. Direct,
benchmark, foreign, or ambiguously labelled containers are preserved and make
the operation fail. Direct application launch applies the reciprocal check.

## API and status

The initial public surface is deliberately small:

- `GET /health`
- `GET /v1/models`
- `POST /v1/chat/completions`
- `GET /paracetamol/v1/status`
- `GET /paracetamol/v1/requests/REQUEST_ID`
- `GET /paracetamol/v1/clients/maki/providers.toml`
- `GET /paracetamol/v1/clients/maki/init.lua`

Every response identifies the public service with
`Server: paracetamol/0.1.0-dev`, derived from the built command identity and
version, and the stable protocol marker
`X-Paracetamol-Gateway: paracetamol.gateway.v1`. A proxied backend cannot
replace either value with its own identity. The marker detects an accidental
connection to a different service. It is public and forgeable, so it is not a
credential or proof that the endpoint is authentic.

Use the human status client for the versioned endpoint:

```bash
./paracetamol status gateway
./paracetamol status gateway --gateway-url http://gateway.example:7455/v1
./paracetamol status gateway --requests 10
```

The v7 status schema reports the frozen applications, active allocation,
lifecycle state, active and queued request counts, inventory fingerprint, and
exact per-model residency. It also includes the current random `instance_id`.
Every model reports its catalog-configured context.
For a resident llama.cpp allocation, the gateway performs a bounded read-only
`GET /models` against the private router. Loaded models may also provide
effective context, training context, parameter count, model byte size, and
quantization type. The decoder keeps only those fields plus frozen model IDs
and unloaded, loading, loaded, sleeping, failed, or unknown state. It never
adds the router's mutating `reload` query and discards paths, child arguments,
presets, and models outside the frozen inventory.

Status also contains process-lifetime request, outcome, token, timing, and
backend-inference aggregates for the gateway and each used frozen model. Token
metrics carry an observation count so partial upstream reporting cannot look
like a complete total. Request timing totals are cumulative gateway wall
times, and the human client presents their per-request mean. llama.cpp prompt
processing and token generation rates come from the sum of reported token
counts divided by the sum of their matching durations. They are not averages
of per-request rates. For token generation, llama.cpp's first sampled token
comes from the final prompt logits and is not part of `predicted_ms`, so the
aggregate uses `predicted_n - 1` timed decode steps for each completion.
Speculative decoding totals include drafted and accepted tokens when both are
reported.

Streaming requests also report time to first generated output, measured from
gateway receipt to the first complete SSE event containing text, reasoning,
refusal text, or a function name/argument fragment. Empty role chunks,
keepalives and usage events do not count. This is observed first-output latency,
not the backend's exact first-token timestamp. It includes scheduler wait,
loading, processing and transport. Non-streaming requests and streams without
an observed generated event leave it unavailable. The mean carries its own
observation count rather than counting missing measurements as zero.

Cache reuse is the cached fraction of total input tokens. Aggregates use only
requests with both valid counts, sum their counts before dividing, and report
how many paired requests contributed. A missing cache count is not a cache
miss. llama.cpp's `timings.cache_n` is a fallback when standard usage metadata
is absent, and `prompt_n` counts only the newly evaluated part of that input.
Fallback counts use the final observed metadata when usage and timings arrive
in separate streaming events. Standard usage counts take precedence.

These measurements do not invent detailed loading phases. Gateway wait combines
scheduling, allocation switching and backend readiness. Upstream time can also
contain llama.cpp router child loading, prompt processing and generation.
Backend-reported prompt and generation durations remain separately labelled.

Each status request also takes a read-only host resource snapshot. It reports
system `MemAvailable` and total RAM, plus activity, temperature and supported
SoC power sensors for only the gateway-selected render nodes. There is no
background sampler or monitoring subprocess. Missing or malformed readings are
unavailable, not zero. CPU mode samples no GPU. RAM is host-wide, not a model's
footprint. VRAM/GTT counters are deliberately not used to estimate model memory
because they do not cover every unified-memory allocation on Strix Halo.
The [kernel's amdgpu power sensors](https://docs.kernel.org/gpu/amdgpu/thermal.html#hwmon-interfaces)
include CPU power on APUs and are not wall-socket readings.

Recent-request history is opt-in. `--requests N`, where N is 1 through 64,
requests `/paracetamol/v1/status?requests=N` and prints the newest completed
records from a fixed 64-entry in-memory ring. A record contains its correlation
ID, optional validated session ID, frozen model and application, direct TCP
peer, bounded User-Agent, stream mode, outcome, HTTP status, timestamps,
gateway wait/upstream/total wall time, and input/output/cached/reasoning token
counts when the upstream reports them. Stream records may also contain observed
first-output latency. llama.cpp records may also contain
prompt and generation token counts with their durations, plus drafted and
accepted token counts. A malformed or missing timing field makes only that
measurement unavailable. Reasoning tokens are a detail within output tokens
rather than an additional token total. The record also
distinguishes explicit client values for `temperature`, `top_p`, `top_k`,
`min_p`, `presence_penalty`, `repeat_penalty`, `reasoning_effort`,
`reasoning_strength`, and the reviewed `chat_template_kwargs` reasoning fields
from `gateway-default` values and null or absent fields still delegated to
backend defaults. `provided` records whether the client included a field,
even when it was null and received a gateway default. A managed sampling
policy also records `sampling_policy` and `sampling_mode`. Reasoning validation
applies only to policy-bearing models. For other models, observed malformed
or conflicting controls remain diagnostic metadata rather than enforcement.

The ledger and aggregates are process-local and never retain prompts, messages,
tools, arbitrary request fields, complete request/response bodies, response
content, authorization headers, or forwarded-address headers. Normal JSON and
SSE are observed through bounded incremental readers without delaying or
changing their bytes or flushing. Proxied SSE responses add
`X-Accel-Buffering: no` so compatible reverse proxies do not coalesce events.
Missing or malformed usage data is simply reported as unavailable. Chat
responses carry `X-Paracetamol-Request-ID` for correlation.
Every response also carries `X-Paracetamol-Instance-ID`. Request IDs contain
that instance identity and a sequence number, so an ID from before a restart
cannot resolve to a different request. Treat the complete ID as opaque.
At most 64 session aggregates are retained, with least-recently-used eviction;
`--requests` returns the lifetime-since-gateway-start aggregates for sessions
represented in that recent request window. Omitting `--requests` excludes both
individual request history and session identifiers. Raw prefixed runtime
output remains on gateway stderr; status exposes no host path or process
detail.

Native Maki configuration comes from the [gateway export](guides/remote-maki.md).
The client needs no checkout, local catalog or controller. Download and merge
again when the frozen inventory changes. The provider selects one gateway
URL for every backend and does not inject a shared session ID.

The controller's `status gateway` command still accepts `--gateway-url`,
`PARACETAMOL_GATEWAY_URL` and `[gateway.client].url`. Its bounded metadata
probe checks the versioned gateway marker before trusting a response. These
controller settings do not configure native Maki.

## Request reports for testing

Use `GET /paracetamol/v1/requests/REQUEST_ID` to retrieve one request without
scanning recent history, probing model residency or sampling host resources.
It uses the ordinary authentication policy and sends `Cache-Control: no-store`.
There is no special testing mode and no alternate sampling or scheduling path.

For example, send a normal request with sampler fields omitted:

```bash
curl --fail-with-body -i http://gateway.example:7455/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"unsloth-qwen3.8-flash-next-125b-a6b-ud-q4-k-xl","reasoning_effort":"none","max_tokens":64,"messages":[{"role":"user","content":"Reply exactly true."}]}'
```

Copy its `X-Paracetamol-Request-ID` response header, then replace `REQUEST_ID`:

```bash
curl --fail-with-body http://gateway.example:7455/paracetamol/v1/requests/REQUEST_ID
```

Use the same Bearer credential as inference when authentication is enabled.
The versioned `paracetamol.request-report.v1` envelope contains the gateway
instance, frozen inventory fingerprint, retention limits and one of:

- `state: active`, with an `active` metadata snapshot and `phase` of
  `receiving`, `waiting` or `upstream`.
- `state: completed`, with the ordinary completed `record`, including resolved
  samplers and their provenance, tokens, native timings and completion evidence.

These phases describe the gateway only. `waiting` covers allocation and
readiness. `upstream` can include native model loading, slot queues, prefill
and generation. It is not proof that GPU work has started. Active reports do
not invent final durations, token counts or outcomes. The stream setting is
omitted while the request body is still being received. Probes can poll until a
report is completed using their own cancellation policy.

The gateway retains the most recent 64 completed requests and at most 128
active metadata snapshots. Active tracking is best-effort and never limits
inference admission. Read reports promptly and save any required benchmark
evidence on the client. HTTP 404 with `request_not_retained` covers unknown,
evicted, other-instance and currently untracked IDs. It is not evidence of
success, cancellation or an idle backend. Malformed selectors and query
parameters return HTTP 400. There are no tombstones or durable request records.

`record.completion` separates observed model completion from HTTP transport:

- `finish_reasons` retains at most 16 choice indexes with `stop`, `length`,
  `tool_calls`, `function_call`, `content_filter` or the fixed marker `other`.
- `stream_done` is present only for streaming responses and records whether
  the SSE `[DONE]` marker was observed.
- `observation` is `available`, `partial` or `unavailable`. Missing, malformed,
  oversized, conflicting or incomplete observations are not promoted to a
  successful model finish.

HTTP success alone is not a completed-task quality result. A `length` finish
reports an output limit, not proof of context loss or its exact cause. A
canceled gateway request does not prove that its native slot has become idle.
Prompts, answers, tools and arbitrary backend strings are never retained.
Reports use the existing completion ring, not a second telemetry store.
The Go control plane exposes `gateway.FetchRequestReport` for reusable probes.
It checks the gateway marker, schema and exact request/instance correlation,
does not follow redirects, and makes no inference or backend-diagnostic calls.

For truthful comparisons, retain the request controls, inventory fingerprint,
native timings, cache observations and finish evidence together. Benchmark
fixtures must check their own answers. The inventory fingerprint identifies
frozen models and policies, not the executing image or hardware. Record the
image ID, profile, runtime arguments and fixture identity separately when
comparing engine builds. Direct engine and numerical tests
remain separate from production-gateway tests. No slot/metrics polling,
runtime log parser or request-level backend-phase attribution is enabled.

## Durable usage statistics

The gateway keeps daily per-model aggregates in a local SQLite database by
default. This is separate from the process-lifetime status ledger described
above. It survives ordinary gateway restarts without retaining individual
requests or conversations.

```toml
[gateway.statistics]
enabled = true
retention_days = 90
```

`run gateway --statistics=false` disables database reads and writes for that
run without deleting existing history. `--statistics-retention-days N`
overrides configuration, with a supported range of 30 through 365 days.
Expired daily aggregates are deleted automatically while statistics are
enabled. The database belongs to the configured data root at
`apps/gateway/statistics/usage.sqlite`. Its directory is private and its files
are owner-only. Keep it on a local filesystem, not NFS or another network
mount. SQLite manages its `-wal` and `-shm` sidecars. Stop the gateway before
copying its statistics directory for a simple consistent backup.

The Statistics tab uses UTC calendar days, including today. Seven days means
today and the six preceding UTC dates, not the last 168 hours. Requests count
on their completion date. A recording-start date makes a newly created
history distinguishable from a full period with no traffic. Historical model
IDs remain visible even when those models leave the current inventory.

The protected read-only endpoint is
`GET /paracetamol/v1/statistics?days=7`. It accepts only `1`, `7` or `30`, with
seven days as the default, and returns the `paracetamol.statistics.v1` schema.
It uses the same optional Bearer authentication as inference. Reads do not
query model residency, sample the host or enter the request ledger. Without
authentication, anyone who can reach the gateway can read these totals.

Stored values are limited to UTC dates, exact known model/application IDs,
request outcomes, token counters, timing totals and recording-health metadata.
Neither the database nor its background queue receives prompts, answers, tool
arguments, request/session IDs, addresses, User-Agent strings, sampler values
or credentials. This still reveals which models were used and on which days.
It is local accounting, not anonymous data or an encrypted vault. There is no
external analytics service or telemetry.

Counts come from observed backend usage, not estimates from text length.
Input includes conversation history sent again on later requests, including
cached input. Reasoning tokens are part of output, not additional output.
Missing counts, especially on canceled streams, are unavailable rather than
zero. Each metric retains its reporting count. Cache reuse divides matched
cached-token totals by matched input-token totals, not the number of requests
that happened to reuse some cache. Partial observations are labelled in the UI.

A bounded background writer records completed-request totals, never individual
stream chunks. The page reports pending writes. Shutdown drains accepted
updates after HTTP requests finish. If storage fails or the queue fills,
inference continues and statistics are marked incomplete. An unclean stop may
lose queued updates without a known loss count, so this is not a billing ledger.
Recording-health warnings cover the retained history rather than only the
selected period, and expire with retention. A database failure makes the API
unavailable until the gateway restarts after the storage problem is addressed.
The gateway does not silently replace a corrupt or unsupported database.
Session analytics and durable request-level history are deliberately absent.

## Deliberate current limits

- llama.cpp is the only managed text-inference engine. The gateway owns one
  resource pool and one private router allocation.
- It adds no manual unload operation or gateway idle timer. Foreground shutdown
  stops the private backend, while llama.cpp's count-limited router retains its
  own idle-child LRU policy.
- It does not add TLS, retries, request rewriting, or an
  arbitrary upstream registry.
- The in-memory request ledger is not a persistent request archive, full
  traffic capture or Prometheus endpoint.
- Durable statistics contain aggregates only, not a queryable request history
  or remote backend logs. Runtime logs remain attached to the foreground process.
- Direct servers remain supported diagnostic surfaces, not hidden gateway
  dependencies.

Changes to model capability fields belong in `internal/catalog/` and
`internal/textmodel/`. Request scheduling, HTTP behavior, and lifecycle belong
in `internal/gateway/`; Podman command confinement remains in
`internal/runtime/` and `internal/podman/`. Test the scheduler and proxy with
fakes first, then accept real lazy start, switching, streaming, cancellation,
and cleanup on representative GPU hardware.
