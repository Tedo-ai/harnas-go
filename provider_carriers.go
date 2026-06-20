package harnas

func providerCarrierWire(carriers any, destination string) (any, bool) {
	for _, raw := range asSlice(carriers) {
		carrier := asMap(raw)
		if stringValue(carrier["carrier_destination"]) != destination {
			continue
		}
		if wire, ok := carrier["wire"]; ok {
			return wire, true
		}
	}
	return nil, false
}

func providerCarrierWires(carriers any, destination string) ([]any, bool) {
	wire, ok := providerCarrierWire(carriers, destination)
	if !ok {
		return nil, false
	}
	return asSlice(wire), true
}

func providerPartWire(block map[string]any, destination string) (map[string]any, bool) {
	wire, ok := providerCarrierWire(block["provider_parts"], destination)
	if !ok {
		return nil, false
	}
	return asMap(wire), true
}

func providerCarrier(destination string, index int, kind string, wire any, refs []string) map[string]any {
	carrier := map[string]any{
		"carrier_destination": destination,
		"index":               index,
		"kind":                kind,
		"wire":                wire,
	}
	if len(refs) > 0 {
		carrier["canonical_refs"] = refs
	}
	return carrier
}
