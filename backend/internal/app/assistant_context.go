package app

import (
	"context"
	"strings"
)

// This policy controls data access only. The model handles normal conversation;
// an empty search result is context for a reply, never a substitute for one.
type assistantKnowledge struct {
	Hits                []Evidence
	Mode, State, Notice string
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
func knowledgeQuestion(text string) bool {
	return containsAny(text, "报修", "维修", "故障", "宿舍", "楼栋", "灯", "电器", "照明", "水管", "漏水", "漏电", "冒烟", "插座", "安全", "校方", "学校", "规定", "须知", "制度", "时限", "资料", "流程", "给排水")
}
func evidenceQuestion(text string) bool {
	return containsAny(text, "规定", "制度", "校方", "学校", "时限", "多久", "几天", "几小时", "安全", "需要", "要求", "流程", "根据资料")
}
func lastUserInput(history []chatMessage) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			return history[i].Content
		}
	}
	return ""
}
func knowledgeQuery(input string, history []chatMessage) string {
	previous := lastUserInput(history)
	// Keep explicit greetings/thanks independent of a previous repair topic.
	if containsAny(input, "你好", "谢谢", "再见", "你是谁", "你能做什么", "你有什么功能") && !knowledgeQuestion(input) {
		return ""
	}
	query := input
	if !knowledgeQuestion(input) {
		if !knowledgeQuestion(previous) {
			return ""
		}
		query = previous + " " + input
	}
	chars := []rune(strings.TrimSpace(query))
	if len(chars) > 500 {
		chars = chars[:500]
	}
	return string(chars)
}
func (s *Server) assistantKnowledge(ctx context.Context, u User, agentID int64, input string, history []chatMessage) assistantKnowledge {
	result := assistantKnowledge{Hits: []Evidence{}, Mode: "none", State: "not_requested"}
	query := knowledgeQuery(input, history)
	if query == "" {
		return result
	}
	result.Mode = "keyword"
	// No bound published material means there is no evidence to search. Do not
	// make an embedding call or depend on Qdrant just to continue a conversation.
	scope, args := knowledgeScope(u, agentID)
	var count int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM knowledge_materials m WHERE "+scope+" AND m.published_generation>0", args...).Scan(&count); err != nil {
		result.State = "unavailable"
		result.Notice = "资料查询暂不可用；本轮可以继续梳理问题，但未核实校方规定和维修时限。"
		return result
	}
	if count == 0 {
		result.State = "empty"
		return result
	}
	if vectorConfig().configured() {
		result.Mode = "hybrid"
	}
	hits, err := s.searchKnowledge(ctx, u, agentID, query, result.Mode)
	if err != nil && result.Mode == "hybrid" {
		hits, err = s.searchKnowledge(ctx, u, agentID, query, "keyword")
		result.Mode = "keyword"
		result.Notice = "部分资料检索暂不可用，本轮已使用可用的文字资料。"
	}
	if err != nil {
		result.State = "unavailable"
		result.Notice = "资料查询暂不可用；本轮可以继续梳理问题，但未核实校方规定和维修时限。"
		return result
	}
	result.Hits = hits
	result.State = "empty"
	if len(hits) > 0 {
		result.State = "found"
	}
	if result.Notice != "" {
		result.State = "degraded"
	}
	return result
}
func assistantContextNote(role, input string, history []chatMessage, knowledge assistantKnowledge) string {
	note := map[string]string{
		"not_requested": "本轮无需查询资料。正常回应用户，不要无故提及缺少资料。",
		"empty":         "本轮已查询授权资料但没有相关命中。仍应正常交流、解释通用报修操作或追问故障信息；只有涉及校方事实时说明无法确认，禁止伪造出处。",
		"found":         "本轮有授权资料，只有实际使用某片段内容才引用其编号。追问和普通交流不必强行引用。",
		"degraded":      "部分检索能力不可用，已回退到可用文字资料。只使用实际提供的片段，不能宣称完整资料均已核实。",
		"unavailable":   "本轮资料查询失败。明确说明资料暂不可用，仍可继续交流、追问位置和现象；禁止将失败描述为未找到制度或虚构校方事实。",
	}[knowledge.State]
	if role == "admin" && dispatchQuestion(input) && repairCategory(input) == "" {
		note += " 用户在询问维修人员，但未指定故障类别；当前只能按照明、电器、给排水类别查询。请自然询问想了解哪一类，不要猜测总人数或人员负载。"
	}
	return note
}
