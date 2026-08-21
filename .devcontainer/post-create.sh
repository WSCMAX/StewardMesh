#!/usr/bin/env bash
set -Eeuo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repository_root}"

mkdir -p .devcontainer/run data storage

if ! sudo pg_lsclusters 2>/dev/null | awk '$1 == "18" { found=1 } END { exit !found }'; then
  sudo pg_createcluster 18 main
fi
if ! sudo pg_lsclusters 2>/dev/null | awk '$1 == "18" && $2 == "main" && $4 == "online" { found=1 } END { exit !found }'; then
  sudo pg_ctlcluster 18 main start
fi

if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='stewardmesh'" | grep -q 1; then
  sudo -u postgres psql -c "CREATE USER stewardmesh WITH PASSWORD 'stewardmesh' CREATEDB;"
fi
if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='stewardmesh'" | grep -q 1; then
  sudo -u postgres psql -c "CREATE DATABASE stewardmesh OWNER stewardmesh;"
fi

go mod download
(cd web && npm ci)

if [[ ! -f .env ]]; then
  cp .env.example .env
fi

bash .devcontainer/start-services.sh
