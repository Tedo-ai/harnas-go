package conformance

import "fmt"

type ScriptedProvider struct {
	responses []map[string]any
}

func NewScriptedProvider(responses []map[string]any) *ScriptedProvider {
	return &ScriptedProvider{responses: append([]map[string]any(nil), responses...)}
}

func (p *ScriptedProvider) Call(_ map[string]any) (map[string]any, error) {
	if len(p.responses) == 0 {
		return nil, fmt.Errorf("scripted provider exhausted")
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}
