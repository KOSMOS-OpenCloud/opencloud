package config

// DefaultOCConfig returns sane defaults for OpenCloud integration
func DefaultOCConfig() *OCConfig {
	return &OCConfig{
		Service: OCService{
			Name: "jobengine",
		},
		HTTP: HTTP{
			Addr: "0.0.0.0:9260",
		},
		Debug: Debug{
			Addr: "0.0.0.0:9261",
		},
		MaxWorkers: 4,
		QueueSize:  100,
		TempDir:    "/tmp/jobengine",
		PipelineDirs: []string{
			"/etc/opencloud/jobs/pipelines.d",
		},
	}
}
