package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

const (
	hookOutputFormatCodex  = "codex"
	hookOutputFormatGemini = "gemini"
)

func writeProviderHookContext(stdout io.Writer, format, content string) error {
	if content == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case hookOutputFormatGemini:
		return json.NewEncoder(stdout).Encode(geminiHookAdditionalContext(content))
	case hookOutputFormatCodex:
		return json.NewEncoder(stdout).Encode(codexHookAdditionalContext(content))
	}
	_, err := io.WriteString(stdout, content)
	return err
}

func codexHookAdditionalContext(content string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     strings.TrimSpace(os.Getenv("GC_HOOK_EVENT_NAME")),
			"additionalContext": strings.TrimRight(content, "\n"),
		},
	}
}

func geminiHookAdditionalContext(content string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"additionalContext": strings.TrimRight(content, "\n"),
		},
	}
}
