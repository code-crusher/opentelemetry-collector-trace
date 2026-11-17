# Supply chain security

This page describes how the artifacts published for the OpenTelemetry Collector are produced and
signed, how to verify them, and what is currently not covered. It is intended for operators who
need to assess the Collector for a regulated environment, and for contributors working on a
hardened image for the [container.gov.de](https://container.gov.de/) (ZenDiS) initiative tracked in
[issue #15862](https://github.com/open-telemetry/opentelemetry-collector/issues/15862).

The [opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases)
repository is responsible for building and publishing the release artifacts. This repository
(`opentelemetry-collector`) contains the source code and does not build or publish any official
binary, image, package, or SBOM by itself.

## What is published

For every release, the releases repository publishes, for each distribution (`otelcol`,
`otelcol-contrib`, `otelcol-otlp`, `otelcol-k8s`, `otelcol-prometheus`, `otelcol-ebpf-profiler`):

- Container images on Docker Hub and ghcr.io (`otel/opentelemetry-collector*` and
  `ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector*`).
- Archives (`.tar.gz`), Linux packages (`.deb`, `.rpm`) and Windows installers (`.msi`) on the
  GitHub release.
- A `.sha256` checksum next to every archive, package and SBOM.
- An SPDX 2.3 JSON SBOM (`<artifact>.sbom.json`) produced with [syft](https://github.com/anchore/syft)
  for each archive and package. The SBOM lists the Go modules and files that make up the artifact,
  with package URLs (`pkg:golang/...`) and CPEs. The SBOM for `otelcol_0.165.0_linux_amd64.tar.gz`
  in the v0.165.0 release lists 462 packages and was produced by syft 1.54.0.
- A [Sigstore](https://www.sigstore.dev/) bundle (`<artifact>.sigstore.json`) for the artifact, for
  its `.sha256` file and for its `.sbom.json` file and its `.sbom.json.sha256` file. Signatures are
  produced with `cosign sign-blob --bundle` without any long-lived key, using GitHub Actions OIDC,
  so no key material is stored anywhere. The signing identity is the reusable release workflow, for example:
  `https://github.com/open-telemetry/opentelemetry-collector-releases/.github/workflows/base-release.yaml@refs/tags/v0.165.0`
  issued by `https://token.actions.githubusercontent.com`.

Container images are signed with `cosign sign` in the same workflow. See
[Verifying the images signatures](../README.md#verifying-the-images-signatures) in the README for
the exact command.

## Verifying release artifacts

Install [cosign](https://docs.sigstore.dev/cosign/installation/) first. Set `<RELEASE_TAG>` to the
tag you downloaded (for example `v0.165.0`) and `<ARTIFACT>` to the file you want to verify:

```console
$ cosign verify-blob \
  --bundle <ARTIFACT>.sigstore.json \
  --certificate-identity=https://github.com/open-telemetry/opentelemetry-collector-releases/.github/workflows/base-release.yaml@refs/tags/<RELEASE_TAG> \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  <ARTIFACT>
Verified OK
```

The SBOM can be verified the same way with `<ARTIFACT>.sbom.json` as the artifact. To also confirm
the checksum matches the published value:

```console
$ sha256sum -c <ARTIFACT>.sha256
```

To inspect the SBOM itself, for example with [syft](https://github.com/anchore/syft) or
`jq`:

```console
$ jq '{spdxVersion, packages: (.packages | length)}' <ARTIFACT>.sbom.json
```

Note that the signature proves *which workflow produced the artifact*. It does not say anything
about the contents of that artifact being free of vulnerabilities.

## What is not provided today

As of November 2026 (v0.165.0), the releases pipeline does **not** provide:

- No VEX (Vulnerability Exploitability eXchange) documents of any kind, such as OpenVEX or CSAF VEX.
  In particular, there is no machine-readable statement about which vulnerabilities reported against
  dependencies in the SBOMs are not exploitable in the published Collector builds.
- No SLSA provenance attestation or other build provenance attestation for the images or the
  release artifacts. The signing identity above proves the workflow that signed, not the build
  steps or parameters.
- No image SBOM attestation attached to the container images in the registry. SBOMs are published
  as assets on the GitHub release only.
- No hardened variant of the published images. The published images are built `FROM scratch`
  (container image config sets no shell, package manager, or C library beyond CA certificates) and run
  as `USER 10001:10001`, except `otelcol-ebpf-profiler`, which runs as `USER 0`. There is no
  additional hardening such as a hardened base image, seccomp/AppArmor profile shipped in the
  image, or systemd sandboxing for the container image itself.
  (The separate `deb` and `rpm` packages do ship systemd unit files; a separate change hardened
  those units with `NoNewPrivileges`, `RestrictRealtime`, `RestrictSUIDSGID`, `LockPersonality`,
  `ProtectKernelModules`, `ProtectKernelTunables`, `ProtectControlGroups` and `ProtectHostname`.)

## Status of a hardened image for container.gov.de

[container.gov.de](https://container.gov.de/) is the publishing platform operated by ZenDiS on
behalf of the German Federal Ministry for Digital and State Modernization. It distributes reviewed
and hardened container images for public administration, and its publication process expects, among
other things, hardening of the image and a VEX document that disposition the vulnerabilities found
in it.

As of November 2026:

- No hardened Collector image is published by this project, neither on container.gov.de nor
  anywhere else. The images listed above are the only official images.
- No VEX document is produced or published for any Collector artifact.
- The [request in issue #15862](https://github.com/open-telemetry/opentelemetry-collector/issues/15862)
  (publish a hardened image on container.gov.de, including a VEX) is open, labelled `needs triage`,
  and has no assignee and no implementation. The maintainer response so far has been to point the
  reporter to the Collector SIG meetings; no approach has been agreed.
- The work cannot be done in this repository. This repo does not build or publish any container
  image. Image changes, image hardening, VEX generation in the release pipeline, and any
  publication to container.gov.de would belong to
  [opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases),
  alongside the existing GoReleaser configuration and workflow files.

If you want to drive this, the practical path is:

1. Raise the proposal at a Collector SIG meeting, or on
   [#otel-collector-dev](https://cloud-native.slack.com/archives/C01N6P7KR6W), and in
   [opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases)
   with a concrete design: the base image, the hardening measures, who maintains them, how the VEX
   is produced and kept current (format, for example [OpenVEX](https://openvex.dev/) or CSAF, and
   the tooling that emits it), and what the consumers are expected to do with it.
2. Note that this affects the release pipeline and the release managers, not just code, so it
   needs SIG approval before implementation starts. Related prior art for a distribution-level
   security effort is the [FIPS 140-3 distribution proposal](https://github.com/open-telemetry/opentelemetry-collector-releases/issues/1744)
   in the releases repository.

Until such a proposal is accepted and implemented, consumers who need a hardened image should
either build one themselves with the
[OpenTelemetry Collector Builder](../cmd/builder/README.md), or consume the published artifacts
listed above and apply their own hardening, SBOM and VEX tooling in their own pipeline. The SBOMs
attached to each release are the intended input for that; every artifact, checksum and SBOM is
signed as described above.
