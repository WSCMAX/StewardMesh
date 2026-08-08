FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /out/stewardmesh ./cmd/stewardmesh

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/stewardmesh /stewardmesh
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/stewardmesh"]
