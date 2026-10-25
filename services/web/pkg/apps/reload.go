package apps

import (
	"errors"
	"hash/fnv"
	"io/fs"
	"os"
	"path"
	"slices"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"

	occfg "github.com/opencloud-eu/opencloud/pkg/config"
	"github.com/opencloud-eu/opencloud/pkg/config/defaults"
	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/services/web/pkg/config"
)

// Reloader keeps the list of external applications up to date.
// It rebuilds the list whenever apps.yaml or one of the application files changes,
// a broken apps.yaml keeps the last valid state.
type Reloader struct {
	logger     log.Logger
	appsFile   string
	entrypoint string
	baseApps   []config.ExternalApp
	fSystems   []fs.FS
	bindApps   func(dst *map[string]config.App) error

	mu           sync.Mutex
	signature    uint64
	appsConfig   map[string]config.App
	externalApps []config.ExternalApp
}

// NewReloader returns a Reloader, the initial list is built from the given apps configuration.
// baseApps are the external applications configured outside apps.yaml, they are always part of the list.
func NewReloader(logger log.Logger, appsConfig map[string]config.App, entrypoint string, baseApps []config.ExternalApp, fSystems ...fs.FS) *Reloader {
	reloader := &Reloader{
		logger:     logger,
		appsFile:   path.Join(defaults.BaseConfigPath(), "apps.yaml"),
		entrypoint: entrypoint,
		baseApps:   slices.Clone(baseApps),
		fSystems:   fSystems,
		appsConfig: appsConfig,
	}
	reloader.bindApps = reloader.bindAppsFile

	reloader.signature = reloader.computeSignature()
	reloader.externalApps = reloader.buildExternalApps(appsConfig)

	return reloader
}

// ExternalApps returns the current list of external applications,
// the list is rebuilt first if any of the watched files changed.
func (r *Reloader) ExternalApps() []config.ExternalApp {
	r.mu.Lock()
	defer r.mu.Unlock()

	currentSignature := r.computeSignature()
	if currentSignature == r.signature {
		return r.externalApps
	}
	// remember the signature in any case, a broken apps.yaml is reported only once per change
	r.signature = currentSignature

	var reloadedConfig map[string]config.App
	if err := r.bindApps(&reloadedConfig); err != nil {
		r.logger.Error().Err(err).Str("file", r.appsFile).Msg("failed to reload apps configuration, keeping the previous state")
	} else {
		r.appsConfig = reloadedConfig
	}

	r.externalApps = r.buildExternalApps(r.appsConfig)
	r.logger.Info().Int("apps", len(r.externalApps)).Msg("reloaded external applications")

	return r.externalApps
}

func (r *Reloader) buildExternalApps(appsConfig map[string]config.App) []config.ExternalApp {
	externalApps := slices.Clone(r.baseApps)
	for _, application := range List(r.logger, appsConfig, r.fSystems...) {
		externalApps = append(externalApps, application.ToExternal(r.entrypoint))
	}

	return externalApps
}

// bindAppsFile validates apps.yaml before binding it, the config binding itself ignores syntax errors.
func (r *Reloader) bindAppsFile(dst *map[string]config.App) error {
	appsContent, err := os.ReadFile(r.appsFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// no apps.yaml is a valid state, every application uses its default configuration
		return nil
	case err != nil:
		return err
	}

	var syntaxCheck map[string]any
	if err := yaml.Unmarshal(appsContent, &syntaxCheck); err != nil {
		return err
	}

	return occfg.BindSourcesToStructs("apps", dst)
}

// computeSignature hashes the modification state of apps.yaml and of all application files
// that contribute to the list, a changed signature triggers a rebuild.
func (r *Reloader) computeSignature() uint64 {
	hasher := fnv.New64a()
	addStat := func(name string, info fs.FileInfo, err error) {
		_, _ = hasher.Write([]byte(name))
		if err != nil {
			_, _ = hasher.Write([]byte{0})
			return
		}
		_, _ = hasher.Write([]byte(strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)))
	}

	appsFileInfo, appsFileErr := os.Stat(r.appsFile)
	addStat(r.appsFile, appsFileInfo, appsFileErr)

	for fsIndex, fSystem := range r.fSystems {
		if fSystem == nil {
			continue
		}
		_, _ = hasher.Write([]byte("fs" + strconv.Itoa(fsIndex)))

		entries, err := fs.ReadDir(fSystem, ".")
		if err != nil {
			continue
		}

		for _, entry := range entries {
			appName := entry.Name()
			for _, appFile := range []string{
				appName,
				path.Join(appName, _manifest),
				path.Join(appName, _config),
				path.Join(appName, "dist"),
				path.Join(appName, "dist", _manifest),
				path.Join(appName, "dist", _config),
			} {
				appFileInfo, appFileErr := fs.Stat(fSystem, appFile)
				addStat(appFile, appFileInfo, appFileErr)
			}
		}
	}

	return hasher.Sum64()
}
