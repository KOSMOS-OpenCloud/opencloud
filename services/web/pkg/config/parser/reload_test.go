package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/gomega"

	"github.com/opencloud-eu/opencloud/services/web/pkg/config"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config/defaults"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config/parser"
)

func TestReloadWeb(t *testing.T) {
	g := gomega.NewWithT(t)

	configDir := t.TempDir()
	t.Setenv("OC_CONFIG_DIR", configDir)
	// an empty variable still counts as set, the variables have to be absent
	unsetEnv := func(name string) {
		t.Setenv(name, "restored after the test")
		g.Expect(os.Unsetenv(name)).To(gomega.Succeed())
	}
	unsetEnv("OC_URL")
	unsetEnv("WEB_UI_THEME_SERVER")
	unsetEnv("WEB_UI_CONFIG_FILE")

	webFile := filepath.Join(configDir, "web.yaml")
	writeFile := func(name, content string) {
		g.Expect(os.WriteFile(name, []byte(content), 0o644)).To(gomega.Succeed())
	}

	runningCfg := defaults.FullDefaultConfig()
	runningWeb := runningCfg.Web

	// without any file the defaults are delivered
	reloadedWeb, err := parser.ReloadWeb(runningCfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reloadedWeb.ThemePath).To(gomega.Equal(runningWeb.ThemePath))
	g.Expect(reloadedWeb.Config.ExternalApps).To(gomega.BeEmpty())

	// web.yaml is read, the running configuration stays untouched
	writeFile(webFile, `
web:
  theme_server: https://themes.example
  config:
    server: https://cloud.example
    external_apps:
      - id: fixed
        path: /fixed.js
`)
	reloadedWeb, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reloadedWeb.ThemeServer).To(gomega.Equal("https://themes.example"))
	g.Expect(reloadedWeb.Config.Server).To(gomega.Equal("https://cloud.example"))
	g.Expect(reloadedWeb.Config.ExternalApps).To(gomega.Equal([]config.ExternalApp{{ID: "fixed", Path: "/fixed.js"}}))
	g.Expect(runningCfg.Web).To(gomega.Equal(runningWeb))

	// the environment still wins over the file
	t.Setenv("WEB_UI_THEME_SERVER", "https://env.example")
	reloadedWeb, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reloadedWeb.ThemeServer).To(gomega.Equal("https://env.example"))
	unsetEnv("WEB_UI_THEME_SERVER")

	// the web section of opencloud.yaml is read as well, web.yaml wins
	writeFile(filepath.Join(configDir, "opencloud.yaml"), "web:\n  web:\n    theme_path: /themes/custom/theme.json\n    config:\n      server: https://ignored.example\n")
	reloadedWeb, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reloadedWeb.ThemePath).To(gomega.Equal("/themes/custom/theme.json"))
	g.Expect(reloadedWeb.Config.Server).To(gomega.Equal("https://cloud.example"))

	// the json config file takes precedence
	jsonFile := filepath.Join(configDir, "config.json")
	writeFile(jsonFile, `{"server": "https://json.example"}`)
	t.Setenv("WEB_UI_CONFIG_FILE", jsonFile)
	reloadedWeb, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(reloadedWeb.Config.Server).To(gomega.Equal("https://json.example"))

	// a broken json config file is an error
	writeFile(jsonFile, `{"server": `)
	_, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).To(gomega.HaveOccurred())
	unsetEnv("WEB_UI_CONFIG_FILE")

	// a broken web.yaml is an error instead of a silent reset to the defaults
	writeFile(webFile, "web:\n  config: [unclosed\n")
	_, err = parser.ReloadWeb(runningCfg)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("web.yaml"))
}
