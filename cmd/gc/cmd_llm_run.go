package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/spf13/cobra"
)

const (
	llmBackendMetadataKey         = "llm_backend"
	llmModelMetadataKey           = "llm_model"
	legacyAgentBackendMetadataKey = "agent_backend"
	legacyAgentModelMetadataKey   = "agent_model"

	executedLLMBackendMetadataKey  = "executed_llm_backend"
	executedLLMModelMetadataKey    = "executed_llm_model"
	executedLLMAtMetadataKey       = "executed_llm_at"
	executedLLMExitCodeMetadataKey = "executed_llm_exit_code"
)

var (
	llmRunLookPath = exec.LookPath
	llmRunExec     = exec.CommandContext
)

type llmRunOptions struct {
	Backend    string
	Model      string
	Prompt     string
	PromptFile string
	Format     string
	CWD        string
	BeadID     string
	Timeout    time.Duration
	WriteBack  bool
}

type llmRunResponse struct {
	OK       bool              `json:"ok"`
	Backend  string            `json:"backend"`
	Model    string            `json:"model,omitempty"`
	CWD      string            `json:"cwd"`
	Stdout   string            `json:"stdout,omitempty"`
	Stderr   string            `json:"stderr,omitempty"`
	ExitCode int               `json:"exit_code"`
	Error    string            `json:"error,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type llmBackendAdapter struct {
	command           string
	args              []string
	preflightCommand  []string
	metadataExtractor func(string) map[string]string
}

func newLLMRunCmd(stdout, stderr io.Writer) *cobra.Command {
	var opts llmRunOptions
	cmd := &cobra.Command{
		Use:   "llm-run [prompt]",
		Short: "Run a one-shot LLM backend from the shell",
		Long: `Run a one-shot LLM backend from the shell.

This command provides one headless execution surface for panels, polecats,
and humans. Backend/model selection can come from flags or bead metadata.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 && strings.TrimSpace(opts.Prompt) == "" && strings.TrimSpace(opts.PromptFile) == "" {
				opts.Prompt = strings.Join(args, " ")
			}
			if cmdLLMRun(opts, os.Stdin, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Backend, "backend", "", "backend to use (cursor, codex, claude)")
	cmd.Flags().StringVar(&opts.Model, "model", "", "backend-specific model identifier")
	cmd.Flags().StringVar(&opts.Prompt, "prompt", "", "inline prompt text")
	cmd.Flags().StringVar(&opts.PromptFile, "prompt-file", "", "read prompt text from a file")
	cmd.Flags().StringVar(&opts.Format, "format", "text", "output format (text or json)")
	cmd.Flags().StringVar(&opts.CWD, "cwd", "", "working directory to run in (defaults to current directory)")
	cmd.Flags().StringVar(&opts.BeadID, "bead", "", "bead ID to read llm_backend/llm_model metadata from")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", 5*time.Minute, "execution timeout")
	cmd.Flags().BoolVar(&opts.WriteBack, "writeback", true, "write executed_llm_* metadata back to the bead when --bead is used")
	return cmd
}

func cmdLLMRun(opts llmRunOptions, stdin io.Reader, stdout, stderr io.Writer) int {
	resp, err := runLLM(opts, stdin)
	if opts.Format == "json" {
		if err != nil {
			resp.OK = false
			resp.Error = err.Error()
		}
		if encodeErr := jsonEncoder(stdout).Encode(resp); encodeErr != nil {
			fmt.Fprintf(stderr, "gc llm-run: encode json: %v\n", encodeErr) //nolint:errcheck // best-effort stderr
			return 1
		}
		if err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		if strings.TrimSpace(resp.Stderr) != "" {
			fmt.Fprint(stderr, resp.Stderr) //nolint:errcheck // best-effort stderr
			if !strings.HasSuffix(resp.Stderr, "\n") {
				fmt.Fprintln(stderr) //nolint:errcheck // best-effort stderr
			}
		}
		fmt.Fprintf(stderr, "gc llm-run: %v\n", err) //nolint:errcheck // best-effort stderr
		return 1
	}
	fmt.Fprint(stdout, resp.Stdout) //nolint:errcheck // best-effort stdout
	return 0
}

func runLLM(opts llmRunOptions, stdin io.Reader) (llmRunResponse, error) {
	cwd := strings.TrimSpace(opts.CWD)
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return llmRunResponse{}, fmt.Errorf("resolve cwd: %w", err)
		}
	}

	resp := llmRunResponse{
		Backend: strings.TrimSpace(opts.Backend),
		Model:   strings.TrimSpace(opts.Model),
		CWD:     cwd,
	}

	if opts.Format != "text" && opts.Format != "json" {
		return resp, fmt.Errorf("unsupported format %q (expected text or json)", opts.Format)
	}
	if strings.TrimSpace(opts.Prompt) != "" && strings.TrimSpace(opts.PromptFile) != "" {
		return resp, fmt.Errorf("provide only one of --prompt or --prompt-file")
	}

	store, bead, err := llmRunResolveBead(opts.BeadID)
	if err != nil {
		return resp, err
	}
	resp.Backend = firstNonEmptyString(resp.Backend, bead.Metadata[llmBackendMetadataKey], bead.Metadata[legacyAgentBackendMetadataKey])
	resp.Model = firstNonEmptyString(resp.Model, bead.Metadata[llmModelMetadataKey], bead.Metadata[legacyAgentModelMetadataKey])
	if resp.Backend == "" {
		return resp, fmt.Errorf("backend is required (pass --backend or set %q metadata on the bead)", llmBackendMetadataKey)
	}

	prompt, err := llmRunResolvePrompt(opts.Prompt, opts.PromptFile, stdin)
	if err != nil {
		return resp, err
	}
	if strings.TrimSpace(prompt) == "" {
		return resp, fmt.Errorf("prompt is required")
	}

	adapter, err := llmRunAdapter(resp.Backend, resp.Model, prompt)
	if err != nil {
		return resp, err
	}
	if err := llmRunPreflight(cwd, resp.Backend, adapter); err != nil {
		return resp, err
	}

	ctx := context.Background()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := llmRunExec(ctx, adapter.command, adapter.args...)
	cmd.Dir = cwd
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		resp.Stdout = outBuf.String()
		resp.Stderr = errBuf.String()
		resp.ExitCode = llmRunExitCode(err)
		resp.Metadata = adapter.metadataExtractor(resp.Stdout)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return resp, fmt.Errorf("timed out after %s", opts.Timeout)
		}
		return resp, fmt.Errorf("%s backend failed with exit code %d", resp.Backend, resp.ExitCode)
	}

	resp.OK = true
	resp.Stdout = outBuf.String()
	resp.Stderr = errBuf.String()
	resp.ExitCode = 0
	resp.Metadata = adapter.metadataExtractor(resp.Stdout)

	if store != nil && bead.ID != "" && opts.WriteBack {
		if err := store.SetMetadataBatch(bead.ID, map[string]string{
			executedLLMBackendMetadataKey:  resp.Backend,
			executedLLMModelMetadataKey:    resp.Model,
			executedLLMAtMetadataKey:       time.Now().UTC().Format(time.RFC3339),
			executedLLMExitCodeMetadataKey: "0",
		}); err != nil {
			return resp, fmt.Errorf("write bead metadata: %w", err)
		}
	}

	return resp, nil
}

func llmRunResolveBead(beadID string) (beads.Store, beads.Bead, error) {
	if strings.TrimSpace(beadID) == "" {
		return nil, beads.Bead{}, nil
	}
	store, code := openCityStore(io.Discard, "gc llm-run")
	if store == nil {
		return nil, beads.Bead{}, exitForCode(code)
	}
	bead, err := store.Get(strings.TrimSpace(beadID))
	if err != nil {
		return nil, beads.Bead{}, fmt.Errorf("load bead %s: %w", beadID, err)
	}
	return store, bead, nil
}

func llmRunResolvePrompt(prompt, promptFile string, stdin io.Reader) (string, error) {
	if strings.TrimSpace(promptFile) != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("read prompt file %s: %w", promptFile, err)
		}
		return string(data), nil
	}
	if strings.TrimSpace(prompt) != "" {
		return prompt, nil
	}
	if hasReadableStdin() {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func llmRunAdapter(backend, model, prompt string) (llmBackendAdapter, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "cursor":
		args := []string{"--print", "--trust", "--output-format", "text"}
		if strings.TrimSpace(model) != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return llmBackendAdapter{
			command:          "cursor-agent",
			args:             args,
			preflightCommand: []string{"cursor-agent", "whoami"},
			metadataExtractor: func(stdout string) map[string]string {
				return nil
			},
		}, nil
	case "codex":
		args := []string{"exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}
		if strings.TrimSpace(model) != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return llmBackendAdapter{
			command:          "codex",
			args:             args,
			preflightCommand: nil,
			metadataExtractor: func(stdout string) map[string]string {
				return nil
			},
		}, nil
	case "claude":
		args := []string{"--print", "--output-format", "text", "--dangerously-skip-permissions"}
		if strings.TrimSpace(model) != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		return llmBackendAdapter{
			command:          "claude",
			args:             args,
			preflightCommand: nil,
			metadataExtractor: func(stdout string) map[string]string {
				return nil
			},
		}, nil
	default:
		return llmBackendAdapter{}, fmt.Errorf("unsupported backend %q (expected cursor, codex, or claude)", backend)
	}
}

func llmRunPreflight(cwd, backend string, adapter llmBackendAdapter) error {
	if _, err := llmRunLookPath(adapter.command); err != nil {
		return fmt.Errorf("%s binary %q not found in PATH", backend, adapter.command)
	}
	if len(adapter.preflightCommand) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := llmRunExec(ctx, adapter.preflightCommand[0], adapter.preflightCommand[1:]...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s preflight failed: %s", backend, msg)
	}
	return nil
}

func llmRunExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err == nil {
		return 0
	}
	return 1
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hasReadableStdin() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}
