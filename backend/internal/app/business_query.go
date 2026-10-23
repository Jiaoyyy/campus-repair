package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MCPQueryOrders = "query_work_orders"

type OrderQuery struct {
	Queue    string `json:"queue,omitempty"`
	Status   string `json:"status,omitempty"`
	Category string `json:"category,omitempty"`
	Number   string `json:"number,omitempty"`
	Period   string `json:"period,omitempty"`
	Location string `json:"location,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
}
type BusinessOrder struct {
	ID          int64  `json:"id"`
	Number      string `json:"number"`
	Status      string `json:"status"`
	Category    string `json:"category"`
	Location    string `json:"location"`
	Description string `json:"description"`
}
type BusinessCandidate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"display_name"`
	Categories []string `json:"categories"`
	Pending    int      `json:"pending_count"`
	Progress   int      `json:"in_progress_count"`
}
type BusinessResult struct {
	Kind       string              `json:"kind"`
	Query      OrderQuery          `json:"query"`
	Total      int                 `json:"total"`
	Statuses   map[string]int      `json:"statuses"`
	Items      []BusinessOrder     `json:"items"`
	Candidates []BusinessCandidate `json:"candidates,omitempty"`
	At         time.Time           `json:"queried_at"`
	TimeBasis  string              `json:"time_basis,omitempty"`
	Historical bool                `json:"historical"`
}

var businessStatusNames = map[string]string{"submitted": "待审核", "needs_info": "待补充", "unassigned": "待派单", "assigned": "待接单", "in_progress": "处理中", "awaiting_confirmation": "待确认", "closed": "已关闭", "rejected": "已驳回", "cancelled": "已取消"}
var businessCategoryNames = map[string]string{"lighting": "照明", "electrical": "电器", "plumbing": "给排水"}
var orderNumberPattern = regexp.MustCompile(`(?i)RP[0-9a-z]{8,38}`)

func (q OrderQuery) valid() bool {
	if q.Queue != "" && q.Queue != "all" && q.Queue != "action" && q.Queue != "active" && q.Queue != "finished" {
		return false
	}
	if q.Status != "" && businessStatusNames[q.Status] == "" {
		return false
	}
	if q.Category != "" && !categoryOK(q.Category) {
		return false
	}
	if !queryContains([]string{"", "all", "today", "recent", "yesterday", "this_week", "last_week", "this_month"}, q.Period) {
		return false
	}
	if len([]rune(q.Location)) > 200 || len([]rune(q.Keyword)) > 200 || strings.ContainsAny(q.Location+q.Keyword, "\x00\r\n") {
		return false
	}
	if q.DateFrom != "" || q.DateTo != "" {
		if q.Period != "" && q.Period != "all" {
			return false
		}
		a, e1 := time.Parse("2006-01-02", q.DateFrom)
		b, e2 := time.Parse("2006-01-02", q.DateTo)
		if e1 != nil || e2 != nil || b.Before(a) || b.Sub(a) > 365*24*time.Hour {
			return false
		}
	}
	return q.Number == "" || (len(q.Number) <= 40 && orderNumberPattern.FindString(q.Number) == q.Number)
}
func chinaToday(now time.Time) (time.Time, time.Time) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	t := now.In(zone)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, zone)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}
func businessQuerySQL(u User, q OrderQuery, now time.Time) (string, []any, string, error) {
	if !q.valid() || (u.Role != "requester" && u.Role != "technician" && u.Role != "admin") {
		return "", nil, "", bad("查询条件无效")
	}
	where, args := orderFilter(u)
	queue, v, err := queueFilter(u.Role, q.Queue)
	if err != nil {
		return "", nil, "", err
	}
	where += queue
	args = append(args, v...)
	if q.Status != "" {
		where += " AND status=?"
		args = append(args, q.Status)
	}
	if q.Category != "" {
		where += " AND category=?"
		args = append(args, q.Category)
	}
	if q.Number != "" {
		where += " AND number=?"
		args = append(args, q.Number)
	}
	for _, filter := range []struct{ column, value string }{{"location", q.Location}, {"description", q.Keyword}} {
		if filter.value != "" {
			where += " AND LOCATE(?," + filter.column + ")>0"
			args = append(args, filter.value)
		}
	}
	basis := ""
	if q.Period != "" && q.Period != "all" || q.DateFrom != "" {
		start, end := queryDates(q, now)
		if u.Role == "technician" {
			// Last assignment to the CURRENT assignee, not last update or an earlier round.
			stamp := "(SELECT MAX(e.created_at) FROM work_order_events e WHERE e.work_order_id=w.id AND e.action IN ('assign','reassign') AND e.assignee_id=w.assignee_id)"
			where += " AND " + stamp + ">=? AND " + stamp + "<?"
			basis = "当前指派时间（北京时间）"
		} else {
			where += " AND created_at>=? AND created_at<?"
			basis = "创建时间（北京时间）"
		}
		basis += "：" + start.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02") + " 至 " + end.Add(-time.Second).In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
		args = append(args, start, end)
	}
	return where, args, basis, nil
}
func queryBusinessOrders(ctx context.Context, db *sql.DB, u User, q OrderQuery) (BusinessResult, error) {
	result := BusinessResult{Kind: "orders", Query: q, Statuses: map[string]int{}, Items: []BusinessOrder{}, At: time.Now().UTC()}
	where, args, basis, err := businessQuerySQL(u, q, result.At)
	if err != nil {
		return result, err
	}
	result.TimeBasis = basis
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT status,COUNT(*) FROM work_orders w WHERE "+where+" GROUP BY status", args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var status string
		var count int
		if err = rows.Scan(&status, &count); err != nil {
			break
		}
		result.Statuses[status] = count
		result.Total += count
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,number,status,category,location,description FROM work_orders w WHERE "+where+" ORDER BY updated_at DESC,id DESC LIMIT 5", args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var o BusinessOrder
		if err = rows.Scan(&o.ID, &o.Number, &o.Status, &o.Category, &o.Location, &o.Description); err != nil {
			break
		}
		result.Items = append(result.Items, o)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func queryLabel(role string, q OrderQuery) string {
	label := map[string]string{"requester": "你的报修", "technician": "当前指派给你的工单", "admin": "本组织工单"}[role]
	if q.Queue == "action" {
		label = map[string]string{"requester": "待你补充或确认的报修", "technician": "待你接单或处理的工单", "admin": "待你审核或派单的工单"}[role]
	}
	if q.Queue == "active" {
		label += "（未结束）"
	}
	if q.Queue == "finished" {
		label += "（已结束）"
	}
	if q.Status != "" {
		label += " · " + businessStatusNames[q.Status]
	}
	if q.Category != "" {
		label += " · " + businessCategoryNames[q.Category]
	}
	if q.Period == "today" {
		label += " · 今天"
	}
	if q.Number != "" {
		label += " · " + q.Number
	}
	if q.Location != "" {
		label += " · 位置包含“" + q.Location + "”"
	}
	if q.Keyword != "" {
		label += " · 故障包含“" + q.Keyword + "”"
	}
	return label
}
func formatBusinessResult(role string, b BusinessResult) string {
	if b.Kind == "candidates" {
		if len(b.Candidates) == 0 {
			return "这个类别暂时没有可派人员，请在工单页面人工核对安排。我没有派单或修改工单。"
		}
		c := b.Candidates[0]
		return fmt.Sprintf("查到 %d 位类别匹配的可派人员。可优先考虑%s：待接单 %d 单，处理中 %d 单，当前候选中负载较低。请结合现场情况在工单页面确认派单，我没有修改工单。", len(b.Candidates), c.Name, c.Pending, c.Progress)
	}
	label := queryLabel(role, b.Query)
	if b.Total == 0 {
		return label + "目前没有匹配记录。" + b.TimeBasis + "。这不代表其他范围也没有工单。"
	}
	text := fmt.Sprintf("%s共有 %d 单。", label, b.Total)
	parts := []string{}
	for _, s := range []string{"submitted", "unassigned", "assigned", "in_progress", "needs_info", "awaiting_confirmation", "closed", "rejected", "cancelled"} {
		if n := b.Statuses[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d 单", businessStatusNames[s], n))
		}
	}
	text += "\n" + strings.Join(parts, "，") + "。"
	for _, item := range b.Items {
		text += "\n" + item.Location + "：" + item.Description + "（" + businessStatusNames[item.Status] + "，" + item.Number + "）"
	}
	if b.Total > len(b.Items) {
		text += fmt.Sprintf("下方显示最近更新的 %d 单，总数按全部匹配记录统计。", len(b.Items))
	}
	if b.TimeBasis != "" {
		text += "\n" + b.TimeBasis + "。"
	}
	return text
}
func decodeBusinessResult(raw any) (*BusinessResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result BusinessResult
	if json.Unmarshal(b, &result) != nil || result.Kind != "orders" || !result.Query.valid() || result.Total < 0 || result.At.IsZero() || result.Statuses == nil || result.Items == nil {
		return nil, errors.New("invalid business query result")
	}
	sum := 0
	for s, n := range result.Statuses {
		if businessStatusNames[s] == "" || n < 0 {
			return nil, errors.New("invalid counts")
		}
		sum += n
	}
	if sum != result.Total || len(result.Items) > 5 || len(result.Items) > result.Total {
		return nil, errors.New("invalid result count")
	}
	for _, item := range result.Items {
		if item.ID < 1 || businessStatusNames[item.Status] == "" || !categoryOK(item.Category) || item.Number == "" {
			return nil, errors.New("invalid order")
		}
	}
	return &result, nil
}
func (s *Server) businessHistory(ctx context.Context, u User, cid, mid int64) (*BusinessResult, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT result FROM assistant_business_results WHERE message_id=?", mid).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result BusinessResult
	if json.Unmarshal(raw, &result) != nil {
		return nil, errors.New("invalid stored query")
	}
	result.Historical = true
	visibleItems := []BusinessOrder{}
	for _, item := range result.Items {
		_, err := s.getOrder(ctx, u, item.ID)
		if err != nil {
			var problem *Problem
			if errors.As(err, &problem) && problem.Status == 404 {
				continue
			}
			return nil, err
		}
		visibleItems = append(visibleItems, item)
	}
	result.Items = visibleItems
	return &result, nil
}
func (s *Server) lastBusinessResult(ctx context.Context, cid int64) (*BusinessResult, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT b.result FROM assistant_business_results b JOIN messages m ON m.id=b.message_id WHERE m.id=(SELECT MAX(id) FROM messages WHERE conversation_id=? AND role='assistant' AND status='complete')", cid).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result BusinessResult
	if json.Unmarshal(raw, &result) != nil {
		return nil, errChat
	}
	return &result, nil
}
func businessIntent(role, input string, previous *OrderQuery) (OrderQuery, bool) {
	q := strings.TrimSpace(input)
	query := OrderQuery{Queue: "all", Period: "all"}
	if role != "requester" && role != "technician" && role != "admin" {
		return query, false
	}
	if draftIntent(role, q, false) {
		return query, false
	}
	if containsAny(q, "怎么", "如何", "流程", "制度", "规定", "安全", "资料", "时限", "多久", "几天", "多少时间", "多长时间", "多少钱") {
		return query, false
	}
	number := orderNumberPattern.FindString(q)
	businessNoun := containsAny(q, "工单", "报修", "任务", "待办", "单子", "哪些单", "几单", "多少单", "有哪些活")
	filterSignal := containsAny(q, "待审核", "等我审核", "待我确认", "待我补充", "没修完", "未完成", "待派单", "待接单", "处理中", "待补充", "待确认", "未结束", "没处理完", "已结束", "已关闭", "已取消", "已驳回", "今天", "全部类别", "全部状态", "不限日期", "全部日期", "所有日期")
	count := containsAny(q, "多少", "几单", "几张", "几条", "数量", "统计", "概览", "有几", "总数", "总共")
	shortFollowup := previous != nil && len([]rune(q)) <= 25 && (filterSignal || repairCategory(q) != "") && !businessNoun
	explicit := number != "" || businessNoun && (count || filterSignal || containsAny(q, "待处理", "待我", "要处理", "需要处理", "需要我处理", "待办", "列出", "有哪些", "查", "查看"))
	if !explicit && !shortFollowup {
		return query, false
	}
	if shortFollowup {
		query = *previous
	}
	if containsAny(q, "全部工单", "所有工单", "全部报修", "所有任务") {
		query = OrderQuery{Queue: "all", Period: "all"}
	}
	if containsAny(q, "待处理", "待我", "要处理", "需要我处理", "需要处理", "待办", "等我处理") {
		query.Queue = "action"
		query.Status = ""
	}
	if containsAny(q, "未结束", "没处理完", "没修完", "未完成") {
		query.Queue = "active"
		query.Status = ""
	}
	if containsAny(q, "已结束", "已完成") {
		query.Queue = "finished"
		query.Status = ""
	}
	for _, pair := range [][2]string{{"待审核", "submitted"}, {"等我审核", "submitted"}, {"待派单", "unassigned"}, {"待接单", "assigned"}, {"处理中", "in_progress"}, {"待补充", "needs_info"}, {"待确认", "awaiting_confirmation"}, {"待我确认", "awaiting_confirmation"}, {"待我补充", "needs_info"}, {"已关闭", "closed"}, {"已驳回", "rejected"}, {"已取消", "cancelled"}} {
		if strings.Contains(q, pair[0]) {
			query.Queue = "all"
			query.Status = pair[1]
			break
		}
	}
	if cat := repairCategory(q); cat != "" {
		query.Category = cat
	}
	if containsAny(q, "全部类别", "所有类别") {
		query.Category = ""
	}
	if containsAny(q, "全部状态", "所有状态") {
		query.Queue = "all"
		query.Status = ""
	}
	if strings.Contains(q, "今天") {
		query.Period = "today"
	}
	if containsAny(q, "不限日期", "全部日期", "所有日期") {
		query.Period = "all"
	}
	if number != "" {
		query.Number = number
		query.Queue = "all"
		query.Status = ""
	}
	return query, true
}

func candidateBusiness(result *mcp.CallToolResult) (*BusinessResult, error) {
	if result == nil || result.IsError || result.StructuredContent == nil {
		return nil, errors.New("invalid candidates")
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var data struct {
		Items []BusinessCandidate `json:"items"`
	}
	if json.Unmarshal(raw, &data) != nil || data.Items == nil {
		return nil, errors.New("invalid candidates")
	}
	for _, c := range data.Items {
		if c.ID < 1 || c.Name == "" || c.Pending < 0 || c.Progress < 0 {
			return nil, errors.New("invalid candidate")
		}
	}
	sort.SliceStable(data.Items, func(i, j int) bool {
		a, b := data.Items[i], data.Items[j]
		if a.Pending+a.Progress == b.Pending+b.Progress {
			return a.ID < b.ID
		}
		return a.Pending+a.Progress < b.Pending+b.Progress
	})
	return &BusinessResult{Kind: "candidates", Candidates: data.Items, Total: len(data.Items), At: time.Now().UTC(), Items: []BusinessOrder{}, Statuses: map[string]int{}}, nil
}

func businessWriteRequest(input string) bool {
	return containsAny(input, "帮我派单", "直接派单", "帮我接单", "帮我关闭", "帮我结单", "帮我改派", "帮我取消工单", "帮我取消报修", "帮我审核通过") || (strings.Contains(input, "派给") && containsAny(input, "帮我", "直接", "请把", "请将", "把工单"))
}

func queryClarification(role, input string, previous bool) string {
	if draftIntent(role, input, false) || containsAny(input, "规定", "制度", "资料", "流程", "安全", "怎么", "如何") {
		return ""
	}
	subject := containsAny(input, "工单", "报修", "任务", "待办") || previous
	if !subject {
		return ""
	}
	if containsAny(input, "昨天", "前天", "本周", "这周", "上周", "本月", "上个月", "最近一周", "近七天") {
		return "目前可按今天或不限日期查询，今天按北京时间计算。你可以说“今天有多少待办工单”，或“不限日期的待办工单有多少”。"
	}
	categories := 0
	for _, hit := range []bool{containsAny(input, "照明", "灯"), containsAny(input, "电器", "插座", "空调"), containsAny(input, "给排水", "漏水", "水管", "水龙头")} {
		if hit {
			categories++
		}
	}
	if categories > 1 && containsAny(input, "多少", "几单", "哪些", "筛选", "查", "谁", "推荐") {
		return "目前每次支持一个故障类别，也可以查询全部类别。你可以说“照明工单有多少”，或“全部类别的工单有多少”。"
	}
	return ""
}

// queryDates uses half-open UTC bounds for inclusive Beijing calendar dates.
func queryDates(q OrderQuery, now time.Time) (time.Time, time.Time) {
	start, end := chinaToday(now)
	switch q.Period {
	case "recent":
		start = start.AddDate(0, 0, -6)
	case "yesterday":
		end = start
		start = start.AddDate(0, 0, -1)
	case "this_week", "last_week":
		day := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Weekday()
		offset := (int(day) + 6) % 7
		start = start.AddDate(0, 0, -offset)
		if q.Period == "last_week" {
			end = start
			start = start.AddDate(0, 0, -7)
		}
	case "this_month":
		t := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
		start = start.AddDate(0, 0, 1-t.Day())
	}
	if q.DateFrom != "" {
		zone := time.FixedZone("Asia/Shanghai", 8*3600)
		a, _ := time.ParseInLocation("2006-01-02", q.DateFrom, zone)
		b, _ := time.ParseInLocation("2006-01-02", q.DateTo, zone)
		return a.UTC(), b.AddDate(0, 0, 1).UTC()
	}
	return start, end
}

func queryContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
