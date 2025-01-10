package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencloud-eu/opencloud/services/frontend/pkg/command"
	"github.com/opencloud-eu/opencloud/services/frontend/pkg/config/defaults"
)

func main() {
	cfg := defaults.DefaultConfig()
	cfg.Context, _ = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	if err := command.Execute(cfg); err != nil {
		os.Exit(1)
	}
}
