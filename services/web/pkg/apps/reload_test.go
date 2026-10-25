package apps_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/web/pkg/apps"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config"
)

func TestReloader_ExternalApps(t *testing.T) {
	g := gomega.NewWithT(t)

	configDir := t.TempDir()
	appsDir := t.TempDir()
	t.Setenv("OC_CONFIG_DIR", configDir)

	appsFile := filepath.Join(configDir, "apps.yaml")
	modTime := time.Now()
	// every write gets a new modification time, the filesystem resolution must not hide a change
	writeFile := func(name, content string) {
		g.Expect(os.MkdirAll(filepath.Dir(name), 0o755)).To(gomega.Succeed())
		g.Expect(os.WriteFile(name, []byte(content), 0o644)).To(gomega.Succeed())
		modTime = modTime.Add(time.Second)
		g.Expect(os.Chtimes(name, modTime, modTime)).To(gomega.Succeed())
	}
	appConfig := func(externalApps []config.ExternalApp, id string) map[string]any {
		for _, externalApp := range externalApps {
			if externalApp.ID == id {
				return externalApp.Config
			}
		}
		return nil
	}

	writeFile(filepath.Join(appsDir, "chat", "manifest.json"), `{"entrypoint": "index.js"}`)
	writeFile(filepath.Join(appsDir, "chat", "index.js"), `// chat`)
	writeFile(appsFile, "chat:\n  config:\n    models: [first]\n")

	initialConfig := map[string]config.App{"chat": {Config: map[string]any{"models": []any{"first"}}}}
	baseApps := []config.ExternalApp{{ID: "base", Path: "/base.js"}}

	reloader := apps.NewReloader(log.NopLogger(), initialConfig, "/assets/apps", baseApps, os.DirFS(appsDir))

	externalApps := reloader.ExternalApps()
	g.Expect(externalApps).To(gomega.HaveLen(2))
	g.Expect(appConfig(externalApps, "base")).To(gomega.BeNil())
	g.Expect(appConfig(externalApps, "chat")).To(gomega.HaveKeyWithValue("models", []any{"first"}))

	// a changed apps.yaml is picked up without restart
	writeFile(appsFile, "chat:\n  config:\n    models: [first, second]\n")
	externalApps = reloader.ExternalApps()
	g.Expect(externalApps).To(gomega.HaveLen(2))
	g.Expect(appConfig(externalApps, "chat")).To(gomega.HaveKeyWithValue("models", []any{"first", "second"}))

	// a broken apps.yaml keeps the previous state
	writeFile(appsFile, "chat:\n  config:\n    models: [first\n")
	externalApps = reloader.ExternalApps()
	g.Expect(appConfig(externalApps, "chat")).To(gomega.HaveKeyWithValue("models", []any{"first", "second"}))

	// the local config.json of the application has priority and is picked up as well
	writeFile(appsFile, "chat:\n  config:\n    models: [third]\n")
	writeFile(filepath.Join(appsDir, "chat", "config.json"), `{"config": {"models": ["local"]}}`)
	externalApps = reloader.ExternalApps()
	g.Expect(appConfig(externalApps, "chat")).To(gomega.HaveKeyWithValue("models", []any{"local"}))

	// a disabled application disappears, the base apps stay
	writeFile(filepath.Join(appsDir, "chat", "config.json"), `{"disabled": true}`)
	externalApps = reloader.ExternalApps()
	g.Expect(externalApps).To(gomega.HaveLen(1))
	g.Expect(externalApps[0].ID).To(gomega.Equal("base"))

	// a new application directory is found
	writeFile(filepath.Join(appsDir, "notes", "manifest.json"), `{"entrypoint": "index.js"}`)
	writeFile(filepath.Join(appsDir, "notes", "index.js"), `// notes`)
	externalApps = reloader.ExternalApps()
	g.Expect(externalApps).To(gomega.HaveLen(2))
	g.Expect(appConfig(externalApps, "notes")).To(gomega.BeNil())
}
