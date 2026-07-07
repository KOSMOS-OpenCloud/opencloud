# Graph API EDMS Extensions — Task-Übersicht

**Herkunft:** Analyse des VIS-Polizei JobProcessors (PDV/Materna EDMS)
**Kontext:** VIS nutzt SOAP als Control Plane über einem WebDAV Data Plane. OpenClouds Graph API
ist das natürliche Äquivalent — braucht aber Erweiterungen für EDMS-Szenarien.

---

## Bestandsaufnahme: Was reva/OpenCloud heute schon kann

| Feature | Implementierung | Wo |
|---------|----------------|-----|
| **Revisions** | `{nodeID}.REV.{timestamp}` im Filesystem, xattr-kopiert | `posix/tree/revisions.go` |
| **Arbitrary Metadata** | `user.oc.md.*` xattrs, Set/Unset über CS3 API | `decomposedfs/metadata.go` |
| **Immutable** | `user.oc.immutable` xattr | `prefixes/prefixes.go` |
| **Favorites** | `user.oc.fav.{userid}` xattr | `prefixes/prefixes.go` |
| **Trash/Soft Delete** | `user.oc.trash.origin` xattr + Trashbin | `posix/trashbin/` |
| **Checksums** | `user.oc.cs.{algo}` xattrs | `prefixes/prefixes.go` |
| **Locking** | WebDAV LOCK | reva ocdav |
| **Scan Status** | `user.oc.scanstatus` / `user.oc.scandate` xattrs | `prefixes/prefixes.go` |
| **Search/Index** | Bleve (embedded) + OpenSearch (optional), indexiert **alle** ArbitraryMetadata dynamisch | `search/pkg/bleve/` |

### Kritische Erkenntnis: Die Rendition-Infrastruktur existiert bereits vollständig

Die gesamte Kette für Renditions (und Custom Properties generell) ist schon da:

```
Schicht              Komponente                    Status
═══════              ══════════                    ══════

Storage:             decomposedfs xattrs           user.oc.md.* existiert
                     SetArbitraryMetadata()        existiert (decomposedfs/metadata.go)
                     UnsetArbitraryMetadata()      existiert

Indexierung:         Search-Service basic.go       indexiert ALLE user.oc.md.* Keys
                     Zeile 37-44:                  automatisch in doc.Metadata

Suchindex:           Bleve index.go                Dynamic=true auf Metadata-Mapping
                     Zeile 52-54:                  → jeder neue Key sofort suchbar
                                                   Kein Schema-Update nötig

PROPFIND:            node.go Zeile 1004            ArbitraryMetadata wird in
                                                   ResourceInfo eingebunden

Fehlt NUR:           Graph API Handler             ~30 Zeilen pro Endpoint
                     in opencloud/services/graph/  REST-Wrapper um bestehende CS3-Calls
```

**Decomposedfs muss Renditions NICHT kennen.** Es sind reguläre ArbitraryMetadata-Einträge.
Die Rückwärts-Suche ("alle Renditions von Dokument X") läuft über den bestehenden
Search-Service + Bleve, der alle `user.oc.md.*` Keys dynamisch indexiert.

---

## Task 1: Renditions

### Problem

Ein Worker konvertiert `brief.docx` nach `brief.pdf` (PDF/A). Beide Dateien liegen danach nebeneinander
im selben Ordner — ohne erkennbare Verknüpfung. Niemand weiß:
- Dass `brief.pdf` von `brief.docx` abgeleitet wurde
- Aus welcher Version es erzeugt wurde
- Ob es noch aktuell ist
- Welche Eigenschaften die Rendition hat (pdfaValid, pageCount)

### Implementierung: xattr am Kind, Suche über Bleve

Renditions sind reguläre Dateien mit `user.oc.md.rendition.*` Metadaten.
Kein neuer reva-Code. Kein neuer Prefix. Kein neues Indexschema.

**Rendition-Datei (xattrs via SetArbitraryMetadata):**

```
brief.pdf (eigenständige Datei/Node)
  user.oc.md.rendition.source       = {nodeID-von-brief.docx}
  user.oc.md.rendition.sourceVersion = 2025-07-07T12:00:00Z  (Revision-Key)
  user.oc.md.rendition.type          = pdf/a
  user.oc.md.rendition.profile       = 2b
  user.oc.md.rendition.valid         = 1
  user.oc.md.rendition.pages         = 42
  user.oc.md.rendition.createdBy     = worker-pdf-01
  user.oc.md.rendition.createdAt     = 2025-07-07T12:05:00Z
```

**Wie es fließt:**

```
1. Worker:     SetArbitraryMetadata(brief.pdf, {"rendition.source": nodeID})
               → reva schreibt user.oc.md.rendition.source als xattr

2. Search:     basic.go indexiert automatisch:
               doc.Metadata["rendition.source"] = nodeID

3. Bleve:      Dynamic=true → Metadata.rendition.source ist sofort suchbar
               Query: Metadata.rendition.source == {nodeID}
               → findet alle Renditions eines Dokuments

4. PROPFIND:   ri.ArbitraryMetadata enthält rendition.* Keys
               → WebDAV-Clients sehen die Rendition-Metadaten
```

**Was der Worker tut (bestehende APIs, keine Änderungen):**

```python
# OpenWorks Executor — setzt Rendition-Metadaten nach Upload
fs.write("brief.pdf", pdf_bytes)   # ← regulärer WebDAV PUT

# Rendition-Verknüpfung über ArbitraryMetadata (CS3 API existiert)
set_metadata(brief_pdf_node, {
    "rendition.source": source_node_id,
    "rendition.sourceVersion": "2025-07-07T12:00:00Z",
    "rendition.type": "pdf/a",
    "rendition.profile": "2b",
    "rendition.valid": "1",
    "rendition.pages": "42",
})
```

### Graph API Endpoint (einziger neuer Code)

```
GET /graph/v1/drives/{spaceId}/items/{itemId}/renditions
→ Search-Request: Metadata.rendition.source == {itemId}
→ Ergebnisse als Rendition-Liste formatieren
→ [
    {
      "id": "abc-123",
      "name": "brief.pdf",
      "type": "pdf/a",
      "profile": "2b",
      "valid": true,
      "pages": 42,
      "sourceVersion": "2025-07-07T12:00:00Z",
      "createdBy": "worker-pdf-01",
      "createdAt": "2025-07-07T12:05:00Z"
    }
  ]

DELETE /graph/v1/drives/{spaceId}/items/{itemId}/renditions/{renditionId}
→ Löscht die Rendition-Datei (reguläres Delete)

POST /graph/v1/drives/{spaceId}/items/{itemId}/renditions
→ Setzt rendition.* xattrs auf einer existierenden Datei
→ Body: { "renditionId": "abc-123", "type": "pdf/a", ... }
→ Intern: SetArbitraryMetadata auf renditionId-Node
```

### Aufwand

| Schicht | Änderung | Aufwand |
|---------|----------|---------|
| reva / decomposedfs | **Keine** | 0 |
| Search-Service / Bleve | **Keine** — Dynamic=true indexiert automatisch | 0 |
| Graph API | 3 Endpoints (~30 Zeilen pro Handler) | **Gering** |
| Worker/OpenWorks | SetArbitraryMetadata aufrufen | **Gering** |

---

## Task 2: Custom Properties über Graph API

### Problem

ArbitraryMetadata existiert in reva (`user.oc.md.*` xattrs), wird im Search-Index
gespeichert (Bleve Dynamic=true), und über PROPFIND exponiert — aber die Graph API
hat keinen eigenen REST-Endpoint dafür.

### Vorschlag

```
PATCH /graph/v1/drives/{spaceId}/items/{itemId}/properties
{ "pdfaValid": true, "pdfaProfile": "2b", "pageCount": 42 }
→ SetArbitraryMetadata: user.oc.md.pdfaValid = "true", ...

GET /graph/v1/drives/{spaceId}/items/{itemId}/properties
→ Liest alle user.oc.md.* xattrs aus ResourceInfo.ArbitraryMetadata

DELETE /graph/v1/drives/{spaceId}/items/{itemId}/properties/{key}
→ UnsetArbitraryMetadata
```

### Aufwand

| Schicht | Änderung | Aufwand |
|---------|----------|---------|
| reva / decomposedfs | **Keine** — SetArbitraryMetadata existiert | 0 |
| Graph API | 3 Endpoints, reine Wrapper um CS3-Calls | **Gering** |

---

## Task 3: Explizite Versionierung mit Kommentar

### Problem

Reva erstellt Revisions implizit beim Überschreiben. Es gibt keinen Weg:
- Eine Version bewusst anzulegen ("Snapshot jetzt")
- Einen Kommentar zur Version zu hinterlegen
- Eine Version zu pinnen (vor Cleanup schützen)

### Vorschlag

```
POST /graph/v1/drives/{spaceId}/items/{itemId}/versions
{ "comment": "PDF/A konvertiert durch Worker", "pin": true }
→ CreateRevision() + xattrs auf Revision-Node:
  user.oc.md.version.comment = "PDF/A konvertiert durch Worker"
  user.oc.md.version.pinned  = "true"

GET /graph/v1/drives/{spaceId}/items/{itemId}/versions
→ ListRevisions() + Kommentar/Pin-xattrs lesen
```

### Aufwand

| Schicht | Änderung | Aufwand |
|---------|----------|---------|
| reva | `CreateRevision` erweitern: Kommentar + Pin als xattrs | **Gering** |
| reva | Cleanup-Loop: Pin-Flag respektieren | **Gering** |
| Graph API | 2 Endpoints | **Gering** |

---

## Task 4: Pre-Flight Checks (checkMove)

### Problem

MOVE/COPY kann fehlschlagen wegen Berechtigungen, Sperren, Immutable-Flags, Quota.
Der Client erfährt das erst nach dem Versuch.

### Vorschlag

```
POST /graph/v1/drives/{spaceId}/items/{itemId}/checkMove
{ "destination": "/archiv/2026/" }
→ { "allowed": true, "warnings": [...] }
  ODER
→ { "allowed": false, "reason": "destination is immutable", "code": "IMMUTABLE_TARGET" }
```

### Aufwand

| Schicht | Änderung | Aufwand |
|---------|----------|---------|
| reva | Permission-Checks ohne Ausführung wrappen | **Mittel** |
| Graph API | 1 Endpoint | **Gering** |

---

## Task 5: Control Plane Type in OpenWorks Job-Spec

### Problem

Verschiedene Backends (OpenCloud, VIS, CMIS) haben verschiedene Control-Plane-APIs.
Der Worker muss wissen, wie er mit dem Backend interagiert.

### Vorschlag

`controlPlane` Feld in der Job-Spec (OpenWorks-Protokollerweiterung):

```yaml
pipelines:
  convert-office:
    job:
      type: "convert-office"
      controlPlane:
        type: "opencloud"       # opencloud | vis | cmis | custom

  convert-office-vis:
    job:
      type: "convert-office"
      controlPlane:
        type: "vis"
        services:
          pool:
            url: "${VIS_POOL_WSDL}"
            auth: "kerberos"
```

### Aufwand

| Schicht | Änderung | Aufwand |
|---------|----------|---------|
| reva / OpenCloud | **Keine** | 0 |
| OpenWorks-Spec | `controlPlane` Feld dokumentieren | **Gering** |
| Worker | ServiceClient für SOAP/REST | **Gering** |

---

## Zusammenfassung: Aufwand und Priorität

| Task | reva | Graph API | Aufwand | Priorität |
|------|------|-----------|---------|-----------|
| **1. Renditions** | Null — xattrs + Bleve-Suche existieren | 3 Endpoints (~90 Zeilen) | **Gering** | Hoch |
| **2. Custom Properties** | Null — ArbitraryMetadata existiert | 3 Endpoints (~60 Zeilen) | **Gering** | Hoch |
| **3. Explizite Versions** | Gering — CreateRevision + xattrs | 2 Endpoints | Gering | Mittel |
| **4. Pre-Flight Checks** | Mittel — Permission-Dry-Run | 1 Endpoint | Mittel | Niedrig |
| **5. Control Plane Type** | Null | Null (OpenWorks-Spec) | Gering | Hoch |

### Reihenfolge

```
1. Custom Properties (Task 2) ← fast geschenkt, alles existiert außer Graph-Handler
2. Renditions (Task 1)        ← alles existiert, nur Graph-Handler + Bleve-Query
3. Control Plane Type (Task 5) ← nur Spec-Erweiterung
4. Explizite Versions (Task 3) ← nice-to-have
5. Pre-Flight Checks (Task 4)  ← Komfort, nicht Blocker
```
