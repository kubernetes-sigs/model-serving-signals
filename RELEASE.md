# Release Process

This guide describes the release steps supported by the repository's current
configuration. A release is coordinated in a tracking issue and approved by the
project's approvers in [OWNERS](OWNERS). Only an authorized maintainer should
push release branches or tags, publish staging images, or request production
image promotion.

## Prerequisites

- The release owner has signed the Kubernetes Contributor License Agreement
  and has write permission to this repository.
- The release tagger has a registered signing key for creating signed tags.
- The release owner can create GitHub releases and push the release branch and
  tag. Staging image pushes use the Cloud Build service account configured in
  [cloudbuild.yaml](cloudbuild.yaml); contributors should not use or copy its
  credentials.
- The release tracking issue records the target version, the changes included,
  the release owner, and approval from the [OWNERS](OWNERS) approvers.
- Before the first public image release, an owner has confirmed that the image
  promoter configuration exists in the Kubernetes registry repository. This
  repository configures a staging registry but does not contain the production
  promotion mapping.

## Version and branch

Use `vMAJOR.MINOR.PATCH` for product release tags. The repository currently has
only a `v0.0.0` pre-release tag, so the release issue must state the next version
and get approver agreement before a branch or tag is created. Use the following
version increments unless approvers document a different decision in that
issue:

- Before `1.0.0`, use a patch for bug fixes and a minor version for new signals,
  engine profiles, or contract changes. The pre-1.0 contract may still change.
- After `1.0.0`, use a patch for compatible fixes, a minor version for
  compatible additions, and a major version for incompatible contract changes.

The image build's generated `vYYYYMMDD-hash` value is a build identifier, not a
product release version. `cloudbuild.yaml` supports release branch names such as
`release-0.2`; use `release-MAJOR.MINOR` for a maintained release line. Create
the first branch from `main`. For a patch release, use the existing branch for
that version line. The release issue should identify the branch and base commit.
For a release candidate, append `-rc.N` to the product version (for example,
`v0.2.0-rc.1`) and mark its GitHub release as a pre-release.

## Prepare the release

1. Open a release tracking issue. List the proposed version, release branch,
   included pull requests, known limitations, and release owner. The repository
   uses the release-note block in the pull request template; it does not
   currently maintain a `CHANGELOG.md`.
2. Wait for all intended changes to merge. Confirm required pull request checks
   are green, including `make test`, `make test-e2e`, and the strict docs build
   (`make build-docs-netlify`). The e2e target requires the local kind
   prerequisites described in [test/e2e/README.md](test/e2e/README.md).
3. Ask the [OWNERS](OWNERS) approvers to review the release issue and confirm
   the version, included changes, and release branch.
4. Create or update the release branch from the approved base commit. For the
   first `0.2` release line, an authorized maintainer would run:

   ```bash
   git fetch origin
   git switch --create release-0.2 origin/main
   git push origin release-0.2
   ```

   For a patch release, check out the existing branch with
   `git switch --track origin/release-0.2`. Land release-only fixes through
   reviewed pull requests. Re-run the checks for the final release commit.

## Build and publish images

The repository builds a multi-architecture `linux/amd64` and `linux/arm64`
exporter image. The Cloud Build postsubmit configuration calls
`make image-pushing-postsubmit`, which builds and pushes to the Kubernetes
staging Artifact Registry:

```text
us-central1-docker.pkg.dev/k8s-staging-images/model-serving-signals/metrics-exporter:<build-tag>
```

The build tag comes from `GIT_TAG` (`git describe --tags --dirty --always` in
the [Makefile](Makefile)); `EXTRA_TAG` can also add the source branch or tag as
an alias. After updating a release branch or tag, the release owner must verify
that the configured postsubmit job ran for that ref and record the image tag
and digest in the release issue. A local `make image-build` is for development;
it does not publish an artifact.

Production publication is a separate image-promotion step managed in the
Kubernetes registry repository. Do not announce a `registry.k8s.io` image until
the owner has confirmed the promotion mapping, submitted the required mapping
change, and verified that the promoted digest is available. If promotion has
not been configured, record that as a blocker in the release issue.

## Tag and publish the release

After the approved release commit and image are ready, create a signed tag on
the release branch. For example, for `v0.2.1`:

```bash
git switch release-0.2
git pull --ff-only origin release-0.2
git tag -s v0.2.1 -m "model-serving-signals v0.2.1"
git push origin v0.2.1
```

The release owner confirms the tag points to the approved commit and the
postsubmit image build completed before creating the GitHub release. A tag push
does not by itself prove that production image promotion completed; verify the
image digest separately.

## Create the GitHub release

After the final tag and image are verified, an authorized maintainer creates a
GitHub release from the tag. Use the release issue and merged pull request
release-note blocks to prepare the notes; call out contract changes, supported
engine profiles, image references, and known limitations. This project
currently publishes its container image rather than downloadable binaries.
Mark a release as a pre-release when approvers have designated it as one.

## After release and patch releases

- Update documentation and examples on `main` if they pin an older image or
  describe behavior changed by the release.
- Record the GitHub release and promoted image digest in the tracking issue,
  announce the release through the SIG Autoscaling channels listed in
  [CONTRIBUTING.md](CONTRIBUTING.md), and close the issue when follow-up work is
  captured.
- Treat published version tags and image tags as immutable. If a release has a
  problem before promotion, stop the promotion. If a published release needs a
  fix, prepare a patch on its `release-MAJOR.MINOR` branch, run the same checks,
  and publish a new patch version and image. Do not move or reuse the old tag.
- For rollback, stop further promotion and document the affected version and
  digest in the release issue. If the image is already in use, coordinate any
  rollback with its consumers; publish a corrected patch release rather than
  silently replacing the image behind an existing tag.
