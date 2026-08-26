FROM golang:1.27-alpine AS builder

ARG VERSION
ARG COMMIT

ENV VERSION=$VERSION
ENV COMMIT=$COMMIT

RUN apk add make

WORKDIR /workspace
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    make build

FROM gcr.io/distroless/static:nonroot
WORKDIR /bin
COPY --from=builder /workspace/build/cosmoseed .
USER nonroot:nonroot
ENTRYPOINT ["cosmoseed"]