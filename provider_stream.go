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
	Complete()
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
	handler.Complete()
	return nil
}

func dispatchSSEBlock(lines []string, handler sseHandler) error {
	for _, line := range lines {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			return nil
		}
		return handler.Data(data)
	}
	return nil
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

func (s *streamState) Complete() {
	s.emit(EventArgs{Type: EventAssistantTurnDone, Payload: map[string]any{
		"turn_id":     s.turnID,
		"stop_reason": s.stop,
		"usage":       s.usage,
	}})
	s.emit(EventArgs{Type: EventAssistantMessage, Payload: map[string]any{
		"text":        strings.Join(s.textParts, ""),
		"stop_reason": s.stop,
		"usage":       s.usage,
	}})
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
	tools map[float64]*anthropicToolState
}

func newAnthropicStreamState(emit func(EventArgs)) *anthropicStreamState {
	return &anthropicStreamState{streamState: newStreamState(emit), tools: map[float64]*anthropicToolState{}}
}

func (s *anthropicStreamState) Data(data string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	switch payload["type"] {
	case "message_start":
		usage := asMap(asMap(payload["message"])["usage"])
		s.mergeUsage(usage)
	case "content_block_start":
		cb := asMap(payload["content_block"])
		if cb["type"] == "tool_use" {
			index := asFloat(payload["index"])
			tool := &anthropicToolState{ID: stringValue(cb["id"]), Name: stringValue(cb["name"])}
			s.tools[index] = tool
			s.emit(EventArgs{Type: EventToolUseBegin, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"name":        tool.Name,
			}})
		}
	case "content_block_delta":
		delta := asMap(payload["delta"])
		switch delta["type"] {
		case "text_delta":
			s.emitText(stringValue(delta["text"]))
		case "input_json_delta":
			tool := s.tools[asFloat(payload["index"])]
			if tool != nil {
				chunk := stringValue(delta["partial_json"])
				tool.ArgChunks = append(tool.ArgChunks, chunk)
				s.emit(EventArgs{Type: EventToolUseArgumentDelta, Payload: map[string]any{
					"turn_id":     s.turnID,
					"tool_use_id": tool.ID,
					"chunk":       chunk,
				}})
			}
		}
	case "content_block_stop":
		tool := s.tools[asFloat(payload["index"])]
		if tool != nil {
			tool.Arguments = parseArguments(tool.ArgChunks)
			s.emit(EventArgs{Type: EventToolUseEnd, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"arguments":   tool.Arguments,
			}})
		}
	case "message_delta":
		delta := asMap(payload["delta"])
		if stop := stringValue(delta["stop_reason"]); stop != "" {
			s.stop = anthropicStopReason(stop)
		}
		s.mergeUsage(asMap(payload["usage"]))
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

func (s *anthropicStreamState) Complete() {
	s.streamState.Complete()
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
	tools map[float64]*openAIToolState
}

func newOpenAIStreamState(emit func(EventArgs)) *openAIStreamState {
	return &openAIStreamState{streamState: newStreamState(emit), tools: map[float64]*openAIToolState{}}
}

func (s *openAIStreamState) Data(data string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
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
		s.handleDelta(delta)
	}
	if finish := stringValue(choice["finish_reason"]); finish != "" {
		s.stop = openAIStopReason(finish)
		for _, tool := range s.tools {
			tool.Arguments = parseArguments(tool.ArgChunks)
			s.emit(EventArgs{Type: EventToolUseEnd, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"arguments":   tool.Arguments,
			}})
		}
	}
	return nil
}

func (s *openAIStreamState) handleDelta(delta map[string]any) {
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
			tool.ArgChunks = append(tool.ArgChunks, chunk)
			s.emit(EventArgs{Type: EventToolUseArgumentDelta, Payload: map[string]any{
				"turn_id":     s.turnID,
				"tool_use_id": tool.ID,
				"chunk":       chunk,
			}})
		}
	}
}

func (s *openAIStreamState) Complete() {
	s.streamState.Complete()
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
	tools []geminiToolState
}

func newGeminiStreamState(emit func(EventArgs)) *geminiStreamState {
	return &geminiStreamState{streamState: newStreamState(emit)}
}

func (s *geminiStreamState) Data(data string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	candidate := firstMap(payload["candidates"])
	for _, raw := range asSlice(asMap(candidate["content"])["parts"]) {
		part := asMap(raw)
		if text := stringValue(part["text"]); text != "" {
			s.emitText(text)
		}
		if functionCall := asMap(part["functionCall"]); len(functionCall) > 0 {
			id := fmt.Sprintf("gemini_fc_%d", len(s.tools))
			tool := geminiToolState{
				ID:        id,
				Name:      stringValue(functionCall["name"]),
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
		s.stop = geminiStopReason(finish)
	}
	if usage := asMap(payload["usageMetadata"]); len(usage) > 0 {
		s.usage["input_tokens"] = usage["promptTokenCount"]
		s.usage["output_tokens"] = usage["candidatesTokenCount"]
	}
	return nil
}

func (s *geminiStreamState) Complete() {
	s.streamState.Complete()
	for _, tool := range s.tools {
		s.emit(EventArgs{Type: EventToolUse, Payload: map[string]any{
			"id":        tool.ID,
			"name":      tool.Name,
			"arguments": tool.Arguments,
		}})
	}
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

func parseArguments(chunks []string) map[string]any {
	joined := strings.Join(chunks, "")
	if joined == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(joined), &out); err != nil {
		return map[string]any{}
	}
	return out
}
