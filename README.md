![KOSMOS Logo](logo.png)

# OpenCore — Server Backend

OpenCore ist der zentrale, revisionssichere und geschützte Datenort, der
digitale Prozesse vertikal und horizontal skaliert.

OpenCore besteht aus zwei Komponenten:

- **OpenCloud** (dieses Repo) — das Server-Backend: Go-Services für
  WebDAV, CS3-API, Suche, Job-Engine, IDP, Collaboration-Anbindung.
- **OpenCosmos** (Komponenten: open_taki, microllm, Qdrant, Collabora) —
  die KI- und Analyse-Schicht: LLM-basierte Dokumentenanalyse,
  Semantiksuche, Audio-Transkription.

Basis ist [OpenCloud](https://github.com/opencloud-eu/opencloud)
(opencloud-eu, Apache 2.0). Die KOSMOS-Erweiterungen liegen im Branch
`kosmos`.

## Funktionen

- Revisionssicherer, unveränderbarer Dateispeicher (Immutable Spaces)
- Volltext- und Semantiksuche (Bleve + Qdrant)
- KI-gestützte Dokumentenverarbeitung (open_taki: OCR, Zusammenfassung,
  Klassifikation, Transkription)
- Metadatenpflege inklusive Aktenzeichen
- WebDAV, Web-Client, Mobile Apps
- Online-Office (Collabora)
- Job-Engine mit typisierten Pipelines und Worker-Registry
- Integration von Scanner, E-Mail-Posteingang und Dokumentenworkflows

## Komponenten des OpenCore-Stacks

| Komponente | Zweck | Repo |
|---|---|---|
| opencloud | Server-Backend (dieses Repo) | KOSMOS-OpenCloud/opencloud |
| opencloud_reva | Storage- und Orchestrierungsschicht (Reva) | KOSMOS-OpenCloud/opencloud_reva |
| opencloud_web | Web-Client (Vue.js) | KOSMOS-OpenCloud/opencloud_web |
| open_taki | KI-Dokumentenanalyse (Tika-Ersatz) | KOSMOS-EU/open_taki |
| microllm | LLM-Routing und Loadbalancing | — |

## Build

``` console
make generate
make -C opencloud build
opencloud/bin/opencloud init && opencloud/bin/opencloud server
```

Für das KOSMOS-Build (Container-Image) siehe `BUILD_HOWTO.md` und
`build_kosmos.sh`.

## Technology

- **Authentication**: OpenID Connect via eingebautem IDP
  (LibreGraph Connect) oder externem IdP
- **Storage**: Dateisystem (kein Datenbank-Abhängigkeit), Reva als
  Storage-Backend
- **Search**: Bleve (Metadaten, Tags, Favoriten) + Qdrant (semantisch)
- **AI**: open_taki → microllm → lokale LLM-Backends (vLLM)

## Security

Sicherheitsrelevante Probleme: [info@kosmos.technology](mailto:info@kosmos.technology)

## License

[Apache 2.0](LICENSE)
