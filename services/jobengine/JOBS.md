# Job Engine — Warum intern?

## Motivation

Die Job Engine lebt **innerhalb** der OpenCloud, nicht als externer Service. Das ist eine bewusste Architekturentscheidung.

### Vorteile der internen Positionierung

**Direkter Dateizugriff**
Die Engine hat direkten Zugriff auf Dateien über reva und WebDAV — kein Umweg über externe APIs, kein Token-Handling, kein Upload/Download über HTTP. Dateien lesen, verarbeiten, zurückschreiben — alles intern.

**IPC statt Netzwerk**
Entscheidungen und Reportings laufen über interne Go-Channels und gRPC, nicht über HTTP. Status-Updates, Fortschrittsmeldungen, Fehlerbehandlung — alles ohne Netzwerk-Overhead.

**Berechtigungen und Kontext**
Die Engine läuft im User-Kontext. Sie hat Zugriff auf Permissions, Space-Zugehörigkeit, Quota — alles was nötig ist um zu entscheiden ob ein Job erlaubt ist und wohin das Ergebnis geschrieben werden darf.

**Web-API als Host**
Die API ist direkt im OpenCloud-Prozess eingebettet. Keine separate Proxy-Konfiguration, kein CORS, keine zusätzliche Authentifizierung. Der Proxy leitet Requests an die Engine weiter wie an jeden anderen Service.

### Was die Engine NICHT ist

Die Engine ist **kein Ausführungsort für alle Jobs**. Sie ist ein **Dispatcher**.

```
Job Engine (intern)                    Externe Worker
┌─────────────────┐                   ┌──────────────┐
│ Queue            │                   │ Collabora    │
│ Dispatch         │──── HTTP ────────▶│ (doc→pdf)    │
│ Status-Tracking  │                   └──────────────┘
│ File I/O         │                   ┌──────────────┐
│ Permissions      │──── HTTP ────────▶│ vLLM/OCR     │
│ Result-Handling  │                   │ (scan, read)  │
│                  │                   └──────────────┘
│                  │                   ┌──────────────┐
│                  │──── exec ────────▶│ lokaler Proc │
│                  │                   │ (unzip, tar)  │
└─────────────────┘                   └──────────────┘
```

## Job-Typen

### Synchron-lokal (exec)
Einfache, schnelle Jobs die im OpenCloud-Container laufen.
- `unzip` — Archiv entpacken
- `tar` — Archiv entpacken
- `zip` — Dateien komprimieren

Diese sind die **Ausnahme**, nicht die Regel. Skalierungslimits sind bekannt.

### Dispatch an Sidecar (http)
Jobs die an einen HTTP-Sidecar-Service delegiert werden.
- `doc-to-pdf` — Collabora wandelt um, Engine schreibt Ergebnis zurück
- `md-to-pdf` — pandoc-Sidecar konvertiert

Die Engine kümmert sich um: Datei holen, an Sidecar senden, Ergebnis empfangen, zurückschreiben.

### Externalisierte Jobs (fetch-and-callback)
Langläufer die an externe Dienste abgegeben werden. Der externe Dienst:
1. Erhält eine URL zum Fetchen der Quelldatei
2. Verarbeitet eigenständig (OCR, Transkription, Analyse)
3. Meldet sich zurück (Callback oder Polling)

```
Engine                          Externer Dienst
  │                                    │
  ├─── POST job {source_url} ─────────▶│
  │                                    │ verarbeitet...
  │                                    │ (kann Minuten/Stunden dauern)
  │◀── Callback {result_url} ─────────┤
  │                                    │
  ├─── GET result ─────────────────────▶│
  │◀── Ergebnis ───────────────────────┤
  │                                    │
  └─── Schreibt in OpenCloud           │
```

Alternativ: Engine pollt einen Status-Endpoint des externen Dienstes (Ticket-basiert).

Beispiele:
- **vLLM OCR** — Bild/PDF an LLM-Vision senden, Text zurück
- **Whisper** — Audio an Transkriptions-Service, Text zurück
- **KI-Analyse** — Dokument an Analyse-Pipeline, Metadaten zurück

### Implementierungsbedingte Flexibilität

Die Pipeline-YAML definiert den Executor-Typ. Ob ein Job lokal, als HTTP-Call oder als externalisierter Langläufer läuft, entscheidet die Konfiguration — nicht der Code.

```yaml
# Schnell, lokal
executor:
  type: exec
  command: "unzip"

# Sidecar-Dispatch
executor:
  type: http
  url: "${COLLABORA_URL}/cool/convert-to/pdf"

# Externalisiert mit Callback
executor:
  type: webhook
  url: "${VLLM_URL}/ocr"
  callback_url: "${OC_URL}/api/v0/jobs/callback"
  poll_interval: 10s
  timeout: 3600s
```

## Skalierung

Die interne Engine hat ein konfigurierbares Worker-Limit (`max_workers`). Für Skalierung über dieses Limit hinaus:

1. **Externe Worker** — Sidecars skalieren unabhängig (Collabora-Cluster, GPU-Nodes)
2. **NATS-basierte Queue** — mehrere OpenCloud-Instanzen teilen sich die Job-Queue (geplant)
3. **Priority-Lanes** — schnelle Jobs (unzip) vs. langsame Jobs (OCR) in separaten Queues (geplant)

## Warum kein externer Job-Service?

Ein externer Service hätte:
- Eigenes Auth-Handling (Token, OIDC)
- Eigenes File-Handling (Download via WebDAV, Upload via WebDAV)
- Eigene Berechtigungsprüfung (darf der User das?)
- Eigene Container-Konfiguration (Networking, Volumes)
- Kommunikations-Overhead für Status-Updates zurück zur Cloud

All das ist in der internen Engine **kostenlos** — sie lebt im selben Prozess, hat denselben Kontext, spricht dieselben internen APIs.
