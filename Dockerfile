# Build the manager binary
FROM golang:1.27 AS builder

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

WORKDIR /workspace
COPY . .

# Build the promotions manager.
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	make action-build CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH}

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/cloudoperators/greenhouse-promotions"
WORKDIR /
COPY --from=builder /workspace/bin/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]