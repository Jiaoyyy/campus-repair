package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTurnToolCacheReusesResultsAtLimit(t *testing.T) {
	result := &mcp.CallToolResult{StructuredContent: map[string]any{"total": 2}}
	cache := &turnToolCache{calls: 3, results: map[string]*mcp.CallToolResult{`query_work_orders:{"queue":"all","status":"assigned"}`: result}}
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := cache.call(context.Background(), nil, nil, 1, MCPQueryOrders, map[string]any{"status": "assigned", "queue": "all"})
			if err != nil || got != result {
				t.Error("identical result was not reused", err)
			}
		}()
	}
	group.Wait()
	if _, err := cache.call(context.Background(), nil, nil, 1, MCPQueryOrders, json.RawMessage(`{"queue":"action","queue":"all","status":"assigned"}`)); err == nil {
		t.Fatal("duplicate arguments bypassed validation through cache")
	}
	if cache.calls != 3 {
		t.Fatal("cached requests consumed additional budget")
	}
	_, err := cache.call(context.Background(), nil, nil, 1, MCPQueryOrders, map[string]any{"queue": "action"})
	if !errors.Is(err, errMCPToolLimit) {
		t.Fatal("new call bypassed budget", err)
	}
}

func TestGatewayPromptEvidenceContainsNoPromptText(t *testing.T) {
	metric := &turnPerformance{Stages: map[string]stageTiming{}}
	ctx := context.WithValue(context.Background(), performanceKey{}, metric)
	run := &SkillRun{ID: "safe_self_check", DefinitionVersion: 2, ConfigVersion: 3}
	ctx = context.WithValue(ctx, promptContextKey{}, promptExpectation{Role: "private-role-marker", Skill: run})
	recordGatewayInstructions(ctx, []chatMessage{{Role: "system", Content: "private-role-marker" + skillPrompt(run)}})
	if len(metric.Prompts) != 1 || !metric.Prompts[0].RoleIncluded || !metric.Prompts[0].SkillIncluded || len(metric.Prompts[0].RoleHash) != 64 {
		t.Fatal("missing request fingerprint evidence")
	}
	raw, err := json.Marshal(metric.Prompts)
	if err != nil || strings.Contains(string(raw), "private-role-marker") || strings.Contains(string(raw), "不要自行") {
		t.Fatal("prompt text leaked to audit")
	}
}
