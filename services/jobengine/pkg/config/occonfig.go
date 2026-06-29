package config

import (
	"context"

	"github.com/opencloud-eu/opencloud/pkg/shared"
)

// OCConfig is the OpenCloud-integrated configuration
type OCConfig struct {
	Commons *shared.Commons `yaml:"-"`
	Context context.Context `yaml:"-"`

	Service  OCService `yaml:"-"`
	LogLevel string    `yaml:"loglevel" env:"OC_LOG_LEVEL;JOBENGINE_LOG_LEVEL"`
	Debug    Debug     `yaml:"debug"`
	HTTP     HTTP      `yaml:"http"`

	ConfigFile   string   `yaml:"config_file" env:"JOBENGINE_CONFIG_FILE"`
	MaxWorkers   int      `yaml:"max_workers" env:"JOBENGINE_MAX_WORKERS"`
	QueueSize    int      `yaml:"queue_size" env:"JOBENGINE_QUEUE_SIZE"`
	TempDir      string   `yaml:"temp_dir" env:"JOBENGINE_TEMP_DIR"`
	PipelineDirs []string `yaml:"pipeline_dirs" env:"JOBENGINE_PIPELINE_DIRS"`
}

type OCService struct {
	Name string `yaml:"-"`
}

type Debug struct {
	Addr  string `yaml:"addr" env:"JOBENGINE_DEBUG_ADDR"`
	Token string `yaml:"token" env:"JOBENGINE_DEBUG_TOKEN"`
}

type HTTP struct {
	Addr string `yaml:"addr" env:"JOBENGINE_HTTP_ADDR"`
}
