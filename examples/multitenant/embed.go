// Package multitenant is a reference for embedding harnas-go in a concurrent,
// multi-tenant Go server (the shape Tedo/Ovin uses): one Session per
// conversation, persisted to a SQL StorageAdapter, tools routed through the
// host's registry behind an enforce-by-default pre_tool_use gate, and the
// Observation bus bridged to distributed tracing.
//
// It compiles against harnas-go and the runnable Example uses MemoryStorageAdapter
// + MockProvider so it runs with no DB or API keys. Every seam is annotated with
// how a real stack (an operations registry, imprint/OTel tracing, Postgres,
// per-workspace tenancy) plugs in. See README.md for the full mapping.
package multitenant

import (
	"database/sql"
	"errors"
	"fmt"

	harnas "github.com/Tedo-ai/harnas-go"
)

// ---------------------------------------------------------------------------
// 1. Session lifecycle: one Session per conversation/agent-run, loaded from and
//    persisted to the StorageAdapter. NOT one per HTTP request — a conversation
//    is a single Session resumed across many requests.
// ---------------------------------------------------------------------------

// loadSession rebuilds a Session's Log from the adapter (resume) or returns a
// fresh one (first turn). It returns the count of already-persisted events so
// persistNewEvents knows where the turn's new events begin.
func loadSession(adapter harnas.StorageAdapter, sessionID string, metadata map[string]any) (*harnas.Session, int, error) {
	rows, err := adapter.EventsSince(nil)
	if err != nil {
		return nil, 0, err
	}
	log := harnas.NewLog()
	for _, row := range rows {
		log.Restore(harnas.Event{
			ID:        row.ID,
			Seq:       row.Seq,
			Timestamp: row.Timestamp,
			Type:      row.Type,
			Payload:   row.Payload,
		})
	}
	return harnas.NewSession(sessionID, log, metadata), len(rows), nil
}

// persistNewEvents writes the events appended during the turn back to the
// adapter, each with the OCC fence (expectedNextSeq). A StorageConflictError
// means another instance advanced this session concurrently.
//
// IMPORTANT: this runs AFTER loop.Run(), so the OCC fence is post-execution —
// two concurrent same-session turns can both run tools before the loser
// conflicts here. Use claimTurn (below) to fence BEFORE side effects.
func persistNewEvents(adapter harnas.StorageAdapter, session *harnas.Session, alreadyPersisted int) error {
	events := session.Log.Events()
	for _, e := range events[alreadyPersisted:] {
		expected := e.Seq
		_, err := adapter.AppendEvent(harnas.EventDraft{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			Type:      e.Type,
			Payload:   e.Payload,
		}, &expected)
		if err != nil {
			var conflict *harnas.StorageConflictError
			if errors.As(err, &conflict) {
				return fmt.Errorf("session advanced concurrently (retry the turn): %w", err)
			}
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// 2. Pre-execution turn claim — the cross-instance OCC fence the storage-layer
//    fence cannot give you, because it fires before any provider/tool side
//    effect. Acquire it before loop.Run(); release after persistNewEvents.
//    Semantics: one in-flight turn per session, which is correct (turn N+1
//    depends on N). This is intentionally a host concern (orchestration policy).
// ---------------------------------------------------------------------------

// claimTurn tries to take an exclusive, fail-fast lease on a session before any
// side effects run. The advisory-lock key scopes by tenant so it is unique
// cross-instance. Returns false if another instance owns the turn.
func claimTurn(db *sql.DB, workspaceID, sessionID string) (release func(), ok bool, err error) {
	var locked bool
	// hashtextlikes a single text key; compose tenant+session so it is unique.
	key := workspaceID + ":" + sessionID
	if err = db.QueryRow(`SELECT pg_try_advisory_lock(hashtext($1))`, key).Scan(&locked); err != nil {
		return nil, false, err
	}
	if !locked {
		return nil, false, nil // ErrSessionBusy: another instance has the turn
	}
	return func() { _, _ = db.Exec(`SELECT pg_advisory_unlock(hashtext($1))`, key) }, true, nil
}

// ---------------------------------------------------------------------------
// 3. Tools via the host registry, behind an enforce-by-default permission gate.
// ---------------------------------------------------------------------------

// buildRegistry adapts host operations into harnas tools. Each tool's Call is a
// closure that funnels through the host's invoke path (auth/scope/quota/
// idempotency/tracing) — never the raw handler. In Ovin this closure is
// operations.Invoke(ctx, mw.Call{OperationKey: name, ...}); here it is a stub.
func buildRegistry(invoke func(name string, args map[string]any) (string, error), specs []ToolSpec) (*harnas.Registry, error) {
	reg := harnas.NewRegistry()
	for _, s := range specs {
		name := s.Name
		err := reg.Register(harnas.Tool{
			Name:        name,
			Handler:     name,
			Description: s.Description,
			InputSchema: s.InputSchema,
			Call:        func(args map[string]any) (string, error) { return invoke(name, args) },
		})
		if err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// ToolSpec is the host's view of an AI-enabled operation (Ovin: the AIEnabled
// subset of OperationDefinition, keyed by op.Key).
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// installEnforceGate registers an enforce-by-default pre_tool_use hook: return
// {allow:false, reason} to deny, nil to allow. Any one deny wins. Returning
// nil here = allow; a real policy checks the actor's permissions for the tool.
func installEnforceGate(session *harnas.Session, allowed func(toolName string) (bool, string)) {
	session.Hooks.On("pre_tool_use", func(ctx map[string]any) any {
		toolUse, _ := ctx["tool_use"].(harnas.Event)
		name, _ := toolUse.Payload["name"].(string)
		if ok, reason := allowed(name); !ok {
			return map[string]any{"allow": false, "reason": reason}
		}
		return nil
	})
}

// ---------------------------------------------------------------------------
// 4. Observation -> tracing bridge. Map provider calls to spans; correlate
//    tools by tool_use_id across the event_appended pair. Seed the trace from
//    the inbound traceparent via Session metadata.trace.* at construction.
// ---------------------------------------------------------------------------

// Tracer is the host tracing surface (Ovin: internal/tracing.Client over imprint-go).
type Tracer interface {
	StartSpan(name string, attrs map[string]any) (end func(map[string]any))
}

func bridgeObservationToTracing(session *harnas.Session, tracer Tracer) {
	var endProvider func(map[string]any)
	session.Observation.Subscribe(func(event string, payload map[string]any) {
		switch event {
		case "provider_called":
			endProvider = tracer.StartSpan("harnas.provider.call", map[string]any{"provider": payload["provider"]})
		case "provider_responded", "provider_failed":
			if endProvider != nil {
				endProvider(map[string]any{"outcome": event, "usage": payload["usage"]})
				endProvider = nil
			}
		case "event_appended":
			// tool_use opens / tool_result closes a tool span; correlate by tool_use_id.
			// (No first-class tool Observation event today — a queued spec ask.)
		}
	})
}

// ---------------------------------------------------------------------------
// 5. Assembling and running a turn (construct-from-parts; no manifest file).
// ---------------------------------------------------------------------------

func runTurn(session *harnas.Session, registry *harnas.Registry, provider harnas.Provider, userText string) (string, error) {
	session.Log.Append(harnas.EventUserMessage, map[string]any{"text": userText})
	loop := harnas.AgentLoop{
		Session:      session,
		Projection:   harnas.AnthropicProjection{Model: "claude-test", MaxTokens: 1024, Registry: registry, ProviderKind: "anthropic"},
		Provider:     provider,
		ProviderKind: "anthropic",
		Ingestor:     harnas.AnthropicIngestor{},
		Runner:       &harnas.Runner{Registry: registry},
		MaxTurns:     8,
	}
	return loop.Run()
}

// ---------------------------------------------------------------------------
// 6. The SQL adapter wired for production: SQLSTATE-23505 conflict detection
//    (lib/pq) so detection is exact, not message-string matching. Per-workspace
//    tenancy: until the adapter gains a workspace_id column, scope by composing
//    the key (today's pattern); swap to the native param when it lands.
// ---------------------------------------------------------------------------

func newProductionAdapter(db *sql.DB, workspaceID, sessionID string) *harnas.SQLStorageAdapter {
	return harnas.NewSQLStorageAdapter(db, workspaceID+":"+sessionID, harnas.SQLStorageOptions{
		Dialect: harnas.SQLStorageDialectPostgres,
		// Exact unique-violation detection via the driver's typed error, keeping
		// harnas-go driver-free. lib/pq surfaces SQLSTATE 23505 for the
		// (session_id, seq) PK violation.
		ConflictDetector: func(err error) bool {
			type sqlStateError interface{ SQLState() string }
			var pqErr sqlStateError
			return errors.As(err, &pqErr) && pqErr.SQLState() == "23505"
		},
	})
}
