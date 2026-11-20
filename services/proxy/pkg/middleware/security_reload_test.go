package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/opencloud-eu/opencloud/services/proxy/pkg/config"
)

func TestSecurityReloader(t *testing.T) {
	cspFile := filepath.Join(t.TempDir(), "csp.yaml")
	writeCSPFile := func(content string) {
		assert.NilError(t, os.WriteFile(cspFile, []byte(content), 0o644))
	}
	writeCSPFile("directives:\n  frame-src:\n    - 'https://first.example/'\n")

	securityReloader, err := NewSecurityReloader(&config.Config{CSPConfigFileLocation: cspFile})
	assert.NilError(t, err)

	securedHandler := securityReloader.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	currentPolicy := func() string {
		recorder := httptest.NewRecorder()
		securedHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "https://cloud.example/", nil))
		return recorder.Header().Get("Content-Security-Policy")
	}

	initialPolicy := currentPolicy()
	assert.Assert(t, cmp.Contains(initialPolicy, "https://first.example/"))

	// a changed file is active after the reload, without a reload nothing changes
	writeCSPFile("directives:\n  frame-src:\n    - 'https://second.example/'\n")
	assert.Equal(t, currentPolicy(), initialPolicy)
	assert.NilError(t, securityReloader.Reload())
	reloadedPolicy := currentPolicy()
	assert.Assert(t, cmp.Contains(reloadedPolicy, "https://second.example/"))
	// a removed source must not survive the reload
	assert.Assert(t, !cmp.Contains(reloadedPolicy, "https://first.example/")().Success())

	// a broken file keeps the previous policy
	writeCSPFile("directives:\n  frame-src: [unclosed\n")
	assert.Assert(t, securityReloader.Reload() != nil)
	assert.Equal(t, currentPolicy(), reloadedPolicy)

	// a missing file keeps the previous policy
	assert.NilError(t, os.Remove(cspFile))
	assert.Assert(t, securityReloader.Reload() != nil)
	assert.Equal(t, currentPolicy(), reloadedPolicy)

	// the repaired file is picked up again
	writeCSPFile("directives:\n  frame-src:\n    - 'https://third.example/'\n")
	assert.NilError(t, securityReloader.Reload())
	assert.Assert(t, cmp.Contains(currentPolicy(), "https://third.example/"))
}

func TestNewSecurityReloader_BrokenFile(t *testing.T) {
	_, err := NewSecurityReloader(&config.Config{CSPConfigFileLocation: filepath.Join(t.TempDir(), "missing.yaml")})
	assert.Assert(t, err != nil)
}
