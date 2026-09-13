# Native inference gateway

Paracetamol's gateway is the normal shared API entry point for managed text
inference. It presents verified llama.cpp and DwarfStar models on one port,
loads no model at startup, and owns one mutually exclusive GPU resource pool.
It is native Go code in the host control-plane binary. It does not embed
llama-swap, run a gateway container, or receive the Podman socket.

Start every currently runnable text application in the foreground without
extra selection:

```bash
./paracetamol run gateway
```

Automatic discovery considers llama.cpp and DwarfStar, then includes an
application only when its image is built and it contributes at least one
compatible receipt-verified model. Any explicit `-a` or `--application`
options replace discovery and are strict: a missing image or model fails
startup. Use one for an exact single-application endpoint or repeat it to
demand both applications:

```bash
./paracetamol run gateway -a llama-cpp
./paracetamol run gateway -a dwarfstar
./paracetamol run gateway -a llama-cpp -a dwarfstar
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
# Used by managed Pi, Maki, and `status gateway`.
url = "http://127.0.0.1:7455/v1"

[gateway.llama-cpp]
backend = "rocm"
models_max = 1
```

Select another file globally either before or after the command:

```bash
./paracetamol -c configs/aion.toml run gateway
./paracetamol run gateway --config configs/aion.toml
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
backend cleanup, and Ctrl-C stops accepting work on every address, drains
active HTTP requests, removes its exact private backend container, prints a
successful stop, and exits cleanly. A non-loopback `--listen` is visibly warned
about. Without a configured key it is an unauthenticated trusted-LAN
publication. With a key, the warning still identifies unencrypted HTTP.

## Optional authentication

Authentication is off unless a server key file is explicitly configured. When
enabled, every route requires `Authorization: Bearer KEY`, including health,
model inventory and status. Loopback and LAN listeners enforce the same rule.
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
client section. Local Pi and Maki clients also need this section. It can point
to the same local file as the server section.

```toml
[gateway.client]
url = "http://gpu-host.local:7455/v1"
api_key_file = "/home/USER/.config/paracetamol/gateway.key"
```

`agent run pi`, `agent run maki` and `status gateway` also accept
`--gateway-api-key-file PATH`. Server precedence is the command flag,
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
  curl --fail --header @- http://gpu-host.local:7455/v1/models
```

The server reads its key once at startup and retains only a SHA-256 digest for
constant-time comparison. Restart it to rotate or disable the key. Managed
clients read their key on session launch and place it only in their existing
private generated provider files, with mode `0600` for Pi or `0700` for Maki's
provider script under a `0700` state directory. These files are credentials
and should not be shared. A normal relaunch refreshes or removes that copy.
Informational and package-management commands do not read the key or contact
the gateway. Agent tools run as the same user and can read their own client's
credential, so this is not a secret boundary against the agent itself.

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

llama.cpp models share one private router allocation. DwarfStar v1 exposes its
single reviewed non-DSpark preset as a separate allocation. The first Chat
Completions request starts the required application in a constrained,
Paracetamol-owned container with an ephemeral loopback host port. That port is
never the public interface. Once started, the container's stdout and stderr are
followed in the foreground terminal with a `llama-cpp |` or `dwarfstar |`
prefix. The log follower changes with the allocation and is joined during
backend stop, so it cannot outlive the gateway-owned container.

Gateway controller and HTTP lines use the `gateway |` prefix. Each accepted
Chat Completions request receives a process-local correlation ID and matching
start/finish lines; an early rejection receives one reject line. Backend and
controller prefixes are emitted as complete physical lines, so concurrent
output cannot splice one prefix into another.

Clients may additionally correlate the requests belonging to one coding task
with a UUID-shaped session identifier. Generic callers send
`X-Paracetamol-Session-ID`; the gateway consumes that private header rather
than forwarding it. It also recognizes `X-Session-Affinity` and `X-Session-ID`
from compatible clients. Invalid values are ignored, and the gateway never
fingerprints messages or other request content to infer a session.

Requests compatible with the active allocation run concurrently, subject to
the backend's own slots; DwarfStar is limited to one active request. A request
for another allocation enters the bounded FIFO queue. Once existing requests
finish, the scheduler removes the current backend, starts the requested one,
waits for its readiness endpoint, and forwards the original request. The
gateway preserves the request body—including unknown fields—and streams SSE
responses without aggregation.

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

Keep `--models-max 1` for Flash-Next on a 128 GB Strix Halo host. Its improved
memory headroom does not mean it can coexist with the dense Q8 model. Switching
between them uses the existing idle-child eviction. Flash-Next's allocation
opt-out is local to its child process, so switching back does not alter the
dense model's policy.

## Ownership and security

Backends retain the normal rootless, read-only, capability-free runtime
policy, exact GPU device set, application data mount, and read-only content
mount. They publish only an ephemeral `127.0.0.1` port. The gateway never
guesses render nodes and DwarfStar still requires exactly one GPU.

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
./paracetamol status gateway --gateway-url http://aion.local:7455/v1
./paracetamol status gateway --requests 10
```

The v5 status schema reports the frozen applications, active allocation,
lifecycle state, active and queued request counts, inventory fingerprint, and
exact per-model residency. Every model reports its catalog-configured context.
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

DwarfStar residency follows its single allocation lifecycle. An unavailable,
malformed, missing, or contradictory result becomes `unknown` with a fixed
diagnostic rather than leaking backend detail.

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
measurement unavailable. DwarfStar currently reports no compatible inference
timings. Reasoning tokens are a detail within output tokens rather than an
additional token total. The record also
distinguishes explicit client values for `temperature`, `top_p`, `top_k`,
`min_p`, `presence_penalty`, `repeat_penalty`, `reasoning_effort`,
`reasoning_strength`, and the reviewed `chat_template_kwargs` reasoning fields
from null or absent fields delegated to managed/backend defaults. Conflicting
or malformed control values are diagnostic metadata only: the gateway neither
rejects nor rewrites an otherwise valid request.

The ledger and aggregates are process-local and never retain prompts, messages,
tools, arbitrary request fields, complete request/response bodies, response
content, authorization headers, or forwarded-address headers. Normal JSON and
SSE are observed through bounded incremental readers without delaying or
changing their bytes or flushing. Proxied SSE responses add
`X-Accel-Buffering: no` so compatible reverse proxies do not coalesce events.
Missing or malformed usage data is simply reported as unavailable. Chat
responses carry `X-Paracetamol-Request-ID` for correlation.
At most 64 session aggregates are retained, with least-recently-used eviction;
`--requests` returns the lifetime-since-gateway-start aggregates for sessions
represented in that recent request window. Omitting `--requests` excludes both
individual request history and session identifiers. Raw prefixed runtime
output remains on gateway stderr; status exposes no host path or process
detail.

Pi and Maki accept one `--gateway-url` setting, with
`PARACETAMOL_GATEWAY_URL` and `[gateway.client].url` as durable alternatives.
Normal sessions query
the live inventory, require its exact versioned Paracetamol gateway marker,
and intersect it with local catalog capabilities before generating one
`paracetamol` provider. A different service on the configured host and port is
rejected before its status or response body is treated as model inventory.
Management and informational client commands do not require a running gateway.
Keep client and server checkouts on the same revision so model IDs, protocol
markers, and capability metadata agree.

Managed Pi enables its native OpenAI-compatible session-affinity headers, so
the real Pi session UUID follows session restore, fork, and switching. Maki's
llama.cpp provider currently receives but does not transmit Maki's internal
session ID; Paracetamol therefore gives one launcher invocation a fresh UUID
and places it in the private Paracetamol header. All Maki tabs and subagents in
that invocation intentionally form one launch-level correlation group.

## Deliberate current limits

- DwarfStar uses the reviewed direct model only; DSpark remains a direct-server
  and benchmark option.
- The gateway owns one resource pool and does not keep llama.cpp and DwarfStar
  resident together.
- It adds no manual unload operation or gateway idle timer. Allocation switches
  and foreground shutdown retain their existing lifecycle, while llama.cpp's
  count-limited router retains its own idle-child LRU policy.
- It does not add TLS, retries, request rewriting, or an
  arbitrary upstream registry.
- The in-memory ledger and aggregates are not a persistent metrics store, full
  traffic capture, Prometheus endpoint, or web UI.
- It does not retain a persistent key-value history or provide a remote
  backend-log API. Runtime logs remain attached to the foreground process.
- Direct servers remain supported diagnostic surfaces, not hidden gateway
  dependencies.

Changes to model capability fields belong in `internal/catalog/` and
`internal/textmodel/`. Request scheduling, HTTP behavior, and lifecycle belong
in `internal/gateway/`; Podman command confinement remains in
`internal/runtime/` and `internal/podman/`. Test the scheduler and proxy with
fakes first, then accept real lazy start, switching, streaming, cancellation,
and cleanup on representative GPU hardware.
