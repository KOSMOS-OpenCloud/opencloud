#!/usr/bin/env python3
"""Zustands-Snapshot einer OpenCloud-Instanz vor und nach einem Deploy vergleichen.

Anlass: Ein `opencloud init` (oder ein Deploy-Schritt, der die Runtime-Config neu erzeugt) vergibt neue IDs und
Secrets. Vorfall os.brandis 2026-10-12: neue `storage_users.mount_id`, `admin_user_id`, `system_user_id`,
Service-Accounts und Secrets, alte Clients liefen danach auf 404. Dieses Skript hält den Stand vorher fest und
meldet danach jede verschwundene oder geänderte ID.

    python3 state_snapshot.py save cloud.xwork.cloud        # vor dem Deploy
    python3 state_snapshot.py compare cloud.xwork.cloud     # nach dem Deploy, Exit 1 bei Abweichung
    python3 state_snapshot.py show cloud.xwork.cloud        # gespeicherten Stand ansehen

Das Skript liest nur. Es ändert nichts an der Instanz.

Quellen:
  Host (ssh root@<host>, podman exec in den opencloud-Container):
    - /etc/opencloud/opencloud.yaml: alle Werte, Secrets nur als Fingerabdruck (sha256, 12 Zeichen)
    - Schlüsseldateien unter /var/lib/opencloud/idm und /idp als Fingerabdruck
  API (Graph, optional):
    - Anwendung und globale Rollen, Freigabe-Rollen, OIDC-Schlüssel
    - Spaces (ID mit Storage-ID davor, Name, Typ, Besitzer) und Mitglieder der Project-Spaces mit Rolle
    - Nutzer mit globaler Rolle, Gruppen mit Mitgliedern (nur mit der Rolle Admin lesbar)

Zugang zur API, in dieser Reihenfolge:
  1. Umgebungsvariable OC_BEARER (Access-Token einer Sitzung, 5 Minuten gültig)
  2. testbench/DIST: SNAPSHOT_ADMIN__<HOST>=login:app-token, sonst E2E_ADMIN__<HOST>
  Ohne Zugang: nur die Host-Quellen (--no-api macht das ausdrücklich).

Exit: 0 gleich, 1 Abweichung, 2 Ablauf abgebrochen, 3 kein Snapshot oder Quelle nicht erreichbar.
"""

import argparse
import base64
import hashlib
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

TESTBENCH_DIR = os.path.dirname(os.path.abspath(__file__))
DIST_FILE = os.path.join(TESTBENCH_DIR, "DIST")
SNAPSHOT_DIR = os.path.join(TESTBENCH_DIR, "snapshots")

CONFIG_FILE = "/etc/opencloud/opencloud.yaml"
KEY_FILES = [
    "/var/lib/opencloud/idm/ldap.crt",
    "/var/lib/opencloud/idm/ldap.key",
    "/var/lib/opencloud/idp/private-key.pem",
    "/var/lib/opencloud/idp/encryption.key",
]
# Blattschlüssel der opencloud.yaml, deren Wert nie im Klartext in den Snapshot darf
SECRET_KEY_PATTERN = re.compile(r"secret|password|passwd|token|api_key|_key$|^key$|private", re.IGNORECASE)


class SnapshotError(Exception):
    """Abbruch mit Hinweis, was zu tun ist."""

    def __init__(self, message, hint="", exit_code=2):
        super().__init__(message)
        self.hint = hint
        self.exit_code = exit_code


def fingerprint(value):
    """Kurzer, nicht umkehrbarer Fingerabdruck eines geheimen Werts."""
    return "sha256:" + hashlib.sha256(value.encode()).hexdigest()[:12]


def flatten_yaml(text):
    """Macht aus einfachem YAML (Mappings, Listen, Skalare) ein flaches Dict Pfad -> Wert.

    Kein vollständiger YAML-Parser (die Testbench nutzt nur die Standardbibliothek). Reicht für die von
    `opencloud init` erzeugte opencloud.yaml. Listenelemente bekommen den Index als Pfadteil: `clients[0].id`.
    """
    flat = {}
    stack = []  # Einträge (Einrückung, Pfadteil, ist_Listenelement)
    list_counters = {}

    def current_path():
        path = ""
        for _, part, is_item in stack:
            path += part if is_item else ("." if path else "") + part
        return path

    for raw_line in text.splitlines():
        if not raw_line.strip() or raw_line.lstrip().startswith("#") or raw_line.strip() == "---":
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        content = raw_line.strip()

        if content == "-" or content.startswith("- "):
            # ein Listenelement darf auf der Einrückung seines Schlüssels stehen
            while stack and (stack[-1][0] > indent or (stack[-1][2] and stack[-1][0] >= indent)):
                stack.pop()
            parent_path = current_path()
            item_index = list_counters.get(parent_path, 0)
            list_counters[parent_path] = item_index + 1
            stack.append((indent, f"[{item_index}]", True))
            content = content[1:].strip()
            if not content:
                continue
            if not (content.endswith(":") or ": " in content):
                flat[current_path()] = unquote(content)
                continue
            indent += 2  # "- key: value": der Schlüssel gehört in das Listenelement
        else:
            while stack and stack[-1][0] >= indent:
                stack.pop()

        key, separator, value = content.partition(":")
        if not separator:
            flat[(current_path() + "." if current_path() else "") + content] = ""
            continue
        key = unquote(key.strip())
        value = strip_comment(value.strip())
        if value == "":
            stack.append((indent, key, False))
            list_counters.pop(current_path(), None)
            continue
        parent_path = current_path()
        flat[(parent_path + "." if parent_path else "") + key] = unquote(value)
    return flat


def strip_comment(value):
    if value.startswith(("'", '"')):
        return value
    return value.split(" #", 1)[0].strip()


def unquote(value):
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "'\"":
        return value[1:-1]
    return value


def mask_secrets(flat_config):
    """Ersetzt geheime Werte durch ihren Fingerabdruck. Der Schlüssel ist der letzte Pfadteil ohne Listenindex."""
    masked = {}
    for path, value in flat_config.items():
        leaf_key = re.sub(r"\[\d+\]$", "", path.rsplit(".", 1)[-1])
        if value != "" and SECRET_KEY_PATTERN.search(leaf_key):
            masked[path] = fingerprint(value)
        else:
            masked[path] = value
    return masked


# ---------------------------------------------------------------------------------------------------------------
# Host

def run_ssh(ssh_target, remote_command):
    completed = subprocess.run(
        ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", ssh_target, remote_command],
        capture_output=True, text=True, timeout=120, check=False)
    return completed.returncode, completed.stdout, completed.stderr.strip()


def find_container(ssh_target, container_name=""):
    if container_name:
        return container_name
    return_code, output, error_text = run_ssh(ssh_target, "podman ps --format '{{.Names}}'")
    if return_code != 0:
        raise SnapshotError(f"ssh {ssh_target}: {error_text or 'podman ps fehlgeschlagen'}",
                            "SSH-Zugang prüfen oder --no-host", exit_code=3)
    candidates = [name for name in output.split() if name.endswith("-opencloud")]
    if len(candidates) != 1:
        raise SnapshotError(f"kein eindeutiger opencloud-Container auf {ssh_target}: {candidates or 'keiner'}",
                            "Container mit --container angeben", exit_code=3)
    return candidates[0]


def collect_host(ssh_target, container_name=""):
    """Liest opencloud.yaml und die Schlüsseldateien. Gibt (state, info) zurück."""
    container = find_container(ssh_target, container_name)
    state, info = {}, {"container": container}

    return_code, config_text, error_text = run_ssh(ssh_target, f"podman exec '{container}' cat {CONFIG_FILE}")
    if return_code != 0:
        raise SnapshotError(f"{CONFIG_FILE} in {container} nicht lesbar: {error_text}", exit_code=3)
    for path, value in mask_secrets(flatten_yaml(config_text)).items():
        state[f"config/{path}"] = value
    state["coverage/config"] = "yes"

    return_code, checksum_output, _ = run_ssh(
        ssh_target, f"podman exec '{container}' sha256sum {' '.join(KEY_FILES)} 2>/dev/null")
    found_key_files = set()
    for checksum_line in checksum_output.splitlines():
        checksum, _, file_name = checksum_line.partition("  ")
        if file_name:
            state[f"keyfile/{file_name.strip()}"] = "sha256:" + checksum[:12]
            found_key_files.add(file_name.strip())
    for key_file in KEY_FILES:
        if key_file not in found_key_files:
            state[f"keyfile/{key_file}"] = "missing"

    # nur zur Anzeige, wird nicht verglichen (ändert sich bei jedem Deploy)
    return_code, image_output, _ = run_ssh(
        ssh_target, f"podman inspect '{container}' --format '{{{{.ImageName}}}} {{{{.State.StartedAt}}}}'")
    if return_code == 0:
        info["image_and_start"] = image_output.strip()
    return_code, stat_output, _ = run_ssh(ssh_target, f"podman exec '{container}' stat -c '%y' {CONFIG_FILE}")
    if return_code == 0:
        info["config_mtime"] = stat_output.strip()
    return state, info


# ---------------------------------------------------------------------------------------------------------------
# API

def read_dist(dist_file=DIST_FILE):
    values = {}
    if not os.path.exists(dist_file):
        return values
    with open(dist_file, encoding="utf-8") as dist_handle:
        for dist_line in dist_handle:
            dist_line = dist_line.strip()
            if not dist_line or dist_line.startswith("#") or "=" not in dist_line:
                continue
            key, _, value = dist_line.partition("=")
            values[key.strip()] = value.strip()
    return values


def dist_key(prefix, host):
    return f"{prefix}__{re.sub(r'[^A-Z0-9]', '_', host.upper())}"


def auth_header(host, environment=None, dist_values=None):
    """Gibt (Authorization-Header, Beschreibung) zurück oder (None, Grund)."""
    environment = os.environ if environment is None else environment
    dist_values = read_dist() if dist_values is None else dist_values
    if environment.get("OC_BEARER"):
        return "Bearer " + environment["OC_BEARER"].strip(), "OC_BEARER"
    for prefix in ("SNAPSHOT_ADMIN", "E2E_ADMIN"):
        key = dist_key(prefix, host)
        if dist_values.get(key):
            login, separator, app_token = dist_values[key].partition(":")
            if not separator or not login or not app_token:
                raise SnapshotError(f"DIST-Schlüssel {key} hat nicht die Form login:app-token")
            return "Basic " + base64.b64encode(f"{login}:{app_token}".encode()).decode(), key
    return None, (f"weder OC_BEARER noch {dist_key('SNAPSHOT_ADMIN', host)} / {dist_key('E2E_ADMIN', host)} "
                  f"in {DIST_FILE}")


class Api:
    def __init__(self, host, authorization, insecure=False):
        self.base_url = f"https://{host}"
        self.authorization = authorization
        self.ssl_context = None
        if insecure:
            import ssl
            self.ssl_context = ssl._create_unverified_context()

    def get(self, path, authenticated=True, cache_buster=False):
        """Gibt (Statuscode, JSON oder None) zurück."""
        url = self.base_url + path
        if cache_buster:  # nicht an die Graph-API: OData lehnt unbekannte Parameter mit 400 ab
            url += ("&" if "?" in url else "?") + f"t={int(time.time() * 1000)}"
        headers = {"Authorization": self.authorization} if authenticated and self.authorization else {}
        http_request = urllib.request.Request(url, headers=headers)
        try:
            with urllib.request.urlopen(http_request, timeout=60, context=self.ssl_context) as response:
                return response.status, parse_json(response.read())
        except urllib.error.HTTPError as http_error:
            return http_error.code, parse_json(http_error.read())
        except (urllib.error.URLError, OSError) as network_error:
            raise SnapshotError(f"GET {url}: {network_error}",
                                "Erreichbarkeit der Instanz prüfen, bei Step-CA --insecure", exit_code=3)

    def get_expected(self, path, **options):
        status, payload = self.get(path, **options)
        if status != 200 or payload is None:
            raise SnapshotError(f"GET {path}: HTTP {status}: {json.dumps(payload)[:200]}",
                                "Zugang (Token, Rolle) prüfen", exit_code=3)
        return payload


def parse_json(raw_bytes):
    try:
        return json.loads(raw_bytes.decode("utf-8", "replace"))
    except ValueError:
        return None


def collect_api(api):
    """Liest den über die API sichtbaren Stand. Gibt (state, info) zurück."""
    state, info = {}, {}

    # OIDC: der Schlüssel selbst (Modulus), nicht die kid (die heißt auf jeder Instanz gleich)
    openid_config = api.get_expected("/.well-known/openid-configuration", authenticated=False, cache_buster=True)
    state["oidc/issuer"] = openid_config.get("issuer", "")
    jwks_path = urllib.parse.urlparse(openid_config.get("jwks_uri", "")).path or "/konnect/v1/jwks.json"
    key_ids_by_material = {}
    for signing_key in api.get_expected(jwks_path, authenticated=False, cache_buster=True).get("keys", []):
        key_material = signing_key.get("n") or signing_key.get("x") or json.dumps(signing_key, sort_keys=True)
        key_ids_by_material.setdefault(fingerprint(key_material), []).append(signing_key.get("kid", ""))
    for key_fingerprint, key_ids in key_ids_by_material.items():
        # derselbe Schlüssel steht unter mehreren kid in der Liste, die Reihenfolge wechselt
        state[f"oidc/key/{key_fingerprint}"] = ",".join(sorted(key_ids))

    current_user = api.get_expected("/graph/v1.0/me?$expand=appRoleAssignments")
    info["api_user"] = current_user.get("onPremisesSamAccountName", "")

    for application in api.get_expected("/graph/v1.0/applications").get("value", []):
        state[f"application/{application.get('id')}"] = application.get("displayName", "")
        for app_role in application.get("appRoles", []):
            state[f"approle/{app_role.get('id')}"] = app_role.get("displayName", "")

    role_definitions = api.get_expected("/graph/v1beta1/roleManagement/permissions/roleDefinitions")
    for role_definition in role_definitions if isinstance(role_definitions, list) else role_definitions.get("value", []):
        state[f"sharerole/{role_definition.get('id')}"] = role_definition.get("displayName", "")

    # Spaces: /drives liefert alle nur mit dem Recht, alle Spaces zu listen. Sonst die eigenen.
    status, drives_payload = api.get("/graph/v1beta1/drives")
    if status != 200 or drives_payload is None:
        drives_payload = api.get_expected("/graph/v1beta1/me/drives")
        state["coverage/drives"] = "own"
    else:
        state["coverage/drives"] = "all"
    members_unreadable = 0
    for drive in drives_payload.get("value", []):
        drive_type = drive.get("driveType", "")
        if drive_type not in ("project", "personal"):
            continue  # virtual, mountpoint: hängen an der Sicht des abfragenden Nutzers
        drive_id = drive.get("id", "")
        state[f"drive/{drive_id}/type"] = drive_type
        state[f"drive/{drive_id}/name"] = drive.get("name", "")
        state[f"drive/{drive_id}/alias"] = drive.get("driveAlias", "")
        state[f"drive/{drive_id}/owner"] = drive.get("owner", {}).get("user", {}).get("id", "")
        state[f"drive/{drive_id}/root"] = drive.get("root", {}).get("id", "")
        if drive_type != "project":
            continue
        status, permissions_payload = api.get(
            "/graph/v1beta1/drives/" + urllib.parse.quote(drive_id) + "/root/permissions")
        if status != 200 or permissions_payload is None:
            # ohne Mitgliedschaft nicht lesbar: festhalten, damit ein späterer Lauf mit mehr Rechten auffällt
            state[f"drive/{drive_id}/members"] = "not readable"
            members_unreadable += 1
            continue
        state[f"drive/{drive_id}/members"] = "readable"
        for permission in permissions_payload.get("value", []):
            for principal_type, principal in permission.get("grantedToV2", {}).items():
                if not isinstance(principal, dict) or "id" not in principal:
                    continue
                roles = ",".join(sorted(permission.get("roles", []))) or "actions-only"
                state[f"drive/{drive_id}/member/{principal_type}:{principal['id']}"] = roles
    info["project_spaces_without_readable_members"] = members_unreadable

    # Nutzer und Gruppen: nur mit der Rolle Admin vollständig lesbar
    status, users_payload = api.get("/graph/v1.0/users?$expand=appRoleAssignments")
    if status == 200 and users_payload is not None and "value" in users_payload:
        state["coverage/users"] = "all"
        for user in users_payload["value"]:
            user_id = user.get("id", "")
            state[f"user/{user_id}/login"] = user.get("onPremisesSamAccountName", "")
            state[f"user/{user_id}/role"] = ",".join(sorted(
                assignment.get("appRoleId", "") for assignment in user.get("appRoleAssignments", [])))
    else:
        state["coverage/users"] = "self"
        user_id = current_user.get("id", "")
        state[f"user/{user_id}/login"] = current_user.get("onPremisesSamAccountName", "")
        state[f"user/{user_id}/role"] = ",".join(sorted(
            assignment.get("appRoleId", "") for assignment in current_user.get("appRoleAssignments", [])))

    status, groups_payload = api.get("/graph/v1.0/groups?$expand=members")
    if status == 200 and groups_payload is not None and "value" in groups_payload:
        state["coverage/groups"] = "all"
        for group in groups_payload["value"]:
            group_id = group.get("id", "")
            state[f"group/{group_id}/name"] = group.get("displayName", "")
            state[f"group/{group_id}/members"] = ",".join(sorted(
                member.get("id", "") for member in group.get("members", [])))
    else:
        state["coverage/groups"] = "none"
    return state, info


# ---------------------------------------------------------------------------------------------------------------
# Snapshot und Vergleich

def collect(arguments):
    state, info = {}, {"host": arguments.host, "taken": time.strftime("%Y-%m-%dT%H:%M:%S%z")}
    if not arguments.no_host:
        host_state, host_info = collect_host(arguments.ssh or f"root@{arguments.host}", arguments.container)
        state.update(host_state)
        info.update(host_info)
    if not arguments.no_api:
        authorization, source = auth_header(arguments.host)
        if authorization is None:
            if arguments.require_api:
                raise SnapshotError(f"kein API-Zugang: {source}", exit_code=3)
            info["api"] = f"übersprungen: {source}"
        else:
            api_state, api_info = collect_api(Api(arguments.host, authorization, arguments.insecure))
            state.update(api_state)
            info.update(api_info)
            info["api"] = f"Zugang über {source}"
    if not state:
        raise SnapshotError("keine Quelle gelesen (--no-host und kein API-Zugang)", exit_code=3)
    return {"info": info, "state": state}


def compare_states(before, after):
    """Gibt (fehlend, geändert, neu) zurück. Fehlend und geändert sind Abweichungen, neu ist ein Hinweis."""
    removed = {key: before[key] for key in sorted(before) if key not in after}
    changed = {key: (before[key], after[key]) for key in sorted(before) if key in after and before[key] != after[key]}
    added = {key: after[key] for key in sorted(after) if key not in before}
    return removed, changed, added


def comparable_sources(before, after):
    """Bereiche, die in beiden Ständen gelesen wurden. Ein Vergleich über verschiedene Quellen wäre nur Rauschen."""
    host_in_both = "coverage/config" in before and "coverage/config" in after
    api_in_both = "oidc/issuer" in before and "oidc/issuer" in after
    return host_in_both, api_in_both


def restrict(state, host_part, api_part):
    def is_host_key(key):
        return key.startswith(("config/", "keyfile/", "coverage/config"))
    return {key: value for key, value in state.items() if (host_part and is_host_key(key)) or (api_part and not is_host_key(key))}


def snapshot_path(arguments):
    return arguments.snapshot or os.path.join(SNAPSHOT_DIR, arguments.host + ".json")


def summarize(state):
    prefixes = {}
    for key in state:
        prefix = key.split("/", 1)[0]
        prefixes[prefix] = prefixes.get(prefix, 0) + 1
    return ", ".join(f"{prefix} {count}" for prefix, count in sorted(prefixes.items()))


def command_save(arguments):
    snapshot = collect(arguments)
    target_file = snapshot_path(arguments)
    os.makedirs(os.path.dirname(target_file), exist_ok=True)
    if os.path.exists(target_file) and not arguments.force:
        os.replace(target_file, target_file + ".previous")
        print(f"[save] vorheriger Snapshot liegt unter {target_file}.previous")
    with open(target_file, "w", encoding="utf-8") as snapshot_handle:
        json.dump(snapshot, snapshot_handle, indent=1, sort_keys=True, ensure_ascii=False)
    os.chmod(target_file, 0o600)
    print(f"[save] {target_file}")
    for info_key, info_value in sorted(snapshot["info"].items()):
        print(f"  {info_key}: {info_value}")
    print(f"  Einträge: {summarize(snapshot['state'])}")
    return 0


def load_snapshot(arguments):
    source_file = snapshot_path(arguments)
    if not os.path.exists(source_file):
        raise SnapshotError(f"kein Snapshot {source_file}", "zuerst `save` vor dem Deploy ausführen", exit_code=3)
    with open(source_file, encoding="utf-8") as snapshot_handle:
        return json.load(snapshot_handle)


def command_show(arguments):
    snapshot = load_snapshot(arguments)
    for info_key, info_value in sorted(snapshot["info"].items()):
        print(f"{info_key}: {info_value}")
    for key in sorted(snapshot["state"]):
        print(f"{key} = {snapshot['state'][key]}")
    return 0


def command_compare(arguments):
    before_snapshot = load_snapshot(arguments)
    after_snapshot = collect(arguments)
    host_part, api_part = comparable_sources(before_snapshot["state"], after_snapshot["state"])
    print(f"[compare] {arguments.host}: Snapshot vom {before_snapshot['info'].get('taken', '?')} gegen jetzt")
    for label, part_compared in (("Host (opencloud.yaml, Schlüsseldateien)", host_part), ("API", api_part)):
        print(f"  {label}: {'verglichen' if part_compared else 'NICHT verglichen (in einem der Stände nicht gelesen)'}")
    if not host_part and not api_part:
        raise SnapshotError("kein gemeinsamer Bereich zwischen Snapshot und jetzigem Stand", exit_code=3)
    for info_key in ("image_and_start", "config_mtime"):
        if before_snapshot["info"].get(info_key) != after_snapshot["info"].get(info_key):
            print(f"  {info_key}: {before_snapshot['info'].get(info_key)} -> {after_snapshot['info'].get(info_key)}")

    removed, changed, added = compare_states(
        restrict(before_snapshot["state"], host_part, api_part),
        restrict(after_snapshot["state"], host_part, api_part))
    for key, old_value in removed.items():
        print(f"FEHLT    {key} (war: {old_value})")
    for key, (old_value, new_value) in changed.items():
        print(f"GEÄNDERT {key}: {old_value} -> {new_value}")
    for key, new_value in added.items():
        print(f"neu      {key} = {new_value}")
    print(f"[compare] {len(removed)} fehlen, {len(changed)} geändert, {len(added)} neu (neu ist nur ein Hinweis)")
    if removed or changed:
        print("[compare] ABWEICHUNG")
        return 1
    print("[compare] gleich")
    return 0


def build_parser():
    parser = argparse.ArgumentParser(
        description="Zustand einer OpenCloud-Instanz vor und nach einem Deploy vergleichen (nur lesend).")
    parser.add_argument("command", choices=("save", "compare", "show"))
    parser.add_argument("host", help="Domain der Instanz, z. B. cloud.xwork.cloud")
    parser.add_argument("--snapshot", help=f"Snapshot-Datei (Vorgabe {SNAPSHOT_DIR}/<host>.json)")
    parser.add_argument("--ssh", help="SSH-Ziel des Hosts (Vorgabe root@<host>)")
    parser.add_argument("--container", default="", help="Name des opencloud-Containers (Vorgabe: der eine auf -opencloud)")
    parser.add_argument("--no-host", action="store_true", help="opencloud.yaml und Schlüsseldateien nicht lesen")
    parser.add_argument("--no-api", action="store_true", help="Graph-API nicht lesen")
    parser.add_argument("--require-api", action="store_true", help="ohne API-Zugang abbrechen statt überspringen")
    parser.add_argument("--insecure", action="store_true", help="TLS-Zertifikat nicht prüfen (Step-CA)")
    parser.add_argument("--force", action="store_true", help="save: vorherigen Snapshot nicht als .previous behalten")
    return parser


def main(argv=None):
    arguments = build_parser().parse_args(argv)
    try:
        return {"save": command_save, "compare": command_compare, "show": command_show}[arguments.command](arguments)
    except SnapshotError as snapshot_error:
        print(f"ABBRUCH: {snapshot_error}", file=sys.stderr)
        if snapshot_error.hint:
            print(f"  -> {snapshot_error.hint}", file=sys.stderr)
        return snapshot_error.exit_code


if __name__ == "__main__":
    sys.exit(main())
