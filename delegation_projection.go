package harnas

import (
	"fmt"
	"sort"
)

type SessionMap map[string]*Session

func (m SessionMap) LoadSession(id string) (*Session, error) {
	session, ok := m[id]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return session, nil
}

type SessionResolver interface {
	LoadSession(id string) (*Session, error)
}

func DelegationTree(sessionID string, resolver SessionResolver) (map[string]any, error) {
	return delegationTree(sessionID, resolver, map[string]bool{})
}

func delegationTree(sessionID string, resolver SessionResolver, visiting map[string]bool) (map[string]any, error) {
	if visiting[sessionID] {
		return nil, fmt.Errorf("delegation cycle detected at %s", sessionID)
	}
	visiting[sessionID] = true
	defer delete(visiting, sessionID)

	session, err := resolver.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}
	children := []map[string]any{}
	for _, spawn := range agentSpawns(session) {
		childID := stringValue(spawn.Payload["child_session_id"])
		if err := validateChildLink(session, spawn, resolver); err != nil {
			return nil, err
		}
		childTree, err := delegationTree(childID, resolver, visiting)
		if err != nil {
			return nil, err
		}
		result, err := agentResultForSpawn(session, stringValue(spawn.Payload["spawn_id"]))
		if err != nil {
			return nil, err
		}
		status := "open"
		var resultPayload any
		var errorPayload any
		if result != nil {
			status = stringValue(result.Payload["status"])
			resultPayload = result.Payload["result"]
			errorPayload = result.Payload["error"]
		} else if statusEvent := lastStatusForSpawn(session, stringValue(spawn.Payload["spawn_id"])); statusEvent != nil {
			status = stringValue(statusEvent.Payload["status"])
		}
		children = append(children, map[string]any{
			"spawn_id":         spawn.Payload["spawn_id"],
			"child_session_id": childID,
			"task":             spawn.Payload["task"],
			"join_policy":      firstNonEmptyAny(spawn.Payload["join_policy"], "async"),
			"metadata":         firstNonEmptyAny(spawn.Payload["metadata"], map[string]any{}),
			"status":           status,
			"result":           resultPayload,
			"error":            errorPayload,
			"children":         childTree["children"],
		})
	}
	return map[string]any{"session_id": session.ID, "children": children}, nil
}

func OpenChildren(sessionID string, resolver SessionResolver) ([]string, error) {
	session, err := resolver.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}
	open := []string{}
	for _, spawn := range agentSpawns(session) {
		spawnID := stringValue(spawn.Payload["spawn_id"])
		if err := validateChildLink(session, spawn, resolver); err != nil {
			return nil, err
		}
		result, err := agentResultForSpawn(session, spawnID)
		if err != nil {
			return nil, err
		}
		if result == nil {
			open = append(open, spawnID)
		}
	}
	return open, nil
}

func DescendantTimeline(sessionID string, resolver SessionResolver) ([]map[string]any, error) {
	sessions, err := collectDescendants(sessionID, resolver, map[string]bool{})
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, session := range sessions {
		for _, event := range session.Log.Events() {
			rows = append(rows, map[string]any{
				"session_id": session.ID,
				"seq":        event.Seq,
				"id":         event.ID,
				"type":       string(event.Type),
				"payload":    event.Payload,
				"timestamp":  stringValue(event.Payload["timestamp"]),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftTS := stringValue(rows[i]["timestamp"])
		rightTS := stringValue(rows[j]["timestamp"])
		if leftTS != rightTS {
			return leftTS < rightTS
		}
		if rows[i]["session_id"] != rows[j]["session_id"] {
			return stringValue(rows[i]["session_id"]) < stringValue(rows[j]["session_id"])
		}
		return int(asFloat(rows[i]["seq"])) < int(asFloat(rows[j]["seq"]))
	})
	return rows, nil
}

func DescendantUsage(sessionID string, resolver SessionResolver) (map[string]any, error) {
	sessions, err := collectDescendants(sessionID, resolver, map[string]bool{})
	if err != nil {
		return nil, err
	}
	promptTokens := 0
	completionTokens := 0
	totalTokens := 0
	for _, session := range sessions {
		for _, event := range session.Log.Events() {
			if event.Type == EventAssistantMessage {
				usage := asMap(event.Payload["usage"])
				in := int(asFloat(firstNonEmptyAny(usage["prompt_tokens"], usage["input_tokens"])))
				out := int(asFloat(firstNonEmptyAny(usage["completion_tokens"], usage["output_tokens"])))
				promptTokens += in
				completionTokens += out
				totalTokens += usageTotal(usage, in, out)
			}
			if event.Type == EventAgentResult {
				usage := asMap(event.Payload["usage"])
				in := int(asFloat(firstNonEmptyAny(usage["prompt_tokens"], usage["input_tokens"])))
				out := int(asFloat(firstNonEmptyAny(usage["completion_tokens"], usage["output_tokens"])))
				promptTokens += in
				completionTokens += out
				totalTokens += usageTotal(usage, in, out)
			}
		}
	}
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      totalTokens,
	}, nil
}

func collectDescendants(sessionID string, resolver SessionResolver, visiting map[string]bool) ([]*Session, error) {
	if visiting[sessionID] {
		return nil, fmt.Errorf("delegation cycle detected at %s", sessionID)
	}
	visiting[sessionID] = true
	defer delete(visiting, sessionID)
	session, err := resolver.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}
	out := []*Session{session}
	for _, spawn := range agentSpawns(session) {
		if err := validateChildLink(session, spawn, resolver); err != nil {
			return nil, err
		}
		childID := stringValue(spawn.Payload["child_session_id"])
		children, err := collectDescendants(childID, resolver, visiting)
		if err != nil {
			return nil, err
		}
		out = append(out, children...)
	}
	return out, nil
}

func validateChildLink(parent *Session, spawn Event, resolver SessionResolver) error {
	spawnID := stringValue(spawn.Payload["spawn_id"])
	childID := stringValue(spawn.Payload["child_session_id"])
	child, err := resolver.LoadSession(childID)
	if err != nil {
		return err
	}
	if child.ParentSessionID != parent.ID || child.SpawnID != spawnID {
		return fmt.Errorf("broken delegation link parent=%s spawn=%s child=%s", parent.ID, spawnID, childID)
	}
	return nil
}

func agentSpawns(session *Session) []Event {
	spawns := []Event{}
	for _, event := range session.Log.Events() {
		if event.Type == EventAgentSpawn {
			spawns = append(spawns, event)
		}
	}
	return spawns
}

func agentResultForSpawn(session *Session, spawnID string) (*Event, error) {
	var found *Event
	for _, event := range session.Log.Events() {
		if event.Type != EventAgentResult || stringValue(event.Payload["spawn_id"]) != spawnID {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple agent_result events for spawn_id %s", spawnID)
		}
		copied := event
		found = &copied
	}
	return found, nil
}

func lastStatusForSpawn(session *Session, spawnID string) *Event {
	var found *Event
	for _, event := range session.Log.Events() {
		if event.Type == EventAgentStatus && stringValue(event.Payload["spawn_id"]) == spawnID {
			copied := event
			found = &copied
		}
	}
	return found
}

func usageTotal(usage map[string]any, input int, output int) int {
	total := int(asFloat(usage["total_tokens"]))
	if total > 0 {
		return total
	}
	return input + output
}

func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if value != nil && value != "" {
			return value
		}
	}
	return nil
}
