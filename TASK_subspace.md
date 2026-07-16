# TASK: Subspace — ACL-Breakpoints innerhalb eines Space

## Problem

OpenCloud-Berechtigungen sind additiv: Space-Mitgliedschaft gibt Basis-Zugriff,
alles darunter erbt. Keine Möglichkeit, Unterbereiche exklusiv für bestimmte
Gruppen zugänglich zu machen.

Deny-Grants sind ungeeignet:
- Wirken nur auf zum Zeitpunkt T0 existierende Gruppen
- Neue Gruppen-Mitglieder haben sofort vollen Zugriff (Sicherheitslücke)
- Müssen manuell nachgepflegt werden

## Lösung: Subspace

Ein Verzeichnis mit eigenen Zugriffsregeln. Ab diesem Punkt gelten NUR die
expliziten Grants auf dem Subspace-Node — die Space-Root-Vererbung wird
unterbrochen. Listing (Navigation) wird additiv vom Space-Root geerbt.

## Beispiel

```
Space-Root (Aktenplan) → Mitglieder: Alle (Viewer)
  ├── Finanzverwaltung/        ← SUBSPACE: Gruppe "Finanzen" = Editor
  │   ├── Haushalt/            ← erbt von Subspace
  │   └── Steuern/
  ├── Personalverwaltung/      ← SUBSPACE: Gruppe "Personal" = Editor
  └── Allgemein/               ← kein Subspace, erbt von Space-Root
```

### Effektive Rechte für User in Gruppe "Finanzen"

| Pfad | Rechte | Begründung |
|---|---|---|
| / (Space-Root) | listing only | Mitglied, aber Subspaces blockieren Content |
| /Finanzverwaltung/ | Editor | Subspace-Grant |
| /Finanzverwaltung/Haushalt/ | Editor | erbt vom Subspace |
| /Personalverwaltung/ | listing only | Subspace ohne Grant → nur Listing |
| /Allgemein/ | Viewer | erbt von Space-Root (kein Subspace) |

## Algorithmus

### Bestehende Logik (Blatt → Root, additiv)

```
cn = target_node (Blatt)
ap = {}
while cn != space_root:
    ap += cn.ReadUserPermissions()     ← additiv sammeln
    cn = cn.Parent()
ap += space_root.ReadUserPermissions() ← Space-Root Grants dazu
```

### Neue Logik mit Subspace

```
cn = target_node (Blatt)
ap = {}
subspaces = space_root.GetSubspaceList()   ← gecacht, Liste von {NodeID, Pfad}

# 1. Finde ob Pfad auf/unter einem Subspace liegt
subspace_node = findAncestorSubspace(cn, subspaces)

if subspace_node != nil:
    # Fall 1: Pfad liegt innerhalb/auf einem Subspace
    # Walk nur bis Subspace-Node, NICHT weiter bis Root
    while cn != subspace_node:
        ap += cn.ReadUserPermissions()
        cn = cn.Parent()
    ap += subspace_node.ReadUserPermissions()  ← Subspace-Grants
    # Fertig. Kein Listing addieren — User ist im Subspace,
    # hat entweder Rechte oder nicht.

else:
    # Fall 2: Pfad liegt OBERHALB von Subspaces (z.B. Space-Root)
    # Normaler Walk bis Root
    while cn != space_root:
        ap += cn.ReadUserPermissions()
        cn = cn.Parent()
    ap += space_root.ReadUserPermissions()

    # Zusätzlich: Prüfe ob UNTER dem aktuellen Pfad ein Subspace
    # liegt in dem der User einen Grant hat
    if userHasGrantInChildSubspace(target_node.path, subspaces, user):
        ap += {ListContainer, Stat, GetPath}
        # → User kann zum Subspace navigieren, auch wenn er
        #   keine Space-Mitgliedschaft hat
```

### Listing-Additivität

Listing (`ListContainer`, `Stat`, `GetPath`) wird NUR in Fall 2 addiert,
und NUR wenn der User in einem darunterliegenden Subspace einen Grant hat.
Damit kann ein User der ausschliesslich Subspace-Mitglied ist (kein Space-
Mitglied) trotzdem dorthin navigieren.

In Fall 1 wird NICHTS addiert — die Subspace-Grants bestimmen allein
was der User darf.

## API und Speicherung

### Bestehende Graph API reicht

Space-Mitglieder werden über `SpaceRootInvite` → `Invite` → `CreateShare`
gesetzt. Subspace-Grants nutzen den gleichen Mechanismus:

- `Invite` auf den Subspace-Ordner → erzeugt Grant (wie Space-Mitgliedschaft)
- Kein neuer API-Endpunkt für Grant-Vergabe nötig
- Neuer Endpunkt nur für Subspace-Markierung (Ordner als Subspace setzen/entfernen)

### Unterschied zu normalen Shares

| | Share (Invite auf Datei/Ordner) | Subspace-Grant |
|---|---|---|
| Erscheint in "Freigaben" | ja | **nein** |
| Vom Empfänger ablehnbar | ja | **nein** |
| Navigiert im Pfad | nein (eigener Mount) | **ja** (im Space-Pfad) |
| Gespeichert | Share-Manager (jsoncs3) | Share-Manager (gleich) |
| Vererbung unterbricht | nein | **ja** |

Die Unterscheidung ob ein Grant ein "normaler Share" oder ein "Subspace-Grant"
ist, ergibt sich daraus ob der Ordner in der Subspace-Liste des Space steht.

### Subspace-Liste am Space-Root

xattr `user.oc.subspaces` am Space-Root-Node:
```json
["node-id-1", "node-id-2", ...]
```

Gecacht im Space-Object. Wird aktualisiert wenn ein Subspace erstellt/entfernt wird.

### Subspace-Node

Kein spezielles xattr auf dem Subspace-Node selbst — die Identifizierung
läuft über die Liste am Space-Root. Die Grants am Subspace-Node sind
normale Grants (`user.oc.grant.*`), erzeugt über die bestehende Invite-API.

## UI

### Ordner-Dialog (Sidebar/Properties)

Neuer Bereich "Subspace-Mitglieder" — Gruppen/User-Picker:
- Solange leer → normaler Ordner (erbt vom Space)
- Sobald ein Mitglied hinzugefügt → wird zum Subspace (in Liste eingetragen)
- Letztes Mitglied entfernt → wieder normaler Ordner (aus Liste entfernt)

### Space-Dialog (Space-Sidebar)

Sektion "Subspaces" unterhalb der Mitglieder:

```
Mitglieder
  Alle Mitarbeiter (Viewer)
  Admin (Manager)

Subspaces
  📁 Finanzverwaltung → Gruppe "Finanzen" (Editor)    [→ Ordner öffnen]
  📁 Personalverwaltung → Gruppe "Personal" (Editor)  [→ Ordner öffnen]
```

Jeder Subspace zeigt: Ordnername (klickbar → navigiert dorthin),
Mitglieder mit Rollen. Übersicht wer wo was darf.

### Ordner-Indikator

Subspace-Ordner zeigen ein Icon/Badge das sie als Subspace kennzeichnet.
Über bestehenden `resourceIndicator` Extension Point.

## Abgrenzung

| | Space-Mitglied | Share | Subspace |
|---|---|---|---|
| Scope | ganzer Space | einzelne Datei/Ordner | ab Verzeichnis abwärts |
| Vererbung | ja, komplett | nein | ja, ab Subspace-Node |
| Navigation | ja | nur Ziel (Mount) | ja (im Space-Pfad, Listing additiv) |
| In "Freigaben" | nein | ja | nein |
| Ablehnbar | nein | ja | nein |
| Neue Gruppenmitglieder | sofort alles | nur wenn geteilt | nur wenn im Subspace-Grant |
| Sicherheitsmodell | offen (additiv) | explizit | explizit (Vererbung bricht) |

## Scope

### Phase 1: reva (Kern)
- [ ] `assemblePermissions` mit Subspace-Logik
- [ ] Subspace-Liste am Space-Root (xattr)
- [ ] API zum Erstellen/Entfernen von Subspaces

### Phase 2: opencloud (Graph API)
- [ ] Subspace CRUD über Graph API
- [ ] Subspace-Info in Space-Response

### Phase 3: web (UI)
- [ ] Subspace-Indikator
- [ ] Subspace-Grant-Dialog
- [ ] Space-Sidebar Subspace-Sektion

## Eingriffsfläche und Risikobewertung

### reva — Kern (SENSIBEL)

| Stelle | Datei | Risiko | Begründung |
|---|---|---|---|
| `assemblePermissions` | `node/permissions.go:131-209` | **HOCH** | Zentrale Zugriffskontrolle. Jeder Bug = Sicherheitslücke oder Zugriffssperre für ALLE Operationen (PROPFIND, GET, PUT, MOVE, DELETE) |
| Subspace-Liste cachen | `node/node.go` (neu) | MITTEL | Cache-Kohärenz bei Subspace-Änderungen |
| Listing-Additivität (Fall 2) | `node/permissions.go` (neu) | MITTEL | Extra-Reads (Grants der Subspace-Nodes für fremde User), Performance |

**Nicht betroffen (kein Eingriff nötig):**
- `node/node.go` → `ReadUserPermissions()` — Grants pro Node bleiben wie sie sind
- `grants.go` → `AddGrant/RemoveGrant` — Grant-Logik selbst ändert sich nicht

### Bestehender Algorithmus (permissions.go:131-209)

```
1. Walk Blatt → Root (Zeile 160-185):
   - Pro Node: ReadUserPermissions → Deny-Check → AddPermissions (additiv)
   - Parent holen, weiter
2. Root-Node Grants dazuaddieren (Zeile 187-196)
3. Owner-Override (Zeile 199-200): Owner bekommt immer alles
4. Service-Account-Override (Zeile 204-206)
```

**Eingriff:** Im Walk (Zeile 161 `for cn.ID != rn.ID`) prüfen ob `cn.ID` in
der Subspace-Liste ist. Wenn ja → Walk stoppen (wie wenn rn.ID erreicht wäre).
Für Fall 2: nach dem Walk prüfen ob User Grants in Kind-Subspaces hat → Listing addieren.

### opencloud Graph API — Mittel

| Stelle | Datei | Risiko |
|---|---|---|
| Subspace markieren/entmarkieren | `api_driveitem_permissions.go` (neuer Endpoint) | MITTEL — neuer Code, keine bestehende Logik betroffen |
| Space-Info um Subspaces erweitern | `api_drives.go` | NIEDRIG — additiv |
| `Invite()` | keine Änderung | KEIN — Grants auf Ordner funktionieren schon |
| `ListPermissions()` | Subspaces in Response | NIEDRIG — additiv |

### web UI — Niedrig

| Stelle | Risiko |
|---|---|
| Space-Sidebar Subspace-Sektion | NIEDRIG — additiv |
| Ordner-Dialog Subspace-Mitglieder | NIEDRIG — additiv |
| ResourceIndicator Subspace-Badge | NIEDRIG — additiv |

## Testanforderungen

### Unit-Tests für `assemblePermissions` (PFLICHT vor Produktiv)

Alle Kombinationen müssen getestet werden:

**Subspace-Grundfunktion:**
- [ ] User mit Grant auf Subspace → bekommt Subspace-Rechte
- [ ] User ohne Grant auf Subspace → bekommt nur Listing (Fall 2)
- [ ] User auf Pfad innerhalb Subspace → erbt vom Subspace, NICHT vom Root
- [ ] User auf Pfad außerhalb Subspace → normal wie bisher (kein Einfluss)

**Edge Cases:**
- [ ] Owner → immer volle Rechte (darf nicht durch Subspace eingeschränkt werden)
- [ ] Service-Account → immer Service-Permissions
- [ ] Deny-Grant auf Subspace-Node → Deny hat Vorrang (wie bisher)
- [ ] Verschachtelte Subspaces → innerer Subspace gewinnt
- [ ] Subspace-Liste leer → alles wie bisher (keine Regression)
- [ ] Subspace gelöscht/verschoben → Liste wird stale, Fallback auf normal

**Performance:**
- [ ] Subspace-Liste laden darf nicht teuer sein (gecacht am Space-Object)
- [ ] Fall 2 (Listing-Check) darf nicht N Grants lesen pro Request

### Integration-Tests (PFLICHT)

- [ ] PROPFIND auf Subspace-Ordner: nur Subspace-Grants
- [ ] PROPFIND auf übergeordneten Ordner: Listing but kein Content
- [ ] GET/Download aus Subspace: nur mit Subspace-Grant
- [ ] PUT/Upload in Subspace: nur mit Subspace-Grant
- [ ] MOVE in/aus Subspace: korrekte Rechte auf Quelle UND Ziel
- [ ] DELETE in Subspace: nur mit Subspace-Grant
- [ ] Share-Erstellung innerhalb Subspace: nur mit Share-Recht im Subspace

### Regressions-Tests

- [ ] Alle bestehenden Permission-Tests müssen weiterhin bestehen
- [ ] Spaces ohne Subspaces: keinerlei Verhaltensänderung

## Betroffene Dateien

### reva
- `pkg/storage/pkg/decomposedfs/node/permissions.go` — assemblePermissions (KERN)
- `pkg/storage/pkg/decomposedfs/node/node.go` — GetSubspaceList, Cache
- `pkg/storage/pkg/decomposedfs/metadata/prefixes/prefixes.go` — SubspacesAttr Konstante

### opencloud
- `services/graph/pkg/service/v0/api_driveitem_permissions.go` — Subspace markieren
- `services/graph/pkg/service/v0/api_drives.go` — Subspace-Info in Space-Response

### web
- `packages/web-pkg/` — Indikator, Grant-Dialog, Space-Sidebar
