package middleware

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestLoadCSPConfig(t *testing.T) {
	// setup test env
	yaml := `
directives:
  frame-src:
    - '''self'''
    - 'https://embed.diagrams.net/'
    - 'https://${ONLYOFFICE_DOMAIN|onlyoffice.opencloud.test}/'
    - 'https://${COLLABORA_DOMAIN|collabora.opencloud.test}/'
`

	config, err := loadCSPConfig([]byte(yaml))
	if err != nil {
		t.Error(err)
	}
	assert.Equal(t, config.Directives["frame-src"][0], "'self'")
	assert.Equal(t, config.Directives["frame-src"][1], "https://embed.diagrams.net/")
	assert.Equal(t, config.Directives["frame-src"][2], "https://onlyoffice.opencloud.test/")
	assert.Equal(t, config.Directives["frame-src"][3], "https://collabora.opencloud.test/")
}
