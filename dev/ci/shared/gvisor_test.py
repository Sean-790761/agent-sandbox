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

"""Unit tests for dev/ci/shared/gvisor.py."""

from __future__ import annotations

import os
import sys
import tempfile
import unittest
from unittest import mock

_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
if _REPO_ROOT not in sys.path:
    sys.path.insert(0, _REPO_ROOT)

from dev.ci.shared import gvisor  # noqa: E402


class GvisorArchTest(unittest.TestCase):
    def test_maps_common_machine_names(self):
        self.assertEqual(gvisor.gvisor_arch("x86_64"), "x86_64")
        self.assertEqual(gvisor.gvisor_arch("amd64"), "x86_64")
        self.assertEqual(gvisor.gvisor_arch("aarch64"), "aarch64")
        self.assertEqual(gvisor.gvisor_arch("arm64"), "aarch64")

    def test_rejects_unknown_arch(self):
        with self.assertRaises(ValueError):
            gvisor.gvisor_arch("ppc64le")


class WriteKindConfigTest(unittest.TestCase):
    def test_embeds_bin_dir_mounts(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "kind.yaml")
            path = gvisor.write_kind_config("/tmp/gvisor-bins", dest)
            self.assertEqual(path, dest)
            with open(dest, encoding="utf-8") as f:
                content = f.read()
            self.assertIn("runtime_type = \"io.containerd.runsc.v1\"", content)
            self.assertIn("hostPath: \"/tmp/gvisor-bins/runsc\"", content)
            self.assertIn("containerPath: /usr/local/bin/runsc", content)
            self.assertIn("hostPath: \"/tmp/gvisor-bins/containerd-shim-runsc-v1\"", content)

    def test_quotes_bin_dir_with_spaces(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "kind.yaml")
            gvisor.write_kind_config("/opt/gvisor bins", dest)
            with open(dest, encoding="utf-8") as f:
                content = f.read()
            self.assertIn('hostPath: "/opt/gvisor bins/runsc"', content)
            self.assertIn('hostPath: "/opt/gvisor bins/containerd-shim-runsc-v1"', content)


class ResolveKindConfigTest(unittest.TestCase):
    def test_generates_config_for_usr_local_bin(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "usr-local.yaml")
            path = gvisor.resolve_kind_config(_REPO_ROOT, "/usr/local/bin", dest_path=dest)
            self.assertEqual(path, dest)
            with open(dest, encoding="utf-8") as f:
                content = f.read()
            self.assertIn("hostPath: \"/usr/local/bin/runsc\"", content)
            self.assertIn("containerPath: /usr/local/bin/containerd-shim-runsc-v1", content)
            self.assertNotIn("extraPortMappings", content)

    def test_generates_config_for_custom_bin_dir(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "generated.yaml")
            path = gvisor.resolve_kind_config(_REPO_ROOT, tmp, dest_path=dest)
            self.assertEqual(path, dest)
            with open(dest, encoding="utf-8") as f:
                self.assertIn(f"hostPath: \"{tmp}/runsc\"", f.read())

    def test_default_dest_under_repo_bin(self):
        with mock.patch.object(gvisor, "write_kind_config", return_value="stub") as write:
            gvisor.resolve_kind_config(_REPO_ROOT, "/opt/gvisor")
        write.assert_called_once_with(
            "/opt/gvisor", os.path.join(_REPO_ROOT, "bin", "kind-config-gvisor.yaml")
        )


class GvisorReleaseTagTest(unittest.TestCase):
    def test_defaults_to_pinned_release(self):
        with mock.patch.dict(os.environ, {}, clear=False):
            os.environ.pop("GVISOR_RELEASE", None)
            self.assertEqual(gvisor.gvisor_release_tag(), gvisor.DEFAULT_GVISOR_RELEASE)

    def test_env_override(self):
        with mock.patch.dict(os.environ, {"GVISOR_RELEASE": "release-20260101.0"}):
            self.assertEqual(gvisor.gvisor_release_tag(), "release-20260101.0")


class EnsureGvisorBinariesTest(unittest.TestCase):
    def test_skips_download_when_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            for name in ("runsc", "containerd-shim-runsc-v1"):
                path = os.path.join(tmp, name)
                with open(path, "wb") as f:
                    f.write(b"#!/bin/sh\n")
                os.chmod(path, 0o755)
            with mock.patch.object(gvisor.urllib.request, "urlopen") as urlopen:
                got = gvisor.ensure_gvisor_binaries(tmp)
            self.assertEqual(got, tmp)
            urlopen.assert_not_called()

    def test_downloads_when_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            def fake_download(arch, tag, dest_path):
                import tarfile

                self.assertEqual(arch, "aarch64")
                self.assertEqual(tag, gvisor.DEFAULT_GVISOR_RELEASE)
                with tarfile.open(dest_path, "w:bz2") as tf:
                    for name in ("runsc", "containerd-shim-runsc-v1"):
                        path = os.path.join(tmp, f"src-{name}")
                        with open(path, "wb") as f:
                            f.write(b"#!/bin/sh\n")
                        tf.add(path, arcname=name)

            with mock.patch.object(gvisor, "gvisor_arch", return_value="aarch64"), \
                 mock.patch.object(gvisor, "_download_release_tarball", side_effect=fake_download) as download:
                got = gvisor.ensure_gvisor_binaries(tmp)
            self.assertEqual(got, tmp)
            download.assert_called_once()
            self.assertTrue(os.access(os.path.join(tmp, "runsc"), os.X_OK))
            self.assertTrue(os.access(os.path.join(tmp, "containerd-shim-runsc-v1"), os.X_OK))

    def test_download_retries_transient_failures(self):
        class FakeResponse:
            def __init__(self, data):
                self._data = data

            def read(self, n=-1):
                data, self._data = self._data, b""
                return data

            def __enter__(self):
                return self

            def __exit__(self, *exc):
                return False

        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "gvisor-x86_64.tar.bz2")
            with mock.patch.object(
                gvisor.urllib.request,
                "urlopen",
                side_effect=[OSError("transient"), OSError("transient"), FakeResponse(b"payload")],
            ) as urlopen, mock.patch.object(gvisor.time, "sleep") as sleep:
                gvisor._download_release_tarball("x86_64", "release-1", dest)
            self.assertEqual(urlopen.call_count, 3)
            self.assertEqual(sleep.call_count, 2)
            with open(dest, "rb") as f:
                self.assertEqual(f.read(), b"payload")

    def test_download_fails_after_exhausting_retries(self):
        with tempfile.TemporaryDirectory() as tmp:
            dest = os.path.join(tmp, "gvisor-x86_64.tar.bz2")
            with mock.patch.object(
                gvisor.urllib.request, "urlopen", side_effect=OSError("down")
            ) as urlopen, mock.patch.object(gvisor.time, "sleep"):
                with self.assertRaises(RuntimeError):
                    gvisor._download_release_tarball("x86_64", "release-1", dest)
            self.assertEqual(urlopen.call_count, gvisor._DOWNLOAD_ATTEMPTS)


class ApplyRuntimeClassTest(unittest.TestCase):
    def test_invokes_kubectl_apply(self):
        with mock.patch.object(gvisor.subprocess, "run") as run:
            run.return_value = mock.Mock(returncode=0, stdout="runtimeclass.node.k8s.io/gvisor created", stderr="")
            gvisor.apply_gvisor_runtime_class("/tmp/kubeconfig", kubectl="kubectl")
        run.assert_called_once()
        args, kwargs = run.call_args
        self.assertEqual(args[0][:3], ["kubectl", "apply", "-f"])
        self.assertEqual(kwargs["input"], gvisor.RUNTIME_CLASS_MANIFEST)
        self.assertEqual(kwargs["env"]["KUBECONFIG"], "/tmp/kubeconfig")

    def test_raises_on_kubectl_failure(self):
        with mock.patch.object(gvisor.subprocess, "run") as run:
            run.return_value = mock.Mock(returncode=1, stdout="", stderr="boom")
            with self.assertRaises(RuntimeError):
                gvisor.apply_gvisor_runtime_class("/tmp/kubeconfig", kubectl="kubectl")


if __name__ == "__main__":
    unittest.main()
