# Copyright 2026 Adrien Ndikumana and NOCFoundry Contributors
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

FROM --platform=$BUILDPLATFORM golang:1.25.8-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG BUILD_TYPE=container
ARG COMMIT_SHA=""

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -trimpath \
      -ldflags="-s -w -X github.com/adrien19/noc-foundry/cmd.buildType=${BUILD_TYPE} -X github.com/adrien19/noc-foundry/cmd.commitSha=${COMMIT_SHA}" \
      -o /out/nocfoundry \
      .

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app
COPY --from=build /out/nocfoundry /usr/local/bin/nocfoundry

EXPOSE 5000

LABEL io.modelcontextprotocol.server.name="io.github.noc-foundry/noc-foundry"

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/nocfoundry"]
