# Contributing to ROCmplete

ROCmplete is a public pre-release project. Focused fixes, hardware results, and
careful improvements are welcome. Interfaces can still change, but changes
should make the whole design clearer rather than add compatibility layers for
interfaces that have not been declared stable.

Small documentation and test fixes can go straight to a pull request. For a
new application, command family, storage format, hardware policy, or other
change with a broad user-visible effect, open an issue first so the intended
scope can be agreed before substantial work begins.

## Finding your way around

The root `README.md` provides the quick start. The unified documentation index
at [`docs/README.md`](docs/README.md) routes users into `docs/guides/` and
maintainers into the source, upgrade, catalog, testing, and hardware
references.

Keep application, hardware profile, command mode, bundle variant, and workflow
renderer changes in their documented extension points. Do not treat a
successful image build or CPU startup as GPU inference acceptance.

## Validation

ROCmplete's host launcher uses the Python standard library. Run these checks for
every change:

```bash
python3 -m compileall -q applications containers src/rocmplete tests tools
bash -n applications/comfyui/entrypoint.sh \
  applications/llama-cpp/entrypoint.sh \
  applications/dwarfstar/entrypoint.sh
python3 -m json.tool catalog/catalog.json >/dev/null
PYTHONPATH=src python3 -m unittest discover -s tests
git diff --check
```

Then follow the higher validation tier in
[`docs/testing-and-release.md`](docs/testing-and-release.md) for the part of the
system you changed. Catalog changes require the full installer dry run. Image,
runtime, hardware-policy, and inference changes require their documented
container or target-hardware checks.

It is fine to submit a change without every GPU class available. State exactly
what you tested and what remains unverified. Do not turn an unavailable host or
external service into a claimed project failure or success.

## Pull requests

Keep commits focused and independently reviewable. A pull request should say:

- what user-visible or maintenance problem it solves;
- why the chosen boundary owns the fix;
- which checks were run and on which relevant hardware; and
- what could not be verified.

Update user documentation when commands, flags, defaults, storage, exit
behavior, or requirements change. Update maintainer documentation when source
ownership or maintenance procedure changes. New third-party code, models,
workflows, or dependencies must carry exact provenance and license information.

Do not commit model weights, generated media, local benchmark results, secrets,
or machine-specific state.

## Bug reports

For an ordinary bug, use
[GitHub Issues](https://github.com/fff7d1bc/rocmplete/issues) and include the
smallest useful set of details:

- `./rocmplete --version` and the exact command that failed;
- relevant `./rocmplete doctor` output;
- distribution, kernel, Podman version, GPU, and system memory;
- the selected profile, render nodes, application, and content bundle; and
- complete error output or logs around the failure.

Remove access tokens, credentials, private URLs, and unrelated personal paths.
Security-sensitive reports follow [`SECURITY.md`](SECURITY.md), not a public
issue.
