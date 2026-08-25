![KOSMOS Logo](logo.png)

# OpenCore — Server Backend

OpenCore ist der zentrale, revisionssichere und geschützte Datenort, der
digitale Prozesse vertikal und horizontal skaliert.

OpenCore basiert auf [OpenCloud](https://github.com/opencloud-eu/opencloud)
(opencloud-eu, Apache 2.0) und OpenCosmos-Komponenten. Dieses Repo ist
das Server-Backend des OpenCore-Stacks. Die KOSMOS-Erweiterungen liegen
im Branch `kosmos`.

## OpenCore-Komponenten

| Komponente | Zweck |
|---|---|
| **opencloud** (dieses Repo) | Server-Backend: Go-Services, WebDAV, CS3-API, Suche, IDP |
| **opencloud_reva** | Storage- und Orchestrierungsschicht (Reva-Backend) |
| **opencloud_web** | Web-Client (Vue.js) |
| **open_taki** | KI-Dokumentenanalyse (Tika-Ersatz) |
| **openyard** | DMS-Adapter für Legacy-DMS-Clients |

OpenCloud und OpenCloud Web sind keine Upstream-Repos mehr — sie sind
OpenCore, also OpenCloud + OpenCosmos.

## OpenCosmos-Features in opencloud

- **Job-Engine**: typisierte Pipelines, Worker-Registry, NATS/Events
- **Subspaces**: isolierte Datenbereiche pro Space
- **Neue Rollen**: erweitertes Rechtemodell
- **Chat-Engine**: Kollaboration und Kommunikation
- **Todo-Engine**: Aufgabenmanagement
- **Immutable Spaces**: revisionssicherer, unveränderbarer Dateispeicher
- **Volltext- und Semantiksuche** (Bleve + Qdrant)
- **KI-Dokumentenverarbeitung** (open_taki: OCR, Zusammenfassung,
  Klassifikation, Transkription)
- **Metadatenpflege** inklusive Aktenzeichen
- **Online-Office** (Collabora)
- **Integration** von Scanner, E-Mail-Posteingang und Dokumentenworkflows

## Build

``` console
make generate
make -C opencloud build
opencloud/bin/opencloud init && opencloud/bin/opencloud server
```

Für das OpenCore-Container-Build siehe `build_kosmos.sh`.

## Technology

- **Authentication**: OpenID Connect via eingebautem IDP
  (LibreGraph Connect) oder externem IdP
- **Storage**: Dateisystem, Reva als Storage-Backend
- **Search**: Bleve (Metadaten, Tags, Favoriten) + Qdrant (semantisch)
- **AI**: open_taki → microllm → lokale LLM-Backends (vLLM)
- **Events**: NATS Jetstream

## Security

Sicherheitsrelevante Probleme:
[info@kosmos.technology](mailto:info@kosmos.technology)

## License

[Apache 2.0](LICENSE)
