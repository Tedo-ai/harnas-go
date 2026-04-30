package harnas

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

const SupportedManifestVersion = "0.1"

type ManifestError struct {
	Message string
}

func (e ManifestError) Error() string {
	return e.Message
}

type Manifest struct {
	Version    string         `json:"harnas_version"`
	Name       string         `json:"name"`
	System     string         `json:"system,omitempty"`
	Provider   ProviderSpec   `json:"provider"`
	Tools      []ToolSpec     `json:"tools"`
	Strategies []StrategySpec `json:"strategies"`
}

type ProviderSpec struct {
	Kind      string `json:"kind"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Handler     string         `json:"handler"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type StrategySpec struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}

type ToolHandler func(map[string]any) (string, error)
type ApprovalHandler func(Event) bool

type ManifestOptions struct {
	ToolHandlers     map[string]ToolHandler
	StrategyHandlers map[string]ApprovalHandler
	Providers        map[string]Provider
	APIKeys          map[string]string
}

type LoadedManifest struct {
	Name           string
	Session        *Session
	Projection     Projection
	Provider       Provider
	StreamProvider StreamProvider
	Ingestor       Ingestor
	Registry       *Registry
	Strategies     []StrategyInstallation
}

type StrategyInstallation interface {
	Install(session *Session)
}

func LoadManifest(source string, options ManifestOptions) (*LoadedManifest, error) {
	manifest, err := ReadManifest(source)
	if err != nil {
		return nil, err
	}
	return BuildManifest(manifest, options)
}

func ReadManifest(path string) (Manifest, error) {
	var manifest Manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, ValidateManifest(manifest)
}

func BuildManifest(manifest Manifest, options ManifestOptions) (*LoadedManifest, error) {
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	registry, err := BuildRegistry(manifest.Tools, options.ToolHandlers)
	if err != nil {
		return nil, err
	}
	projection := ProjectionFor(manifest.Provider, manifest.System)
	provider, err := providerFor(manifest.Provider.Kind, options)
	if err != nil {
		return nil, err
	}
	ingestor := IngestorFor(manifest.Provider.Kind)
	strategies, err := BuildStrategiesWithRuntime(
		manifest.Strategies,
		options.StrategyHandlers,
		projection,
		provider,
		ingestor,
	)
	if err != nil {
		return nil, err
	}
	return &LoadedManifest{
		Name:       manifest.Name,
		Session:    CreateSession(map[string]any{"manifest_name": manifest.Name}),
		Projection: projection,
		Provider:   provider,
		Ingestor:   ingestor,
		Registry:   registry,
		Strategies: strategies,
	}, nil
}

func (l *LoadedManifest) InstallStrategies() {
	for _, strategy := range l.Strategies {
		strategy.Install(l.Session)
	}
}

func (l *LoadedManifest) Runner() *Runner {
	return &Runner{Registry: l.Registry}
}

func (l *LoadedManifest) Loop() AgentLoop {
	loop := AgentLoop{
		Session:        l.Session,
		Projection:     l.Projection,
		Provider:       l.Provider,
		StreamProvider: l.StreamProvider,
		Ingestor:       l.Ingestor,
		MaxTurns:       10,
	}
	if l.Registry != nil && l.Registry.Size() > 0 {
		loop.Runner = l.Runner()
	}
	return loop
}

func ValidateManifest(manifest Manifest) error {
	if manifest.Version != SupportedManifestVersion {
		return manifestError("manifest version %q not in supported [%q]", manifest.Version, SupportedManifestVersion)
	}
	if manifest.Name == "" {
		return manifestError("manifest name must not be empty")
	}
	if manifest.Provider.Kind == "" {
		return manifestError("provider.kind is required")
	}
	if !knownProvider(manifest.Provider.Kind) {
		return manifestError("unknown provider kind: %q", manifest.Provider.Kind)
	}
	if manifest.Provider.Kind != "mock" && manifest.Provider.Model == "" {
		return manifestError("provider.model is required for provider %q", manifest.Provider.Kind)
	}
	if manifest.Provider.MaxTokens < 1 {
		return manifestError("provider.max_tokens must be >= 1")
	}
	if err := validateTools(manifest.Tools); err != nil {
		return err
	}
	if err := validateStrategies(manifest.Strategies); err != nil {
		return err
	}
	return nil
}

func validateTools(tools []ToolSpec) error {
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" {
			return manifestError("tool.name must not be empty")
		}
		if seen[tool.Name] {
			return manifestError("duplicate tool name: %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Handler == "" {
			return manifestError("tool %q handler must not be empty", tool.Name)
		}
		if tool.InputSchema == nil {
			return manifestError("tool %q input_schema is required", tool.Name)
		}
	}
	return nil
}

func validateStrategies(strategies []StrategySpec) error {
	pattern := regexp.MustCompile(`^[A-Z][A-Za-z0-9]+::[A-Z][A-Za-z0-9]+$`)
	for _, strategy := range strategies {
		if !pattern.MatchString(strategy.Name) {
			return manifestError("strategy name %q is not canonical", strategy.Name)
		}
		if !knownStrategy(strategy.Name) {
			return manifestError("unknown canonical strategy: %q", strategy.Name)
		}
	}
	return nil
}

func BuildRegistry(specs []ToolSpec, handlers map[string]ToolHandler) (*Registry, error) {
	registry := NewRegistry()
	for _, spec := range specs {
		handler, ok := handlers[spec.Handler]
		if !ok {
			return nil, manifestError("tool handler %q not in tool_handlers", spec.Handler)
		}
		if err := registry.Register(Tool{
			Name:        spec.Name,
			Handler:     spec.Handler,
			Description: spec.Description,
			InputSchema: spec.InputSchema,
			Call:        handler,
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func BuildStrategies(specs []StrategySpec, handlers map[string]ApprovalHandler) ([]StrategyInstallation, error) {
	return BuildStrategiesWithRuntime(specs, handlers, nil, nil, nil)
}

func BuildStrategiesWithRuntime(
	specs []StrategySpec,
	handlers map[string]ApprovalHandler,
	projection Projection,
	provider Provider,
	ingestor Ingestor,
) ([]StrategyInstallation, error) {
	strategies := make([]StrategyInstallation, 0, len(specs))
	for _, spec := range specs {
		switch spec.Name {
		case "Compaction::MarkerTail":
			strategies = append(strategies, MarkerTail{
				MaxMessages: intValue(spec.Config["max_messages"]),
				KeepRecent:  intValue(spec.Config["keep_recent"]),
			})
		case "Compaction::ToolOutputCap":
			strategies = append(strategies, ToolOutputCap{
				MaxBytes:      intValue(spec.Config["max_bytes"]),
				PrefixBytes:   intValue(spec.Config["prefix_bytes"]),
				SummaryFormat: stringValue(spec.Config["summary_format"]),
			})
		case "Compaction::TokenMarkerTail":
			strategies = append(strategies, TokenMarkerTail{
				MaxTokens:     intValue(spec.Config["max_tokens"]),
				Threshold:     floatValue(spec.Config["threshold"]),
				KeepRecent:    intValue(spec.Config["keep_recent"]),
				SummaryFormat: stringValue(spec.Config["summary_format"]),
			})
		case "Compaction::SummaryTail":
			strategies = append(strategies, SummaryTail{
				Projection:  projection,
				Provider:    provider,
				Ingestor:    ingestor,
				MaxMessages: intValue(spec.Config["max_messages"]),
				KeepRecent:  intValue(spec.Config["keep_recent"]),
				Prompt:      stringValue(spec.Config["prompt"]),
			})
		case "Permission::DenyByName":
			strategies = append(strategies, DenyByName{
				Names:        stringSlice(spec.Config["names"]),
				ReasonFormat: stringValue(spec.Config["reason_format"]),
			})
		case "Permission::AlwaysAllow":
			strategies = append(strategies, AlwaysAllow{})
		case "Permission::HumanApproval":
			name := stringValue(spec.Config["prompt"])
			handler := handlers[name]
			if handler == nil {
				return nil, manifestError("strategy handler %q not in strategy_handlers", name)
			}
			strategies = append(strategies, HumanApproval{
				Prompt:       handler,
				DenialReason: stringValue(spec.Config["denial_reason"]),
			})
		default:
			return nil, manifestError("unknown canonical strategy: %q", spec.Name)
		}
	}
	return strategies, nil
}

func ProjectionFor(provider ProviderSpec, system string) Projection {
	switch provider.Kind {
	case "openai":
		return OpenAIProjection{Model: provider.Model, System: system}
	case "gemini":
		return GeminiProjection{Model: provider.Model, System: system}
	default:
		return AnthropicProjection{
			Model:     provider.Model,
			MaxTokens: provider.MaxTokens,
			System:    system,
		}
	}
}

func IngestorFor(kind string) Ingestor {
	switch kind {
	case "openai":
		return OpenAIIngestor{}
	case "gemini":
		return &GeminiIngestor{}
	default:
		return AnthropicIngestor{}
	}
}

func providerFor(kind string, options ManifestOptions) (Provider, error) {
	if options.Providers != nil {
		if provider := options.Providers[kind]; provider != nil {
			return provider, nil
		}
	}
	if kind == "mock" {
		return MockProvider{}, nil
	}
	key := apiKeyFor(kind, options.APIKeys)
	if key == "" {
		return nil, manifestError("api_keys[%q] is required for provider %s", kind, kind)
	}
	switch kind {
	case "anthropic":
		return NewAnthropicProvider(key), nil
	case "openai":
		return NewOpenAIProvider(key), nil
	case "gemini":
		return NewGeminiProvider(key), nil
	default:
		return nil, manifestError("unknown provider kind: %q", kind)
	}
}

func apiKeyFor(kind string, explicit map[string]string) string {
	if explicit != nil {
		if key := explicit[kind]; key != "" {
			return key
		}
	}
	switch kind {
	case "anthropic":
		return os.Getenv("ANTHROPIC_API_KEY")
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "gemini":
		return os.Getenv("GEMINI_API_KEY")
	default:
		return ""
	}
}

func knownProvider(kind string) bool {
	switch kind {
	case "anthropic", "openai", "gemini", "mock":
		return true
	default:
		return false
	}
}

func knownStrategy(name string) bool {
	switch name {
	case "Compaction::MarkerTail", "Compaction::SummaryTail",
		"Compaction::TokenMarkerTail", "Compaction::ToolOutputCap",
		"Permission::AlwaysAllow", "Permission::DenyByName", "Permission::HumanApproval":
		return true
	default:
		return false
	}
}

func manifestError(format string, args ...any) error {
	return ManifestError{Message: fmt.Sprintf(format, args...)}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	default:
		return 0
	}
}
