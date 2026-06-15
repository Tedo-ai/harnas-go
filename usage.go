package harnas

func NormalizeUsage(value any) map[string]any {
	usage := asMap(value)
	if isCanonicalUsage(usage) {
		return map[string]any{
			"input_tokens":             int(asFloat(usage["input_tokens"])),
			"output_tokens":            int(asFloat(usage["output_tokens"])),
			"total_tokens":             int(asFloat(usage["total_tokens"])),
			"cache_read_input_tokens":  optionalInt(usage["cache_read_input_tokens"]),
			"cache_write_input_tokens": optionalInt(usage["cache_write_input_tokens"]),
			"reasoning_tokens":         optionalInt(usage["reasoning_tokens"]),
			"provider_raw":             usage["provider_raw"],
			"provenance":               stringValue(usage["provenance"]),
		}
	}
	input := int(asFloat(firstNonEmptyAny(usage["input_tokens"], usage["prompt_tokens"], usage["promptTokenCount"])))
	output := int(asFloat(firstNonEmptyAny(usage["output_tokens"], usage["completion_tokens"], usage["candidatesTokenCount"])))
	totalValue := firstNonEmptyAny(usage["total_tokens"], usage["totalTokenCount"])
	total := int(asFloat(totalValue))
	if total == 0 && (input > 0 || output > 0) {
		total = input + output
	}

	return map[string]any{
		"input_tokens":             input,
		"output_tokens":            output,
		"total_tokens":             total,
		"cache_read_input_tokens":  optionalInt(firstNonEmptyAny(nestedValue(usage, "prompt_tokens_details", "cached_tokens"), nestedValue(usage, "input_token_details", "cache_read"), usage["cache_read_input_tokens"])),
		"cache_write_input_tokens": optionalInt(firstNonEmptyAny(nestedValue(usage, "cache_creation", "input_tokens"), usage["cache_write_input_tokens"])),
		"reasoning_tokens":         optionalInt(firstNonEmptyAny(nestedValue(usage, "completion_tokens_details", "reasoning_tokens"), usage["reasoning_tokens"])),
		"provider_raw":             cloneProviderRaw(usage),
		"provenance":               usageProvenance(usage),
	}
}

func isCanonicalUsage(usage map[string]any) bool {
	if len(usage) == 0 {
		return false
	}
	_, hasInput := usage["input_tokens"]
	_, hasOutput := usage["output_tokens"]
	_, hasTotal := usage["total_tokens"]
	_, hasProvenance := usage["provenance"]
	_, hasProviderRaw := usage["provider_raw"]
	return hasInput && hasOutput && hasTotal && hasProvenance && hasProviderRaw
}

func normalizeAssistantPayload(payload map[string]any, provider, model string) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["usage"]; ok {
		payload["usage"] = NormalizeUsage(payload["usage"])
	} else {
		payload["usage"] = NormalizeUsage(nil)
	}
	if provider != "" {
		payload["provider"] = provider
	}
	if stringValue(payload["model"]) == "" && model != "" {
		payload["model"] = model
	}
	return payload
}

func nestedValue(input map[string]any, keys ...string) any {
	var current any = input
	for _, key := range keys {
		m := asMap(current)
		if len(m) == 0 {
			return nil
		}
		current = m[key]
	}
	return current
}

func optionalInt(value any) any {
	if value == nil {
		return nil
	}
	return int(asFloat(value))
}

func cloneProviderRaw(usage map[string]any) any {
	if len(usage) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, value := range usage {
		out[key] = value
	}
	return out
}

func usageProvenance(usage map[string]any) string {
	if len(usage) == 0 {
		return "unavailable"
	}
	return "provider_reported"
}
