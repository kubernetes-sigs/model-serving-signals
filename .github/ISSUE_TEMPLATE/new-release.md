---
name: New release
about: Propose and track a model-serving-signals release
title: "Release v0.x.0"
labels: ""
assignees: ""
---

<!-- Update the version, branch, and previous release before requesting review. -->

## Contents

- [Introduction](#introduction)
- [Release details](#release-details)
- [Checklist](#checklist)
  - [Planning and approvals](#planning-and-approvals)
  - [Validation](#validation)
  - [Tag and artifacts](#tag-and-artifacts)
  - [Publication](#publication)
- [Links](#links)

## Introduction

This issue tracks a model-serving-signals release using the procedure in
[`RELEASE.md`](../../RELEASE.md).

## Release details

- Proposed version: `v0.x.0`
- Release branch: `release-0.x`
- Previous release: `v0.0.0`
- Release manager:

## Checklist

### Planning and approvals

- [ ] The version follows the SemVer and `release-MAJOR.MINOR` convention in [`RELEASE.md`](../../RELEASE.md).
- [ ] The release manager has the required repository, GitHub, and registry permissions.
- [ ] The release scope and release notes have been reviewed by the OWNERS.

### Validation

- [ ] CI is green for the release commit.
- [ ] `make test` passes.
- [ ] `make test-e2e` passes.
- [ ] `make build-docs-netlify` passes.
- [ ] Documentation, catalog profiles, and examples are up to date.

### Tag and artifacts

- [ ] The release branch is pushed and up to date.
- [ ] A signed tag was created and verified.
- [ ] The model-serving-signals Prow image-pushing job succeeded for this release tag.
- [ ] The multi-architecture staging image was published.
- [ ] The staging image digest was recorded.
- [ ] A PR updated the [`k8s.io` promotion manifest](https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-model-serving-signals/images.yaml) with the image digest and release tag.
- [ ] The image was promoted to `registry.k8s.io`.
- [ ] The promoted image digest matches the staging digest.
- [ ] The promoted image was smoke-tested.

### Publication

- [ ] The GitHub release was created with reviewed release notes.
- [ ] The image reference and digest were included in the release notes.
- [ ] Any requested project-channel announcement was sent; no mailing-list announcement is required.
- [ ] Main-branch documentation was updated if needed.
- [ ] This issue was updated with the release, image, and announcement links.
- [ ] This issue was closed after all release steps completed.

## Links

- GitHub release:
- Staging image:
- Public image:
- Image digest:
- Project-channel announcement, if requested:
