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

### Grenzen

- Die Oberfläche (Icon, Indikator, Panels) prüft das Skript nicht.
- Nicht abgedeckt: Gruppen, öffentliche Links, Suche, verschachtelte Subspaces. Verschachtelte Subspaces und
  Freigabe plus Mitglied am selben Ordner prüfen die reva-Specs
  (`opencloud_reva/pkg/storage/pkg/decomposedfs/subspace_share_boundary_test.go`, Lauf: `opencloud_reva/testbench/`).
- Stand 2026-11-18: noch nie gegen eine Instanz gelaufen, Testnutzer fehlen. Ungeprüfte Annahmen: Basic Auth mit
  App-Token an Graph und WebDAV, Lesen per Datei-ID für Freigabe-Empfänger, die Rollen-IDs im Skript.
