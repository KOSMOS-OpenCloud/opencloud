package reload_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/rs/zerolog"

	"github.com/opencloud-eu/opencloud/pkg/log"
	"github.com/opencloud-eu/opencloud/pkg/reload"
)

type closingLine struct {
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Parts   int               `json:"parts"`
	Failed  int               `json:"failed"`
	Results map[string]string `json:"results"`
}

func parseClosingLine(g *gomega.WithT, logOutput string) closingLine {
	logLines := strings.Split(strings.TrimSpace(logOutput), "\n")
	g.Expect(logLines).To(gomega.HaveLen(1), "exactly one closing line per reload")

	var parsedLine closingLine
	g.Expect(json.Unmarshal([]byte(logLines[0]), &parsedLine)).To(gomega.Succeed())
	return parsedLine
}

func TestRegistry_Run(t *testing.T) {
	g := gomega.NewWithT(t)

	// the test binary starts with the global level of the go-micro logger setup, log.NewLogger raises it at runtime
	previousGlobalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	defer zerolog.SetGlobalLevel(previousGlobalLevel)

	logOutput := &bytes.Buffer{}
	registry := reload.NewRegistry()
	registry.SetLogger(log.Logger{Logger: zerolog.New(logOutput)})

	var callOrder []string
	registry.Register("web.config", func() error {
		callOrder = append(callOrder, "web.config")
		return nil
	})
	registry.Register("proxy.csp", func() error {
		callOrder = append(callOrder, "proxy.csp")
		return nil
	})

	g.Expect(registry.Run()).To(gomega.BeEmpty())
	g.Expect(callOrder).To(gomega.Equal([]string{"proxy.csp", "web.config"}))

	successLine := parseClosingLine(g, logOutput.String())
	g.Expect(successLine.Level).To(gomega.Equal("info"))
	g.Expect(successLine.Message).To(gomega.Equal(reload.FinishedMessage))
	g.Expect(successLine.Parts).To(gomega.Equal(2))
	g.Expect(successLine.Failed).To(gomega.Equal(0))
	g.Expect(successLine.Results).To(gomega.Equal(map[string]string{"proxy.csp": "ok", "web.config": "ok"}))

	// a failing and a panicking part are reported, the other parts still run
	logOutput.Reset()
	callOrder = nil
	registry.Register("proxy.csp", func() error { return errors.New("broken yaml") })
	registry.Register("panicking", func() error { panic("boom") })

	failures := registry.Run()
	g.Expect(failures).To(gomega.HaveLen(2))
	g.Expect(failures["proxy.csp"]).To(gomega.MatchError("broken yaml"))
	g.Expect(callOrder).To(gomega.Equal([]string{"web.config"}))

	failureLine := parseClosingLine(g, logOutput.String())
	g.Expect(failureLine.Level).To(gomega.Equal("error"))
	g.Expect(failureLine.Parts).To(gomega.Equal(3))
	g.Expect(failureLine.Failed).To(gomega.Equal(2))
	g.Expect(failureLine.Results).To(gomega.Equal(map[string]string{
		"panicking":  "error: panic: boom",
		"proxy.csp":  "error: broken yaml",
		"web.config": "ok",
	}))
}

func TestRegistry_Listen(t *testing.T) {
	g := gomega.NewWithT(t)

	registry := reload.NewRegistry()
	registry.SetLogger(log.NopLogger())

	reloaded := make(chan struct{}, 1)
	registry.Register("part", func() error {
		reloaded <- struct{}{}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.Listen(ctx)

	g.Expect(syscall.Kill(syscall.Getpid(), syscall.SIGHUP)).To(gomega.Succeed())

	select {
	case <-reloaded:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP did not trigger a reload")
	}
}
