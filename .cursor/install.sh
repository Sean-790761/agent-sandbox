#!/usr/bin/env bash
# Idempotent Cloud Agent bootstrap for agent-sandbox.
#
# Runs after the repository is checked out. Installs the container tooling the
# repo's `make deploy-kind` / `make test-e2e` flows need (Docker, kind, kubectl)
# on top of the base image's Go + Python toolchains, then primes the Go module
# cache and build cache. Kept idempotent so it is safe to re-run against a
# cached or partially-prepared environment. Per-boot daemons (dockerd) live in
# `start`, not here.
set -euo pipefail

KIND_VERSION="v0.30.0"

log() { printf '\n=== %s ===\n' "$*"; }

# --- Docker Engine (provides dockerd + buildx, needed by kind and image builds) ---
if ! command -v docker >/dev/null 2>&1; then
  log "Installing Docker Engine"
  export DEBIAN_FRONTEND=noninteractive
  sudo install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
    | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  sudo chmod a+r /etc/apt/keyrings/docker.gpg
  . /etc/os-release
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" \
    | sudo tee /etc/apt/sources.list.d/docker.list >/dev/null
  sudo apt-get update -y
  sudo apt-get install -y docker-ce docker-ce-cli containerd.io \
    docker-buildx-plugin docker-compose-plugin
else
  log "Docker already installed ($(docker --version))"
fi

# Let the unprivileged agent user talk to the daemon socket without sudo.
sudo groupadd -f docker
sudo usermod -aG docker "$(whoami)" || true

# --- kind ---
if ! command -v kind >/dev/null 2>&1; then
  log "Installing kind ${KIND_VERSION}"
  curl -fsSL -o /tmp/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-$(dpkg --print-architecture)"
  sudo install -m 0755 /tmp/kind /usr/local/bin/kind
  rm -f /tmp/kind
else
  log "kind already installed ($(kind version))"
fi

# --- kubectl (track the current stable release) ---
if ! command -v kubectl >/dev/null 2>&1; then
  log "Installing kubectl"
  KVER="$(curl -fsSL https://dl.k8s.io/release/stable.txt)"
  curl -fsSL -o /tmp/kubectl "https://dl.k8s.io/release/${KVER}/bin/linux/$(dpkg --print-architecture)/kubectl"
  sudo install -m 0755 /tmp/kubectl /usr/local/bin/kubectl
  rm -f /tmp/kubectl
else
  log "kubectl already installed ($(kubectl version --client -o yaml | head -1))"
fi

# --- Prime Go module + build caches so the first build/test is fast ---
log "Downloading Go modules"
go mod download

log "Building controller binaries (warms the Go build cache)"
make build

log "Bootstrap complete"
