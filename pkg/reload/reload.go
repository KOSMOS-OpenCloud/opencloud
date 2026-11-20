// Package reload re-reads selected configuration at runtime, triggered by SIGHUP.
//
// Services register a function for every part that can be re-read without a restart.
// All services of the single binary run in the same process, so one signal receiver serves all of them.
package reload

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

// FinishedMessage is the message of the single log line written at the end of every reload.
// Deploy scripts wait for it.
const FinishedMessage = "config reload finished"

// Func re-reads one part of the configuration.
// On error the previous state has to stay active.
type Func func() error

// Registry holds the reload functions of the running services.
type Registry struct {
	mu     sync.Mutex
	funcs  map[string]Func
	logger *log.Logger
}

var defaultRegistry = NewRegistry()

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{funcs: map[string]Func{}}
}

// Register adds a reload function under the given name, an existing function with the same name is replaced.
// A service that is restarted by the supervisor registers again and replaces its previous function.
func Register(name string, reloadFunc Func) {
	defaultRegistry.Register(name, reloadFunc)
}

// SetLogger sets the logger used for the reload log lines.
func SetLogger(logger log.Logger) {
	defaultRegistry.SetLogger(logger)
}

// Listen runs all registered reload functions on every SIGHUP until the context is done.
// It returns immediately, the signal is handled in the background.
func Listen(ctx context.Context) {
	defaultRegistry.Listen(ctx)
}

// Register adds a reload function under the given name, an existing function with the same name is replaced.
func (r *Registry) Register(name string, reloadFunc Func) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.funcs[name] = reloadFunc
}

// SetLogger sets the logger used for the reload log lines.
func (r *Registry) SetLogger(logger log.Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger = &logger
}

// Listen runs all registered reload functions on every SIGHUP until the context is done.
func (r *Registry) Listen(ctx context.Context) {
	hangupSignals := make(chan os.Signal, 1)
	signal.Notify(hangupSignals, syscall.SIGHUP)

	go func() {
		defer signal.Stop(hangupSignals)
		for {
			select {
			case <-ctx.Done():
				return
			case <-hangupSignals:
				r.Run()
			}
		}
	}()
}

// Run calls all registered reload functions in the order of their names and writes one closing log line
// with the result of every part. It returns the errors by name, the map is empty if every part succeeded.
func (r *Registry) Run() map[string]error {
	// the lock is held for the whole run, two signals in short succession are handled one after the other
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.funcs))
	for name := range r.funcs {
		names = append(names, name)
	}
	slices.Sort(names)

	failures := map[string]error{}
	results := zerolog.Dict()
	for _, name := range names {
		if reloadErr := runProtected(r.funcs[name]); reloadErr != nil {
			failures[name] = reloadErr
			results.Str(name, "error: "+reloadErr.Error())
			continue
		}
		results.Str(name, "ok")
	}

	logger := r.currentLogger()
	closingLine := logger.Info()
	if len(failures) > 0 {
		closingLine = logger.Error()
	}
	closingLine.
		Int("parts", len(names)).
		Int("failed", len(failures)).
		Dict("results", results).
		Msg(FinishedMessage)

	return failures
}

func (r *Registry) currentLogger() log.Logger {
	if r.logger != nil {
		// the closing line is written at info level even if the service logs errors only
		return log.Logger{Logger: r.logger.Level(zerolog.InfoLevel)}
	}
	// no service set a logger, the closing line still has to be visible
	return log.NewLogger(log.Name("reload"), log.Level("info"))
}

// runProtected turns a panic of a reload function into an error, a reload must never stop the process.
func runProtected(reloadFunc Func) (reloadErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			reloadErr = fmt.Errorf("panic: %v", recovered)
		}
	}()

	return reloadFunc()
}
