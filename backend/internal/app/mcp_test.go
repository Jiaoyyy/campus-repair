package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPCredentialRejectsTamperingAndExpiry(t *testing.T) {
	secret := strings.Repeat("s", 32)
	token := SignMCPCall(secret, strings.Repeat("a", 64), 12, strings.Repeat("b", 32))
	if _, err := verifyMCPCall(secret, token); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyMCPCall(secret, token+"x"); err == nil {
		t.Fatal("tampered signature accepted")
	}
	if _, err := verifyMCPCall(strings.Repeat("z", 32), token); err == nil {
		t.Fatal("wrong key accepted")
	}
	claims := mcpClaims{Session: strings.Repeat("a", 64), AgentID: 12, Request: strings.Repeat("b", 32), Audience: mcpAudience, Issued: time.Now().Add(-2 * time.Minute).Unix(), Expires: time.Now().Add(-time.Minute).Unix()}
	b, _ := json.Marshal(claims)
	expired := base64.RawURLEncoding.EncodeToString(b)
	// A changed payload without a fresh signature is invalid, even if it names a user.
	if _, err := verifyMCPCall(secret, expired+"."+strings.Split(token, ".")[1]); err == nil {
		t.Fatal("forged expiry accepted")
	}
}

func TestMCPClientProtocolFailureAndTimeout(t *testing.T) {
	t.Setenv("MCP_CALL_SECRET", strings.Repeat("x", 32))
	for _, slow := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slow {
				time.Sleep(200 * time.Millisecond)
				http.Error(w, "late response", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"not":"a valid MCP initialize result"}`))
		}))
		t.Setenv("MCP_SERVER_URL", server.URL+"/mcp")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, _, err := (&Server{}).connectMCP(ctx, strings.Repeat("a", 64), 1, strings.Repeat("b", 32))
		cancel()
		server.Close()
		if err == nil {
			t.Fatalf("invalid or unresponsive MCP peer accepted (slow=%v)", slow)
		}
	}
}

func TestMCPAssistantIntentAndResult(t *testing.T) {
	for _, tt := range []struct{ role, question, want string }{
		{"requester", "我的工单进度", MCPMyOrders},
		{"technician", "我有哪些已分配任务", MCPAssignedOrders},
		{"admin", "照明类可派人员有哪些", MCPDispatch},
		{"requester", "照明类可派人员有哪些", ""},
		{"admin", "请直接派单", ""},
	} {
		got, _ := mcpIntent(tt.role, tt.question)
		if got != tt.want {
			t.Fatalf("intent %q/%q = %q, want %q", tt.role, tt.question, got, tt.want)
		}
	}
	res := &mcp.CallToolResult{StructuredContent: map[string]any{"items": []any{map[string]any{"number": "RP-1", "status": "assigned", "location": "三号楼"}}}}
	answer, err := formatMCPAnswer(MCPMyOrders, res)
	if err != nil || !strings.Contains(answer, "待接单") || !strings.Contains(answer, "RP-1") {
		t.Fatal("tool data was not formatted accurately")
	}
	if _, err := formatMCPAnswer(MCPMyOrders, &mcp.CallToolResult{IsError: true}); err == nil {
		t.Fatal("tool failure was presented as data")
	}
}

func TestMCPArgumentsRejectIdentityFields(t *testing.T) {
	for _, name := range []string{MCPMyOrders, MCPAssignedOrders} {
		if !validMCPArguments(json.RawMessage(`{}`), name) {
			t.Fatalf("empty args refused for %s", name)
		}
		if validMCPArguments(json.RawMessage(`{"user_id":2}`), name) {
			t.Fatalf("model identity accepted for %s", name)
		}
	}
	if !validMCPArguments(json.RawMessage(`{"category":"lighting"}`), MCPDispatch) {
		t.Fatal("valid category refused")
	}
	if validMCPArguments(json.RawMessage(`{"category":"lighting","org_id":2}`), MCPDispatch) {
		t.Fatal("model org accepted")
	}
}
