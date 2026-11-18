#!/usr/bin/env python3
"""Testbench: Abgrenzung normale Freigabe / Subspace und Rechte-Walk, gegen eine laufende Instanz.

Prüft die ganze Kette Graph-API → Gateway → Storageprovider → decomposedfs mit fünf echten Nutzern
(Basic Auth mit Login und App-Token) über Graph-API und WebDAV. Gegenstück ohne Instanz:
opencloud_reva/pkg/storage/pkg/decomposedfs/subspace_share_boundary_test.go.

Aufruf (Details: README.md):
    python3 subspace_share_boundary.py --plan cloud.xwork.cloud     # nur zeigen, was passieren würde
    python3 subspace_share_boundary.py cloud.xwork.cloud            # Test ausführen

Exit: 0 alle Prüfungen bestanden, 1 Prüfung fehlgeschlagen oder Fehler im Ablauf, 3 Zugangsdaten fehlen.
"""
import argparse
import base64
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROLES = ("ADMIN", "MEMBER", "RECIPIENT", "SUBMEMBER", "OUTSIDER")
ROLE_TEXT = {
    "ADMIN": "legt Space, Ordner, Freigaben und Subspaces an (braucht Spaces anlegen und „Manage space properties“)",
    "MEMBER": "Mitglied am Space-Root (Space Editor)",
    "RECIPIENT": "erhält nur normale Freigaben",
    "SUBMEMBER": "nur Subspace-Mitglied",
    "OUTSIDER": "ohne jedes Recht",
}

# unified roles (libre graph), fest in OpenCloud
SPACE_EDITOR_ROLE = "58c63c02-1d89-4572-916a-870abc5a1b7d"
FOLDER_EDITOR_ROLE = "fb6c3e19-e378-47e5-b277-9732f9de6e21"

RUN_LAYOUT = {
    "folders": ["open", "secret", "secret/inner", "mixed", "mixed/vault", "plain"],
    "files": ["open/a.txt", "secret/s.txt", "secret/inner/i.txt", "mixed/m.txt", "mixed/vault/v.txt", "plain/p.txt"],
}

TESTBENCH_DIR = os.path.dirname(os.path.abspath(__file__))
DIST_FILE = os.path.join(TESTBENCH_DIR, "DIST")


class TestbenchError(Exception):
    """Fehler im Ablauf, Exit 1. `hint` nennt den nächsten Schritt."""
    exit_code = 1

    def __init__(self, message, hint=""):
        super().__init__(message)
        self.hint = hint


class SetupError(TestbenchError):
    """Vorbedingung fehlt (Zugangsdaten), Exit 3."""
    exit_code = 3


def read_dist(dist_file=DIST_FILE):
    """KEY=wert je Zeile, # Kommentar. Werte nie ausgeben."""
    values = {}
    if not os.path.exists(dist_file):
        return values
    with open(dist_file, encoding="utf-8") as dist_handle:
        for line in dist_handle:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, value = line.partition("=")
            values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def load_credentials(host, dist_values):
    credentials = {}
    for role in ROLES:
        key = dist_key(role, host)
        if key not in dist_values:
            raise SetupError(f"DIST-Schlüssel {key} fehlt",
                             f"in {DIST_FILE} eintragen: {key}=login:app-token ({ROLE_TEXT[role]})")
        credentials[role] = split_credentials(dist_values[key], key)
    return credentials


def dist_key(role, host):
    """DIST-Schlüssel einer Rolle für einen Host: E2E_ADMIN__CLOUD_XWORK_CLOUD."""
    return f"E2E_{role}__{re.sub(r'[^A-Z0-9]', '_', host.upper())}"


def split_credentials(value, key):
    login, separator, app_token = value.partition(":")
    if not separator or not login or not app_token:
        raise SetupError(f"DIST-Schlüssel {key} hat nicht die Form login:app-token",
                                 "Wert als login:app-token eintragen")
    return login, app_token


def encode_dav_path(path):
    return "/".join(urllib.parse.quote(segment) for segment in path.split("/"))


def is_under(path, folder):
    """True, wenn path der Ordner selbst ist oder darunter liegt (Pfade mit oder ohne führenden /)."""
    normalized_path = "/" + path.strip("/")
    normalized_folder = "/" + folder.strip("/")
    return normalized_path == normalized_folder or normalized_path.startswith(normalized_folder + "/")


class Recorder:
    """Sammelt Prüfungen. Eine Prüfung vergleicht einen erwarteten mit einem gemessenen Wert."""

    def __init__(self):
        self.checks = []
        self.info = []
        self.step = ""

    def check(self, name, expected, actual):
        self.checks.append({"step": self.step, "name": name, "expected": expected, "actual": actual,
                            "ok": expected == actual})

    def note(self, name, observed):
        self.info.append({"step": self.step, "name": name, "observed": observed})

    def failed(self):
        return [entry for entry in self.checks if not entry["ok"]]


class Session:
    """HTTP-Zugriff eines Testnutzers (Basic Auth mit Login und App-Token)."""

    def __init__(self, host, role, login, app_token, insecure=False):
        self.base_url = f"https://{host}"
        self.role = role
        self.login = login
        basic = base64.b64encode(f"{login}:{app_token}".encode()).decode()
        self.headers = {"Authorization": "Basic " + basic}
        self.ssl_context = None
        if insecure:
            import ssl
            self.ssl_context = ssl._create_unverified_context()
        self.user_id = ""

    def request(self, method, path, body=None, headers=None, cache_buster=False):
        """Gibt (Statuscode, Antworttext) zurück. Netzfehler → TestbenchError."""
        url = self.base_url + path
        if cache_buster:  # nicht an die Graph-API: OData lehnt unbekannte Parameter mit 400 ab
            url += ("&" if "?" in url else "?") + f"t={int(time.time() * 1000)}"
        request_headers = dict(self.headers)
        request_headers.update(headers or {})
        data = None
        if body is not None:
            data = body if isinstance(body, bytes) else json.dumps(body).encode()
            request_headers.setdefault("Content-Type", "application/json")
        http_request = urllib.request.Request(url, data=data, method=method, headers=request_headers)
        try:
            with urllib.request.urlopen(http_request, timeout=60, context=self.ssl_context) as response:
                return response.status, response.read().decode("utf-8", "replace")
        except urllib.error.HTTPError as http_error:
            return http_error.code, http_error.read().decode("utf-8", "replace")
        except (urllib.error.URLError, OSError) as network_error:
            raise TestbenchError(f"{method} {url} als {self.role}: {network_error}",
                            "Erreichbarkeit der Instanz prüfen, bei Step-CA --insecure")

    def graph_json(self, method, path, body=None, expected=(200, 201)):
        status, text = self.request(method, path, body)
        if status not in expected:
            raise TestbenchError(f"{method} {path} als {self.role} ({self.login}): HTTP {status}: {text[:300]}",
                            "Rolle und App-Token des Nutzers prüfen")
        return json.loads(text) if text.strip() else {}


class Instance:
    """Die Fachschritte des Tests gegen eine Instanz."""

    def __init__(self, sessions):
        self.sessions = sessions
        self.admin = sessions["ADMIN"]
        self.drive_id = ""
        self.item_ids = {}

    def quoted_drive(self):
        return urllib.parse.quote(self.drive_id, safe="")

    def load_users(self):
        for session in self.sessions.values():
            session.user_id = session.graph_json("GET", "/graph/v1.0/me")["id"]
        user_ids = [session.user_id for session in self.sessions.values()]
        if len(set(user_ids)) != len(user_ids):
            raise SetupError("Die fünf Rollen müssen fünf verschiedene Nutzer sein",
                                     "DIST prüfen: jede Rolle braucht ein eigenes Konto")

    def find_space(self, space_name):
        drives = self.admin.graph_json(
            "GET", "/graph/v1.0/me/drives?$filter=" + urllib.parse.quote("driveType eq 'project'"))
        for drive in drives.get("value", []):
            if drive.get("name") == space_name:
                return drive["id"]
        return ""

    def ensure_space(self, space_name):
        self.drive_id = self.find_space(space_name)
        if not self.drive_id:
            created = self.admin.graph_json("POST", "/graph/v1.0/drives",
                                            {"name": space_name, "driveType": "project",
                                             "description": "Automatischer Test opencloud/testbench/subspace_share_boundary.py"})
            self.drive_id = created["id"]
        return self.drive_id

    def dav_url(self, path):
        return f"/dav/spaces/{self.quoted_drive()}/{encode_dav_path(path)}"

    def make_folder(self, path):
        status, text = self.admin.request("MKCOL", self.dav_url(path))
        if status not in (201, 405):  # 405: gibt es schon
            raise TestbenchError(f"MKCOL {path}: HTTP {status}: {text[:200]}", "Schreibrecht des ADMIN im Test-Space prüfen")

    def put_file(self, path):
        status, text = self.admin.request("PUT", self.dav_url(path), body=f"subspace-e2e {path}\n".encode(),
                                          headers={"Content-Type": "text/plain"})
        if status not in (201, 204):
            raise TestbenchError(f"PUT {path}: HTTP {status}: {text[:200]}", "Schreibrecht des ADMIN im Test-Space prüfen")

    def resolve_item_id(self, path):
        propfind_body = (b'<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns">'
                         b'<d:prop><oc:fileid/></d:prop></d:propfind>')
        status, text = self.admin.request("PROPFIND", self.dav_url(path), body=propfind_body,
                                          headers={"Depth": "0", "Content-Type": "application/xml"}, cache_buster=True)
        found = re.search(r"<oc:fileid>([^<]+)</oc:fileid>", text)
        if status != 207 or not found:
            raise TestbenchError(f"PROPFIND {path}: HTTP {status}, keine fileid", "WebDAV-Zugriff des ADMIN prüfen")
        self.item_ids[path] = found.group(1)
        return found.group(1)

    def item_url(self, path, suffix):
        return (f"/graph/v1beta1/drives/{self.quoted_drive()}/items/"
                f"{urllib.parse.quote(self.item_ids[path], safe='')}{suffix}")

    def invite_body(self, recipient, role_id):
        return {"recipients": [{"objectId": recipient.user_id, "@libre.graph.recipient.type": "user"}],
                "roles": [role_id]}

    def add_space_member(self, member):
        """Mitglied am Space-Root. Schon vorhanden (409) ist in Ordnung."""
        status, text = self.admin.request(
            "POST", f"/graph/v1beta1/drives/{self.quoted_drive()}/root/invite",
            self.invite_body(member, SPACE_EDITOR_ROLE))
        if status not in (200, 201, 409):
            raise TestbenchError(f"Space-Mitglied {member.login} hinzufügen: HTTP {status}: {text[:300]}",
                            "ADMIN muss Manager des Test-Space sein")

    def share_plain(self, path, recipient, actor=None):
        """Normale Freigabe (Freigabe-Panel). Gibt (Statuscode, Permission-ID) zurück."""
        status, text = (actor or self.admin).request(
            "POST", self.item_url(path, "/invite"), self.invite_body(recipient, FOLDER_EDITOR_ROLE))
        return status, self.permission_id(status, text)

    def add_subspace_member(self, path, member):
        """Subspace-Mitglied (Subspace-Panel). Gibt (Statuscode, Permission-ID) zurück."""
        status, text = self.admin.request(
            "POST", self.item_url(path, "/subspace/permissions"), self.invite_body(member, FOLDER_EDITOR_ROLE))
        return status, self.permission_id(status, text)

    @staticmethod
    def permission_id(status, text):
        if status not in (200, 201):
            return ""
        values = json.loads(text).get("value", [])
        return values[0].get("id", "") if values else ""

    def remove_plain_share(self, path, permission_id):
        status, _ = self.admin.request(
            "DELETE", self.item_url(path, "/permissions/" + urllib.parse.quote(permission_id, safe="")))
        return status

    def remove_subspace_member(self, path, permission_id):
        status, _ = self.admin.request(
            "DELETE", self.item_url(path, "/subspace/permissions/" + urllib.parse.quote(permission_id, safe="")))
        return status

    def subspace_paths(self, run_folder):
        """Subspace-Pfade dieses Laufs, relativ zum Lauf-Ordner."""
        listing = self.admin.graph_json("GET", f"/graph/v1beta1/drives/{self.quoted_drive()}/subspaces")
        relative_paths = []
        for entry in listing.get("value", []):
            if is_under(entry.get("path", ""), run_folder):
                relative_paths.append(("/" + entry["path"].strip("/"))[len("/" + run_folder.strip("/")) + 1:])
        return sorted(relative_paths)

    def space_context(self, path):
        return self.admin.graph_json("GET", self.item_url(path, "/space")).get("type", "")

    def can_read(self, session, path):
        """Lesen der Datei über ihre ID. So greifen auch Freigabe-Empfänger zu, die kein Space-Mitglied sind."""
        resource_id = urllib.parse.quote(self.item_ids[path], safe="")
        status, _ = session.request("GET", f"/dav/spaces/{resource_id}", cache_buster=True)
        return status == 200


def access_matrix(instance, recorder, run_folder, expectations):
    """expectations: {datei: {ROLLE: True/False}} relativ zum Lauf-Ordner."""
    for file_path, per_role in expectations.items():
        for role, expected in per_role.items():
            actual = instance.can_read(instance.sessions[role], f"{run_folder}/{file_path}")
            if actual != expected:  # Rechte können kurz nachlaufen: einmal neu messen
                time.sleep(2)
                actual = instance.can_read(instance.sessions[role], f"{run_folder}/{file_path}")
            recorder.check(f"{role} liest {file_path}", expected, actual)


def plan(host, space_name, logins):
    return {
        "host": host,
        "plan": True,
        "users": logins,
        "would": [
            f"Project-Space „{space_name}“ als ADMIN anlegen, falls er fehlt (wird nie gelöscht)",
            "MEMBER als Space Editor am Space-Root eintragen",
            "Ordner run-<zeit>/ mit open, secret, secret/inner, mixed, mixed/vault, plain und je einer Datei anlegen",
            "1 normale Freigabe open → RECIPIENT: kein Subspace, Mitglieder behalten Zugriff",
            "2 Subspace secret mit SUBMEMBER: Walk stoppt, MEMBER verliert Zugriff, Manager behält ihn",
            "3 normale Freigabe secret/inner → RECIPIENT: nur dieser Unterordner, Subspace bleibt",
            "4 Freigabe mixed → RECIPIENT, Subspace mixed/vault: Freigabe reicht nicht in den Subspace",
            "5 MEMBER versucht eine normale Freigabe (nur Beobachtung, rollenabhängig)",
            "6 alle in diesem Lauf angelegten Freigaben und Subspace-Mitglieder wieder entfernen und prüfen, "
            "dass Subspaces verschwinden und die Space-Rechte wieder gelten",
            "Der Lauf-Ordner mit seinen Dateien bleibt im Test-Space stehen",
        ],
    }


def run_scenario(instance, recorder, run_folder):
    def path_of(relative):
        return f"{run_folder}/{relative}"

    admin_all = {"ADMIN": True}

    recorder.step = "0 Ausgangslage"
    recorder.check("keine Subspaces im Lauf-Ordner", [], instance.subspace_paths(run_folder))
    access_matrix(instance, recorder, run_folder, {
        "plain/p.txt": {"ADMIN": True, "MEMBER": True, "RECIPIENT": False, "SUBMEMBER": False, "OUTSIDER": False},
    })

    recorder.step = "1 normale Freigabe open → RECIPIENT"
    status, open_share = instance.share_plain(path_of("open"), instance.sessions["RECIPIENT"])
    recorder.check("Freigabe angelegt (HTTP 200)", 200, status)
    recorder.check("Freigabe erzeugt keinen Subspace", [], instance.subspace_paths(run_folder))
    recorder.check("Kontext von open ist space", "space", instance.space_context(path_of("open")))
    access_matrix(instance, recorder, run_folder, {
        "open/a.txt": {**admin_all, "MEMBER": True, "RECIPIENT": True, "SUBMEMBER": False, "OUTSIDER": False},
        "plain/p.txt": {"RECIPIENT": False},
        "secret/s.txt": {"RECIPIENT": False},
    })

    recorder.step = "2 Subspace secret mit SUBMEMBER"
    status, secret_member = instance.add_subspace_member(path_of("secret"), instance.sessions["SUBMEMBER"])
    recorder.check("Subspace-Mitglied angelegt (HTTP 200)", 200, status)
    recorder.check("secret ist als Subspace registriert", ["secret"], instance.subspace_paths(run_folder))
    recorder.check("Kontext von secret/s.txt ist subspace", "subspace", instance.space_context(path_of("secret/s.txt")))
    recorder.check("Kontext von open bleibt space", "space", instance.space_context(path_of("open")))
    access_matrix(instance, recorder, run_folder, {
        "secret/s.txt": {**admin_all, "MEMBER": False, "RECIPIENT": False, "SUBMEMBER": True, "OUTSIDER": False},
        "secret/inner/i.txt": {**admin_all, "MEMBER": False, "SUBMEMBER": True},
        "plain/p.txt": {"MEMBER": True, "SUBMEMBER": False},
        "open/a.txt": {"MEMBER": True, "RECIPIENT": True, "SUBMEMBER": False},
    })

    recorder.step = "3 normale Freigabe secret/inner → RECIPIENT (im Subspace)"
    status, inner_share = instance.share_plain(path_of("secret/inner"), instance.sessions["RECIPIENT"])
    recorder.check("Freigabe angelegt (HTTP 200)", 200, status)
    recorder.check("weiterhin nur secret als Subspace", ["secret"], instance.subspace_paths(run_folder))
    access_matrix(instance, recorder, run_folder, {
        "secret/inner/i.txt": {"RECIPIENT": True, "MEMBER": False, "SUBMEMBER": True, "OUTSIDER": False},
        "secret/s.txt": {"RECIPIENT": False, "MEMBER": False},
    })

    recorder.step = "4 Freigabe mixed → RECIPIENT, Subspace mixed/vault mit SUBMEMBER"
    status, mixed_share = instance.share_plain(path_of("mixed"), instance.sessions["RECIPIENT"])
    recorder.check("Freigabe angelegt (HTTP 200)", 200, status)
    status, vault_member = instance.add_subspace_member(path_of("mixed/vault"), instance.sessions["SUBMEMBER"])
    recorder.check("Subspace-Mitglied angelegt (HTTP 200)", 200, status)
    recorder.check("secret und mixed/vault sind Subspaces", ["mixed/vault", "secret"],
                   instance.subspace_paths(run_folder))
    access_matrix(instance, recorder, run_folder, {
        "mixed/m.txt": {**admin_all, "MEMBER": True, "RECIPIENT": True, "SUBMEMBER": False, "OUTSIDER": False},
        "mixed/vault/v.txt": {**admin_all, "MEMBER": False, "RECIPIENT": False, "SUBMEMBER": True, "OUTSIDER": False},
    })

    recorder.step = "5 MEMBER gibt plain frei (Beobachtung)"
    status, member_share = instance.share_plain(path_of("plain"), instance.sessions["OUTSIDER"],
                                                actor=instance.sessions["MEMBER"])
    recorder.note("HTTP-Status der Freigabe durch einen Space Editor", status)
    recorder.check("Versuch erzeugt keinen Subspace", ["mixed/vault", "secret"], instance.subspace_paths(run_folder))
    if member_share:
        recorder.note("Freigabe von MEMBER wieder entfernt, HTTP",
                      instance.remove_plain_share(path_of("plain"), member_share))

    recorder.step = "6 Rückbau der Freigaben und Mitglieder dieses Laufs"
    recorder.check("Freigabe secret/inner entfernt (HTTP 204)", 204,
                   instance.remove_plain_share(path_of("secret/inner"), inner_share))
    recorder.check("Subspace secret bleibt, solange ein Mitglied da ist", ["mixed/vault", "secret"],
                   instance.subspace_paths(run_folder))
    recorder.check("Subspace-Mitglied secret entfernt (HTTP 204)", 204,
                   instance.remove_subspace_member(path_of("secret"), secret_member))
    recorder.check("secret ist kein Subspace mehr", ["mixed/vault"], instance.subspace_paths(run_folder))
    recorder.check("Freigabe mixed entfernt (HTTP 204)", 204,
                   instance.remove_plain_share(path_of("mixed"), mixed_share))
    recorder.check("Subspace mixed/vault bleibt beim Entfernen der Freigabe darüber", ["mixed/vault"],
                   instance.subspace_paths(run_folder))
    recorder.check("Subspace-Mitglied mixed/vault entfernt (HTTP 204)", 204,
                   instance.remove_subspace_member(path_of("mixed/vault"), vault_member))
    recorder.check("Freigabe open entfernt (HTTP 204)", 204,
                   instance.remove_plain_share(path_of("open"), open_share))
    recorder.check("keine Subspaces mehr im Lauf-Ordner", [], instance.subspace_paths(run_folder))
    access_matrix(instance, recorder, run_folder, {
        "secret/s.txt": {"MEMBER": True, "SUBMEMBER": False, "RECIPIENT": False},
        "mixed/vault/v.txt": {"MEMBER": True, "SUBMEMBER": False, "RECIPIENT": False},
        "open/a.txt": {"MEMBER": True, "RECIPIENT": False},
        "secret/inner/i.txt": {"MEMBER": True, "RECIPIENT": False},
    })


def run(host, space_name, insecure, credentials):
    sessions = {role: Session(host, role, login, app_token, insecure)
                for role, (login, app_token) in credentials.items()}
    instance = Instance(sessions)
    instance.load_users()
    instance.ensure_space(space_name)
    instance.add_space_member(sessions["MEMBER"])

    run_folder = time.strftime("run-%Y%m%d-%H%M%S")
    instance.make_folder(run_folder)
    for folder in RUN_LAYOUT["folders"]:
        instance.make_folder(f"{run_folder}/{folder}")
    for file_path in RUN_LAYOUT["files"]:
        instance.put_file(f"{run_folder}/{file_path}")
    for relative_path in RUN_LAYOUT["folders"] + RUN_LAYOUT["files"]:
        instance.resolve_item_id(f"{run_folder}/{relative_path}")

    recorder = Recorder()
    run_scenario(instance, recorder, run_folder)
    return {
        "host": host,
        "space": instance.drive_id,
        "run_folder": run_folder,
        "passed": len(recorder.checks) - len(recorder.failed()),
        "failed": len(recorder.failed()),
        "checks": recorder.checks,
        "info": recorder.info,
    }


def print_report(result):
    current_step = None
    for entry in result["checks"]:
        if entry["step"] != current_step:
            current_step = entry["step"]
            print(f"\n== {current_step}")
        if entry["ok"]:
            print(f"  ok    {entry['name']}")
        else:
            print(f"  FAIL  {entry['name']}: erwartet {entry['expected']!r}, gemessen {entry['actual']!r}")
    for entry in result["info"]:
        print(f"\n  info  [{entry['step']}] {entry['name']}: {entry['observed']!r}")
    print(f"\n{result['host']}: {result['passed']} bestanden, {result['failed']} fehlgeschlagen "
          f"(Space {result['space']}, Ordner {result['run_folder']})")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("host", help="Instanz, z. B. cloud.xwork.cloud")
    parser.add_argument("--space-name", default="zz-subspace-e2e",
                        help="Name des Test-Space. Wird angelegt, falls er fehlt, und wiederverwendet.")
    parser.add_argument("--plan", action="store_true", help="nur zeigen, was passieren würde; ändert nichts")
    parser.add_argument("--insecure", action="store_true", help="TLS-Zertifikat nicht prüfen (Step-CA)")
    parser.add_argument("--json", action="store_true", help="Ergebnis als JSON ausgeben")
    args = parser.parse_args(argv)

    try:
        credentials = load_credentials(args.host, read_dist())
        if args.plan:
            print(json.dumps(plan(args.host, args.space_name, {role: credentials[role][0] for role in ROLES}),
                             indent=2, ensure_ascii=False))
            return 0
        result = run(args.host, args.space_name, args.insecure, credentials)
    except TestbenchError as error:
        print(f"Fehler: {error}", file=sys.stderr)
        if error.hint:
            print(f"Hinweis: {error.hint}", file=sys.stderr)
        return error.exit_code

    if args.json:
        print(json.dumps(result, indent=2, ensure_ascii=False))
    else:
        print_report(result)
    return 1 if result["failed"] else 0


if __name__ == "__main__":
    sys.exit(main())
