# Connect native Maki to Paracetamol

Run Maki on your Mac or Linux workstation and use a Paracetamol gateway on
another host. The client needs Maki 0.6.3 or newer and network access to the
gateway. It does not need Go, Podman, Node.js or a Paracetamol checkout.

This is native client configuration, not a sandbox. Maki's permission prompts
and folder trust do not provide the filesystem isolation of Paracetamol's
former Linux bubblewrap launcher. The Paracetamol controller and GPU services
remain Linux-only.

## Download the configuration

Start the gateway on the inference host with its intended LAN address and
port. On the client, download into a temporary directory first.

```sh
maki_setup_dir=$(mktemp -d)
curl --fail --show-error \
  http://gateway.example:7455/paracetamol/v1/clients/maki/providers.toml \
  -o "$maki_setup_dir/providers.toml"
curl --fail --show-error \
  http://gateway.example:7455/paracetamol/v1/clients/maki/init.lua \
  -o "$maki_setup_dir/init.lua"
```

Replace `gateway.example:7455` with your gateway address. Check both downloads
succeeded before installing either file. Inspect their contents as you would
any configuration downloaded from another host. In particular, `init.lua` is
Lua code which Maki will execute. Use a trusted LAN or encrypted transport
such as HTTPS or a private VPN. Bearer authentication does not encrypt HTTP.

The provider file includes only the gateway's verified, coding-capable models,
their context limits and supported reasoning choices. It uses the gateway
address you contacted, not a private backend port. Fetching these files does
not load a model.

### Fresh Maki setup

Maki uses `~/.config/maki` on macOS and Linux by default. If you set
`XDG_CONFIG_HOME`, use its `maki` directory instead. These commands refuse to
replace existing files. If either already exists, use the merge instructions.

```sh
maki_config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/maki"
mkdir -p "$maki_config_dir"
test ! -e "$maki_config_dir/providers.toml" && test ! -L "$maki_config_dir/providers.toml" && \
  install -m 600 "$maki_setup_dir/providers.toml" "$maki_config_dir/providers.toml"
test ! -e "$maki_config_dir/init.lua" && test ! -L "$maki_config_dir/init.lua" && \
  install -m 600 "$maki_setup_dir/init.lua" "$maki_config_dir/init.lua"
maki models
maki
```

### Existing Maki setup

Back up your configuration before merging.

- Add the exported `paracetamol` provider and its model tables to
  `providers.toml`. If it already exists, replace its complete block, including
  nested model and thinking tables. Do not append duplicate tables or replace
  unrelated providers.
- Merge the startup preferences into the existing `maki.setup(...)` call in
  `init.lua`. Maki permits one such call per file. Do not append a second call
  or replace your other settings.
- If an existing `allowed_models` or `excluded_models` policy hides this
  provider, adjust that policy deliberately. The export does not override it.

The defaults select regular Unsloth Qwen3.8 Flash-Next when available, then
dense Qwen3.8 27B MTP Q8, then the first available reviewed model. Adaptive
thinking selects xhigh for regular Flash-Next and the reviewed default for
each other model. The example limits concurrent task subagents to one. Saved
tier assignments are not reset.

Use Maki's native model picker and `/thinking` to change selections. Neither
requires a Paracetamol wrapper. Gateway sampling defaults follow the requested
thinking mode. Explicit client sampler values still take precedence.

## Authentication

Both downloads follow the same Bearer policy as inference requests. For an
authenticated gateway, add `--header @/absolute/path/to/private-header-file`
to each curl command. That file should contain the single header
`Authorization: Bearer YOUR_KEY` and be readable only by you. Never put a key
in the URL or commit the header file.

The exported provider references `PARACETAMOL_API_KEY`, not the server's key
value. Supply your client key through that environment variable or save it
with Maki's native credential prompt after installing the provider.

```sh
maki auth login paracetamol
```

For an unauthenticated gateway, the export supplies `paracetamol-local` as a
dummy credential because Maki expects a value. It grants no access to an
authenticated gateway. Refresh the file if the gateway's authentication mode
changes. Environment variables and saved Maki credentials take precedence
over that dummy value.

## Refreshes and alternative addresses

Download and merge again when models or capabilities change. The export is a
snapshot of the running gateway's startup inventory. Newly installed content
requires restarting the gateway before it can appear. Maki does not discover
additional models lacking exported capabilities.

If a reverse proxy changes the externally visible address, supply the client
API URL explicitly. Apply the same parameter to both downloads.

```sh
curl --fail --show-error --get \
  --data-urlencode 'base_url=https://ai.example.net/v1' \
  http://127.0.0.1:7455/paracetamol/v1/clients/maki/providers.toml \
  -o "$maki_setup_dir/providers.toml"
```

Only credential-free HTTP(S) URLs with the `/v1` path are accepted. Forwarded
headers are not trusted and the gateway never contacts the supplied URL.

## Verify the connection

Check that `maki models` contains the expected `paracetamol/` models. Start
Maki, check its model and `/thinking`, and ask it to read a harmless file in a
disposable directory. Gateway logs should show that model and reasoning
selection, followed by the tool-result request.

The example does not inject a shared session identifier. Request IDs and
usage remain observable, but launcher-generated Maki session grouping is not
part of native configuration.

Maki is a maintained example, not a required gateway frontend. Other clients
can use the same OpenAI-compatible API. Configure their model limits and
reasoning controls explicitly rather than assuming `/v1/models` describes
every capability.
