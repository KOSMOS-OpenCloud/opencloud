"""Tests des Skripts state_snapshot.py selbst, ohne Instanz: python3 -m unittest -v"""

import unittest

import state_snapshot

SAMPLE_CONFIG = """
# erzeugt von opencloud init
admin_user_id: aeea3a35-34d5-4f77-beeb-20028cd0f8f6
machine_auth_api_key: topsecret-machine
token_manager:
  jwt_secret: topsecret-jwt
gateway:
  storage_registry:
    storage_users_mount_id: b894e89e-a7fd-4385-92fc-b5e0bb2659fd
settings:
  service_account_ids:
  - 5c556c9c-b1c1-4e97-8980-07b300162d8d
  - second-account
idp:
  clients:
    - id: web
      name: "OpenCloud Web"
      redirect_uris:
        - https://cloud.example/
        - https://cloud.example/oidc-callback.html
      application_type: web
    - id: desktop
      secret: client-secret   # Kommentar
storage_users:
  mount_id: b894e89e-a7fd-4385-92fc-b5e0bb2659fd
"""


class FlattenYamlTest(unittest.TestCase):
    def setUp(self):
        self.flat = state_snapshot.flatten_yaml(SAMPLE_CONFIG)

    def test_scalars_and_nesting(self):
        self.assertEqual(self.flat["admin_user_id"], "aeea3a35-34d5-4f77-beeb-20028cd0f8f6")
        self.assertEqual(self.flat["gateway.storage_registry.storage_users_mount_id"],
                         "b894e89e-a7fd-4385-92fc-b5e0bb2659fd")
        self.assertEqual(self.flat["storage_users.mount_id"], "b894e89e-a7fd-4385-92fc-b5e0bb2659fd")

    def test_list_on_the_indentation_of_its_key(self):
        self.assertEqual(self.flat["settings.service_account_ids[0]"], "5c556c9c-b1c1-4e97-8980-07b300162d8d")
        self.assertEqual(self.flat["settings.service_account_ids[1]"], "second-account")

    def test_list_of_mappings_with_nested_list(self):
        self.assertEqual(self.flat["idp.clients[0].id"], "web")
        self.assertEqual(self.flat["idp.clients[0].name"], "OpenCloud Web")
        self.assertEqual(self.flat["idp.clients[0].redirect_uris[1]"], "https://cloud.example/oidc-callback.html")
        self.assertEqual(self.flat["idp.clients[0].application_type"], "web")
        self.assertEqual(self.flat["idp.clients[1].id"], "desktop")
        self.assertEqual(self.flat["idp.clients[1].secret"], "client-secret")

    def test_key_after_a_list_is_not_swallowed(self):
        self.assertIn("idp.clients[0].id", self.flat)
        self.assertNotIn("settings.idp.clients[0].id", self.flat)


class MaskSecretsTest(unittest.TestCase):
    def test_secrets_are_fingerprints_ids_stay_readable(self):
        masked = state_snapshot.mask_secrets(state_snapshot.flatten_yaml(SAMPLE_CONFIG))
        self.assertEqual(masked["admin_user_id"], "aeea3a35-34d5-4f77-beeb-20028cd0f8f6")
        self.assertEqual(masked["storage_users.mount_id"], "b894e89e-a7fd-4385-92fc-b5e0bb2659fd")
        for secret_path in ("machine_auth_api_key", "token_manager.jwt_secret", "idp.clients[1].secret"):
            self.assertTrue(masked[secret_path].startswith("sha256:"), secret_path)
        self.assertNotIn("topsecret", " ".join(masked.values()))
        self.assertNotIn("client-secret", " ".join(masked.values()))

    def test_fingerprint_changes_with_the_value(self):
        self.assertNotEqual(state_snapshot.fingerprint("one"), state_snapshot.fingerprint("two"))
        self.assertEqual(state_snapshot.fingerprint("one"), state_snapshot.fingerprint("one"))


class CompareTest(unittest.TestCase):
    def test_removed_and_changed_are_deviations_added_is_a_note(self):
        before = {"config/admin_user_id": "old", "drive/a/name": "Space A", "drive/b/name": "Space B"}
        after = {"config/admin_user_id": "new", "drive/a/name": "Space A", "drive/c/name": "Space C"}
        removed, changed, added = state_snapshot.compare_states(before, after)
        self.assertEqual(removed, {"drive/b/name": "Space B"})
        self.assertEqual(changed, {"config/admin_user_id": ("old", "new")})
        self.assertEqual(added, {"drive/c/name": "Space C"})

    def test_only_sources_read_in_both_states_are_compared(self):
        before = {"coverage/config": "yes", "config/admin_user_id": "x", "oidc/issuer": "https://a", "drive/a/name": "A"}
        after = {"coverage/config": "yes", "config/admin_user_id": "x"}
        host_part, api_part = state_snapshot.comparable_sources(before, after)
        self.assertEqual((host_part, api_part), (True, False))
        removed, changed, added = state_snapshot.compare_states(
            state_snapshot.restrict(before, host_part, api_part), state_snapshot.restrict(after, host_part, api_part))
        self.assertEqual((removed, changed, added), ({}, {}, {}))


class AuthTest(unittest.TestCase):
    def test_bearer_wins_then_snapshot_admin_then_e2e_admin(self):
        dist_values = {"SNAPSHOT_ADMIN__CLOUD_XWORK_CLOUD": "admin:token-a", "E2E_ADMIN__CLOUD_XWORK_CLOUD": "e2e:token-b"}
        header, source = state_snapshot.auth_header("cloud.xwork.cloud", {"OC_BEARER": "abc"}, dist_values)
        self.assertEqual((header, source), ("Bearer abc", "OC_BEARER"))
        header, source = state_snapshot.auth_header("cloud.xwork.cloud", {}, dist_values)
        self.assertEqual(source, "SNAPSHOT_ADMIN__CLOUD_XWORK_CLOUD")
        self.assertTrue(header.startswith("Basic "))
        header, source = state_snapshot.auth_header("cloud.xwork.cloud", {}, {"E2E_ADMIN__CLOUD_XWORK_CLOUD": "e2e:token-b"})
        self.assertEqual(source, "E2E_ADMIN__CLOUD_XWORK_CLOUD")

    def test_no_access_is_reported_not_raised(self):
        header, reason = state_snapshot.auth_header("cloud.xwork.cloud", {}, {})
        self.assertIsNone(header)
        self.assertIn("SNAPSHOT_ADMIN__CLOUD_XWORK_CLOUD", reason)


if __name__ == "__main__":
    unittest.main()
