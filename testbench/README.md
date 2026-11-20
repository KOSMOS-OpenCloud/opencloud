# Testbench OpenCloud KOSMOS

Tests gegen eine laufende Instanz. Sie prüfen Ketten über mehrere Dienste, die die Go-Tests der einzelnen Pakete
nicht erreichen. Jeder Test ist ein eigenes Skript in diesem Ordner, ohne Fremdpakete (nur Python-Standardbibliothek).

Zugangsdaten stehen in `testbench/DIST` (gitignored, nie committen), je Zeile `SCHLÜSSEL=wert`.

## subspace_share_boundary.py — Freigabe gegen Subspace, Rechte-Walk

Anlass: cloud.brandis.eu 2026-11-17. Eine normale Freigabe von `kisa/loga/up` wurde zum Subspace, weil reva nach
jedem Grant `autoAddSubspace` rief. Regel seitdem: Ein freigegebener Ordner ist eine Freigabe. Ein Subspace entsteht
nur über den Endpunkt `…/subspace/permissions` (Panel „Subspace“) und hat ab seinem Ordner eigene Zugriffsrechte.

```
cd /data/source/gitapps/opencloud/testbench
python3 -m unittest -v                                        # Tests des Skripts selbst, ohne Instanz
python3 subspace_share_boundary.py --plan cloud.xwork.cloud   # zeigt Nutzer und Schritte, ändert nichts
python3 subspace_share_boundary.py cloud.xwork.cloud          # Test ausführen
```

Pflicht nach jedem Deploy mit Änderungen an Freigaben, Grants, Subspaces oder Rechten (opencloud, reva, web):
zuerst gegen cloud.xwork.cloud, erst danach Produktion. Exit 0 = alle Prüfungen bestanden, 1 = Prüfung
fehlgeschlagen oder Ablauf abgebrochen, 3 = Zugangsdaten fehlen.

### Testnutzer

Fünf verschiedene Konten je Instanz, je ein App-Token (Einstellungen → App-Token). In `DIST`:

```
E2E_ADMIN__CLOUD_XWORK_CLOUD=login:app-token
E2E_MEMBER__CLOUD_XWORK_CLOUD=login:app-token
E2E_RECIPIENT__CLOUD_XWORK_CLOUD=login:app-token
E2E_SUBMEMBER__CLOUD_XWORK_CLOUD=login:app-token
E2E_OUTSIDER__CLOUD_XWORK_CLOUD=login:app-token
```

Der Host steht in Großbuchstaben im Schlüssel, alles außer A–Z und 0–9 als `_`.

| Rolle | Bedeutung |
|---|---|
| ADMIN | legt Space, Ordner, Freigaben und Subspaces an. Braucht „Spaces anlegen“ und „Manage space properties“ |
| MEMBER | wird Mitglied am Space-Root (Space Editor) |
| RECIPIENT | erhält nur normale Freigaben |
| SUBMEMBER | wird nur Subspace-Mitglied |
| OUTSIDER | bekommt nie ein Recht |

MEMBER, RECIPIENT, SUBMEMBER und OUTSIDER dürfen kein „Manage space properties“ haben, sonst verdeckt der
Verwaltungs-Bypass fehlende Rechte.

### Was der Lauf anlegt und was bleibt

- Project-Space `zz-subspace-e2e` (`--space-name`), falls er fehlt. Er wird wiederverwendet und nie gelöscht.
- Je Lauf ein Ordner `run-<zeit>` mit sechs Unterordnern und sechs kleinen Dateien. Er bleibt stehen.
- Freigaben und Subspace-Mitglieder des Laufs entfernt Schritt 6 wieder. Das ist Teil des Tests (Rückbau prüfen).
  Bricht der Lauf vorher ab, bleiben sie bestehen: Ordner `run-<zeit>` im Test-Space ansehen.

### Schritte und Prüfungen

| Schritt | Aktion | geprüft |
|---|---|---|
| 0 | Ausgangslage | keine Subspaces, nur ADMIN und MEMBER lesen |
| 1 | normale Freigabe `open` → RECIPIENT | kein Subspace, Kontext `space`, RECIPIENT liest nur `open`, MEMBER behält Zugriff |
| 2 | Subspace `secret` mit SUBMEMBER | registriert, Kontext `subspace`, MEMBER verliert Zugriff, ADMIN behält ihn, SUBMEMBER liest nur `secret` |
| 3 | normale Freigabe `secret/inner` → RECIPIENT | weiterhin nur `secret` Subspace, RECIPIENT liest nur `inner`, MEMBER bleibt draußen |
| 4 | Freigabe `mixed` → RECIPIENT, Subspace `mixed/vault` | die Freigabe reicht nicht in den Subspace darunter |
| 5 | MEMBER gibt `plain` frei | nur Beobachtung des HTTP-Status (rollenabhängig), es darf kein Subspace entstehen |
| 6 | Rückbau | Subspace bleibt bis zum letzten Mitglied, verschwindet dann, Space-Rechte gelten wieder |

Gelesen wird über die Datei-ID (`GET /dav/spaces/<id>`). So greifen auch Empfänger zu, die kein Space-Mitglied sind.

In den Schritten 0 bis 3 und 6 prüft das Skript zusätzlich die beiden Listen, aus denen die Oberfläche die Ansicht
des Empfängers baut: „Mit mir geteilt“ (`GET /graph/v1beta1/me/drive/sharedWithMe`) und die Space-Liste
(`GET /graph/v1.0/me/drives`). Eine Freigabe steht unter „Mit mir geteilt“ und nie als Space. Eine
Subspace-Mitgliedschaft steht nie unter „Mit mir geteilt“, dafür erscheint der Space in der Space-Liste.

### Grenzen

- Das Skript ruft dieselben Endpunkte wie die Oberfläche. Nicht geprüft ist, was der Browser selbst entscheidet:
  welches Panel welchen Endpunkt nimmt und wie Icon und Indikator gezeichnet werden.
- Nicht abgedeckt: Gruppen, öffentliche Links, Suche, verschachtelte Subspaces. Verschachtelte Subspaces und
  Freigabe plus Mitglied am selben Ordner prüfen die reva-Specs
  (`opencloud_reva/pkg/storage/pkg/decomposedfs/subspace_share_boundary_test.go`, Lauf: `opencloud_reva/testbench/`).
- Stand 2026-11-18: noch nie gegen eine Instanz gelaufen, Testnutzer fehlen. Ungeprüfte Annahmen: Basic Auth mit
  App-Token an Graph und WebDAV, Lesen per Datei-ID für Freigabe-Empfänger, die Rollen-IDs im Skript.

## state_snapshot.py — Zustand vor und nach einem Deploy vergleichen

Anlass: os.brandis 2026-10-12. Ein `opencloud init` beim Neuaufbau erzeugte eine neue Runtime-Config mit neuen IDs
und Secrets (`storage_users.mount_id`, `admin_user_id`, `system_user_id`, `graph.application.id`, Service-Accounts,
`jwt_secret` …). Die Space-IDs blieben, die Storage-ID davor war neu, alte Clients liefen auf 404. Das Skript hält den
Stand vor einem OpenCloud-Deploy fest und meldet danach jede verschwundene oder geänderte ID.

```
cd /data/source/gitapps/opencloud/testbench
python3 -m unittest -v                                   # Tests des Skripts selbst, ohne Instanz
python3 state_snapshot.py save cloud.xwork.cloud         # VOR dem Deploy
python3 state_snapshot.py compare cloud.xwork.cloud      # NACH dem Deploy
python3 state_snapshot.py show cloud.xwork.cloud         # gespeicherten Stand ansehen
```

Das Skript liest nur. Exit 0 = gleich, 1 = Abweichung, 2 = Ablauf abgebrochen, 3 = kein Snapshot oder Quelle nicht
erreichbar. Nur für Deployments des Containers opencloud, nicht für Web-Extensions oder andere Dienste.

### Was verglichen wird

| Quelle | Inhalt | Zugang |
|---|---|---|
| Host | alle Werte der `/etc/opencloud/opencloud.yaml`; Secrets nur als Fingerabdruck (sha256, 12 Zeichen) | `ssh root@<host>`, `podman exec` |
| Host | `idm/ldap.crt`, `idm/ldap.key`, `idp/private-key.pem`, `idp/encryption.key` als Fingerabdruck | wie oben |
| API | OIDC-Issuer und Signaturschlüssel, Anwendungs-ID, globale Rollen, Freigabe-Rollen | Graph |
| API | Spaces: ID (mit Storage-ID davor), Name, Alias, Typ, Besitzer, Root | Graph |
| API | Mitglieder der Project-Spaces mit Rollen-ID | Graph, nur wo das Konto Zugriff hat |
| API | Nutzer mit globaler Rolle, Gruppen mit Mitgliedern | Graph, nur mit der Rolle Admin |

- `FEHLT` und `GEÄNDERT` sind Abweichungen (Exit 1). `neu` ist nur ein Hinweis: zwischen den Läufen darf jemand
  einen Space anlegen oder ein Mitglied hinzufügen.
- Verglichen werden nur Quellen, die in beiden Ständen gelesen wurden. Die Ausgabe nennt je Quelle „verglichen“ oder
  „NICHT verglichen“. `coverage/*` hält fest, wie vollständig die API-Sicht war (`drives all|own`, `users all|self`,
  `groups all|none`). Wechselt das Konto zwischen `save` und `compare`, zeigt sich das dort.
- Image und Startzeit des Containers stehen nur zur Anzeige im Snapshot, sie ändern sich bei jedem Deploy.

### Zugang zur API

1. `OC_BEARER=<access-token>` in der Umgebung (Token einer Sitzung, 5 Minuten gültig, z. B. aus dem Brave-Debug).
2. `testbench/DIST`: `SNAPSHOT_ADMIN__<HOST>=login:app-token`, sonst `E2E_ADMIN__<HOST>` (Schreibweise wie oben).

Ohne Zugang liest das Skript nur den Host und sagt das in der Ausgabe. `--no-api` macht das ausdrücklich,
`--require-api` bricht stattdessen ab. Für vollständige Rollen (alle Nutzer, alle Gruppen, Mitglieder aller Spaces)
braucht es ein Konto mit der Rolle Admin, das Mitglied der Spaces ist oder sie verwalten darf.

### Ablage

Snapshots liegen unter `testbench/snapshots/<host>.json` (gitignored, Rechte 600). Sie enthalten IDs, Space- und
Nutzernamen, keine Secrets im Klartext. `save` hebt den vorherigen Snapshot als `<host>.json.previous` auf
(`--force`: nicht aufheben). Andere Datei: `--snapshot <pfad>`.

### Grenzen

- Der YAML-Leser ist kein vollständiger Parser (nur Standardbibliothek): Mappings, Listen, Skalare. Reicht für die
  von `opencloud init` erzeugte Datei, nicht für Anker, mehrzeilige Werte oder Flow-Syntax.
- Globale Rollen aller Nutzer und Gruppen sind bisher nicht gegen eine Instanz geprüft (Stand 2026-11-21: es lag
  kein Admin-Token vor, der Lauf gegen cloud.xwork.cloud lief als Space Admin: `users self`, `groups none`,
  4 von 29 Project-Spaces ohne lesbare Mitglieder).
- Inhalte der Spaces (Dateien, Freigaben auf Ordner, Subspaces) werden nicht erfasst.
