FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /out/stewardmesh ./cmd/stewardmesh

FROM build AS grouper-fixture-build
RUN CGO_ENABLED=0 go build -o /out/grouper-fixture ./cmd/grouper-fixture

FROM build AS grpc-build
RUN CGO_ENABLED=0 go build -o /out/stewardmesh-grpc ./cmd/stewardmesh-grpc

FROM gcr.io/distroless/static-debian12:nonroot AS grouper-fixture
COPY --from=grouper-fixture-build /out/grouper-fixture /grouper-fixture
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/grouper-fixture"]

# All-domain gRPC runtime. Requirements: REQ-API-001, SEC-MCP-001.
# Feature: integrations.protocols.
FROM gcr.io/distroless/static-debian12:nonroot AS stewardmesh-grpc
COPY --from=grpc-build /out/stewardmesh-grpc /stewardmesh-grpc
USER nonroot:nonroot
EXPOSE 9090 9091
ENTRYPOINT ["/stewardmesh-grpc"]

# Keep the application image as the final stage so the existing default
# `docker build` path continues to produce StewardMesh.
FROM gcr.io/distroless/static-debian12:nonroot AS stewardmesh
COPY --from=build /out/stewardmesh /stewardmesh
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/stewardmesh"]
