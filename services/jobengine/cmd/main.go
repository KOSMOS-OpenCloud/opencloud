package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	pipeconfig "codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/config"
	pipeengine "codeberg.org/kosmos-openworks/openworks-pipeworx/pkg/engine"
)

func main() {
	cfgPath := os.Getenv("JOBENGINE_CONFIG")
	if cfgPath == "" {
		cfgPath = "/etc/opencloud/jobs/pipelines.yaml"
	}

	cfg, err := pipeconfig.LoadPipelineConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	listenAddr := ":9310"
	if addr := os.Getenv("JOBENGINE_ADDR"); addr != "" {
		listenAddr = addr
	}

	// Simple auth: any authenticated user is admin (standalone mode)
	auth := pipeengine.AuthExtractorFunc(func(r *http.Request) (*pipeengine.UserInfo, bool) {
		user, _, ok := r.BasicAuth()
		if !ok || user == "" {
			return nil, false
		}
		return &pipeengine.UserInfo{ID: user, IsAdmin: true}, true
	})

	engine := pipeengine.New(cfg, auth)
	defer engine.Shutdown()

	// Load pipe matrix
	matrixFile := os.Getenv("JOBENGINE_MATRIX_FILE")
	if matrixFile == "" {
		matrixFile = "/etc/opencloud/jobs/matrix.yaml"
	}
	if err := engine.LoadMatrix(matrixFile); err != nil {
		log.Printf("matrix: %v (continuing without matrix)", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	engine.RegisterRoutes(r)

	fmt.Printf("jobengine: %d pipelines, dispatcher mode, listening on %s\n",
		len(cfg.Pipelines), listenAddr)
	for id, p := range cfg.Pipelines {
		fmt.Printf("  pipeline: %s (%s) → job:%s\n", id, p.Label, p.Job.Type)
	}

	if err := http.ListenAndServe(listenAddr, r); err != nil {
		log.Fatalf("server: %v", err)
	}
}
