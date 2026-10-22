package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpBearerTransport struct {
	token string
	next  http.RoundTripper
}

func (t mcpBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(copy)
}

func (s *Server) connectMCP(ctx context.Context, sessionHash string, agentID int64, reqID string) (*mcp.ClientSession, string, error) {
	url, secret := os.Getenv("MCP_SERVER_URL"), os.Getenv("MCP_CALL_SECRET")
	if url == "" || len(secret) < 32 {
		return nil, "", errors.New("MCP is not configured")
	}
	credential := SignMCPCall(secret, sessionHash, agentID, reqID)
	client := mcp.NewClient(&mcp.Implementation{Name: "campus-repair-api", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: url, MaxRetries: -1, DisableStandaloneSSE: true, HTTPClient: &http.Client{Timeout: 10 * time.Second, Transport: mcpBearerTransport{credential, http.DefaultTransport}}}
	session, err := client.Connect(ctx, transport, nil)
	return session, credential, err
}

func mcpAvailable(ctx context.Context) bool {
	url := os.Getenv("MCP_SERVER_URL")
	if !strings.HasSuffix(url, "/mcp") || len(os.Getenv("MCP_CALL_SECRET")) < 32 {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(url, "/mcp")+"/health/live", nil)
	if err != nil {
		return false
	}
	res, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func (s *Server) mcpDiscover(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	list, protocol, err := s.discoveredMCP(r.Context(), r)
	if err != nil {
		s.fail(w, r, &Problem{503, "mcp_unavailable", "MCP 工具发现暂不可用"})
		return
	}
	tools := []map[string]any{}
	for _, t := range list {
		tools = append(tools, map[string]any{"name": t.Name, "role": mcpRoles[t.Name], "description": t.Description, "input_schema": t.InputSchema})
	}
	send(w, 200, map[string]any{"items": tools, "protocol_version": protocol})
}

func (s *Server) discoveredMCP(parent context.Context, r *http.Request) ([]*mcp.Tool, string, error) {
	u := current(r)
	var agentID int64
	err := s.DB.QueryRowContext(r.Context(), "SELECT id FROM agents WHERE org_id=? AND role='admin' AND enabled=TRUE ORDER BY id LIMIT 1", u.OrgID).Scan(&agentID)
	if err != nil {
		return nil, "", err
	}
	h := strings.Fields(r.Header.Get("Authorization"))
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	session, _, err := s.connectMCP(ctx, s.tokenHash(h[1]), agentID, requestID(r))
	if err != nil {
		return nil, "", err
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		return nil, "", err
	}
	tools := []*mcp.Tool{}
	for _, t := range list.Tools {
		if mcpRoles[t.Name] == "" {
			continue
		}
		tools = append(tools, t)
	}
	return tools, session.InitializeResult().ProtocolVersion, nil
}

// callMCPTool is the Host-side entry point for a single assistant turn. The
// server repeats every check; the Host check only prevents unnecessary calls.
func (s *Server) callMCPTool(ctx context.Context, r *http.Request, agentID int64, name string, arguments any) (*mcp.CallToolResult, error) {
	u := current(r)
	if mcpRoles[name] == "" || mcpRoles[name] != u.Role {
		return nil, denied()
	}
	h := strings.Fields(r.Header.Get("Authorization"))
	if len(h) != 2 {
		return nil, denied()
	}
	claim := mcpClaims{Session: s.tokenHash(h[1]), AgentID: agentID}
	if err := mcpAllowed(ctx, s.DB, claim, u, name); err != nil {
		return nil, denied()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	session, credential, err := s.connectMCP(ctx, claim.Session, agentID, requestID(r))
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.CallTool(ctx, &mcp.CallToolParams{Meta: mcp.Meta{"campus_repair_auth": credential}, Name: name, Arguments: arguments})
}
