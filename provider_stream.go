package harnas

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Kind identifies this provider in Observation and provider_error events.
func (p AnthropicStreamProvider) Kind() string { return "anthropic" }

type AnthropicStreamProvider struct {
	APIKey     string
	APIVersion string
	Endpoint   string
	Client     HTTPDoer
}

func NewAnthropicStreamProvider(apiKey string) AnthropicStreamProvider {
	return AnthropicStreamProvider{APIKey: apiKey}
}

func (p AnthropicStreamProvider) Call(request map[string]any, emit func(EventArgs)) error {
	body := copyMap(request)
	body["stream"] = true
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = AnthropicEndpoint
	}
	apiVersion := p.APIVersion
	if apiVersion == "" {
		apiVersion = AnthropicAPIVersion
	}
	return streamSSE(p.client(), endpoint, map[string]string{
		"x-api-key":         p.APIKey,
		"anthropic-version": apiVersion,
		"content-type":      "application/json",
		"accept":            "text/event-stream",
	}, body, newAnthropicStreamState(emit))
}

// Kind identifies this provider in Observation and provider_error events.
func (p OpenAIStreamProvider) Kind() string { return "openai" }

type OpenAIStreamProvider struct {
	APIKey   string
	Endpoint string
	Client   HTTPDoer
	NoAuth   bool
}

func NewOpenAIStreamProvider(apiKey string) OpenAIStreamProvider {
	return OpenAIStreamProvider{APIKey: apiKey}
}

func (p OpenAIStreamProvider) Call(request map[string]any, emit func(EventArgs)) error {
	body := copyMap(request)
	body["stream"] = true
	body["stream_options"] = map[string]any{"include_usage": true}
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = OpenAIEndpoint
	}
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
	}
	if !p.NoAuth {
		headers["authorization"] = "Bearer " + p.APIKey
	}
	return streamSSE(p.client(), endpoint, headers, body, newOpenAIStreamState(emit))
}

// Kind identifies this provider in Observation and provider_error events.
func (p OllamaStreamProvider) Kind() string { return "ollama" }

type OllamaStreamProvider struct {
	BaseURL string
	Client  HTTPDoer
}

func NewOllamaStreamProvider(baseURL string) OllamaStreamProvider {
	return OllamaStreamProvider{BaseURL: baseURL}
}

func (p OllamaStreamProvider) Call(request map[string]any, emit func(EventArgs)) error {
	return (OpenAIStreamProvider{
		Endpoint: ollamaChatEndpoint(p.BaseURL),
		Client:   p.Client,
		NoAuth:   true,
	}).Call(request, emit)
}

// Kind identifies this provider in Observation and provider_error events.
func (p GeminiStreamProvider) Kind() string { return "gemini" }

type GeminiStreamProvider struct {
	APIKey       string
	EndpointBase string
	Client       HTTPDoer
}

func NewGeminiStreamProvider(apiKey string) GeminiStreamProvider {
	return GeminiStreamProvider{APIKey: apiKey}
}

func (p GeminiStreamProvider) Call(request map[string]any, emit func(EventArgs)) error {
	model, ok := request["model"].(string)
	if !ok || model == "" {
		return ProviderError{Message: "Gemini request must include 'model'"}
	}
	body := copyMap(request)
	delete(body, "model")
	endpointBase := p.EndpointBase
	if endpointBase == "" {
		endpointBase = GeminiEndpointBase
	}
	endpoint := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse", endpointBase, model)
	return streamSSE(p.client(), endpoint, map[string]string{
		"x-goog-api-key": p.APIKey,
		"content-type":   "application/json",
		"accept":         "text/event-stream",
	}, body, newGeminiStreamState(emit))
}

func (p AnthropicStreamProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

func (p OpenAIStreamProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

func (p GeminiStreamProvider) client() HTTPDoer {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: DefaultProviderHTTPTimeout}
}

type sseHandler interface {
	Start()
	Data(string) error
	Complete() error
	Fail(error)
}

func streamSSE(client HTTPDoer, endpoint string, headers map[string]string, body map[string]any, handler sseHandler) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultProviderHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	handler.Start()
	response, err := client.Do(req)
	if err != nil {
		handler.Fail(err)
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		parsed, parseErr := parseJSONBody(response.Body)
		if parseErr != nil {
			parsed = map[string]any{"raw": parseErr.Error()}
		}
		err := HTTPError{Status: response.StatusCode, Body: parsed}
		handler.Fail(err)
		return err
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	block := []string{}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := dispatchSSEBlock(block, handler); err != nil {
				handler.Fail(err)
				return err
			}
			block = block[:0]
			continue
		}
		block = append(block, line)
	}
	if err := scanner.Err(); err != nil {
		handler.Fail(err)
		return err
	}
	if len(block) > 0 {
		if err := dispatchSSEBlock(block, handler); err != nil {
			handler.Fail(err)
			return err
		}
	}
	if err := handler.Complete(); err != nil {
		handler.Fail(err)
		return err
	}
	return nil
}

func dispatchSSEBlock(lines []string, handler sseHandler) error {
	dataLines := make([]string, 0, 1)
	for _, line := range lines {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimPrefix(line, "data:")
		if strings.HasPrefix(data, " ") {
			data = data[1:]
		}
		dataLines = append(dataLines, data)
	}
	if len(dataLines) == 0 {
		return nil
	}
	data := strings.Join(dataLines, "\n")
	if data == "" {
		return nil
	}
	return handler.Data(data)
}

type streamState struct {
	emit      func(EventArgs)
	turnID    string
	textParts []string
	stop      string
	usage     map[string]any
}

func newStreamState(emit func(EventArgs)) streamState {
	return streamState{
		emit:   emit,
		turnID: "turn_" + newID(),
		stop:   "other",
		usage:  map[string]any{"input_tokens": float64(0), "output_tokens": float64(0)},
	}
}

func (s *streamState) Start() {
	s.emit(EventArgs{Type: EventAssistantTurnStarted, Payload: map[string]any{"turn_id": s.turnID}})
}

func (s *streamState) emitText(chunk string) {
	if chunk == "" {
		return
	}
	s.textParts = append(s.textParts, chunk)
	s.emit(EventArgs{Type: EventAssistantTextDelta, Payload: map[string]any{
		"turn_id": s.turnID,
		"chunk":   chunk,
	}})
}

func (s *streamState) Complete() error {
	s.emit(EventArgs{Type: EventAssistantTurnDone, Payload: map[string]any{
		"turn_id":     s.turnID,
		"stop_reason": s.stop,
		"usage":       s.usage,
	}})
	s.emit(EventArgs{Type: EventAssistantMessage, Payload: map[string]any{
		"text":        strings.Join(s.textParts, ""),
		"stop_reason": s.stop,
		"usage":       NormalizeUsage(s.usage),
	}})
	return nil
}

func (s *streamState) Fail(err error) {
	s.emit(EventArgs{Type: EventAssistantTurnFailed, Payload: map[string]any{
		"turn_id": s.turnID,
		"error":   err.Error(),
	}})
}

type anthropicToolState struct {
	ID        string
	Name      string
	ArgChunks []string
	Arguments map[string]any
}

type anthropicStreamState struct {
	streamState
	tools          map[float64]*anthropicToolState
	openBlocks     map[float64]string
	messageStarted bool
	messageStopped bool
	stopSeen       bool
}

func newAnthropicStreamState(emit func(EventArgs)) *anthropicStreamState {
	return &anthropicStreamState{
		streamState: newStreamState(emit),
		tools:       map[float64]*anthropicToolState{},
		openBlocks:  map[float64]string{},
	}
}

func (s *anthropicStreamState) Data(data string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return protocolError("anthropic", "invalid_json", "invalid SSE JSON: "+err.Error())
	}
	eventType := stringValue(payload["type"])
	if eventType == "" {
		return protocolError("anthropic", "invalid_frame", "SSE event is missing type")
	}
	switch eventType {
	case "error":
		providerError := asMap(payload["error"])
		errorType := stringValue(providerError["type"])
		return ProviderStreamError{
			Provider:  "anthropic",
			Type:      errorType,
			Message:   stringValue(providerError["message"]),
			RequestID: stringValue(payload["request_id"]),
			Status:    anthropicErrorStatus(errorType),
		}
	case "ping":
		// Anthropic may interleave any number of pings.
		return nil
	case "message_start":
		if s.messageStarted {
			return protocolError("anthropic", "duplicate_start", "duplicate message_start event")
		}
		if s.messageStopped {
			return protocolError("anthropic", "invalid_order", "message_start arrived after message_stop")
		}
		s.messageStarted = true
		usage := asMap(asMap(payload["message"])["usage"])
		s.mergeUsage(usage)
	case "content_block_start":
		if err := s.requireActive(eventType); err != nil {
			return err
		}
		index := asFloat(payload["index"])
		if _, exists := s.openBlocks[index]; exists {
			return protocolError("anthropic", "duplicate_block_start", "duplicate content_block_start index")
		}
		cb := asMap(payload["content_block"])
		blockType := stringValue(cb["type"])
		if blockType == "" {
			return protocolError("anthropic", "invalid_frame", "content_block_start is missing content_block.type")
		}
		s.openBlocks[index] = blockType
		if blockType == "tool_use" {
			tool := &anthropicToolState{ID: stringValue(cb["id"]), Name: stringValue(cb["name"])}
			if tool.ID == "" || tool.Name == "" {
				return protocolError("anthropic", "invalid_tool", "tool_use block requires id and name")
			}
			s.tools[index] = tool
			s.emit(EventArgs{Type: EventToolUseBegin, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"name":        tool.Name,
			}})
		}
	case "content_block_delta":
		if err := s.requireActive(eventType); err != nil {
			return err
		}
		index := asFloat(payload["index"])
		blockType, exists := s.openBlocks[index]
		if !exists {
			return protocolError("anthropic", "invalid_order", "content_block_delta has no open block")
		}
		delta := asMap(payload["delta"])
		switch delta["type"] {
		case "text_delta":
			if blockType == "tool_use" {
				return protocolError("anthropic", "invalid_frame", "text delta arrived for tool_use block")
			}
			s.emitText(stringValue(delta["text"]))
		case "input_json_delta":
			tool := s.tools[index]
			if tool == nil {
				return protocolError("anthropic", "invalid_frame", "input_json_delta arrived outside tool_use block")
			}
			chunk := stringValue(delta["partial_json"])
			tool.ArgChunks = append(tool.ArgChunks, chunk)
			s.emit(EventArgs{Type: EventToolUseArgumentDelta, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"chunk":       chunk,
			}})
		default:
			return protocolError("anthropic", "invalid_frame", "content_block_delta has unknown delta.type")
		}
	case "content_block_stop":
		if err := s.requireActive(eventType); err != nil {
			return err
		}
		index := asFloat(payload["index"])
		if _, exists := s.openBlocks[index]; !exists {
			return protocolError("anthropic", "invalid_order", "content_block_stop has no open block")
		}
		delete(s.openBlocks, index)
		tool := s.tools[index]
		if tool != nil {
			arguments, err := parseArgumentsStrict(tool.ArgChunks)
			if err != nil {
				return protocolError("anthropic", "invalid_tool_arguments", err.Error())
			}
			tool.Arguments = arguments
			s.emit(EventArgs{Type: EventToolUseEnd, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"arguments":   tool.Arguments,
			}})
		}
	case "message_delta":
		if err := s.requireActive(eventType); err != nil {
			return err
		}
		delta := asMap(payload["delta"])
		if stop := stringValue(delta["stop_reason"]); stop != "" {
			if s.stopSeen {
				return protocolError("anthropic", "duplicate_terminal", "duplicate stop_reason")
			}
			s.stop = anthropicStopReason(stop)
			s.stopSeen = true
		}
		s.mergeUsage(asMap(payload["usage"]))
	case "message_stop":
		if !s.messageStarted {
			return protocolError("anthropic", "invalid_order", "message_stop arrived before message_start")
		}
		if s.messageStopped {
			return protocolError("anthropic", "duplicate_terminal", "duplicate message_stop event")
		}
		if len(s.openBlocks) != 0 {
			return protocolError("anthropic", "incomplete_block", "message_stop arrived with an open content block")
		}
		if !s.stopSeen {
			return protocolError("anthropic", "missing_stop_reason", "message_stop arrived without a stop_reason")
		}
		s.messageStopped = true
	default:
		// Anthropic may add new event types. Unknown events are forward-compatible
		// as long as the required message_start/message_stop lifecycle remains valid.
	}
	return nil
}

func (s *anthropicStreamState) requireActive(eventType string) error {
	if !s.messageStarted {
		return protocolError("anthropic", "invalid_order", eventType+" arrived before message_start")
	}
	if s.messageStopped {
		return protocolError("anthropic", "invalid_order", eventType+" arrived after message_stop")
	}
	return nil
}

func (s *anthropicStreamState) mergeUsage(usage map[string]any) {
	if len(usage) == 0 {
		return
	}
	if input, ok := usage["input_tokens"]; ok {
		s.usage["input_tokens"] = input
	}
	if output, ok := usage["output_tokens"]; ok {
		s.usage["output_tokens"] = output
	}
}

func (s *anthropicStreamState) Complete() error {
	if !s.messageStarted {
		return protocolError("anthropic", "missing_start", "stream ended before message_start")
	}
	if !s.messageStopped {
		return protocolError("anthropic", "missing_terminal", "stream ended before message_stop")
	}
	if !s.stopSeen {
		return protocolError("anthropic", "missing_stop_reason", "stream ended without a stop_reason")
	}
	if err := s.streamState.Complete(); err != nil {
		return err
	}
	keys := make([]float64, 0, len(s.tools))
	for key := range s.tools {
		keys = append(keys, key)
	}
	sort.Float64s(keys)
	for _, key := range keys {
		tool := s.tools[key]
		s.emit(EventArgs{Type: EventToolUse, Payload: map[string]any{
			"id":        tool.ID,
			"name":      tool.Name,
			"arguments": tool.Arguments,
		}})
	}
	return nil
}

func anthropicErrorStatus(errorType string) int {
	switch errorType {
	case "invalid_request_error":
		return http.StatusBadRequest
	case "authentication_error":
		return http.StatusUnauthorized
	case "billing_error":
		return http.StatusPaymentRequired
	case "permission_error":
		return http.StatusForbidden
	case "not_found_error":
		return http.StatusNotFound
	case "request_too_large":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error":
		return http.StatusTooManyRequests
	case "api_error":
		return http.StatusInternalServerError
	case "timeout_error":
		return http.StatusGatewayTimeout
	case "overloaded_error":
		return 529
	default:
		return 0
	}
}

func anthropicStopReason(stop string) string {
	switch stop {
	case "end_turn":
		return "end_turn"
	case "max_tokens":
		return "max_tokens"
	case "tool_use":
		return "tool_use"
	case "stop_sequence":
		return "stop_sequence"
	case "refusal":
		return "refusal"
	default:
		return "other"
	}
}

type openAIToolState struct {
	ID           string
	Name         string
	ArgChunks    []string
	Arguments    map[string]any
	EmittedBegin bool
}

type openAIStreamState struct {
	streamState
	tools      map[float64]*openAIToolState
	finishSeen bool
	doneSeen   bool
}

func newOpenAIStreamState(emit func(EventArgs)) *openAIStreamState {
	return &openAIStreamState{streamState: newStreamState(emit), tools: map[float64]*openAIToolState{}}
}

func (s *openAIStreamState) Data(data string) error {
	if data == "[DONE]" {
		if s.doneSeen {
			return protocolError("openai", "duplicate_terminal", "duplicate [DONE] sentinel")
		}
		s.doneSeen = true
		return nil
	}
	if s.doneSeen {
		return protocolError("openai", "invalid_order", "data arrived after [DONE]")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return protocolError("openai", "invalid_json", "invalid SSE JSON: "+err.Error())
	}
	if rawError := asMap(payload["error"]); len(rawError) > 0 {
		errorType := stringValue(rawError["type"])
		if errorType == "" {
			errorType = stringValue(rawError["code"])
		}
		return ProviderStreamError{
			Provider:  "openai",
			Type:      errorType,
			Message:   stringValue(rawError["message"]),
			RequestID: stringValue(payload["request_id"]),
			Status:    int(asFloat(rawError["status"])),
		}
	}
	if usage := asMap(payload["usage"]); len(usage) > 0 {
		s.usage["input_tokens"] = usage["prompt_tokens"]
		s.usage["output_tokens"] = usage["completion_tokens"]
	}
	choice := firstMap(payload["choices"])
	if len(choice) == 0 {
		return nil
	}
	if delta := asMap(choice["delta"]); len(delta) > 0 {
		if s.finishSeen {
			return protocolError("openai", "invalid_order", "delta arrived after finish_reason")
		}
		if err := s.handleDelta(delta); err != nil {
			return err
		}
	}
	if finish := stringValue(choice["finish_reason"]); finish != "" {
		if s.finishSeen {
			return protocolError("openai", "duplicate_terminal", "duplicate finish_reason")
		}
		s.finishSeen = true
		s.stop = openAIStopReason(finish)
		for _, tool := range s.tools {
			if !tool.EmittedBegin {
				return protocolError("openai", "invalid_tool", "tool call completed without id and name")
			}
			arguments, err := parseArgumentsStrict(tool.ArgChunks)
			if err != nil {
				return protocolError("openai", "invalid_tool_arguments", err.Error())
			}
			tool.Arguments = arguments
			s.emit(EventArgs{Type: EventToolUseEnd, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"arguments":   tool.Arguments,
			}})
		}
	}
	return nil
}

func (s *openAIStreamState) handleDelta(delta map[string]any) error {
	s.emitText(stringValue(delta["content"]))
	for _, raw := range asSlice(delta["tool_calls"]) {
		call := asMap(raw)
		index := asFloat(call["index"])
		tool := s.tools[index]
		if tool == nil {
			tool = &openAIToolState{}
			s.tools[index] = tool
		}
		if id := stringValue(call["id"]); id != "" {
			tool.ID = id
		}
		function := asMap(call["function"])
		if name := stringValue(function["name"]); name != "" {
			tool.Name = name
		}
		if tool.ID != "" && tool.Name != "" && !tool.EmittedBegin {
			tool.EmittedBegin = true
			s.emit(EventArgs{Type: EventToolUseBegin, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"name":        tool.Name,
			}})
		}
		if chunk := stringValue(function["arguments"]); chunk != "" {
			if !tool.EmittedBegin {
				return protocolError("openai", "invalid_tool", "tool arguments arrived before id and name")
			}
			tool.ArgChunks = append(tool.ArgChunks, chunk)
			s.emit(EventArgs{Type: EventToolUseArgumentDelta, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"chunk":       chunk,
			}})
		}
	}
	return nil
}

func (s *openAIStreamState) Complete() error {
	if !s.doneSeen {
		return protocolError("openai", "missing_terminal", "stream ended before [DONE]")
	}
	if !s.finishSeen {
		return protocolError("openai", "missing_finish_reason", "stream ended without finish_reason")
	}
	if err := s.streamState.Complete(); err != nil {
		return err
	}
	keys := make([]float64, 0, len(s.tools))
	for key := range s.tools {
		keys = append(keys, key)
	}
	sort.Float64s(keys)
	for _, key := range keys {
		tool := s.tools[key]
		s.emit(EventArgs{Type: EventToolUse, Payload: map[string]any{
			"id":        tool.ID,
			"name":      tool.Name,
			"arguments": tool.Arguments,
		}})
	}
	return nil
}

func openAIStopReason(stop string) string {
	switch stop {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "other"
	}
}

type geminiToolState struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type geminiStreamState struct {
	streamState
	tools      []geminiToolState
	finishSeen bool
}

func newGeminiStreamState(emit func(EventArgs)) *geminiStreamState {
	return &geminiStreamState{streamState: newStreamState(emit)}
}

func (s *geminiStreamState) Data(data string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return protocolError("gemini", "invalid_json", "invalid SSE JSON: "+err.Error())
	}
	if rawError := asMap(payload["error"]); len(rawError) > 0 {
		errorType := stringValue(rawError["status"])
		if errorType == "" {
			errorType = stringValue(rawError["type"])
		}
		return ProviderStreamError{
			Provider:  "gemini",
			Type:      errorType,
			Message:   stringValue(rawError["message"]),
			RequestID: stringValue(payload["request_id"]),
			Status:    int(asFloat(rawError["code"])),
		}
	}
	if s.finishSeen {
		return protocolError("gemini", "invalid_order", "data arrived after finishReason")
	}
	candidate := firstMap(payload["candidates"])
	for _, raw := range asSlice(asMap(candidate["content"])["parts"]) {
		part := asMap(raw)
		if text := stringValue(part["text"]); text != "" {
			s.emitText(text)
		}
		if functionCall := asMap(part["functionCall"]); len(functionCall) > 0 {
			name := stringValue(functionCall["name"])
			if name == "" {
				return protocolError("gemini", "invalid_tool", "functionCall requires name")
			}
			id := fmt.Sprintf("gemini_fc_%d", len(s.tools))
			tool := geminiToolState{
				ID:        id,
				Name:      name,
				Arguments: asMap(functionCall["args"]),
			}
			s.tools = append(s.tools, tool)
			s.emit(EventArgs{Type: EventToolUseBegin, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"name":        tool.Name,
			}})
			s.emit(EventArgs{Type: EventToolUseEnd, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"arguments":   tool.Arguments,
			}})
		}
	}
	if finish := stringValue(candidate["finishReason"]); finish != "" {
		if s.finishSeen {
			return protocolError("gemini", "duplicate_terminal", "duplicate finishReason")
		}
		s.finishSeen = true
		s.stop = geminiStopReason(finish)
	}
	if usage := asMap(payload["usageMetadata"]); len(usage) > 0 {
		s.usage["input_tokens"] = usage["promptTokenCount"]
		s.usage["output_tokens"] = usage["candidatesTokenCount"]
	}
	return nil
}

func (s *geminiStreamState) Complete() error {
	if !s.finishSeen {
		return protocolError("gemini", "missing_terminal", "stream ended before finishReason")
	}
	if err := s.streamState.Complete(); err != nil {
		return err
	}
	for _, tool := range s.tools {
		s.emit(EventArgs{Type: EventToolUse, Payload: map[string]any{
			"id":        tool.ID,
			"name":      tool.Name,
			"arguments": tool.Arguments,
		}})
	}
	return nil
}

func geminiStopReason(stop string) string {
	switch stop {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "SAFETY", "RECITATION":
		return "refusal"
	case "OTHER":
		return "other"
	default:
		return "other"
	}
}

func parseArgumentsStrict(chunks []string) (map[string]any, error) {
	joined := strings.Join(chunks, "")
	if joined == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(joined), &out); err != nil {
		return nil, fmt.Errorf("tool arguments are not valid JSON: %w", err)
	}
	if out == nil {
		return nil, fmt.Errorf("tool arguments must be a JSON object")
	}
	return out, nil
}

func protocolError(provider, reason, message string) ProviderProtocolError {
	return ProviderProtocolError{Provider: provider, Reason: reason, Message: message}
}
