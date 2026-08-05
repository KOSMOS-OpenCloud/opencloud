# TASK: Rekursives Archiv-Browsing in archivefs

## Ziel

Archive innerhalb von Archiven navigierbar machen (z.B. ZIP in ISO, ISO in ISO, 7z in ZIP etc.) — alle unterstützten Formate beliebig verschachtelbar.

## Betroffene Dateien

- `reva/pkg/storage/fs/archivefs/archivefs.go` — Cache, ListFolder, Stat, Download, ID-Schema
- `reva/pkg/storage/fs/archivefs/diskfs.go` — OpenFromReader ergänzen
- `reva/pkg/storage/fs/archivefs/zip.go` — OpenFromReader ergänzen
- `reva/pkg/storage/fs/archivefs/sevenz.go` — OpenFromReader ergänzen
- `reva/pkg/storage/utils/decomposedfs/archive.go` — Archiv-Kette auflösen
- `reva/pkg/storage/utils/decomposedfs/node/node.go` — IsArchive-Prüfung für innere Dateien

## Architektur-Entscheidungen

### ID-Schema: Kette von Archiv-Ebenen

```
nodeID!arc.base64(path/to/inner.zip)!arc.base64(dir/file.txt)
```

Jedes `!arc.`-Segment = eine Archiv-Grenze. Letztes Segment = finaler innerPath.

### Opener-Interface erweitern

```go
type Opener interface {
    Open(diskPath string) (fs.FS, io.Closer, error)
    OpenFromReader(r io.ReaderAt, size int64, name string) (fs.FS, io.Closer, error)  // NEU
    CanHandle(name string) bool
    Formats() []ArchiveFormat
}
```

### Kosten nach Format-Kombination

| Äußeres Format | Inneres Format | Zugriffsmethode |
|---|---|---|
| ISO/img/raw → | beliebig | SectionReader (Zero-Copy, kein RAM) |
| ZIP (stored entry) → | beliebig | In RAM laden (Go zip gibt nur io.Reader) |
| ZIP (deflated) → | beliebig | Dekomprimieren in RAM |
| 7z → | beliebig | Dekomprimieren in RAM |
| squashfs → | beliebig | Dekomprimieren in RAM |

### go-diskfs: Kein Temp-File nötig

go-diskfs v1.9.3 bietet `diskfs.OpenBackend(b backend.Storage)`. Das Backend-Interface ist:

```go
type Storage interface {
    fs.File        // Read, Stat, Close
    io.ReaderAt    // Random Access
    io.Seeker      // Seek
    Sys() (*os.File, error)         // → ErrNotSuitable für In-Memory
    Writable() (WritableFile, error) // → ErrIncorrectOpenMode (read-only)
    Path() string                    // → "" für In-Memory
}
```

Ein In-Memory-Backend (`bytes.Reader`) reicht aus. Für ISO-in-ISO kann ein `SectionReader` direkt ohne Kopie verwendet werden.

### Limits (konfigurierbar)

```go
MaxNestedArchiveSize  = 2 * 1024 * 1024 * 1024  // 2 GB — inneres Archiv max
MaxTotalNestingMemory = 4 * 1024 * 1024 * 1024  // 4 GB — gesamt RAM für nested archives
MaxArchiveDepth       = 4                         // max Verschachtelungstiefe
```

Wenn ein inneres Archiv das Size-Limit überschreitet: als normale Datei anzeigen (Download ja, Browsing nein).

## Implementierungsschritte

### 1. ParseArchiveID erweitern — Multi-Level

```go
type ArchiveLevel struct {
    ArchiveNodeID string  // nur bei Level 0 relevant (physischer Node)
    InnerPath     string  // Pfad innerhalb dieser Archiv-Ebene
}

func ParseArchiveID(opaqueID string) (levels []ArchiveLevel, ok bool)
```

Mehrere `!arc.`-Segmente iterativ parsen.

### 2. OpenFromReader für jeden Opener

- **ZIP**: `zip.NewReader(r io.ReaderAt, size int64)` — direkt unterstützt, kein Wrapper nötig
- **diskfs (ISO/img/etc.)**: `memBackend` implementieren → `diskfs.OpenBackend()`
- **7z**: prüfen ob Library ReaderAt unterstützt, sonst bytes.Reader

### 3. Cache erweitern — Nested Keys

```go
// Cache-Key für verschachtelte Archive:
// "/disk/path/to/outer.iso" + "!" + "inner/path/to/nested.zip"
func (c *Cache) GetNested(outerFS fs.FS, innerPath string) (*CachedArchive, error)
```

- Eviction: Nested-Entry invalidieren wenn Parent evicted wird
- Size-Check VOR dem Laden (fs.Stat auf innere Datei)

### 4. ListFolder: Archiv-Dateien als Container markieren

In ListFolder, wenn eine Datei `IsArchiveName(name)` erfüllt UND unter dem Size-Limit liegt:
- `Type` = `RESOURCE_TYPE_CONTAINER` statt `RESOURCE_TYPE_FILE`
- Optional: Attribut setzen um Client zu signalisieren dass es ein Archiv-Container ist

### 5. resolveArchiveChain in decomposedfs/archive.go

```go
func (fs *Decomposedfs) resolveArchiveChain(ctx context.Context, n *node.Node, levels []ArchiveLevel) (*CachedArchive, string, error) {
    // Level 0: physisches Archiv
    current, err := zipfs.GlobalCache().Get(n.InternalPath())

    // Level 1..n-1: verschachtelte Archive öffnen
    for i, lvl := range levels[1:] {
        if i >= MaxArchiveDepth { return nil, "", ErrMaxDepthExceeded }

        info, _ := fs.Stat(current.FS, lvl.InnerPath)
        if info.Size() > MaxNestedArchiveSize { return nil, "", ErrTooLarge }

        current, err = openFromFS(current.FS, lvl.InnerPath)
    }

    return current, levels[len(levels)-1].InnerPath, nil
}
```

### 6. Size-Check und Ablehnung

Wenn `MaxNestedArchiveSize` überschritten:
- ListFolder zeigt die Datei als `RESOURCE_TYPE_FILE` (nicht navigierbar)
- Stat gibt normalen File-Typ zurück
- Download funktioniert weiterhin (Stream aus äußerem Archiv)

## Nicht im Scope

- Schreibzugriff in verschachtelte Archive (bleibt read-only)
- Temp-File-Fallback (bewusst vermieden — nur In-Memory mit Limits)
- Automatisches Entpacken im Hintergrund
