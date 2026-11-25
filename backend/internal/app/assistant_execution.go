package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stageTiming struct {
	Calls      int   `json:"calls"`
	DurationMS int64 `json:"duration_ms"`
}

type performanceKey struct{}
type turnPerformance struct {
	mu      sync.Mutex
	Stages  map[string]stageTiming `json:"stages"`
	Prompts []promptEvidence       `json:"-"`
}

func recordStage(ctx context.Context, stage string, started time.Time) {
	metric, _ := ctx.Value(performanceKey{}).(*turnPerformance)
	if metric == nil {
		return
	}
	metric.mu.Lock()
	defer metric.mu.Unlock()
	value := metric.Stages[stage]
	value.Calls++
	value.DurationMS += time.Since(started).Milliseconds()
	metric.Stages[stage] = value
}

func (s *Server) savePerformance(ctx context.Context, u User, request string, metric *turnPerformance, started time.Time) {
	recordStage(ctx, "end_to_end", started)
	metric.mu.Lock()
	defer metric.mu.Unlock()
	auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := writeAudit(auditCtx, s.DB, request, &u, "assistant.performance", nil, "recorded", metric.Stages); err != nil {
		log.Printf("request=%s outcome=performance_audit_unavailable", request)
	}
	if len(metric.Prompts) > 0 {
		if err := writeAudit(auditCtx, s.DB, request, &u, "assistant.prompt", nil, "recorded", metric.Prompts); err != nil {
			log.Printf("request=%s outcome=prompt_audit_unavailable", request)
		}
	}
}

type turnToolCache struct {
	mu      sync.Mutex
	calls   int
	results map[string]*mcp.CallToolResult
}

func (cache *turnToolCache) call(ctx context.Context, s *Server, r *http.Request, agentID int64, name string, arguments any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	if mcpRoles[name] == "" || !validMCPArguments(raw, name) {
		return nil, bad("工具参数无效")
	}
	// Re-encoding the object gives equivalent parameter orders one cache key.
	var object any
	if json.Unmarshal(raw, &object) != nil {
		return nil, errors.New("invalid arguments")
	}
	raw, _ = json.Marshal(object)
	key := name + ":" + string(raw)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.results == nil {
		cache.results = map[string]*mcp.CallToolResult{}
	}
	if result, ok := cache.results[key]; ok {
		return result, nil
	}
	if cache.calls >= 3 {
		return nil, errMCPToolLimit
	}
	cache.calls++
	result, err := s.callMCPTool(ctx, r, agentID, name, arguments)
	if err != nil {
		return nil, err
	}
	if result == nil || result.IsError {
		return nil, errors.New("tool failed")
	}
	cache.results[key] = result
	return result, nil
}

type promptContextKey struct{}
type promptExpectation struct {
	Role  string
	Skill *SkillRun
}
type promptEvidence struct {
	RoleHash      string `json:"role_prompt_sha256,omitempty"`
	RoleIncluded  bool   `json:"role_prompt_in_request"`
	SkillID       string `json:"skill_id,omitempty"`
	SkillVersion  int    `json:"skill_version,omitempty"`
	SkillIncluded bool   `json:"skill_instruction_in_request"`
}

// Record only fingerprints and presence flags after the gateway accepts the
// serialized request. Prompt text, user messages and credentials stay out of audit.
func recordGatewayInstructions(ctx context.Context, messages []chatMessage) {
	metric, _ := ctx.Value(performanceKey{}).(*turnPerformance)
	expected, _ := ctx.Value(promptContextKey{}).(promptExpectation)
	if metric == nil || expected.Role == "" && expected.Skill == nil {
		return
	}
	systems := []string{}
	for _, message := range messages {
		if message.Role == "system" {
			systems = append(systems, message.Content)
		}
	}
	content := strings.Join(systems, "\n")
	evidence := promptEvidence{}
	if expected.Role != "" {
		hash := sha256.Sum256([]byte(expected.Role))
		evidence.RoleHash = hex.EncodeToString(hash[:])
		evidence.RoleIncluded = strings.Contains(content, expected.Role)
	}
	if expected.Skill != nil {
		evidence.SkillID = expected.Skill.ID
		evidence.SkillVersion = expected.Skill.DefinitionVersion
		evidence.SkillIncluded = strings.Contains(content, skillPrompt(expected.Skill))
	}
	metric.mu.Lock()
	defer metric.mu.Unlock()
	metric.Prompts = append(metric.Prompts, evidence)
}
