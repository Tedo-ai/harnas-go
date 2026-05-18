package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	harnas "github.com/Tedo-ai/harnas-go"
)

func main() {
	provider := flag.String("provider", "", "anthropic|openai|gemini|ollama")
	model := flag.String("model", "", "model identifier")
	streamOnly := flag.Bool("stream-only", false, "only test streaming provider")
	bufferedOnly := flag.Bool("buffered-only", false, "only test buffered provider")
	flag.Parse()

	prompt := strings.Join(flag.Args(), " ")
	if *provider == "" || prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: smoke --provider anthropic|openai|gemini|ollama [--model MODEL] <prompt>")
		os.Exit(1)
	}
	if *streamOnly && *bufferedOnly {
		fmt.Fprintln(os.Stderr, "error: --stream-only and --buffered-only are mutually exclusive")
		os.Exit(1)
	}
	modelName := resolveModel(*provider, *model)
	apiKey := resolveAPIKey(*provider)
	if *provider != "ollama" && apiKey == "" {
		fmt.Fprintf(os.Stderr, "error: %s_API_KEY is not set\n", strings.ToUpper(*provider))
		os.Exit(1)
	}
	request := requestFor(*provider, modelName, prompt)

	if !*streamOnly {
		text, err := callBuffered(*provider, apiKey, request)
		must(*provider, "buffered", err)
		requireText("buffered", text)
		fmt.Printf("[buffered] %s\n", text)
	}
	if !*bufferedOnly {
		text, err := callStreaming(*provider, apiKey, request)
		must(*provider, "streaming", err)
		requireText("streaming", text)
		fmt.Printf("[streaming] %s\n", text)
	}
}

func resolveModel(provider, explicit string) string {
	if explicit != "" {
		return explicit
	}
	env := os.Getenv(strings.ToUpper(provider) + "_MODEL")
	if env != "" {
		return env
	}
	switch provider {
	case "anthropic":
		return "claude-sonnet-4-5"
	case "openai":
		return "gpt-5.4-mini"
	case "gemini":
		return "gemini-flash-latest"
	case "ollama":
		return "llama3.2"
	default:
		return ""
	}
}

func resolveAPIKey(provider string) string {
	return os.Getenv(strings.ToUpper(provider) + "_API_KEY")
}

func requestFor(provider, model, prompt string) map[string]any {
	switch provider {
	case "openai", "ollama":
		return map[string]any{
			"model":    model,
			"messages": []any{map[string]any{"role": "user", "content": prompt}},
		}
	case "gemini":
		return map[string]any{
			"model": model,
			"contents": []any{map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": prompt}},
			}},
			"generationConfig": map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": float64(0)}},
		}
	default:
		return map[string]any{
			"model":      model,
			"max_tokens": 1024,
			"messages":   []any{map[string]any{"role": "user", "content": prompt}},
		}
	}
}

func callBuffered(provider, apiKey string, request map[string]any) (string, error) {
	switch provider {
	case "anthropic":
		response, err := harnas.NewAnthropicProvider(apiKey).Call(request)
		return stringValue(firstMap(response["content"])["text"]), err
	case "openai":
		response, err := harnas.NewOpenAIProvider(apiKey).Call(request)
		choice := firstMap(response["choices"])
		return stringValue(asMap(choice["message"])["content"]), err
	case "ollama":
		response, err := harnas.NewOllamaProvider(os.Getenv("OLLAMA_BASE_URL")).Call(request)
		choice := firstMap(response["choices"])
		return stringValue(asMap(choice["message"])["content"]), err
	case "gemini":
		response, err := harnas.NewGeminiProvider(apiKey).Call(request)
		candidate := firstMap(response["candidates"])
		part := firstMap(asMap(candidate["content"])["parts"])
		return stringValue(part["text"]), err
	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}
}

func callStreaming(provider, apiKey string, request map[string]any) (string, error) {
	var final string
	emit := func(event harnas.EventArgs) {
		if event.Type == harnas.EventAssistantMessage {
			final = stringValue(event.Payload["text"])
		}
	}
	var err error
	switch provider {
	case "anthropic":
		err = harnas.NewAnthropicStreamProvider(apiKey).Call(request, emit)
	case "openai":
		err = harnas.NewOpenAIStreamProvider(apiKey).Call(request, emit)
	case "ollama":
		err = harnas.NewOllamaStreamProvider(os.Getenv("OLLAMA_BASE_URL")).Call(request, emit)
	case "gemini":
		err = harnas.NewGeminiStreamProvider(apiKey).Call(request, emit)
	default:
		err = fmt.Errorf("unknown provider: %s", provider)
	}
	return final, err
}

func must(provider, mode string, err error) {
	if err == nil {
		return
	}
	if provider == "ollama" {
		fmt.Fprintf(os.Stderr, "skip: Ollama is not reachable (%v)\n", err)
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "error: %s smoke failed: %v\n", mode, err)
	os.Exit(1)
}

func requireText(mode, text string) {
	if strings.TrimSpace(text) != "" {
		return
	}
	fmt.Fprintf(os.Stderr, "error: %s response contained no text\n", mode)
	os.Exit(1)
}

func firstMap(value any) map[string]any {
	items, _ := value.([]any)
	if len(items) == 0 {
		return map[string]any{}
	}
	return asMap(items[0])
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}
