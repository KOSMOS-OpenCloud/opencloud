package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// PipelineConfig is the pipeline/executor configuration (loaded from YAML files)
type PipelineConfig struct {
	Service   PipelineServiceConfig `yaml:"service"`
	Pipelines map[string]Pipeline   `yaml:"pipelines"`
}

// PipelineServiceConfig holds runtime settings for the pipeline engine
type PipelineServiceConfig struct {
	MaxWorkers   int      `yaml:"max_workers"`
	QueueSize    int      `yaml:"queue_size"`
	TempDir      string   `yaml:"temp_dir"`
	PipelineDirs []string `yaml:"pipeline_dirs"`
}

// Pipeline defines a conversion/processing pipeline
type Pipeline struct {
	Label               string            `yaml:"label"`
	Icon                string            `yaml:"icon"`
	SourceTypes         []string          `yaml:"source_types"`
	Target              TargetConfig      `yaml:"target"`
	UserChoosableTarget bool              `yaml:"user_choosable_target"`
	Batch               bool              `yaml:"batch"`
	Executor            ExecutorConfig    `yaml:"executor"`
	Options             map[string]string `yaml:"options"`
}

// TargetConfig defines where the result goes
type TargetConfig struct {
	Extension  string `yaml:"extension"`
	Location   string `yaml:"location"`
	CreateDirs bool   `yaml:"create_dirs"`
}

// ExecutorConfig defines how the pipeline step is executed
type ExecutorConfig struct {
	Type        string        `yaml:"type"`
	Command     string        `yaml:"command"`
	Args        []string      `yaml:"args"`
	URL         string        `yaml:"url"`
	Method      string        `yaml:"method"`
	UploadField string        `yaml:"upload_field"`
	Path        string        `yaml:"path"`
	Timeout     time.Duration `yaml:"timeout"`
}

// PipelineDefaults returns a PipelineConfig with sane defaults
func PipelineDefaults() *PipelineConfig {
	return &PipelineConfig{
		Service: PipelineServiceConfig{
			MaxWorkers:   4,
			QueueSize:    100,
			TempDir:      "/tmp/jobengine",
			PipelineDirs: []string{"/etc/opencloud/jobs/pipelines.d"},
		},
		Pipelines: make(map[string]Pipeline),
	}
}

// LoadPipelineConfig reads the main config file and scans pipeline dirs
func LoadPipelineConfig(path string) (*PipelineConfig, error) {
	cfg := PipelineDefaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config %s: %w", path, err)
			}
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parsing config %s: %w", path, err)
			}
		}
	}

	if cfg.Pipelines == nil {
		cfg.Pipelines = make(map[string]Pipeline)
	}

	for _, dir := range cfg.Service.PipelineDirs {
		if err := cfg.LoadPipelineDir(dir); err != nil {
			continue
		}
	}

	return cfg, nil
}

func (c *PipelineConfig) LoadPipelineDir(dir string) error {
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
