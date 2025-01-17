package wrapper

import (
	"fmt"
	"net/http"
	"ocwrapper/common"
	"ocwrapper/log"
	"ocwrapper/opencloud/config"
	"ocwrapper/wrapper/handlers"
)

func Start(port string) {
	defer common.Wg.Done()

	if port == "" {
		port = config.Get("port")
	}

	httpServer := &http.Server{
		Addr: ":" + port,
	}

	var mux = http.NewServeMux()
	mux.HandleFunc("/", http.NotFound)
	mux.HandleFunc("/config", handlers.SetEnvHandler)
	mux.HandleFunc("/rollback", handlers.RollbackHandler)
	mux.HandleFunc("/command", handlers.CommandHandler)
	mux.HandleFunc("/stop", handlers.StopOpencloudHandler)
	mux.HandleFunc("/start", handlers.StartOpencloudHandler)

	httpServer.Handler = mux

	log.Println(fmt.Sprintf("Starting ocwrapper on port %s...", port))

	err := httpServer.ListenAndServe()
	if err != nil {
		log.Panic(err)
	}
}
