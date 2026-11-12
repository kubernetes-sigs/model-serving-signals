# Model-serving Signals Release Guide

This document describes how to release model-serving-signals. A release consists of a versioned source tag, a container image published through the Kubernetes staging registry and promoted to `registry.k8s.io`, and a GitHub release.

## Contents

<!-- toc -->
- [Release policy](#release-policy)
- [Prerequisites](#prerequisites)
- [Choose the version and branch](#choose-the-version-and-branch)
- [Pre-release checks](#pre-release-checks)
- [Create the release tag](#create-the-release-tag)
- [Build and publish the container image](#build-and-publish-the-container-image)
- [Create the GitHub release](#create-the-github-release)
- [Announce and finish the release](#announce-and-finish-the-release)
<!-- /toc -->

## Release policy

Releases are made when the maintainers agree that the changes on `main` form a coherent release. Following the Kubernetes image-pushing convention, use [Semantic Versioning](https://semver.org/) with a `v` prefix:

- `vMAJOR.MINOR.PATCH` is a normal release, for example `v0.1.0`.
- Increment `MAJOR` for incompatible public contract changes.
- Increment `MINOR` for backwards-compatible features or new signals.
- Increment `PATCH` for backwards-compatible fixes.
- Release candidates use `vMAJOR.MINOR.PATCH-rc.N`.

The repository currently has a `v0.0.0` tag. The existing tag is lightweight, and its GitHub release is marked as a prerelease with generated release notes. Future release tags must be signed.

## Prerequisites

The release manager should:

1. Be an OWNER or have explicit approval from the OWNERS for this release.
2. Have permission to create branches and tags in this repository.
3. Have permission to create GitHub releases.
4. Have access to the Kubernetes staging-image publishing and promotion process. The image is staged in the [`k8s-staging-images` Artifact Registry](https://console.cloud.google.com/artifacts/docker/k8s-staging-images/us-central1/model-serving-signals) and is promoted separately to `registry.k8s.io`.
5. Have `git`, Go, Docker with Buildx, and the tools required by the e2e test: `kind`, `kubectl`, and `curl`.
6. Have a signing key registered with GitHub and configured for signed tags.

Before starting, open a release issue using [`.github/ISSUE_TEMPLATE/new-release.md`](.github/ISSUE_TEMPLATE/new-release.md) and record the proposed version, release manager, and previous release.

## Choose the version and branch

The project's [Prow image-pushing job](https://github.com/kubernetes/test-infra/blob/master/config/jobs/image-pushing/k8s-staging-model-serving-signals.yaml) builds `main`, `release-*` branches, and SemVer tags. Use a `release-MAJOR.MINOR` branch for a release line.

Set shell variables for the release. Replace the values in this example:

```shell
export VERSION=v0.1.0
export RELEASE_BRANCH=release-0.1
export REMOTE=origin
```

Use an `-rc.N` suffix for a release candidate and keep the branch name `release-MAJOR.MINOR` without the candidate or patch suffix. For example, `v0.1.0-rc.1` and `v0.1.1` would both belong to `release-0.1`.

For the first release in a minor line, create the branch from an up-to-date `main`:

```shell
git fetch --prune ${REMOTE}
git switch main
git pull --ff-only ${REMOTE} main
git switch -c ${RELEASE_BRANCH}
git push -u ${REMOTE} ${RELEASE_BRANCH}
```

For a patch release or a subsequent release candidate, use the existing release branch:

```shell
git fetch --prune ${REMOTE}
git switch ${RELEASE_BRANCH}
git pull --ff-only ${REMOTE} ${RELEASE_BRANCH}
```

Release-specific fixes should be reviewed and merged into the release branch before continuing.

## Pre-release checks

Complete the release issue checklist and ensure that:

- The release branch contains all intended changes and has owner approval.
- CI is green for the exact commit being released.
- The release notes describe every user-visible change since the previous tag.
- Signal contract changes are reflected in [`site-src/reference/contract.md`](site-src/reference/contract.md) and the catalog profiles.
- Documentation builds successfully:

  ```shell
  make build-docs-netlify
  ```

- Unit, lint, formatting, boilerplate, and vet checks pass:

  ```shell
  make test
  ```

- The end-to-end test passes:

  ```shell
  make test-e2e
  ```

  This test requires Docker, kind, kubectl, Go, and curl and recreates a disposable cluster.
- Optionally, run the real-GPU validation test suite before a release if you have access to a GPU Kubernetes cluster:

  ```shell
  make test-e2e-gpu
  ```

  By default, the test uses the staging `us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:main` image and creates a temporary namespace named `mss-gpu-e2e-<Unix timestamp>`. To use a different image or namespace, set `E2E_GPU_EXPORTER_IMAGE` or `E2E_GPU_NAMESPACE`. The selected namespace must not already exist because the test creates it and deletes it when finished. The exporter image must already be pushed to a registry that the cluster can access.

  ```shell
  E2E_GPU_NAMESPACE=<temporary-namespace> \
    E2E_GPU_EXPORTER_IMAGE=<exporter-image> \
    INFERENCE_ENGINE=<engine> \
    make test-e2e-gpu
  ```

  This manual check deploys the selected real inference-engine example with the exporter sidecar and compares the engine's native metrics with the exported contract metrics. It defaults to `INFERENCE_ENGINE=vLLM`; set `INFERENCE_ENGINE=tokenspeed` to validate the TokenSpeed example. It is not a Prow job or a required PR check, and its result does not need to be recorded in the release issue.
- The release image name and tag are correct, and the image will contain the intended catalog profiles.
- Any release-specific documentation uses the released image rather than the `:main` staging image where appropriate.

## Create the release tag

After the checks pass, record the release commit and create a signed, annotated tag:

```shell
git status --short
git rev-parse HEAD
git tag -s ${VERSION} -m "Release ${VERSION}"
git tag -v ${VERSION}
git push ${REMOTE} ${VERSION}
```

## Build and publish the container image

The Makefile builds the multi-architecture `metrics-exporter` image. The repository's Cloud Build configuration invokes `image-pushing-postsubmit`, which publishes to the Kubernetes staging registry using the dedicated staging service account. The model-serving-signals Prow job is configured for `main`, `release-*` branches, and SemVer tags, and passes the release ref through to Cloud Build.

After the staging build completes:

1. Confirm that the staging image exists with the release tag:

   ```shell
   docker buildx imagetools inspect \
     us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:${VERSION}
   ```

2. Record the image digest and confirm that both `linux/amd64` and `linux/arm64` manifests are present.
3. Open a PR in the [`k8s.io` image-promotion manifest](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-model-serving-signals/images.yaml) mapping `metrics-exporter` to the staging image digest and `${VERSION}`. The manifest currently maps the `v0.0.0` digest to `v0.0.0`; add the new digest-to-tag mapping without replacing existing releases.
4. Ensure the PR is approved by the manifest [OWNERS](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-model-serving-signals/OWNERS), wait for the promotion validation job, and merge it. The image promoter publishes the digest to:

   ```text
   registry.k8s.io/model-serving-signals/metrics-exporter:${VERSION}
   ```

5. Pull the promoted image by its release tag and verify its digest matches the staged digest. Run the documented example or an equivalent smoke test.

If the Prow build is unavailable or maintainers approve a manual staging build, use:

```shell
make staging-image-push \
  GIT_TAG=${VERSION} \
  IMAGE_REPO=us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter
```

Do not run this command unless you have the staging registry permissions and the normal automated build is unavailable.

## Create the GitHub release

Once the public image is available:

1. Open the repository's [new release page](https://github.com/kubernetes-sigs/model-serving-signals/releases/new).
2. Select the tag `${VERSION}` and the release branch as its target.
3. Use `${VERSION}` as the release title.
4. Generate release notes, then review them against the commits since the previous release.
5. Include the promoted image name and immutable digest in the release notes.
6. Mark release candidates as **pre-release**.
7. Publish the release.

The repository currently contains no standalone binaries, Helm charts, or generated release manifests to attach to the GitHub release.

## Announce and finish the release

After publishing the GitHub release:

1. If maintainers request an announcement in an approved project channel, include links to the GitHub release and documentation, plus the promoted image reference.
2. Update the release issue with the GitHub release URL, image digest, and project-channel announcement URL, if an announcement was requested.
3. For a new minor line, ensure `main` continues to use the `:main` staging image and that the next development work is not accidentally committed to the release branch.
4. Close the release issue only after the public image, GitHub release, and announcement are complete.
