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

"""Unit tests for create-kind-cluster --config / KIND_CONFIG handling."""

from __future__ import annotations

import importlib.util
import os
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from unittest import mock

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, _SCRIPT_DIR)

_SCRIPT_PATH = os.path.join(_SCRIPT_DIR, "create-kind-cluster")
_loader = SourceFileLoader("create_kind_cluster", _SCRIPT_PATH)
_spec = importlib.util.spec_from_loader("create_kind_cluster", _loader)
create_kind_cluster = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(create_kind_cluster)
sys.modules["create_kind_cluster"] = create_kind_cluster


class CreateKindClusterConfigTest(unittest.TestCase):
    def _run_main(self, argv, kind_run_side_effect=None):
        kind_calls = []

        def fake_kind_run(cmd, container_engine, **kwargs):
            kind_calls.append((list(cmd), container_engine, kwargs))
            result = mock.Mock()
            result.returncode = 0
            result.stdout = ""
            if cmd[:3] == ["kind", "get", "clusters"]:
                result.stdout = ""
            if kind_run_side_effect is not None:
                return kind_run_side_effect(cmd, container_engine, **kwargs)
            return result

        with mock.patch.object(sys, "argv", argv), \
             mock.patch.object(create_kind_cluster, "_kind_run", side_effect=fake_kind_run), \
             mock.patch.dict(os.environ, {"CONTAINER_ENGINE": "docker"}, clear=False):
            create_kind_cluster.main()
        return kind_calls

    def test_external_config_passed_to_kind_create(self):
        with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
            f.write("kind: Cluster\n")
            config_path = f.name
        try:
            calls = self._run_main([
                "create-kind-cluster", "demo", "--recreate",
                "--config", config_path,
            ])
        finally:
            os.unlink(config_path)

        create_calls = [c for c in calls if c[0][:3] == ["kind", "create", "cluster"]]
        self.assertEqual(len(create_calls), 1)
        cmd = create_calls[0][0]
        self.assertIn("--config", cmd)
        self.assertEqual(cmd[cmd.index("--config") + 1], os.path.abspath(config_path))

    def test_missing_config_exits(self):
        with mock.patch.object(sys, "argv", [
            "create-kind-cluster", "demo", "--config", "/no/such/kind-config.yaml",
        ]), \
             mock.patch.object(create_kind_cluster, "_kind_run") as kind_run, \
             self.assertRaises(SystemExit) as cm:
            create_kind_cluster.main()
        self.assertIn("kind config not found", str(cm.exception))
        kind_run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
