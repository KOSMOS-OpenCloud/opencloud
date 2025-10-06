package middleware

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestLoadCSPConfig(t *testing.T) {
	// setup test env
	presetYaml := `
directives:
  frame-src:
    - '''self'''
    - 'https://embed.diagrams.net/'
    - 'https://${ONLYOFFICE_DOMAIN|onlyoffice.opencloud.test}/'
    - 'https://${COLLABORA_DOMAIN|collabora.opencloud.test}/'
`

	customYaml := `
directives:
  img-src:
    - '''self'''
    - 'data:'
  frame-src:
    - 'https://some.custom.domain/'
`
	config, err := loadCSPConfig([]byte(presetYaml), []byte(customYaml))
	if err != nil {
		t.Error(err)
	}
	// TODO: this needs to be reworked into some contains assertion
	assert.Equal(t, config.Directives["frame-src"][0], "'self'")
	assert.Equal(t, config.Directives["frame-src"][1], "https://embed.diagrams.net/")
	assert.Equal(t, config.Directives["frame-src"][2], "https://onlyoffice.opencloud.test/")
	assert.Equal(t, config.Directives["frame-src"][3], "https://collabora.opencloud.test/")

	assert.Equal(t, config.Directives["img-src"][0], "'self'")
	assert.Equal(t, config.Directives["img-src"][1], "data:")
}
