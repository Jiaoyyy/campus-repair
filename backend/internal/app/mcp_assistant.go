package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Read-only routing never accepts identity or authorization from model output.
func repairCategory(q string) string {
	switch {
	case containsAny(q, "照明", "灯"):
		return "lighting"
	case containsAny(q, "电器", "插座"):
		return "electrical"
	case containsAny(q, "给排水", "水管", "水龙头", "漏水"):
		return "plumbing"
	}
	return ""
}
func dispatchQuestion(q string) bool {
	return containsAny(q, "可派", "派单人员", "可分配人员", "师傅", "维修人员", "维修工") &&
		containsAny(q, "哪些", "几个", "几位", "多少", "谁", "有空", "可派", "可分配", "安排", "找")
}
func mcpIntent(role, question string) (string, any) {
	q := strings.TrimSpace(question)
	switch role {
	case "requester":
		if containsAny(q, "我的工单", "我的报修", "我报的", "我提交的") && containsAny(q, "进度", "状态", "到哪", "怎么样", "有哪些", "查", "看", "修好", "处理", "我的工单") {
			return MCPMyOrders, map[string]any{}
		}
		if containsAny(q, "报修进度", "维修进度") {
			return MCPMyOrders, map[string]any{}
		}
	case "technician":
		if containsAny(q, "已分配", "我的任务", "待处理工单", "分给我", "派给我", "我有哪些活", "今天要修什么") {
			return MCPAssignedOrders, map[string]any{}
		}
	case "admin":
		if category := repairCategory(q); dispatchQuestion(q) && category != "" {
			return MCPDispatch, map[string]any{"category": category}
		}
	}
	return "", nil
}
func followupMCPIntent(role, input string, history []chatMessage) (string, any) {
	if role == "admin" && repairCategory(input) != "" && len([]rune(input)) <= 20 && dispatchQuestion(lastUserInput(history)) {
		return MCPDispatch, map[string]any{"category": repairCategory(input)}
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
		lines := []string{"当前该类别可安排的维修人员："}
		for _, item := range data.Items {
			lines = append(lines, fmt.Sprintf("%s：待接单 %d 单，处理中 %d 单。", item.Name, item.Pending, item.Progress))
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
	lines := []string{"查到以下工单进度（最近 20 条）："}
	for _, item := range data.Items {
		status := statusNames[item.Status]
		if status == "" {
			status = item.Status
		}
		lines = append(lines, fmt.Sprintf("%s：%s，%s。", item.Number, status, item.Location))
	}
	return strings.Join(lines, "\n"), nil
}
