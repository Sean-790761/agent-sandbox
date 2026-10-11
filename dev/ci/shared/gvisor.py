#!/usr/bin/env python3
# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Provision gVisor (runsc) for KIND-based CI (Phase 1 of docs/kata-ci-proposal.md)."""

from __future__ import annotations

import json
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import time
import urllib.request

_GVISOR_GITHUB_RELEASE_BASE = "https://github.com/google/gvisor/releases/download"

DEFAULT_GVISOR_RELEASE = "release-20261005.0"

_DOWNLOAD_ATTEMPTS = 5
_DOWNLOAD_RETRY_DELAY_SECONDS = 3

RUNTIME_CLASS_MANIFEST = textwrap.dedent(
    """\
    apiVersion: node.k8s.io/v1
    kind: RuntimeClass
    metadata:
      name: gvisor
    handler: runsc
    """
)


def gvisor_arch(machine: str | None = None) -> str:
    """Map uname -m / platform.machine() to a gVisor release arch directory."""
    machine = (machine or platform.machine()).lower()
    if machine in ("x86_64", "amd64"):
        return "x86_64"
    if machine in ("aarch64", "arm64"):
        return "aarch64"
    raise ValueError(f"unsupported architecture for gVisor binaries: {machine}")


def default_bin_dir() -> str:
    """HostPath-mount dir for runsc/shim: /usr/local/bin when usable, else a HOME cache."""
    override = os.environ.get("GVISOR_BIN_DIR")
    if override:
        return override

    preferred = "/usr/local/bin"
    runsc = os.path.join(preferred, "runsc")
    shim = os.path.join(preferred, "containerd-shim-runsc-v1")
    if _binaries_present(runsc, shim):
        return preferred

    if os.access(preferred, os.W_OK) or (not os.path.exists(preferred) and os.access("/usr/local", os.W_OK)):
        return preferred

    cache = os.path.join(
        os.environ.get("HOME", tempfile.gettempdir()),
        ".cache",
        "agent-sandbox-gvisor",
    )
    return cache


def _binaries_present(runsc: str, shim: str) -> bool:
    return (
        os.path.isfile(runsc)
        and os.access(runsc, os.X_OK)
        and os.path.isfile(shim)
        and os.access(shim, os.X_OK)
    )


def gvisor_release_tag() -> str:
    """Return the release tag to install; GVISOR_RELEASE overrides the pinned default."""
    return os.environ.get("GVISOR_RELEASE") or DEFAULT_GVISOR_RELEASE


def _download_release_tarball(arch: str, tag: str, dest_path: str) -> None:
    """Download gvisor-<arch>.tar.bz2 for the given release tag."""
    asset = f"gvisor-{arch}.tar.bz2"
    url = f"{_GVISOR_GITHUB_RELEASE_BASE}/{tag}/{asset}"
    req = urllib.request.Request(url, headers={"User-Agent": "agent-sandbox-ci"})
    print(f"  fetching {url}")
    for attempt in range(1, _DOWNLOAD_ATTEMPTS + 1):
        try:
            with urllib.request.urlopen(req, timeout=120) as resp, open(dest_path, "wb") as out:
                shutil.copyfileobj(resp, out)
            return
        except OSError as err:
            if attempt == _DOWNLOAD_ATTEMPTS:
                raise RuntimeError(f"failed to download {url}: {err}") from err
            time.sleep(_DOWNLOAD_RETRY_DELAY_SECONDS)


def _extract_binaries_from_tarball(tarball: str, bin_dir: str) -> None:
    """Extract runsc and containerd-shim-runsc-v1 from a gVisor release tarball."""
    needed = {"runsc", "containerd-shim-runsc-v1"}
    found: set[str] = set()
    with tarfile.open(tarball, mode="r:bz2") as tf:
        for member in tf.getmembers():
            base = os.path.basename(member.name)
            if base not in needed or not member.isfile():
                continue
            dest = os.path.join(bin_dir, base)
            with tf.extractfile(member) as src, open(dest, "wb") as out:
                if src is None:
                    raise RuntimeError(f"failed to read {member.name} from {tarball}")
                shutil.copyfileobj(src, out)
            os.chmod(dest, 0o755)
            found.add(base)
    missing = needed - found
    if missing:
        raise RuntimeError(f"gVisor tarball missing binaries: {sorted(missing)}")


def ensure_gvisor_binaries(bin_dir: str | None = None) -> str:
    """Download runsc/shim into bin_dir when missing; return the resolved dir."""
    bin_dir = bin_dir or default_bin_dir()
    os.makedirs(bin_dir, exist_ok=True)

    runsc = os.path.join(bin_dir, "runsc")
    shim = os.path.join(bin_dir, "containerd-shim-runsc-v1")
    if _binaries_present(runsc, shim):
        print(f"gVisor binaries already present under {bin_dir}")
        return bin_dir

    arch = gvisor_arch()
    tag = gvisor_release_tag()
    print(f"Downloading gVisor {tag} ({arch}) into {bin_dir}...")
    with tempfile.TemporaryDirectory(prefix="gvisor-dl-") as tmp:
        tarball = os.path.join(tmp, f"gvisor-{arch}.tar.bz2")
        _download_release_tarball(arch, tag, tarball)
        _extract_binaries_from_tarball(tarball, bin_dir)

    if not _binaries_present(runsc, shim):
        raise RuntimeError(f"failed to install gVisor binaries under {bin_dir}")
    print(f"gVisor binaries installed under {bin_dir}")
    return bin_dir


def write_kind_config(bin_dir: str, dest_path: str) -> str:
    """Write a KIND config registering runsc, with hostPath mounts at bin_dir."""
    config = textwrap.dedent(
        f"""\
        kind: Cluster
        apiVersion: kind.x-k8s.io/v1alpha4
        containerdConfigPatches:
        - |-
          [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
            runtime_type = "io.containerd.runsc.v1"
        nodes:
        - role: control-plane
          extraMounts:
          - hostPath: {json.dumps(f"{bin_dir}/runsc")}
            containerPath: /usr/local/bin/runsc
          - hostPath: {json.dumps(f"{bin_dir}/containerd-shim-runsc-v1")}
            containerPath: /usr/local/bin/containerd-shim-runsc-v1
          kubeadmConfigPatches:
          - |
            kind: ClusterConfiguration
            apiServer:
              extraArgs:
                enable-admission-plugins: "NodeRestriction,OwnerReferencesPermissionEnforcement"
        """
    )
    os.makedirs(os.path.dirname(os.path.abspath(dest_path)) or ".", exist_ok=True)
    with open(dest_path, "w", encoding="utf-8") as f:
        f.write(config)
    return dest_path


def resolve_kind_config(repo_root: str, bin_dir: str, dest_path: str | None = None) -> str:
    """Write the gVisor KIND config under dest_path (default: repo bin/)."""
    if dest_path is None:
        dest_path = os.path.join(repo_root, "bin", "kind-config-gvisor.yaml")
    return write_kind_config(bin_dir, dest_path)


def apply_gvisor_runtime_class(kubeconfig: str, kubectl: str | None = None) -> None:
    """Create or update the `gvisor` RuntimeClass (handler: runsc)."""
    kubectl = kubectl or shutil.which("kubectl") or "kubectl"
    env = os.environ.copy()
    env["KUBECONFIG"] = kubeconfig
    result = subprocess.run(
        [kubectl, "apply", "-f", "-"],
        input=RUNTIME_CLASS_MANIFEST,
        text=True,
        env=env,
        check=False,
        capture_output=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"failed to apply gvisor RuntimeClass: {result.stderr.strip() or result.stdout.strip()}"
        )
    print(result.stdout.strip() or "RuntimeClass/gvisor applied")


def prepare_gvisor_kind(repo_root: str) -> tuple[str, str]:
    """Install binaries and return (bin_dir, kind_config_path) for KIND create."""
    bin_dir = ensure_gvisor_binaries()
    kind_config = resolve_kind_config(repo_root, bin_dir)
    print(f"Using KIND config: {kind_config}")
    return bin_dir, kind_config


if __name__ == "__main__":
    target = sys.argv[1] if len(sys.argv) > 1 else None
    print(ensure_gvisor_binaries(target))
