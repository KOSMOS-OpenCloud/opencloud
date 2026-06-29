package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level jobengine configuration
type Config struct {
	Service      ServiceConfig          `yaml:"service"`
	Pipelines    map[string]Pipeline    `yaml:"pipelines"`
}

// ServiceConfig holds runtime settings
type ServiceConfig struct {
	MaxWorkers   int      `yaml:"max_workers"`
	QueueSize    int      `yaml:"queue_size"`
	TempDir      string   `yaml:"temp_dir"`
	PipelineDirs []string `yaml:"pipeline_dirs"`
	ListenAddr   string   `yaml:"listen_addr"`
}

// Pipeline defines a conversion/processing pipeline
type Pipeline struct {
	Label              string          `yaml:"label"`
	Icon               string          `yaml:"icon"`
	SourceTypes        []string        `yaml:"source_types"`
	Target             TargetConfig    `yaml:"target"`
	UserChoosableTarget bool           `yaml:"user_choosable_target"`
	Batch              bool            `yaml:"batch"`
	Executor           ExecutorConfig  `yaml:"executor"`
	Options            map[string]string `yaml:"options"`
}

// TargetConfig defines where the result goes
type TargetConfig struct {
	Extension  string `yaml:"extension"`
	Location   string `yaml:"location"`   // same | subfolder:<name> | sibling:<name> | prompt
	CreateDirs bool   `yaml:"create_dirs"`
}

// ExecutorConfig defines how the pipeline step is executed
type ExecutorConfig struct {
	Type        string        `yaml:"type"`    // http | exec | script
	Command     string        `yaml:"command"` // for exec
	Args        []string      `yaml:"args"`    // for exec
	URL         string        `yaml:"url"`     // for http
	Method      string        `yaml:"method"`  // for http (default POST)
	UploadField string        `yaml:"upload_field"` // for http (default "file")
	Path        string        `yaml:"path"`    // for script (relative to converter_dir)
	Timeout     time.Duration `yaml:"timeout"`
}

// Defaults returns a config with sane defaults
func Defaults() *Config {
	return &Config{
		Service: ServiceConfig{
			MaxWorkers:   4,
			QueueSize:    100,
			TempDir:      "/tmp/jobengine",
			PipelineDirs: []string{"/etc/opencloud/jobs/pipelines.d"},
			ListenAddr:   ":9260",
		},
		Pipelines: make(map[string]Pipeline),
	}
}

// Load reads the main config file and scans pipeline dirs
func Load(path string) (*Config, error) {
	cfg := Defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config %s: %w", path, err)
			}
			// config file optional — use defaults
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing config %s: %w", path, err)
			}
		}
	}

	if cfg.Pipelines == nil {
		cfg.Pipelines = make(map[string]Pipeline)
	}

	// Scan pipeline dirs for addon YAMLs
	for _, dir := range cfg.Service.PipelineDirs {
		if err := cfg.LoadPipelineDir(dir); err != nil {
			// non-fatal: dir might not exist yet
			continue
		}
	}

	return cfg, nil
}

func (c *Config) LoadPipelineDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		var addon struct {
			Pipelines map[string]Pipeline `yaml:"pipelines"`
		}
		if err := yaml.Unmarshal(data, &addon); err != nil {
			continue
		}

		for id, p := range addon.Pipelines {
			c.Pipelines[id] = p
		}
	}

	return nil
}
