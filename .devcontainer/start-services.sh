#!/usr/bin/env bash
set -Eeuo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repository_root}"

run_dir="${repository_root}/.devcontainer/run"
mkdir -p "${run_dir}" data storage

start_process() {
  local name=$1
  shift
  local pid_file="${run_dir}/${name}.pid"
  local log_file="${run_dir}/${name}.log"

  if [[ -f "${pid_file}" ]]; then
    local existing_pid
    existing_pid="$(cat "${pid_file}")"
    if kill -0 "${existing_pid}" 2>/dev/null; then
      kill "${existing_pid}" 2>/dev/null || true
      wait "${existing_pid}" 2>/dev/null || true
    fi
    rm -f "${pid_file}"
  fi

  nohup "$@" >"${log_file}" 2>&1 &
  echo $! >"${pid_file}"
}

ensure_postgres_cluster() {
  if ! sudo pg_lsclusters 2>/dev/null | awk '$1 == "18" { found=1 } END { exit !found }'; then
    sudo pg_createcluster 18 main
  fi
  if ! sudo pg_lsclusters 2>/dev/null | awk '$1 == "18" && $2 == "main" && $4 == "online" { found=1 } END { exit !found }'; then
    sudo pg_ctlcluster 18 main start
  fi
}

ensure_postgres_cluster

if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='stewardmesh'" | grep -q 1; then
  sudo -u postgres psql -c "CREATE USER stewardmesh WITH PASSWORD 'stewardmesh' CREATEDB;"
fi
if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname='stewardmesh'" | grep -q 1; then
  sudo -u postgres psql -c "CREATE DATABASE stewardmesh OWNER stewardmesh;"
fi

set -a
# shellcheck disable=SC1091
source "${repository_root}/.devcontainer/devcontainer.env"
if [[ -f "${repository_root}/.env" ]]; then
  # shellcheck disable=SC1091
  source "${repository_root}/.env"
fi
set +a

for attempt in $(seq 1 30); do
  if pg_isready -h 127.0.0.1 -U stewardmesh -d stewardmesh >/dev/null 2>&1; then
    break
  fi
  if [[ "${attempt}" -eq 30 ]]; then
    echo "PostgreSQL did not become ready; inspect ${run_dir}" >&2
    exit 1
  fi
  sleep 1
done

python3 - <<'PY'
import glob
import os
import signal
import time

def listen_inodes(port: int) -> set[str]:
    hex_port = f"{port:04X}"
    inodes: set[str] = set()
    for path in ("/proc/net/tcp", "/proc/net/tcp6"):
        try:
            with open(path, encoding="utf-8") as handle:
                next(handle, None)
                for line in handle:
                    parts = line.split()
                    if len(parts) < 10:
                        continue
                    local = parts[1]
                    if local.rsplit(":", 1)[-1].upper() == hex_port and parts[3] == "0A":
                        inodes.add(parts[9])
        except FileNotFoundError:
            continue
    return inodes

def pids_for_inodes(inodes: set[str]) -> set[int]:
    pids: set[int] = set()
    if not inodes:
        return pids
    for fd in glob.glob("/proc/[0-9]*/fd/[0-9]*"):
        try:
            target = os.readlink(fd)
        except OSError:
            continue
        if target.startswith("socket:[") and target[8:-1] in inodes:
            try:
                pids.add(int(fd.split("/")[2]))
            except ValueError:
                continue
    return pids

inodes = listen_inodes(8080)
for pid in pids_for_inodes(inodes):
    try:
        os.kill(pid, signal.SIGTERM)
    except OSError:
        continue
for _ in range(20):
    if not pids_for_inodes(listen_inodes(8080)):
        break
    time.sleep(0.25)
else:
    for pid in pids_for_inodes(listen_inodes(8080)):
        try:
            os.kill(pid, signal.SIGKILL)
        except OSError:
            continue
PY
pkill -f '/exe/stewardmesh' 2>/dev/null || true
pkill -f 'go-build/.*/stewardmesh' 2>/dev/null || true
pkill -f 'go run ./cmd/stewardmesh' 2>/dev/null || true

start_process stewardmesh go run ./cmd/stewardmesh
start_process vite bash -lc "cd '${repository_root}/web' && npm run dev -- --host 0.0.0.0 --port 5173"

for attempt in $(seq 1 60); do
  if curl --fail --silent --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1 \
    && curl --fail --silent --max-time 2 http://127.0.0.1:5173 >/dev/null 2>&1; then
    echo "StewardMesh dev services are ready:"
    echo "  Web:  http://localhost:5173"
    echo "  API:  http://127.0.0.1:8080"
    echo "  Logs: ${run_dir}"
    exit 0
  fi
  if [[ "${attempt}" -eq 60 ]]; then
    echo "Timed out waiting for StewardMesh services; inspect ${run_dir}" >&2
    exit 1
  fi
  sleep 1
done
