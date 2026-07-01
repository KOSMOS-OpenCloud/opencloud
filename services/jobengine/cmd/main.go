package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/service"
)

func main() {
	cfgPath := os.Getenv("JOBENGINE_CONFIG")
	if cfgPath == "" {
		cfgPath = "/etc/opencloud/jobs/pipelines.yaml"
	}

	cfg, err := config.LoadPipelineConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	listenAddr := ":9310"
	if addr := os.Getenv("JOBENGINE_ADDR"); addr != "" {
		listenAddr = addr
	}

	engine := service.New(cfg)
	defer engine.Shutdown()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	engine.RegisterRoutes(r)

	fmt.Printf("jobengine: %d pipelines, %d workers, listening on %s\n",
		len(cfg.Pipelines), cfg.Service.MaxWorkers, listenAddr)
	for id, p := range cfg.Pipelines {
		fmt.Printf("  pipeline: %s (%s) → %s\n", id, p.Label, p.Executor.Type)
	}

	if err := http.ListenAndServe(listenAddr, r); err != nil {
		log.Fatalf("server: %v", err)
	}
}
