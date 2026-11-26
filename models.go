package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// providerKeys are the variables each model provider's API key is read
// from, by the helper and by most scripts. The first one set is used.
var providerKeys = map[string][]string{
	"anthropic": {"ANTHROPIC_API_KEY"},
	"openai":    {"OPENAI_API_KEY"},
	"google":    {"GOOGLE_GENERATIVE_AI_API_KEY", "GEMINI_API_KEY"},
	"groq":      {"GROQ_API_KEY"},
	"cerebras":  {"CEREBRAS_API_KEY"},
}

func providers() []string {
	var out []string
	for p := range providerKeys {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// checkModel checks a model name is "<provider>/<model>" for a provider
// Stagehand supports, and that its API key is set. On Browserbase the key is
// optional, since Browserbase's model gateway can serve the model itself.
func checkModel(model string, onBrowserbase bool) error {
	provider, id, ok := strings.Cut(model, "/")
	keys, known := providerKeys[provider]
	if !ok || id == "" || !known {
		return fmt.Errorf("--model=%q: want <provider>/<model>, such as anthropic/claude-sonnet-5-5, with a provider Stagehand supports: %s",
			model, strings.Join(providers(), ", "))
	}
	if keySet(keys) || onBrowserbase {
		return nil
	}
	return fmt.Errorf("--model=%q needs %s: add it to the job, for example from a context", model, strings.Join(keys, " or "))
}

// anyModelKey reports whether any provider's key is set.
func anyModelKey() bool {
	for _, keys := range providerKeys {
		if keySet(keys) {
			return true
		}
	}
	return false
}

func keySet(keys []string) bool {
	for _, k := range keys {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}
