FROM golang:1.26-alpine AS build
# Requirement: REQ-DIRECTORY-EXPANSION-007. Feature: platform.foundation.
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/stewardmesh ./cmd/stewardmesh
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/stewardmesh-grpc ./cmd/stewardmesh-grpc
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/grouper-fixture ./cmd/grouper-fixture
RUN CGO_ENABLED=0 go build -tags campusdemo -trimpath -ldflags="-s -w" -o /out/campus-seed ./cmd/campus-seed

FROM node:24-bookworm-slim AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npx tsc -b && npx vite build

FROM gcr.io/distroless/static-debian12:nonroot AS grouper-fixture
COPY --from=build /out/grouper-fixture /grouper-fixture
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/grouper-fixture"]

# All-domain gRPC runtime. Requirements: REQ-API-001, SEC-MCP-001.
# Feature: integrations.protocols.
FROM gcr.io/distroless/static-debian12:nonroot AS stewardmesh-grpc
COPY --from=build /out/stewardmesh-grpc /stewardmesh-grpc
USER nonroot:nonroot
EXPOSE 9090 9091
ENTRYPOINT ["/stewardmesh-grpc"]

# Optional Riverside Community College demo package. Keep it out of the
# production runtime image so operators can load it only when needed.
FROM gcr.io/distroless/static-debian12:nonroot AS campus-demo
COPY --from=build /out/campus-seed /campus-seed
USER nonroot:nonroot
ENTRYPOINT ["/campus-seed"]

# Production HTTP runtime with the built workspace. Campus demo data is not
# compiled into this image.
FROM gcr.io/distroless/static-debian12:nonroot AS stewardmesh
COPY --from=build /out/stewardmesh /stewardmesh
COPY --from=web /web/dist /web
COPY deploy/config.example.yaml /etc/stewardmesh/config.yaml
COPY deploy/reach-endpoints.example.json /etc/stewardmesh/reach-endpoints.json
USER nonroot:nonroot
ENV STEWARDMESH_CONFIG=/etc/stewardmesh/config.yaml
EXPOSE 8080
ENTRYPOINT ["/stewardmesh"]
