# Backend Job Engine + Media Converter Addon

## Architektur: Drei Schichten

```
┌─────────────────────────────────────────────────────────────────┐
│  Schicht 3: Addons (Converter, Entpacker, Exporter, ...)       │
│                                                                 │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────────────┐    │
│  │ doc-converter │ │ zip-unpacker │ │ folderviews-exporter │    │
│  │ (doc→pdf,    │ │ (zip→ordner, │ │ (aktenplan→pdf,      │    │
│  │  md→pdf,     │ │  tar→ordner) │ │  register→csv)       │    │
│  │  odt→pdf)    │ │              │ │                      │    │
│  └──────┬───────┘ └──────┬───────┘ └──────────┬───────────┘    │
│         │                │                     │                │
│  YAML Config      YAML Config           YAML Config            │
│  + Sidecar        + local exec          + custom script        │
│  (Collabora)      (unzip, tar)          (pandoc+latex)         │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│  Schicht 2: Web Extension Points (web/)                        │
│                                                                 │
│  ┌────────────────────────────────────────────────────────┐     │
│  │ Extension Point: app.files.job-actions                 │     │
│  │                                                        │     │
│  │ - Registriert Actions im Context Menu                  │     │
│  │ - Filtert nach MIME-Type / Dateiendung                 │     │
│  │ - Startet Jobs via API                                 │     │
│  │ - Zeigt Progress-Toast / Status                        │     │
│  │ - Zielort-Dialog (wenn Addon es erlaubt)               │     │
│  └────────────────────────────────────────────────────────┘     │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│  Schicht 1: Job Engine (opencloud/)                            │
│                                                                 │
│  ┌────────────────────────────────────────────────────────┐     │
│  │ services/jobengine/                                    │     │
│  │                                                        │     │
│  │ - Job Queue (channel-basiert, konfigurierbar)          │     │
│  │ - Worker Pool (max_workers parallel)                   │     │
│  │ - Job Status (queued → running → completed/failed)     │     │
│  │ - Pipeline YAML Loader                                 │     │
│  │ - Executor Interface (HTTP, exec, script)              │     │
│  │ - Template-Variable Resolver                           │     │
│  │ - Datei-Download/Upload via Gateway                    │     │
│  │ - REST API                                             │     │
│  └────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────┘
```

## Schicht 1: Job Engine (opencloud/)

Minimalinvasiv. Ein neuer Service `jobengine` der:
- Jobs annimmt, queued, parallel ausführt
- Pipeline-Definitionen aus YAML liest (einmountbar)
- Dateien via Gateway liest/schreibt
- Externe Tools orchestriert (HTTP-Call, exec, Script)
- Status + Progress bereitstellt

### API

```
# Verfügbare Pipelines (Addons registrieren sich hier)
GET /api/v0/jobs/pipelines
→ {
    "pipelines": [
      {
        "id": "doc-to-pdf",
        "label": "Als PDF exportieren",
        "icon": "file-pdf",
        "sourceTypes": ["application/msword", ...],
        "targetLocation": "same",
        "userChoosableTarget": false,
        "batch": true
      }
    ]
  }

# Job starten (einzeln oder batch)
POST /api/v0/jobs
{
  "pipeline": "doc-to-pdf",
  "resources": ["storageId$spaceId!opaqueId", ...],
  "targetPath": "/exports/",         # optional
  "createTarget": true                # Verzeichnisse anlegen
}
→ 202 Accepted
{
  "jobId": "uuid",
  "status": "queued",
  "pipeline": "doc-to-pdf",
  "resourceCount": 3
}

# Job Status
GET /api/v0/jobs/{jobId}
→ {
    "jobId": "uuid",
    "status": "running",            # queued | running | completed | failed
    "progress": 66,                 # Prozent (2 von 3 fertig)
    "completed": 2,
    "total": 3,
    "results": [
      {"source": "brief.docx", "target": "brief.pdf", "status": "completed"},
      {"source": "vertrag.odt", "target": "vertrag.pdf", "status": "completed"},
      {"source": "notiz.md", "target": "notiz.pdf", "status": "running"}
    ],
    "error": null
  }

# Alle Jobs des Users
GET /api/v0/jobs?status=running
→ { "jobs": [...] }

# Job abbrechen
DELETE /api/v0/jobs/{jobId}
→ 204 No Content
```

### Pipeline YAML Schema

```yaml
# /etc/opencloud/jobs/pipelines.yaml (einmountbar)

service:
  max_workers: 4
  queue_size: 100
  temp_dir: /tmp/jobs
  pipeline_dirs:                    # Addons mounten ihre YAMLs hier
    - /etc/opencloud/jobs/pipelines.d/

# Eingebaute Pipelines (optional, können auch leer sein)
pipelines: {}

# Executor-Typen (eingebaut)
# - http:   HTTP POST an Sidecar (Collabora, Whisper, ...)
# - exec:   lokaler Prozess (pandoc, ffmpeg, unzip, ...)
# - script: eingemountetes Shell-Script
```

### Addon Pipeline YAML (eingemountet)

```yaml
# /etc/opencloud/jobs/pipelines.d/doc-converter.yaml
# (eingemountet vom Addon-Container oder Host)

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
      location: "same"
      create_dirs: true
    user_choosable_target: false
    batch: true
    executor:
      type: http
      url: "${COLLABORA_URL}/cool/convert-to/pdf"
      method: POST
      upload_field: "file"
      timeout: 120s

  md-to-pdf:
    label: "Markdown als PDF"
    icon: "file-pdf"
    source_types:
      - "text/markdown"
    target:
      extension: ".pdf"
      location: "same"
    user_choosable_target: true
    batch: true
    executor:
      type: exec
      command: "pandoc"
      args:
        - "{{source}}"
        - "-o"
        - "{{target}}"
        - "--pdf-engine=xelatex"
        - "--variable=author:{{user.displayName}}"
        - "--variable=date:{{now}}"
      timeout: 60s
```

```yaml
# /etc/opencloud/jobs/pipelines.d/archive-tools.yaml

pipelines:
  unzip:
    label: "Hier entpacken"
    icon: "folder-zip"
    source_types:
      - "application/zip"
      - "application/x-zip-compressed"
    target:
      extension: ""                 # Ordner, keine Extension
      location: "subfolder:{{source.nameWithoutExt}}"
      create_dirs: true
    batch: false                    # ein ZIP pro Job
    executor:
      type: exec
      command: "unzip"
      args: ["-o", "{{source}}", "-d", "{{target_dir}}"]
      timeout: 300s

  tar-extract:
    label: "Archiv entpacken"
    icon: "folder-zip"
    source_types:
      - "application/x-tar"
      - "application/gzip"
      - "application/x-bzip2"
    target:
      extension: ""
      location: "subfolder:{{source.nameWithoutExt}}"
      create_dirs: true
    batch: false
    executor:
      type: exec
      command: "tar"
      args: ["xf", "{{source}}", "-C", "{{target_dir}}"]
      timeout: 300s
```

### Executor Interface

```go
// Schicht 1: opencloud/ — eingebaut, generisch
type Executor interface {
    Execute(ctx context.Context, job *JobItem) error
}

type JobItem struct {
    Source      string            // temp file path (heruntergeladen)
    Target     string            // temp file path (Ergebnis)
    TargetDir  string            // temp dir (für Entpacken)
    User       UserInfo          // wer hat den Job gestartet
    Space      SpaceInfo
    Resource   ResourceInfo
    Options    map[string]string // aufgelöste Template-Variablen
}

// Eingebaute Executors:
type HttpExecutor struct { ... }    // POST Datei an URL, Ergebnis zurück
type ExecExecutor struct { ... }    // lokaler Prozess ausführen
type ScriptExecutor struct { ... }  // Shell-Script aus pipeline_dirs
```

### Template-Variablen

```
{{source}}              Pfad zur Temp-Quelldatei
{{target}}              Pfad zur Temp-Zieldatei
{{target_dir}}          Pfad zum Temp-Zielverzeichnis (Entpacken)
{{source.name}}         Dateiname mit Extension
{{source.nameWithoutExt}}  Dateiname ohne Extension
{{source.ext}}          Extension
{{user.id}}             User ID
{{user.displayName}}    Anzeigename
{{user.email}}          E-Mail
{{space.name}}          Space-Name
{{space.id}}            Space-ID
{{resource.name}}       Resource-Name
{{resource.path}}       Pfad im Space
{{now}}                 Aktuelles Datum (ISO)
{{now.date}}            Nur Datum (YYYY-MM-DD)
${ENV_VAR}              Umgebungsvariable (z.B. ${COLLABORA_URL})
```

### Verzeichnisstruktur (opencloud/)

```
services/jobengine/
├── pkg/
│   ├── command/
│   │   ├── root.go
│   │   └── server.go
│   ├── config/
│   │   ├── config.go          # Service config
│   │   └── pipeline.go        # Pipeline YAML structs
│   ├── service/
│   │   ├── service.go         # Queue + Worker Pool
│   │   ├── http.go            # REST API handler
│   │   ├── executor.go        # Executor interface
│   │   ├── executor_http.go   # HTTP POST executor
│   │   ├── executor_exec.go   # local exec executor
│   │   ├── executor_script.go # mounted script executor
│   │   ├── template.go        # {{variable}} resolver
│   │   ├── fileio.go          # Download/Upload via Gateway
│   │   └── service_test.go
│   └── server/
│       └── http/
│           └── server.go
└── Makefile
```

## Schicht 2: Web Extension Point (web/)

Minimal: ein Extension Point den Addons nutzen können.

```typescript
// Extension Point Definition
const jobActionsExtensionPoint = {
  id: 'app.files.job-actions',
  type: 'action'
}
```

```typescript
// Generischer Job-Service (web-pkg)
// useJobService.ts
export const useJobService = () => {
  const startJob = async (pipeline: string, resourceIds: string[], opts?) => {
    return httpClient.post('/api/v0/jobs', { pipeline, resources: resourceIds, ...opts })
  }

  const getJobStatus = async (jobId: string) => {
    return httpClient.get(`/api/v0/jobs/${jobId}`)
  }

  const pollJob = (jobId: string, onProgress: (job) => void) => { ... }

  return { startJob, getJobStatus, pollJob }
}
```

```typescript
// Generischer Progress-Toast (web-pkg)
// useJobProgress.ts — zeigt Toast mit Fortschritt, nutzt pollJob
```

### Dateien (web/)

```
packages/web-pkg/src/
  composables/
    jobs/
      useJobService.ts        # API Client
      useJobProgress.ts       # Toast + Polling
      index.ts
  extensionPoints.ts          # + jobActionsExtensionPoint
```

## Schicht 3: Addons

Addons sind eigenständige Pakete die:
1. Pipeline-YAMLs bereitstellen (einmountbar ins Backend)
2. Web-Extensions registrieren (Context-Menu Actions)
3. Ggf. Sidecar-Container mitbringen (Collabora, pandoc)

### Addon: doc-converter

```
opencloud_doc_converter/
├── pipelines/
│   └── doc-converter.yaml     # → mount nach /etc/opencloud/jobs/pipelines.d/
├── web-extension/
│   └── src/
│       └── index.ts           # registriert "Als PDF" Action
├── Dockerfile                 # pandoc + texlive (Sidecar oder im Container)
└── README.md
```

```typescript
// web-extension/src/index.ts
export default defineWebApplication({
  setup() {
    const jobService = useJobService()
    const { showJobProgress } = useJobProgress()

    // Actions aus Backend-Pipelines laden
    const actions = await jobService.getPipelines()

    // Pro Pipeline eine Context-Menu Action registrieren
    return {
      extensions: actions.map(pipeline => ({
        id: `job.${pipeline.id}`,
        extensionPointIds: ['app.files.context-actions'],
        type: 'action',
        action: {
          name: pipeline.id,
          label: () => pipeline.label,
          icon: pipeline.icon,
          isVisible: ({ resources }) =>
            resources.some(r => pipeline.sourceTypes.includes(r.mimeType)),
          handler: async ({ resources }) => {
            const job = await jobService.startJob(
              pipeline.id,
              resources.map(r => r.fileId)
            )
            showJobProgress(job.jobId)
          }
        }
      }))
    }
  }
})
```

### Addon: folderviews-exporter (in opencloud_folderviews)

```typescript
// Typ-spezifische Exporter, z.B. "Aktenplan als PDF"
// Nutzt dieselbe Job-API aber mit Kontext aus dem Aktenplan-Typ
```

## Deployment

```yaml
# docker-compose.yml Ergänzung

services:
  opencloud:
    volumes:
      # Job Engine Config
      - ./config/jobs/pipelines.yaml:/etc/opencloud/jobs/pipelines.yaml
      # Addon Pipeline-Configs einmounten
      - ./addons/doc-converter/pipelines/:/etc/opencloud/jobs/pipelines.d/:ro
      # Custom Converter-Scripts einmounten
      - ./converters/:/converters/:ro

  # Sidecar: pandoc + texlive (für md→pdf)
  pandoc:
    image: pandoc/extra:latest
    # oder: ins opencloud Image einbauen (apk add pandoc texlive-xetex)
```

## Implementierungsreihenfolge

### Phase 1: Job Engine (opencloud/)
1. [ ] Config structs + Pipeline YAML Loader (inkl. pipelines.d/ Scanning)
2. [ ] Job Queue + Worker Pool
3. [ ] Executor Interface + ExecExecutor
4. [ ] Template-Variable Resolver
5. [ ] FileIO: Download/Upload via Gateway
6. [ ] REST API (POST job, GET status, GET pipelines, DELETE job)
7. [ ] HttpExecutor (für Collabora etc.)
8. [ ] ScriptExecutor
9. [ ] Tests

### Phase 2: Web Extension Point (web/)
10. [ ] useJobService (API Client)
11. [ ] useJobProgress (Toast + Polling)
12. [ ] jobActionsExtensionPoint

### Phase 3: Erster Addon (doc-converter)
13. [ ] Pipeline YAML für doc→pdf (Collabora) + md→pdf (pandoc)
14. [ ] Web Extension: Context-Menu Actions aus Pipelines
15. [ ] Test auf cloud.brandis.eu

### Phase 4: Weitere Addons
16. [ ] archive-tools (zip/tar entpacken)
17. [ ] folderviews-exporter (aktenplan→pdf, register→csv)
18. [ ] video-converter (ffmpeg)

## Prinzipien

- **opencloud/ bleibt generisch**: nur Job-Infrastruktur, keine Converter-Logik
- **web/ bleibt generisch**: nur Extension Point + Job-UI, keine Pipeline-Kenntnis
- **Addons sind austauschbar**: Pipeline-YAMLs einmounten, fertig
- **Batch-fähig**: mehrere Dateien in einem Job
- **Abbrechbar**: laufende Jobs können gestoppt werden
- **Transparent**: Status + Progress für den User sichtbar
- **Sicher**: Jobs laufen im User-Kontext (Permissions geprüft)
