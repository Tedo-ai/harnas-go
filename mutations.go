package harnas

func ApplyMutations(log *Log) []Event {
	revoked := map[int]bool{}
	for _, event := range log.Events() {
		if event.Type == EventRevert {
			revoked[int(asFloat(event.Payload["revokes"]))] = true
		}
	}

	shadowed := map[int]bool{}
	summaryAt := map[int]Event{}
	for _, event := range log.Events() {
		if event.Type != EventCompact || revoked[event.Seq] {
			continue
		}
		replaces := asSlice(event.Payload["replaces"])
		if len(replaces) == 0 {
			continue
		}
		minSeq := int(asFloat(replaces[0]))
		for _, seqValue := range replaces {
			seq := int(asFloat(seqValue))
			shadowed[seq] = true
			if seq < minSeq {
				minSeq = seq
			}
		}
		summaryAt[minSeq] = event
	}

	out := []Event{}
	for _, event := range log.Events() {
		if event.Type == EventCompact || event.Type == EventRevert {
			continue
		}
		if compact, ok := summaryAt[event.Seq]; ok {
			out = append(out, Event{
				Seq:  compact.Seq,
				Type: EventSummary,
				Payload: map[string]any{
					"text": compact.Payload["summary"],
				},
			})
			continue
		}
		if shadowed[event.Seq] {
			continue
		}
		out = append(out, event)
	}
	return out
}
