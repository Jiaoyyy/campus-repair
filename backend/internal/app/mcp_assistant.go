package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A narrow, deterministic route keeps identity and tool selection outside the
// model until the project's skills and model tool-calling stage is complete.
func mcpIntent(role, question string) (string, any) {
	q := strings.TrimSpace(question)
	switch role {
	case "requester":
		if strings.Contains(q, "工单") || strings.Contains(q, "报修进度") || strings.Contains(q, "维修进度") {
			if strings.Contains(q, "进度") || strings.Contains(q, "状态") || strings.Contains(q, "我的工单") {
				return MCPMyOrders, map[string]any{}
			}
		}
	case "technician":
		if strings.Contains(q, "已分配") || strings.Contains(q, "我的任务") || strings.Contains(q, "待处理工单") {
			return MCPAssignedOrders, map[string]any{}
		}
	case "admin":
		if strings.Contains(q, "可派") || strings.Contains(q, "派单人员") || strings.Contains(q, "可分配人员") {
			category := ""
			switch {
			case strings.Contains(q, "照明") || strings.Contains(q, "灯"):
				category = "lighting"
			case strings.Contains(q, "电器") || strings.Contains(q, "插座"):
				category = "electrical"
			case strings.Contains(q, "给排水") || strings.Contains(q, "水管") || strings.Contains(q, "水龙头"):
				category = "plumbing"
			}
			if category != "" {
				return MCPDispatch, map[string]any{"category": category}
			}
		}
	}
	return "", nil
}

func formatMCPAnswer(name string, res *mcp.CallToolResult) (string, error) {
	if res == nil || res.IsError || res.StructuredContent == nil {
		return "", errors.New("MCP tool returned no verified data")
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", err
	}
	if name == MCPDispatch {
		var data struct {
			Items []struct {
				ID         int64    `json:"id"`
				Name       string   `json:"display_name"`
				Categories []string `json:"categories"`
				Pending    int      `json:"pending_count"`
				Progress   int      `json:"in_progress_count"`
			} `json:"items"`
		}
		if json.Unmarshal(b, &data) != nil {
			return "", errors.New("invalid MCP tool result")
		}
		if len(data.Items) == 0 {
			return "该类别暂时没有可派的维修人员，请由管理员人工安排。", nil
		}
		lines := []string{"当前可派维修人员（MCP 实时只读查询）："}
		for _, item := range data.Items {
			lines = append(lines, fmt.Sprintf("%s（ID %d）：待接单 %d，处理中 %d。", item.Name, item.ID, item.Pending, item.Progress))
		}
		return strings.Join(lines, "\n"), nil
	}
	var data struct {
		Items []struct {
			Number   string `json:"number"`
			Status   string `json:"status"`
			Category string `json:"category"`
			Location string `json:"location"`
		} `json:"items"`
	}
	if json.Unmarshal(b, &data) != nil {
		return "", errors.New("invalid MCP tool result")
	}
	if len(data.Items) == 0 {
		return "当前没有可见的工单。", nil
	}
	statusNames := map[string]string{"submitted": "待审核", "needs_info": "待补充", "unassigned": "待派单", "assigned": "待接单", "in_progress": "处理中", "awaiting_confirmation": "待确认", "closed": "已关闭", "rejected": "已驳回", "cancelled": "已取消"}
	lines := []string{"当前可见工单（MCP 实时只读查询，最近 20 条）："}
	for _, item := range data.Items {
		status := statusNames[item.Status]
		if status == "" {
			status = item.Status
		}
		lines = append(lines, fmt.Sprintf("%s：%s，%s。", item.Number, status, item.Location))
	}
	return strings.Join(lines, "\n"), nil
}
