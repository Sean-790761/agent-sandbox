#!/usr/bin/env bash
# Per-boot runtime init for agent-sandbox Cloud Agents.
#
# The Docker daemon is not managed by systemd in this environment, so start it
# by hand and wait until its socket answers. Idempotent: if a daemon is already
# up (e.g. a warm boot) it exits cleanly without launching a second one. The
# kind cluster itself is created on demand by `make deploy-kind`, not here, so
# a slow or offline image pull never blocks a successful start.
set -euo pipefail

log() { printf '\n=== %s ===\n' "$*"; }

if docker info >/dev/null 2>&1; then
  log "Docker daemon already running"
  exit 0
fi

log "Starting Docker daemon"
sudo rm -f /var/run/docker.pid /var/run/docker.sock 2>/dev/null || true
sudo nohup dockerd >/tmp/dockerd.log 2>&1 &

# Wait (up to ~30s) for the daemon to accept connections.
for _ in $(seq 1 30); do
  if sudo docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! sudo docker info >/dev/null 2>&1; then
  echo "ERROR: Docker daemon did not become ready; see /tmp/dockerd.log" >&2
  tail -n 20 /tmp/dockerd.log >&2 || true
  exit 1
fi

# Allow the agent user to use docker without sudo for the rest of the session.
sudo chmod 666 /var/run/docker.sock || true

log "Docker daemon ready ($(docker --version))"
