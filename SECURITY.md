# Security policy

## Supported code

Paracetamol is currently a public pre-release project without stable release
branches. Security fixes target the current `master` branch. Older commits are
not maintained as separate supported versions.

## Reporting a vulnerability

Please use GitHub's
[private vulnerability reporting](https://github.com/fff7d1bc/paracetamol/security/advisories/new)
for a security-sensitive report. Do not open a public issue containing exploit
details, credentials, access tokens, private model URLs, or other secrets.

If private reporting is unavailable, open a public issue that contains no
sensitive detail and asks the maintainer to establish a private contact path.
Ordinary correctness, compatibility, and performance bugs belong in
[GitHub Issues](https://github.com/fff7d1bc/paracetamol/issues).

Useful security reports include concrete reproduction steps, affected commit,
host distribution and version, Podman version, and the smallest relevant logs.
Particularly relevant areas include:

- command or argument injection across the host/container boundary;
- token, credential, or private-path disclosure;
- path traversal, unsafe archive handling, or managed-content verification
  bypasses;
- broader mounts, devices, capabilities, networking, or write access than the
  selected workload requires;
- cleanup or installation affecting resources outside Paracetamol's declared
  ownership; and
- a way to bypass license acknowledgment or execute untrusted remote content
  outside the documented import boundary.

Do not include actual secrets or large model files in a report. A minimal
synthetic reproducer is preferred.
