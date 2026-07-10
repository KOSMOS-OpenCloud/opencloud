# IDP Login-Seite: Theming & Anpassung

## Architektur

Die IDP Login-Seite ist eine React-App in `services/idp/`. Beim Server-Build wird sie
kompiliert und ins Go-Binary eingebettet (embedded FS). Zur Laufzeit können Assets
über `IDP_ASSET_PATH` vom Dateisystem überschrieben werden (Fallback auf eingebettete).

## Build-Prozess

```
make -C services/idp node-generate-prod
  → pnpm install && pnpm build
  → Build-Output: services/idp/build/
  → Makefile kopiert favicon.png, icon-lilac.svg nach assets/identifier/static/
  → Alles eingebettet ins Go-Binary
```

### Quelldateien

| Datei | Zweck |
|---|---|
| `services/idp/src/app.css` | Haupt-CSS (Farben, Card, Button, Logo) |
| `services/idp/src/fancy-background.css` | Hintergrundbild-Konfiguration |
| `services/idp/public/index.html` | HTML-Template (Favicon-Referenz, CSP-Nonce-Platzhalter) |
| `services/idp/src/images/favicon.png` | Favicon (wird nach assets kopiert) |
| `services/idp/src/images/icon-lilac.svg` | Fallback-Logo wenn kein Hintergrundbild |
| `services/idp/Makefile` | Asset-Kopierregeln |

## Externes Theming ohne Rebuild

### Verzeichnisstruktur

```
web-idp/
└── identifier/
    └── static/
        ├── css/
        │   └── main.{hash}.css    # Original-CSS + Overrides am Ende
        ├── icon-lilac.svg         # Logo-Override (kann PNG sein trotz .svg Name)
        └── favicon.png            # Nur wirksam nach Rebuild (index.html-Referenz)
```

### Compose-Konfiguration

```yaml
services:
  opencloud:
    volumes:
      - ./web-idp:/var/lib/opencloud/web-idp:ro
    environment:
      IDP_ASSET_PATH: "/var/lib/opencloud/web-idp"
```

### Env-Variablen

| Variable | Wirkung |
|---|---|
| `IDP_ASSET_PATH` | Dateisystem-Pfad, der eingebettete Assets überlagert |
| `IDP_LOGIN_BACKGROUND_URL` | Hintergrundbild-URL, wird als `data-bg-img` in HTML injiziert. JS setzt Inline-Style. Leer = kein Bild, Fallback-Logo `icon-lilac.svg` wird angezeigt |

## Fallstricke

### CSS-Hash

Der Build erzeugt `main.{hash}.css` — bei CSS-Änderungen ändert sich der Hash.
Die `index.html` referenziert den Hash automatisch. Aber `IDP_ASSET_PATH`-Overlays
müssen den **exakten Dateinamen** matchen. Nach jedem `build-opencloud` prüfen:

```bash
curl -s https://cloud.example.com/signin/v1/identifier | grep -o 'main\.[^"]*\.css'
```

### CSP-Nonce

`index.html` enthält `__CSP_NONCE__` Platzhalter, der zur Laufzeit pro Request ersetzt
wird. Deshalb kann `index.html` **nicht** über `IDP_ASSET_PATH` überschrieben werden —
der statische Nonce würde Scripts/Styles blockieren.

### Hintergrundbild vs CSS

`IDP_LOGIN_BACKGROUND_URL` wird per JS als Inline-Style `backgroundImage: url(...)` gesetzt.
Inline-Styles überschreiben CSS-Regeln (auch mit `!important`). Wenn kein Hintergrundbild
gewollt: Variable leer lassen oder nicht setzen.

### Favicon

Das Favicon wird in `public/index.html` referenziert und ins Build-Ergebnis eingebettet.
Es kann **nicht** über `IDP_ASSET_PATH` geändert werden (da `index.html` nicht
überschreibbar ist, s.o.). Favicon-Änderungen erfordern einen Server-Rebuild.

### pnpm-Version

`services/idp/package.json` pinnt `pnpm@11.1.3`. Muss zur Node-Version im
Build-Container passen (`quay.io/opencloudeu/nodejs-ci:24`).

## Kosmos-Anpassungen (Branch kosmos)

- Hintergrundbild entfernt (`fancy-background.css`)
- Footer ausgeblendet
- Card: 360px, 12px Radius, dezenter Schatten
- Logo: 56px Höhe
- Favicon: `favicon.png` (Kosmos-Logo)
