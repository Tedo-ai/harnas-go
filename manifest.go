package harnas

import (
	"bytes"
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

type ValidationError struct{ ManifestError }
type UnsupportedVersionError struct{ ManifestError }
type UnknownProviderError struct{ ManifestError }
type UnknownStrategyError struct{ ManifestError }
type UnresolvedHandlerError struct{ ManifestError }

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
	StreamProviders  map[string]StreamProvider
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
	if err := validateManifestKeys(data); err != nil {
		return manifest, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, validationError("%s", err.Error())
	}
	return manifest, ValidateManifest(manifest)
}

func validateManifestKeys(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	required := []string{"harnas_version", "name", "provider", "tools", "strategies"}
	for _, key := range required {
		if _, ok := raw[key]; !ok {
			return validationError("manifest missing required field %q", key)
		}
	}
	if value, ok := raw["system"]; ok {
		var system string
		if err := json.Unmarshal(value, &system); err != nil {
			return validationError("%s", err.Error())
		}
		if system == "" {
			return validationError("system must not be empty")
		}
	}
	if err := validateProviderKeys(raw["provider"]); err != nil {
		return err
	}
	if err := validateToolKeys(raw["tools"]); err != nil {
		return err
	}
	if err := validateStrategyKeys(raw["strategies"]); err != nil {
		return err
	}
	return nil
}

func validateProviderKeys(raw json.RawMessage) error {
	var provider map[string]json.RawMessage
	if err := json.Unmarshal(raw, &provider); err != nil {
		return validationError("%s", err.Error())
	}
	for _, key := range []string{"kind", "max_tokens"} {
		if _, ok := provider[key]; !ok {
			return validationError("provider missing required field %q", key)
		}
	}
	return nil
}

func validateToolKeys(raw json.RawMessage) error {
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tools); err != nil {
		return validationError("%s", err.Error())
	}
	for index, tool := range tools {
		for _, key := range []string{"name", "handler", "description", "input_schema"} {
			if _, ok := tool[key]; !ok {
				return validationError("tools[%d] missing required field %q", index, key)
			}
		}
	}
	return nil
}

func validateStrategyKeys(raw json.RawMessage) error {
	var strategies []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &strategies); err != nil {
		return validationError("%s", err.Error())
	}
	for index, strategy := range strategies {
		if _, ok := strategy["name"]; !ok {
			return validationError("strategies[%d] missing required field %q", index, "name")
		}
	}
	return nil
}

func BuildManifest(manifest Manifest, options ManifestOptions) (*LoadedManifest, error) {
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	registry, err := BuildRegistry(manifest.Tools, options.ToolHandlers)
	if err != nil {
		return nil, err
	}
	projection := ProjectionForWithRegistry(manifest.Provider, manifest.System, registry)
	provider, err := providerFor(manifest.Provider.Kind, options)
	if err != nil {
		return nil, err
	}
	streamProvider, err := streamProviderFor(manifest.Provider.Kind, options)
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
		Name:           manifest.Name,
		Session:        CreateSession(map[string]any{"manifest_name": manifest.Name}),
		Projection:     projection,
		Provider:       provider,
		StreamProvider: streamProvider,
		Ingestor:       ingestor,
		Registry:       registry,
		Strategies:     strategies,
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
		return unsupportedVersionError("manifest version %q not in supported [%q]", manifest.Version, SupportedManifestVersion)
	}
	if manifest.Name == "" {
		return validationError("manifest name must not be empty")
	}
	if manifest.Provider.Kind == "" {
		return validationError("provider.kind is required")
	}
	if !knownProvider(manifest.Provider.Kind) {
		return unknownProviderError("unknown provider kind: %q", manifest.Provider.Kind)
	}
	if manifest.Provider.Kind != "mock" && manifest.Provider.Model == "" {
		return validationError("provider.model is required for provider %q", manifest.Provider.Kind)
	}
	if manifest.Provider.MaxTokens < 1 {
		return validationError("provider.max_tokens must be >= 1")
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
			return validationError("tool.name must not be empty")
		}
		if seen[tool.Name] {
			return validationError("duplicate tool name: %q", tool.Name)
		}
		seen[tool.Name] = true
		if tool.Handler == "" {
			return validationError("tool %q handler must not be empty", tool.Name)
		}
		if tool.InputSchema == nil {
			return validationError("tool %q input_schema is required", tool.Name)
		}
	}
	return nil
}

func validateStrategies(strategies []StrategySpec) error {
	pattern := regexp.MustCompile(`^[A-Z][A-Za-z0-9]+::[A-Z][A-Za-z0-9]+$`)
	for _, strategy := range strategies {
		if !pattern.MatchString(strategy.Name) {
			return validationError("strategy name %q is not canonical", strategy.Name)
		}
		if !knownStrategy(strategy.Name) {
			return unknownStrategyError("unknown canonical strategy: %q", strategy.Name)
		}
	}
	return nil
}

func BuildRegistry(specs []ToolSpec, handlers map[string]ToolHandler) (*Registry, error) {
	registry := NewRegistry()
	for _, spec := range specs {
		handler, ok := handlers[spec.Handler]
		if !ok {
			return nil, unresolvedHandlerError("tool handler %q not in tool_handlers", spec.Handler)
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
				return nil, unresolvedHandlerError("strategy handler %q not in strategy_handlers", name)
			}
			strategies = append(strategies, HumanApproval{
				Prompt:       handler,
				DenialReason: stringValue(spec.Config["denial_reason"]),
			})
		default:
			return nil, unknownStrategyError("unknown canonical strategy: %q", spec.Name)
		}
	}
	return strategies, nil
}

func ProjectionFor(provider ProviderSpec, system string) Projection {
	return ProjectionForWithRegistry(provider, system, nil)
}

func ProjectionForWithRegistry(provider ProviderSpec, system string, registry *Registry) Projection {
	switch provider.Kind {
	case "openai":
		return OpenAIProjection{Model: provider.Model, System: system, Registry: registry}
	case "gemini":
		return GeminiProjection{Model: provider.Model, System: system, Registry: registry}
	default:
		return AnthropicProjection{
			Model:     provider.Model,
			MaxTokens: provider.MaxTokens,
			System:    system,
			Registry:  registry,
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
		return nil, unknownProviderError("unknown provider kind: %q", kind)
	}
}

func streamProviderFor(kind string, options ManifestOptions) (StreamProvider, error) {
	if options.StreamProviders != nil {
		if provider := options.StreamProviders[kind]; provider != nil {
			return provider, nil
		}
	}
	if kind == "mock" {
		return nil, nil
	}
	key := apiKeyFor(kind, options.APIKeys)
	if key == "" {
		return nil, manifestError("api_keys[%q] is required for stream provider %s", kind, kind)
	}
	switch kind {
	case "anthropic":
		return NewAnthropicStreamProvider(key), nil
	case "openai":
		return NewOpenAIStreamProvider(key), nil
	case "gemini":
		return NewGeminiStreamProvider(key), nil
	default:
		return nil, unknownProviderError("unknown provider kind: %q", kind)
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

func validationError(format string, args ...any) error {
	return ValidationError{ManifestError{Message: fmt.Sprintf(format, args...)}}
}

func unsupportedVersionError(format string, args ...any) error {
	return UnsupportedVersionError{ManifestError{Message: fmt.Sprintf(format, args...)}}
}

func unknownProviderError(format string, args ...any) error {
	return UnknownProviderError{ManifestError{Message: fmt.Sprintf(format, args...)}}
}

func unknownStrategyError(format string, args ...any) error {
	return UnknownStrategyError{ManifestError{Message: fmt.Sprintf(format, args...)}}
}

func unresolvedHandlerError(format string, args ...any) error {
	return UnresolvedHandlerError{ManifestError{Message: fmt.Sprintf(format, args...)}}
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
