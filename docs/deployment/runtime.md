# Runtime container

- **Requirement:** `REQ-DIRECTORY-EXPANSION-007`
- **Feature:** `platform.foundation`

The production image is a distroless static binary plus the built workspace. It
runs migrations on start, serves REST on port 8080, and reads
`/etc/stewardmesh/config.yaml` when `STEWARDMESH_CONFIG` is set. Environment
variables override file values.

```sh
docker compose -f deploy/docker-compose.yml up -d --wait
```

Open `http://127.0.0.1:8080`. The first visit still uses Guard's one-time local
administrator bootstrap. Copy `deploy/config.example.yaml` before changing
organization, database, storage, or origin settings.

## Production shape

- Listen on `0.0.0.0:8080` behind a same-origin TLS reverse proxy.
- Set an HTTPS `listen.allowed_origin`, `session.cookie_secure: true`, and a
  32-byte `session.bootstrap_token`.
- Do not set `listen.insecure_bind`.
- Inject `repository.url` and other secrets from the deployment secret manager
  rather than committing them in the file.
- Leave `seed.campus` and `seed.synthetic` false.

Build the runtime image with `docker build --file deploy/Dockerfile --target
stewardmesh .`. The campus demo generator is not compiled into this image.

## Campus demo package

The Riverside Community College dataset is a separate `campus-demo` image. Load
it only when you want that synthetic dataset:

```sh
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.campus-demo.yml --profile campus-demo up -d --wait
```

The overlay points the HTTP container at the same `demo-campus` organization the
one-shot `campus-seed` job writes. The job uses the campus-demo package, writes
Guard demo credentials under `/tmp`, then exits. The runtime container waits for
that job when the profile is enabled. Operators can instead set
`STEWARDMESH_CONFIG_FILE=./config.campus-demo.yaml` without the overlay. Local
`go run -tags campusdemo ./cmd/campus-seed` is unchanged for host-side reloads
after `./scripts/reset-campus-demo.sh`.
