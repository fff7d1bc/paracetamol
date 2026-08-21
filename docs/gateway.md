# Native inference gateway

Paracetamol's gateway is the normal shared API entry point for managed text
inference. It presents verified llama.cpp and DwarfStar models on one port,
loads no model at startup, and owns one mutually exclusive GPU resource pool.
It is native Go code in the host control-plane binary. It does not embed
llama-swap, run a gateway container, or receive the Podman socket.

Start the normal llama.cpp gateway in the foreground without extra selection:

```bash
./paracetamol run gateway
```

Any explicit `--application` options replace that llama.cpp-only default. Use
one for a DwarfStar-only endpoint or repeat it to expose both applications:

```bash
./paracetamol run gateway --application dwarfstar
./paracetamol run gateway --application llama-cpp --application dwarfstar
```

The compact startup card shows the endpoint, selected profile and render
nodes, applications, verified model count, short inventory fingerprint, and
the copyable status command. The default public address is
`http://127.0.0.1:8080/v1`. There is no daemon or detach mode. The foreground
process owns backend cleanup, and Ctrl-C stops accepting work, drains active
HTTP requests, then removes its exact private backend container. A non-loopback
`--listen` is an unauthenticated trusted-LAN interface and is visibly warned
about.

## Frozen inventory

Startup derives a snapshot from the selected applications, the built-in model
catalog, and current verification receipts. It does not hash large model files
in the runtime gate. An incomplete, unverified, architecture-incompatible, or
unselected model is absent from `GET /v1/models`. Every selected application
must contribute at least one schedulable model or startup fails.

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

## Ownership and security

Backends retain the normal rootless, read-only, capability-free runtime
policy, exact GPU device set, application data mount, and read-only content
mount. They publish only an ephemeral `127.0.0.1` port. The gateway never
guesses render nodes and DwarfStar still requires exactly one GPU.

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

Use the human status client for the versioned endpoint:

```bash
./paracetamol status gateway
./paracetamol status gateway --gateway-url http://aion.local:8080/v1
```

Status reports the frozen applications and models, active allocation,
lifecycle state, active and queued request counts, and inventory fingerprint.
Raw prefixed backend output remains on gateway stderr; the network status
response does not expose host paths or process detail.

Pi and Maki accept one `--gateway-url` setting, with
`PARACETAMOL_GATEWAY_URL` as its environment equivalent. Normal sessions query
the live inventory and intersect it with local catalog capabilities before
generating one `paracetamol` provider. Management and informational client
commands do not require a running gateway. Keep client and server checkouts on
the same revision so model IDs and capability metadata agree.

## Deliberate v1 limits

- DwarfStar uses the reviewed direct model only; DSpark remains a direct-server
  and benchmark option.
- The gateway owns one resource pool and does not keep llama.cpp and DwarfStar
  resident together.
- It does not add authentication, TLS, retries, request rewriting, or an
  arbitrary upstream registry.
- Direct servers remain supported diagnostic surfaces, not hidden gateway
  dependencies.

Changes to model capability fields belong in `internal/catalog/` and
`internal/textmodel/`. Request scheduling, HTTP behavior, and lifecycle belong
in `internal/gateway/`; Podman command confinement remains in
`internal/runtime/` and `internal/podman/`. Test the scheduler and proxy with
fakes first, then accept real lazy start, switching, streaming, cancellation,
and cleanup on representative GPU hardware.
