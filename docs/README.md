# Paracetamol documentation

Start with the [project README](../README.md) for requirements, installation
and the shortest working path. Command-specific `--help` is the interface
reference. These pages explain how to use and maintain the current checkout.

## Using Paracetamol

- [Applications](guides/applications.md) covers ComfyUI, llama.cpp, current
  model choices, direct APIs and multi-GPU runs.
- [Content](guides/content.md) covers recipes, bundles, licensing, verification,
  mirrors, imports and workflows.
- [Gateway](gateway.md) covers configuration, authentication, scheduling,
  API routes, diagnostics and private usage statistics.
- [Remote Maki](guides/remote-maki.md) connects native Linux or macOS Maki
  without installing Paracetamol on the client.
- [Operations](guides/operations.md) covers builds, data and backups, image
  transfer, logs, scoped cleanup and troubleshooting.
- [Tuning and benchmarks](guides/tuning.md) explains host access, runtime
  policies, context capacity and repeatable measurements.
- [Upgrade notes](guides/upgrade-notes.md) covers selecting a source revision,
  updating a checkout, rollback limits and deliberate pre-release changes.

For copyable application walkthroughs, run `./paracetamol guide`.

## Maintaining Paracetamol

Returning after time away? Check `git status --short`, read the reference
matching the change, and establish the
[development baseline](testing-and-release.md#tier-1-static-and-unit-checks).
Inspect current pins and capabilities in source rather than using a dated
experiment as a version list.

- [Architecture and invariants](architecture.md) defines ownership, trust,
  images, storage and the upstream-base-plus-patches approach.
- [Routine upgrade runbook](routine-upgrade-runbook.md) routes a dependency,
  application and model refresh without changing everything at once.
- [llama.cpp maintenance](llama-cpp-maintenance.md) records the accepted
  baseline, open regressions and how to resume an investigation.
- [Upgrading](upgrading.md) describes coupled pins, patch contracts, removal
  gates and application-specific procedures.
- [Content catalog maintenance](content-catalog.md) covers exact artifacts,
  licensing, schema, recipes, workflows and benchmark resources.
- [Extension points](extending.md) explains where new commands, applications,
  modes and profiles belong.
- [Testing and acceptance](testing-and-release.md) separates routine checks,
  image/CPU checks, native fixtures and real inference acceptance.
- [Hardware acceptance](hardware-acceptance.md) is the live coverage matrix
  and result-recording procedure.
- [Contribution guide](../CONTRIBUTING.md) summarizes checks and useful reports.
- [Security policy](../SECURITY.md) defines the threat boundary and reporting.

The project does not promise stable releases or interfaces yet. A full Git
revision identifies the integration recipe. It does not promise byte-identical
images or restore mutable application data. See
[revision selection and rollback](guides/upgrade-notes.md).

## Research and deferred directions

The [research index](research/README.md) holds dated measurements, retired
model/client evaluations and upgrade journals. Those records preserve exact
inputs, failures and limitations. They are evidence for the recorded tuple,
not current defaults or a substitute for fresh hardware acceptance.

The [Gufo recheck tool](../tools/gufo-recheck/README.md) is an explicit
developer experiment with manual backend lifecycle, not a managed engine.
The [vLLM integration direction](vllm-integration-direction.md) describes a
possible additive engine, not implemented support.

## Source ownership

Source and package-local tests are authoritative. Update affected prose in the
same change when behavior, commands, counts or defaults change.

| Concern | Owner |
| --- | --- |
| Base images, ROCm/PyTorch, application commits and patch order | `Containerfile`, `applications/<application>/` |
| Complete content-tools dependency pins | `containers/content_tools/requirements.txt` |
| Product identity and build namespace | `internal/identity/`, `Makefile` |
| Application registry, image tags, capabilities, ports and build DAG | `internal/application/`, `internal/buildplan/` |
| Host configuration and precedence | `internal/config/` |
| Canonical hardware identities and read-only host diagnostics | `internal/platform/`, `internal/hostdoctor/` |
| Public commands, parsing, orchestration and terminal UX | `internal/cli/`, `internal/ui/` |
| Repository resources and PATH delegation | `internal/project/`, `paracetamol`, `bin/paracetamol` |
| Subprocess and constrained Podman runtime | `internal/process/`, `internal/podman/`, `internal/runtime/` |
| Host storage partitions and atomic publication | `internal/storage/`, `internal/atomicfile/` |
| Receipt-aware text inventory and verification | `internal/modelinventory/`, `internal/verification/` |
| Frozen gateway inventory, scheduler, proxy, reports and statistics | `internal/gateway/`, `internal/textmodel/` |
| Portable native Maki examples, without client execution or state ownership | `internal/clientconfig/` |
| Content schema, immutable pins and relationships | `internal/catalog/`, `catalog/` |
| Runnable application-first recipes | `internal/recipes/` |
| Download, verification and installation | `internal/content/` |
| Allowlisted remote metadata and local-pack generation | `internal/remoteimport/`, `internal/contentpack/` |
| Immutable curated workflow installation | `catalog/workflows/`, `internal/cli/commands_content.go` |
| Benchmarks, typed results, resume and cleanup | `internal/benchmark/` |
| Offline managed-image transfer | `internal/imagearchive/` |
| Bounded target-hardware acceptance | `internal/cli/commands_acceptance.go` |
| Container-side profile/application enforcement | `containers/common/profile.py`, application entrypoints |
| Read-only metadata/archive/workflow probes | `internal/probe/`, `tools/` |
| Opt-in Gufo source build and regression gate | `tools/gufo-recheck/` |
| Archived tasks and hidden grading fixtures, not an active Pi runner | `evaluations/coding/` |
| Third-party provenance | `THIRD_PARTY_NOTICES.md` |

Uppercase names such as `CURRENT_IMAGE`, `REVISION` and `WORKFLOW_ID` in
commands are placeholders. Replace them with reviewed values from the current
checkout.
