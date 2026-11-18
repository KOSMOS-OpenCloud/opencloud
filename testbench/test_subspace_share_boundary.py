"""Tests der Testbench ohne Instanz. Lauf: cd testbench && python3 -m unittest -v

Das Szenario läuft gegen ein Modell des Soll-Verhaltens (alle Prüfungen müssen bestehen) und gegen
ein Modell mit dem Fehler vom 2026-11-17 (muss erkannt werden).
"""
import os
import tempfile
import unittest
from unittest import mock

import subspace_share_boundary as bench

RUN_FOLDER = "run-test"


class FakeSession:
    def __init__(self, role):
        self.role = role
        self.login = role.lower()
        self.user_id = "id-" + role.lower()


class FakeInstance:
    """Modell des Soll-Verhaltens: Grants je Ordner, Subspace-Liste, Walk vom Blatt zur Wurzel.

    plain_share_registers_subspace=True bildet den Fehler vom 2026-11-17 nach.
    """

    def __init__(self, plain_share_registers_subspace=False):
        self.sessions = {role: FakeSession(role) for role in bench.ROLES}
        self.grants = {}
        self.subspaces = set()
        self.plain_share_registers_subspace = plain_share_registers_subspace

    def relative(self, path):
        return path[len(RUN_FOLDER) + 1:]

    def grant(self, path, session):
        self.grants.setdefault(self.relative(path), set()).add(session.role)
        return 200, f"{self.relative(path)}|{session.role}"

    def share_plain(self, path, recipient, actor=None):
        if actor is not None and actor.role != "ADMIN":
            return 403, ""
        if self.plain_share_registers_subspace:
            self.subspaces.add(self.relative(path))
        return self.grant(path, recipient)

    def add_subspace_member(self, path, member):
        self.subspaces.add(self.relative(path))
        return self.grant(path, member)

    def remove(self, permission_id):
        folder, role = permission_id.split("|")
        self.grants[folder].discard(role)
        if not self.grants[folder]:
            self.subspaces.discard(folder)
        return 204

    def remove_plain_share(self, path, permission_id):
        return self.remove(permission_id)

    def remove_subspace_member(self, path, permission_id):
        return self.remove(permission_id)

    def subspace_paths(self, run_folder):
        return sorted(self.subspaces)

    def enclosing_subspace(self, relative_path):
        folder = relative_path
        while folder:
            if folder in self.subspaces:
                return folder
            folder = folder.rpartition("/")[0]
        return ""

    def space_context(self, path):
        return "subspace" if self.enclosing_subspace(self.relative(path)) else "space"

    def in_shared_with_me(self, session, path):
        folder = self.relative(path)
        return session.role in self.grants.get(folder, set()) and folder not in self.subspaces

    def sees_space(self, session):
        if session.role in ("ADMIN", "MEMBER"):
            return True
        return any(session.role in self.grants.get(folder, set()) for folder in self.subspaces)

    def can_read(self, session, path):
        if session.role == "ADMIN":  # Space-Manager
            return True
        folder = self.relative(path).rpartition("/")[0]
        while folder:
            if session.role in self.grants.get(folder, set()):
                return True
            if folder in self.subspaces:
                return False
            folder = folder.rpartition("/")[0]
        return session.role == "MEMBER"


class HelperTest(unittest.TestCase):
    def test_dist_key_uses_role_and_normalized_host(self):
        self.assertEqual(bench.dist_key("ADMIN", "cloud.xwork.cloud"), "E2E_ADMIN__CLOUD_XWORK_CLOUD")

    def test_split_credentials_keeps_colons_in_the_token(self):
        self.assertEqual(bench.split_credentials("anna:tok:en", "KEY"), ("anna", "tok:en"))

    def test_split_credentials_rejects_values_without_token(self):
        with self.assertRaises(bench.SetupError):
            bench.split_credentials("anna", "KEY")

    def test_is_under_matches_folder_and_children_only(self):
        self.assertTrue(bench.is_under("/run-1/secret", "run-1"))
        self.assertTrue(bench.is_under("run-1", "run-1"))
        self.assertFalse(bench.is_under("/run-10/secret", "run-1"))

    def test_dav_path_is_encoded_per_segment(self):
        self.assertEqual(bench.encode_dav_path("run 1/ä.txt"), "run%201/%C3%A4.txt")


class ScenarioTest(unittest.TestCase):
    def run_scenario(self, instance):
        recorder = bench.Recorder()
        with mock.patch.object(bench.time, "sleep"):
            bench.run_scenario(instance, recorder, RUN_FOLDER)
        return recorder

    def test_correct_behaviour_passes_every_check(self):
        recorder = self.run_scenario(FakeInstance())
        self.assertEqual(recorder.failed(), [])
        self.assertGreater(len(recorder.checks), 60)

    def test_plain_share_that_registers_a_subspace_is_detected(self):
        recorder = self.run_scenario(FakeInstance(plain_share_registers_subspace=True))
        failed_names = [entry["name"] for entry in recorder.failed()]
        self.assertIn("Freigabe erzeugt keinen Subspace", failed_names)
        self.assertIn("MEMBER liest open/a.txt", failed_names)
        self.assertIn("RECIPIENT hat open unter „Mit mir geteilt“", failed_names)
        self.assertIn("RECIPIENT hat den Test-Space in der Space-Liste", failed_names)

    def test_member_share_attempt_is_only_noted(self):
        recorder = self.run_scenario(FakeInstance())
        self.assertEqual(recorder.info[0]["observed"], 403)


class SetupTest(unittest.TestCase):
    def complete_dist(self):
        return {bench.dist_key(role, "cloud.example.org"): role.lower() + ":geheim" for role in bench.ROLES}

    def test_credentials_are_loaded_per_role(self):
        credentials = bench.load_credentials("cloud.example.org", self.complete_dist())
        self.assertEqual(credentials["ADMIN"], ("admin", "geheim"))

    def test_missing_key_is_a_setup_error(self):
        dist_values = self.complete_dist()
        del dist_values[bench.dist_key("OUTSIDER", "cloud.example.org")]
        with self.assertRaises(bench.SetupError):
            bench.load_credentials("cloud.example.org", dist_values)

    def test_dist_file_is_parsed(self):
        with tempfile.TemporaryDirectory() as temporary_dir:
            dist_file = os.path.join(temporary_dir, "DIST")
            with open(dist_file, "w", encoding="utf-8") as dist_handle:
                dist_handle.write("# Kommentar\nE2E_ADMIN__X=anna:tok:en\n\nohne-gleich\n")
            self.assertEqual(bench.read_dist(dist_file), {"E2E_ADMIN__X": "anna:tok:en"})

    def test_plan_shows_logins_but_no_tokens(self):
        printed = []
        with mock.patch.object(bench, "read_dist", return_value=self.complete_dist()), \
                mock.patch.object(bench, "Session") as session_class, \
                mock.patch("builtins.print", side_effect=lambda *parts, **_: printed.append(" ".join(map(str, parts)))):
            exit_code = bench.main(["--plan", "cloud.example.org"])
        session_class.assert_not_called()
        self.assertEqual(exit_code, 0)
        self.assertIn("outsider", "".join(printed))
        self.assertNotIn("geheim", "".join(printed))

    def test_missing_dist_exits_with_3_before_any_request(self):
        with mock.patch.object(bench, "read_dist", return_value={}), \
                mock.patch.object(bench, "Session") as session_class, \
                mock.patch("builtins.print"):
            exit_code = bench.main(["cloud.example.org"])
        session_class.assert_not_called()
        self.assertEqual(exit_code, 3)


if __name__ == "__main__":
    unittest.main()
