package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	harnas "github.com/Tedo-ai/harnas-go"
)

type providerCarrierFixture struct {
	Name     string `json:"name"`
	Provider struct {
		Kind               string `json:"kind"`
		Model              string `json:"model"`
		CarrierDestination string `json:"carrier_destination"`
	} `json:"provider"`
	Ingest struct {
		ProviderResponse map[string]any `json:"provider_response"`
		ExpectEvent      struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		} `json:"expect_event"`
	} `json:"ingest"`
	Project struct {
		Log           []harnas.Event `json:"log"`
		ExpectRequest map[string]any `json:"expect_request"`
	} `json:"project"`
}

func TestProviderCarrierFixtures(t *testing.T) {
	root := filepath.Join(specRoot(t), "conformance", "provider-carriers")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			var fixture providerCarrierFixture
			data, err := os.ReadFile(filepath.Join(root, entry.Name(), "fixture.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			assertCarrierIngest(t, fixture)
			assertCarrierProjection(t, fixture)
			assertCarrierRoundTrip(t, fixture)
		})
	}
}

func assertCarrierIngest(t *testing.T, fixture providerCarrierFixture) {
	t.Helper()
	events, err := carrierIngestor(fixture.Provider.Kind).Ingest(fixture.Ingest.ProviderResponse)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("ingestor produced no events")
	}
	actual := events[0]
	if actual.Type == harnas.EventAssistantMessage {
		if actual.Payload["provider"] == nil {
			actual.Payload["provider"] = fixture.Provider.Kind
		}
		if actual.Payload["model"] == nil || actual.Payload["model"] == "" {
			actual.Payload["model"] = fixture.Provider.Model
		}
	}
	expected := harnas.EventArgs{
		Type:    harnas.EventType(fixture.Ingest.ExpectEvent.Type),
		Payload: fixture.Ingest.ExpectEvent.Payload,
	}
	if diff := jsonValueDiff(actual, expected); diff != "" {
		t.Fatalf("ingest mismatch: %s", diff)
	}
}

func assertCarrierProjection(t *testing.T, fixture providerCarrierFixture) {
	t.Helper()
	log := harnas.NewLog()
	for _, event := range fixture.Project.Log {
		event.Type = harnas.EventType(event.Type)
		log.Restore(event)
	}
	request, err := carrierProjection(fixture.Provider.Kind, fixture.Provider.Model).Project(log)
	if err != nil {
		t.Fatal(err)
	}
	if diff := jsonValueDiff(request, fixture.Project.ExpectRequest); diff != "" {
		t.Fatalf("projection mismatch: %s", diff)
	}
}

func assertCarrierRoundTrip(t *testing.T, fixture providerCarrierFixture) {
	t.Helper()
	events, err := carrierIngestor(fixture.Provider.Kind).Ingest(fixture.Ingest.ProviderResponse)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("ingestor produced no events")
	}
	payload := events[0].Payload
	expected := fixture.Ingest.ExpectEvent.Payload
	for _, key := range []string{"provider_items", "content", "reasoning"} {
		if _, ok := expected[key]; !ok {
			continue
		}
		if diff := jsonValueDiff(payload[key], expected[key]); diff != "" {
			t.Fatalf("round-trip carrier mismatch %s: %s", key, diff)
		}
	}
}

func carrierIngestor(kind string) harnas.Ingestor {
	switch kind {
	case "anthropic":
		return harnas.AnthropicIngestor{}
	case "openai":
		return harnas.OpenAIIngestor{}
	case "gemini":
		return &harnas.GeminiIngestor{}
	default:
		panic(fmt.Sprintf("unsupported carrier provider kind %q", kind))
	}
}

func carrierProjection(kind string, model string) harnas.Projection {
	switch kind {
	case "anthropic":
		return harnas.AnthropicProjection{Model: model, MaxTokens: 1024}
	case "openai":
		return harnas.OpenAIProjection{Model: model}
	case "gemini":
		return harnas.GeminiProjection{Model: model}
	default:
		panic(fmt.Sprintf("unsupported carrier provider kind %q", kind))
	}
}

func jsonValueDiff(actual any, expected any) string {
	actualJSON, actualErr := json.Marshal(actual)
	expectedJSON, expectedErr := json.Marshal(expected)
	if actualErr != nil || expectedErr != nil {
		return fmt.Sprintf("marshal actualErr=%v expectedErr=%v", actualErr, expectedErr)
	}
	var actualNorm any
	var expectedNorm any
	if err := json.Unmarshal(actualJSON, &actualNorm); err != nil {
		return err.Error()
	}
	if err := json.Unmarshal(expectedJSON, &expectedNorm); err != nil {
		return err.Error()
	}
	if !jsonEqual(actualNorm, expectedNorm) {
		return fmt.Sprintf("actual=%s expected=%s", actualJSON, expectedJSON)
	}
	return ""
}
