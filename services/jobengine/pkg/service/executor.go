package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/opencloud-eu/opencloud/services/jobengine/pkg/config"
)

// Executor runs a pipeline step
type Executor interface {
	Execute(ctx context.Context, item *JobItem, cfg config.ExecutorConfig) error
}

// NewExecutor creates the right executor for the config type
func NewExecutor(cfg config.ExecutorConfig) (Executor, error) {
	switch cfg.Type {
	case "http":
		return &HttpExecutor{}, nil
	case "exec":
		return &ExecExecutor{}, nil
	case "script":
		return &ScriptExecutor{}, nil
	default:
		return nil, fmt.Errorf("unknown executor type: %s", cfg.Type)
	}
}

// HttpExecutor sends files to an HTTP endpoint (e.g. Collabora)
type HttpExecutor struct{}

func (e *HttpExecutor) Execute(ctx context.Context, item *JobItem, cfg config.ExecutorConfig) error {
	sourceFile, err := os.Open(item.SourcePath)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer sourceFile.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	field := cfg.UploadField
	if field == "" {
		field = "file"
	}

	part, err := writer.CreateFormFile(field, filepath.Base(item.SourcePath))
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}
	if _, err := io.Copy(part, sourceFile); err != nil {
		return fmt.Errorf("copying source to form: %w", err)
	}
	writer.Close()

	url := Resolve(cfg.URL, item.Vars)
	method := cfg.Method
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequestWithContext(ctx, method, url, &body)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	if cfg.Timeout > 0 {
		client.Timeout = cfg.Timeout
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	targetFile, err := os.Create(item.TargetPath)
	if err != nil {
		return fmt.Errorf("creating target: %w", err)
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, resp.Body); err != nil {
		return fmt.Errorf("writing target: %w", err)
	}

	return nil
}

// ExecExecutor runs a local command (e.g. pandoc, ffmpeg, unzip)
type ExecExecutor struct{}

func (e *ExecExecutor) Execute(ctx context.Context, item *JobItem, cfg config.ExecutorConfig) error {
	args := ResolveArgs(cfg.Args, item.Vars)
	command := Resolve(cfg.Command, item.Vars)

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("exec %s: %w", command, err)
	}

	return nil
}

// ScriptExecutor runs a mounted shell script
type ScriptExecutor struct {
	ScriptDir string
}

func (e *ScriptExecutor) Execute(ctx context.Context, item *JobItem, cfg config.ExecutorConfig) error {
	scriptPath := filepath.Join(e.ScriptDir, Resolve(cfg.Path, item.Vars))

	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("script not found: %s", scriptPath)
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", scriptPath)
	cmd.Env = append(os.Environ(),
		"JOB_SOURCE="+item.SourcePath,
		"JOB_TARGET="+item.TargetPath,
		"JOB_TARGET_DIR="+item.TargetDir,
		"JOB_USER_ID="+SanitizeValue(item.Vars.User.ID),
		"JOB_USER_NAME="+SanitizeValue(item.Vars.User.DisplayName),
		"JOB_SPACE_NAME="+SanitizeValue(item.Vars.Space.Name),
		"JOB_RESOURCE_NAME="+SanitizeValue(item.Vars.Resource.Name),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("script %s: %w", scriptPath, err)
	}

	return nil
}
