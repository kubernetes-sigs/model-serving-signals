# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG BUILDER_IMAGE=golang:1.27.2
ARG BASE_IMAGE=gcr.io/distroless/static:nonroot

FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} AS builder

ENV CGO_ENABLED=0
ARG TARGETOS
ARG TARGETARCH
ARG COMMIT_SHA=unknown
ARG BUILD_REF

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/
COPY catalog/ catalog/
COPY version/ version/

RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -ldflags="-X sigs.k8s.io/model-serving-signals/version.CommitSHA=${COMMIT_SHA} -X sigs.k8s.io/model-serving-signals/version.BuildRef=${BUILD_REF}" \
    -o exporter ./cmd/exporter

FROM ${BASE_IMAGE}
COPY --from=builder /workspace/exporter /exporter
COPY catalog/ /catalog/
USER 65532:65532

EXPOSE 9464
ENTRYPOINT ["/exporter"]
