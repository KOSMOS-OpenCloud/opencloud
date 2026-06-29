# MediaConverter Service

## Ziel
OpenCloud Service der Dateikonvertierungen orchestriert. Pipeline-basiert, YAML-konfiguriert, mit Job-Queue und Parallelität. Converter sind feste Bestandteile des Service (einmountbare Scripte), die bei Bedarf externe Sidecars (Collabora, Whisper, etc.) nutzen.

## Architektur

```
User → Context Menu → POST /api/v0/mediawork → Job Queue
                                                    │
                                              ┌─────┴─────┐
                                              │ Pipeline   │
                                              │ Executor   │
                                              └─────┬─────┘
                                                    │
                                    ┌───────────────┼───────────────┐
                                    ▼               ▼               ▼
                              local exec      HTTP sidecar    script mount
                              (pandoc,        (Collabora,     (custom .sh
                               ffmpeg)         Whisper)        in /converters/)
                                    │               │               │
                                    └───────────────┼───────────────┘
                                                    ▼
                                              Write result
                                              via WebDAV
```

## YAML Config Schema

```yaml
# /etc/opencloud/mediaconverter.yaml

service:
  max_workers: 4          # parallele Jobs
  queue_size: 100         # max wartende Jobs
  converter_dir: /converters  # eingemountete Scripte

pipelines:
  doc-to-pdf:
    label: "Als PDF exportieren"
    icon: "file-pdf"
    source_types:
      - "application/msword"
      - "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
      - "application/vnd.oasis.opendocument.text"
    target:
      extension: ".pdf"
      location: "same"           # same | subfolder:<name> | sibling:<name> | prompt
      create_dirs: true
    converter: collabora-pdf
    user_choosable_target: false

  md-to-pdf:
    label: "Markdown als PDF"
    icon: "file-pdf"
    source_types:
      - "text/markdown"
    target:
      extension: ".pdf"
      location: "same"
    converter: pandoc-pdf
    user_choosable_target: true
    options:
      template: default
      variables:
        author: "{{user.displayName}}"
        date: "{{now}}"
        organization: "{{space.name}}"

  video-to-mp4:
    label: "Als MP4 konvertieren"
    icon: "video"
    source_types:
      - "video/x-matroska"
      - "video/avi"
      - "video/webm"
    target:
      extension: ".mp4"
      location: "same"
    converter: ffmpeg-mp4
    user_choosable_target: false
    options:
      codec: h264
      quality: medium

converters:
  collabora-pdf:
    type: http
    url: "{{COLLABORA_URL}}/cool/convert-to/pdf"
    method: POST
    upload_field: "file"
    timeout: 120s

  pandoc-pdf:
    type: exec
    command: "pandoc"
    args:
      - "{{source}}"
      - "-o"
      - "{{target}}"
      - "--pdf-engine=xelatex"
      - "--variable=author:{{options.author}}"
      - "--variable=date:{{options.date}}"
    timeout: 60s

  ffmpeg-mp4:
    type: exec
    command: "ffmpeg"
    args: ["-i", "{{source}}", "-c:v", "{{options.codec}}", "-y", "{{target}}"]
    timeout: 300s

  custom-script:
    type: script
    path: "convert.sh"     # relativ zu converter_dir
    timeout: 120s
```

## API

### Job starten
```
POST /api/v0/mediawork
{
  "pipeline": "md-to-pdf",
  "resourceId": "storageId$spaceId!opaqueId",
  "targetPath": "/exports/",       // optional, nur wenn user_choosable_target
  "createTarget": true
}

→ 202 Accepted
{
  "jobId": "uuid",
  "status": "queued",
  "pipeline": "md-to-pdf"
}
```

### Job Status abfragen
```
GET /api/v0/mediawork/{jobId}

→ 200 OK
{
  "jobId": "uuid",
  "status": "completed",          // queued | running | completed | failed
  "progress": 100,
  "targetResourceId": "...",
  "targetName": "document.pdf",
  "error": null
}
```

### Verfügbare Pipelines abfragen
```
GET /api/v0/mediawork/pipelines

→ 200 OK
{
  "pipelines": [
    {
      "id": "doc-to-pdf",
      "label": "Als PDF exportieren",
      "icon": "file-pdf",
      "sourceTypes": ["application/msword", ...],
      "userChoosableTarget": false
    },
    ...
  ]
}
```

## Backend (Go Service)

### Verzeichnisstruktur
```
services/mediaconverter/
├── pkg/
│   ├── command/          # CLI commands (server, health)
│   ├── config/           # Config structs + YAML loader
│   ├── service/
│   │   ├── service.go    # Job queue, worker pool
│   │   ├── http.go       # API handler
│   │   ├── pipeline.go   # Pipeline executor
│   │   ├── converter.go  # Converter interface + implementations
│   │   │                 # - HttpConverter (Collabora etc.)
│   │   │                 # - ExecConverter (pandoc, ffmpeg)
│   │   │                 # - ScriptConverter (mounted scripts)
│   │   └── template.go   # {{variable}} resolver
│   └── server/
│       └── http/         # HTTP server setup
└── Makefile
```

### Job Flow
1. API empfängt Request, validiert Pipeline + Permissions
2. Job in Queue (channel mit buffer = queue_size)
3. Worker nimmt Job, lädt Quelldatei via Gateway/WebDAV
4. Speichert als Temp-Datei
5. Führt Converter aus (HTTP POST / exec / script)
6. Liest Ergebnis
7. Schreibt via WebDAV an Zielort (ggf. mkdir)
8. Setzt Job-Status auf completed

### Template-Variablen
- `{{source}}` — Pfad zur Temp-Quelldatei
- `{{target}}` — Pfad zur Temp-Zieldatei
- `{{user.id}}`, `{{user.displayName}}`, `{{user.email}}`
- `{{space.name}}`, `{{space.id}}`
- `{{resource.name}}`, `{{resource.path}}`
- `{{now}}` — aktuelles Datum (ISO)
- `{{options.*}}` — aus Pipeline-Config

### Converter Interface
```go
type Converter interface {
    Convert(ctx context.Context, job *Job) error
}

type Job struct {
    ID          string
    Pipeline    PipelineConfig
    Source      string            // temp file path
    Target      string            // temp file path
    User        User
    Space       Space
    Resource    Resource
    Options     map[string]string // resolved template vars
    Status      JobStatus
    Progress    int
    Error       error
}
```

## Frontend (Web Extension)

### Context-Menu Integration
- Beim Start: `GET /api/v0/mediawork/pipelines` laden
- Pro Pipeline eine `ActionExtension` registrieren
- Filter: `resource.mimeType` ∈ `pipeline.sourceTypes`
- Klick → `POST /api/v0/mediawork` → Toast mit Fortschritt
- Bei `user_choosable_target`: Dialog für Zielort

### Dateien
```
packages/web-pkg/src/composables/actions/files/useFileActionsMediaConvert.ts
```

## Container / Deployment

### Converter im Container
```dockerfile
RUN apk add --no-cache pandoc texlive-xetex ffmpeg
```

### Einmountbare Scripte
```yaml
# docker-compose.yml
volumes:
  - ./converters:/converters:ro
```

### Sidecars
```yaml
# Collabora ist bereits im Stack
# Weitere Sidecars nach Bedarf
```

## Implementierungsreihenfolge

1. [ ] Config Schema + YAML Loader
2. [ ] Job Queue + Worker Pool
3. [ ] Converter Interface + HttpConverter (Collabora doc→pdf)
4. [ ] API Endpoints (POST job, GET status, GET pipelines)
5. [ ] ExecConverter (pandoc md→pdf)
6. [ ] ScriptConverter (mounted scripts)
7. [ ] Template-Variable Resolver
8. [ ] Web: Pipeline-Actions im Context Menu
9. [ ] Web: Job Status Toast/Progress
10. [ ] Web: Zielort-Dialog (wenn user_choosable_target)
11. [ ] Tests

## Offene Punkte
- Batch-Konvertierung (mehrere Dateien auf einmal)?
- Konvertierungshistorie / Log pro Space?
- Quota-Prüfung vor Schreiben?
- Webhook/Event bei Job-Completion?
