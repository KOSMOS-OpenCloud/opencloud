# Rechtemodell Reva / OpenCloud

## Rechteebenen im Überblick

Es gibt **7 Ebenen**, die den Zugriff auf Spaces, Dateien und Verzeichnisse steuern:

### 1. Eigentümer (Owner)
- Besitzer eines Space oder einer Ressource bekommen **immer alle Rechte** – keine Einschränkung möglich
- Geprüft über `UserIDEqual(user.Id, node.Owner())`
- Liefert `OwnerPermissions()` = alle 19 CS3-Permission-Flags auf `true`

### 2. Service Accounts
- Spezielle System-Nutzer (Typ `USER_TYPE_SERVICE`) für Hintergrundprozesse (Suchindex, Export…)
- Bekommen automatisch erhöhte Rechte (Stat, List, Upload, Download, Delete, Grant-Verwaltung)

### 3. Globale Berechtigungen (Named Permissions)
- Systemweite Gating-Rechte, die über den Permissions-Service geprüft werden:
  - `Drives.List`, `Drives.Create`, `Drives.ReadWrite`, `Drives.DeleteProject` usw.
  - `Shares.Write` – darf User/Group-Shares erstellen
  - `PublicLink.Write` – darf öffentliche Links erstellen
  - `ReadOnlyPublicLinkPassword.Delete`
- Diese werden **vor** der eigentlichen Operation geprüft (Gateway-Ebene)

### 4. Space-Mitgliedschaft (Grants auf dem Space-Root)
- Implementiert als Grants auf dem Root-Node des Spaces
- Bestimmt die **Basisberechtigung** für alle Inhalte im Space
- Rollen: `Manager`, `SpaceEditor`, `SpaceViewer` (mit/ohne Versionen)

### 5. Datei-/Ordner-Grants (User- und Group-Shares)
- **User-Grants**: `grant:u:<userid>` – direkte Freigabe an einen Benutzer
- **Group-Grants**: `grant:egroup:<groupid>` – Freigabe an eine Gruppe
- Gespeichert als Extended Attributes (xattrs) auf dem jeweiligen Node
- Jeder Grant hat eine Rolle mit konkreten CS3-Permissions

### 6. Öffentliche Links (Public Shares)
- Separater Share-Manager, nicht als Node-Grant gespeichert
- Haben eigenes Permission-Modell mit Rollen
- Können Passwortschutz und Ablaufdatum haben

### 7. Explizite Verweigerung (Deny Grants)
- Ein Grant mit **allen Permissions auf `false`** = explizites Deny
- **Blockiert sofort** – keine weitere Auswertung, Zugriff wird komplett verweigert
- Ermöglicht: Unterordner eines geteilten Ordners gezielt sperren

---

## Verfügbare Rollen und ihre Rechte

| Rolle | Lesen | Schreiben | Anlegen | Löschen | Teilen | Besonderheit |
|---|---|---|---|---|---|---|
| **viewer** | ja | – | – | – | – | Nur lesen |
| **editor** | ja | ja | ja | ja | – | Vollbearbeitung, kein Teilen |
| **file-editor** | ja | ja | – | – | – | Nur einzelne Datei bearbeiten |
| **editor-lite** | ja | – | ja | – | – | Upload & Download only |
| **uploader** | – | – | ja | – | – | Nur Upload, kein Download |
| **dropper** | Liste | – | ja | – | – | Verzeichnis sehen + Upload, kein Download |
| **coowner/manager** | ja | ja | ja | ja | ja | Volle Kontrolle inkl. Grant-Verwaltung |
| **secure-viewer** | Meta | – | – | – | – | Nur Stat + Verzeichnisliste |
| **denied** | – | – | – | – | – | Explizite Verweigerung |

Jeweils auch Varianten mit/ohne Versionshistorie: `*-with-versions`

---

## Kontrollierte Operationen

Die CS3-API definiert **19 einzelne Permission-Flags**:

| Operation | CS3-Flag | Bedeutung |
|---|---|---|
| **Listing** | `ListContainer` | Verzeichnisinhalte auflisten |
| **Metadaten sehen** | `Stat`, `GetPath` | Dateiinfo abrufen |
| **Datei öffnen/laden** | `InitiateFileDownload` | Download starten |
| **Datei hochladen** | `InitiateFileUpload` | Upload starten |
| **Ordner anlegen** | `CreateContainer` | Verzeichnis erstellen |
| **Datei löschen** | `Delete` | Datei entfernen |
| **Ordner löschen** | `DeleteContainer` | Verzeichnis entfernen |
| **Verschieben (weg)** | `Move` / `MoveContainer` | Datei/Ordner bewegen |
| **Verschieben (hin)** | `CreateContainer` / `InitiateFileUpload` | Am Ziel anlegen dürfen |
| **Versionen sehen** | `ListFileVersions` | Versionshistorie |
| **Version wiederherstellen** | `RestoreFileVersion` | Alte Version aktivieren |
| **Papierkorb** | `ListRecycle`, `RestoreRecycleItem`, `PurgeRecycle` | Papierkorb-Operationen |
| **Teilen** | `AddGrant`, `UpdateGrant`, `RemoveGrant` | Share-Verwaltung |
| **Zugriff verweigern** | `DenyGrant` | Explizites Deny setzen |

---

## Wie wirken die Ebenen zusammen?

### Aggregationslogik: Union (ODER) + Deny-Override

```
Für jeden Node vom Ziel bis zum Space-Root:
  1. Ist User Owner? → ALLE Rechte, fertig
  2. Alle User-Grants und Group-Grants auf diesem Node lesen
  3. Abgelaufene Grants überspringen
  4. Ist ein Grant ein explizites DENY? → SOFORT alle Rechte verweigern, Abbruch
  5. Alle gefundenen Grants per ODER (Union) zusammenführen
  6. Weiter zum Eltern-Node → Union über alle Ebenen
```

### Zusammengefasst

| Mechanismus | Wirkung |
|---|---|
| **Mehrere Grants auf einem Node** | **Expansiv** (Union/ODER) – jeder Grant erweitert die Rechte |
| **Grants auf verschiedenen Ebenen** (Ordner → Unterordner) | **Expansiv** (Union/ODER) – Rechte werden aufaddiert |
| **Explizites Deny** | **Einschränkend** – blockiert sofort, überschreibt alles |
| **Owner-Check** | **Expansiv** – überschreibt alles, gibt immer volle Rechte |
| **Globale Permissions** | **Einschränkend** – müssen erfüllt sein, bevor Operation überhaupt beginnt (Gate) |

### Wichtige Konsequenzen

1. **Ein Viewer-Grant auf einem Unterordner + ein Editor-Grant auf dem Space-Root** → User hat Editor-Rechte (Union)
2. **Ein Deny-Grant auf einem Unterordner** → User kann diesen Unterordner nicht sehen, auch wenn er Space-Editor ist
3. **Der Owner kann nie ausgesperrt werden** – Owner-Check hat immer Vorrang
4. **Globale Permissions** (wie `Shares.Write`) wirken als Vorbedingung unabhängig von Node-Grants

---

## Wo wird geprüft?

| Schicht | Was wird geprüft |
|---|---|
| **Gateway** | Globale Permissions (`Drives.*`, `Shares.Write`, `PublicLink.Write`) |
| **Share Provider** | Darf User überhaupt teilen? |
| **Storage Provider (decomposedfs)** | Ruft `AssemblePermissions()` für jeden File-/Ordner-Zugriff auf |
| **Node-Ebene** | Liest xattr-basierte Grants, prüft Owner, aggregiert Rechte |

---

## ACL-Modell (xattr-basiert)

Grants werden als Extended Attributes gespeichert:

- **User ACLs**: `u:userid=permissions`
- **Group ACLs**: `egroup:groupname=permissions`
- **Lightweight User ACLs**: `lw:userid=permissions`

Permission-Zeichen:
- `r` = read (Stat, Download)
- `w` = write (Create, Upload, Delete, Move)
- `x` = execute/list (ListContainer, ListVersions)
- `m` = manage/grant (AddGrant, ListGrants, RemoveGrant)
- `q` = quota (GetQuota)
- `+d`/`!d` = delete permission
- `+dc`/`!dc` = delete container
- `+mc`/`!mc` = move container
- `+if` = immutable file
- `+ic` = immutable container

---

## OCS Sharing API Permissions (Kompatibilitätsschicht)

Einfacheres 5-Bit-Modell für die OCS-API:

| Bit | Wert | Bedeutung |
|---|---|---|
| `PermissionRead` | 1 | Lesen/Anzeigen |
| `PermissionWrite` | 2 | Schreiben/Ändern |
| `PermissionCreate` | 4 | Neue Elemente anlegen |
| `PermissionDelete` | 8 | Elemente löschen |
| `PermissionShare` | 16 | Weitergabe-Rechte |
| `PermissionAll` | 31 | Alle Rechte |
| `PermissionsNone` | 64 | Explizite Verweigerung |

---

## WebDAV Permissions (PROPFIND-Darstellung)

| Zeichen | Bedeutung |
|---|---|
| `D` | Delete |
| `NV` | Rename/Move |
| `W` | Write (nur Dateien) |
| `CK` | Create (nur Ordner) |
| `S` | Shared |
| `R` | Shareable (kann teilen) |
| `M` | Mounted |
| `Z` | Deniable |
| `P` | Purge from Trashbin |
| `X` | Secure Viewable |

---

## Praxismuster: Abweichende Rechte auf Unterverzeichnisse

### Mehr Rechte auf Unterverzeichnisse vergeben

Technisch möglich, aber **wirkungslos** wenn der User bereits Space-Mitglied ist. Der Algorithmus läuft vom Blatt zum Space-Root und macht Union (ODER) über alle Ebenen – die Space-Root-Rechte gelten sowieso schon überall. Ein zusätzlicher Editor-Grant auf einem Unterordner ändert nichts, wenn der User bereits Editor auf dem Space-Root ist.

**Ausnahme**: Wenn ein User **kein** Space-Mitglied ist, kann man ihm gezielt einen Grant auf ein Unterverzeichnis geben. Das ist der klassische **File/Folder-Share** – der User sieht dann nur diesen Ordner, nicht den ganzen Space.

### Weniger Rechte auf Unterverzeichnisse vergeben

Es gibt **keine partielle Einschränkung**. Man kann nicht sagen "Editor im Space, aber Viewer in diesem Unterordner". Man kann nur über einen **Deny Grant** den Zugriff **komplett sperren** – alles oder nichts.

### Empfohlenes Muster: Zwei Gruppen + Deny Grant

Um innerhalb eines Spaces unterschiedliche Sichtbarkeiten zu erreichen:

1. **Gruppe A** und **Gruppe B** bekommen einen Grant auf dem Space-Root (z.B. beide als Editor)
2. Auf dem Unterordner `/Vertraulich` setzt man einen **Deny Grant für Gruppe B**
3. Gruppe A arbeitet weiter wie gehabt, Gruppe B sieht den Ordner nicht mehr

```
/Projekt (Space-Root)
  +-- grant:egroup:teamA = Editor       <-- beide Gruppen haben Zugriff
  +-- grant:egroup:teamB = Editor
  |
  +-- /Vertraulich
        +-- grant:egroup:teamB = {}     <-- Deny (alle Permissions = false)
```

**Ablauf fur Gruppe B** beim Zugriff auf `/Vertraulich`:

1. `ReadUserPermissions()` auf `/Vertraulich` findet den Group-Grant fur teamB
2. `PermissionsEqual(g.Permissions, &ResourcePermissions{})` ergibt `true` -> **Deny**
3. Sofortiger Abbruch, `NoPermissions()` zuruck
4. Der Editor-Grant auf dem Space-Root wird **nie erreicht**

**Ablauf fur Gruppe A** beim Zugriff auf `/Vertraulich`:

1. `ReadUserPermissions()` auf `/Vertraulich` -> kein Grant fur teamA hier -> leere Permissions
2. Weiter zum Parent -> Space-Root -> findet Editor-Grant fur teamA
3. Union -> volle Editor-Rechte

### Einschränkungen dieses Musters

| Situation | Verhalten |
|---|---|
| **User in beiden Gruppen (A und B)** | Deny greift. Sobald ein einziger Deny-Grant matched, ist Schluss. |
| **Unterordner des gesperrten Ordners** | Ebenfalls gesperrt. Der Walk geht vom Blatt zum Root – der Deny auf `/Vertraulich` wird auf dem Weg ausgelöst. |
| **Positiver Grant unterhalb des Deny** | Wirkungslos. Der Deny auf `/Vertraulich` wird zuerst gefunden und bricht sofort ab, bevor ein tieferer Grant gelesen wird. |
| **Owner des Spaces** | Nie betroffen. Owner-Check kommt vor der Grant-Auswertung und liefert immer `OwnerPermissions()`. |

### Zusammenfassung: Was geht und was nicht

| Szenario | Möglich? | Wie? |
|---|---|---|
| Space-Mitglied als Editor, ein Unterordner **komplett gesperrt** | Ja | Deny Grant auf den Unterordner |
| Space-Mitglied als Editor, ein Unterordner **nur Viewer** | **Nein** | Partielle Einschränkung nicht unterstützt |
| Kein Space-Mitglied, aber **Zugriff auf einen Ordner** | Ja | Normaler Share (Grant auf den Ordner) |
| Kein Space-Mitglied, zwei Ordner mit **verschiedenen Rollen** | Ja | Separater Grant pro Ordner mit je eigener Rolle |
| Verschiedene User, verschiedene Rechte im selben Space | Ja | Jeder User/Gruppe bekommt eigenen Grant |
| Deny fur eine Gruppe, andere Gruppe behält Zugriff | Ja | Deny Grant ist pro User/Gruppe |

---

## Schlüsseldateien im Code

| Datei | Inhalt |
|---|---|
| `reva-src/pkg/conversions/role.go` | Rollendefinitionen und WebDAV-Permissions |
| `reva-src/pkg/conversions/permissions.go` | OCS Permission Bits |
| `reva-src/pkg/storage/utils/grants/grants.go` | CS3 Grants und ACLs |
| `reva-src/pkg/storage/utils/decomposedfs/node/permissions.go` | Permission Assembly Algorithmus |
| `reva-src/pkg/storage/utils/decomposedfs/grants.go` | Grant Management (Add/Update/Remove/Deny) |
| `reva-src/pkg/storage/utils/decomposedfs/permissions/spacepermissions.go` | Space-Level Permissions |
| `reva-src/internal/grpc/services/usershareprovider/usershareprovider.go` | User Share Provider |
| `reva-src/internal/grpc/services/publicshareprovider/publicshareprovider.go` | Public Link Provider |
| `reva-src/internal/grpc/services/gateway/permissions.go` | Gateway Permission Delegation |
| `reva-src/internal/grpc/services/permissions/permissions.go` | Permissions Service |
| `reva-src/pkg/storage/utils/acl/acl.go` | ACL Model |
