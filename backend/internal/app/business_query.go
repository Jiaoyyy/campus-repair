package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MCPQueryOrders = "query_work_orders"

type OrderQuery struct {
	DateBasis    string `json:"date_basis,omitempty"`
	Metric       string `json:"metric,omitempty"`
	MetricCursor string `json:"metric_cursor,omitempty"`

	IncludeNextSteps bool   `json:"include_next_steps,omitempty"`
	Analysis         string `json:"analysis,omitempty"`
	Queue            string `json:"queue,omitempty"`
	Status           string `json:"status,omitempty"`
	Category         string `json:"category,omitempty"`
	Number           string `json:"number,omitempty"`
	Period           string `json:"period,omitempty"`
	Location         string `json:"location,omitempty"`
	Keyword          string `json:"keyword,omitempty"`
	DateFrom         string `json:"date_from,omitempty"`
	DateTo           string `json:"date_to,omitempty"`
}
type BusinessOrder struct {
	ReviewAddressMissing bool               `json:"review_address_missing,omitempty"`
	CurrentStatus        string             `json:"current_status,omitempty"`
	CurrentVersion       int64              `json:"current_version,omitempty"`
	RecurrenceExists     bool               `json:"recurrence_exists,omitempty"`
	LatestRepair         *OrderRepair       `json:"latest_repair,omitempty"`
	VisitStart           *time.Time         `json:"visit_start,omitempty"`
	VisitEnd             *time.Time         `json:"visit_end,omitempty"`
	Review               *reviewSuggestion  `json:"review,omitempty"`
	Recommended          *BusinessCandidate `json:"recommended,omitempty"`
	Advice               string             `json:"advice,omitempty"`
	Priority             string             `json:"priority,omitempty"`
	Version              int64              `json:"version,omitempty"`
	Responsible          string             `json:"responsible,omitempty"`

	Next        *OrderStep `json:"next_step,omitempty"`
	ID          int64      `json:"id"`
	Number      string     `json:"number"`
	Status      string     `json:"status"`
	Category    string     `json:"category"`
	Location    string     `json:"location"`
	Description string     `json:"description"`
}
type BusinessCandidate struct {
	ID         int64    `json:"id"`
	Name       string   `json:"display_name"`
	Categories []string `json:"categories"`
	Pending    int      `json:"pending_count"`
	Progress   int      `json:"in_progress_count"`
}
type BusinessResult struct {
	Followup      *requesterFollowup `json:"followup,omitempty"`
	Parts         *partsDraft        `json:"parts,omitempty"`
	Completion    *completionDraft   `json:"completion,omitempty"`
	ReviewSource  int64              `json:"review_source,omitempty"`
	ReviewOrder   int64              `json:"review_order,omitempty"`
	sourceMessage int64
	worklistRows  []BusinessOrder
	Natural       bool             `json:"natural,omitempty"`
	Additional    []BusinessResult `json:"additional,omitempty"`

	Detail      *OrderDetail        `json:"detail,omitempty"`
	Warning     string              `json:"warning,omitempty"`
	Unavailable bool                `json:"unavailable,omitempty"`
	Analysis    *BusinessAnalysis   `json:"analysis,omitempty"`
	Kind        string              `json:"kind"`
	Query       OrderQuery          `json:"query"`
	Total       int                 `json:"total"`
	Statuses    map[string]int      `json:"statuses"`
	Items       []BusinessOrder     `json:"items"`
	Candidates  []BusinessCandidate `json:"candidates,omitempty"`
	At          time.Time           `json:"queried_at"`
	TimeBasis   string              `json:"time_basis,omitempty"`
	Historical  bool                `json:"historical"`
}

var businessStatusNames = map[string]string{"submitted": "待审核", "needs_info": "待补充", "unassigned": "待派单", "assigned": "待接单", "in_progress": "处理中", "awaiting_confirmation": "待确认", "closed": "已关闭", "rejected": "已驳回", "cancelled": "已取消"}
var orderNumberPattern = regexp.MustCompile(`(?i)RP[0-9a-z]{8,38}`)

func (q OrderQuery) valid() bool {
	if q.DateBasis != "" && (q.DateBasis != "visit" || q.Analysis != "" || ((q.Period == "" || q.Period == "all") && q.DateFrom == "")) {
		return false
	}
	if q.Metric != "" && (q.Analysis == "" || !queryContains([]string{"aged", "overdue", "reschedule", "parts", "exception_total", "no_show_orders"}, q.Metric)) || q.MetricCursor != "" && (q.Metric == "" || len(q.MetricCursor) > 100) {
		return false
	}
	if !queryContains([]string{"", "overview", "frequent", "no_show", "exceptions"}, q.Analysis) {
		return false
	}
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

const activeVisitStart = "(SELECT v.starts_at FROM work_order_visit_context c JOIN work_order_visits v ON v.id=c.active_visit_id WHERE c.work_order_id=w.id)"

func businessQuerySQL(u User, q OrderQuery, now time.Time) (string, []any, string, error) {
	if !q.valid() || q.DateBasis == "visit" && u.Role != "technician" || (u.Role != "requester" && u.Role != "technician" && u.Role != "admin") {
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
		if q.DateBasis == "visit" {
			where += " AND " + activeVisitStart + " >= ? AND " + activeVisitStart + " < ?"
			basis = "预约上门时间（北京时间）"
		} else if u.Role == "technician" {
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
	if q.Analysis != "" {
		return queryBusinessAnalysis(ctx, db, u, q)
	}
	return queryOrderPage(ctx, db, u, q, time.Now().UTC(), 1, time.Time{})
}

// Relative date filters remain anchored to the original chat turn on later pages.
func queryOrderPage(ctx context.Context, db *sql.DB, u User, q OrderQuery, anchor time.Time, page int, createdBefore time.Time) (BusinessResult, error) {
	result := BusinessResult{Kind: "orders", Query: q, Statuses: map[string]int{}, Items: []BusinessOrder{}, At: time.Now().UTC()}
	where, args, basis, err := businessQuerySQL(u, q, anchor)
	if basis == "" {
		basis = "工单日期不限"
	}
	if err != nil {
		return result, err
	}
	if !createdBefore.IsZero() {
		where += " AND w.created_at<=?"
		args = append(args, createdBefore)
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
	sortBy := "updated_at DESC,id DESC"
	if q.DateBasis == "visit" {
		sortBy = activeVisitStart + ",id"
	}
	statement := "SELECT id,number,status,category,location,description,version FROM work_orders w WHERE " + where + " ORDER BY " + sortBy
	if page > 0 {
		page = min(page, max(1, (result.Total+4)/5))
		statement += " LIMIT 5 OFFSET ?"
		args = append(args, (page-1)*5)
	}
	rows, err = tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var o BusinessOrder
		if err = rows.Scan(&o.ID, &o.Number, &o.Status, &o.Category, &o.Location, &o.Description, &o.Version); err != nil {
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
	if q.IncludeNextSteps || q.DateBasis == "visit" {
		for i := range result.Items {
			item := &result.Items[i]
			visit, e := readVisit(ctx, tx, item.ID)
			if e != nil {
				return result, e
			}
			if q.DateBasis == "visit" && visit.Visit != nil {
				item.VisitStart = &visit.Visit.Start
				item.VisitEnd = &visit.Visit.End
			}
			if q.IncludeNextSteps {
				order, e := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE id=?", item.ID))
				if e != nil {
					return result, e
				}
				next := orderNextStep(order, u, visit, result.At)
				item.Next = &next
			}
		}
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
	if (q.Period == "" || q.Period == "all") && q.DateFrom == "" {
		label += " · 不限日期"
	}
	return label
}

const partialExplanationNotice = "部分解释中的数字或标识无法核实，以下保留可核对的段落和查询结果。"
const unverifiedExplanationNotice = "已保留可核对的查询结果，本轮正文解释未通过校验。"

func technicianOrderSummary(b BusinessResult) string {
	label := queryLabel("technician", b.Query)
	if b.Total == 0 {
		return label + "，暂无匹配任务。"
	}
	counts := []string{}
	for _, status := range []string{"assigned", "in_progress", "awaiting_confirmation", "closed", "cancelled"} {
		n := b.Statuses[status]
		if n == 0 {
			continue
		}
		name := businessStatusNames[status]
		if status == "awaiting_confirmation" {
			name = "已上报，待报修人确认"
		}
		counts = append(counts, fmt.Sprintf("%s %d 单", name, n))
	}
	summary := fmt.Sprintf("%s，共 %d 单。", label, b.Total)
	if len(counts) > 0 {
		summary += "其中" + strings.Join(counts, "，") + "。"
	}
	return summary
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
		text := label + "目前没有匹配记录。"
		if b.TimeBasis != "" {
			text += "\n" + b.TimeBasis + "。"
		}
		if b.Query.Period != "" && b.Query.Period != "all" || b.Query.DateFrom != "" {
			text += "时间范围外仍可能有未结束工单，可以改问“不限日期的未结束工单”。"
		}
		return text + "这不代表其他范围也没有工单。"
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

	return text
}
func decodeBusinessResult(raw any) (*BusinessResult, error) {
	return decodeOrderSnapshot(raw, false)
}

func decodeOrderSnapshot(raw any, complete bool) (*BusinessResult, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result BusinessResult
	if json.Unmarshal(b, &result) != nil || (result.Kind != "orders" && result.Kind != "analysis") || !result.Query.valid() || result.Total < 0 || result.At.IsZero() || result.Statuses == nil || result.Items == nil {
		return nil, errors.New("invalid business query result")
	}
	if result.Kind == "analysis" && (result.Query.Analysis == "" || !analysisFactsValid(result.Analysis)) {
		return nil, errors.New("invalid analysis result")
	}
	sum := 0
	for s, n := range result.Statuses {
		if businessStatusNames[s] == "" || n < 0 {
			return nil, errors.New("invalid counts")
		}
		sum += n
	}
	if sum != result.Total || (!complete && len(result.Items) > 5) || (complete && len(result.Items) != result.Total) || len(result.Items) > result.Total {
		return nil, errors.New("invalid result count")
	}
	for _, item := range result.Items {
		if item.ID < 1 || businessStatusNames[item.Status] == "" || !categoryOK(item.Category) || item.Number == "" {
			return nil, errors.New("invalid order")
		}
	}
	return &result, nil
}
func (s *Server) businessHistory(ctx context.Context, u User, cid, mid int64, displayAgent int64) (*BusinessResult, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT b.result FROM assistant_business_results b JOIN messages m ON m.id=b.message_id WHERE m.id=? AND m.conversation_id=? AND m.role='assistant' AND m.status='complete'", mid, cid).Scan(&raw)
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
	if result.Kind == completionDraftKind || result.Kind == partsDraftKind || result.Kind == requesterFollowupKind {
		if result.Kind == requesterFollowupKind && u.Role != "requester" || result.Kind != requesterFollowupKind && u.Role != "technician" {
			return &BusinessResult{Kind: result.Kind, Unavailable: true}, nil
		}
		for i := range result.Items {
			item := &result.Items[i]
			order, err := s.getOrder(ctx, u, item.ID)
			if err != nil {
				var problem *Problem
				if errors.As(err, &problem) && problem.Status == 404 {
					return &BusinessResult{Kind: result.Kind, Unavailable: true}, nil
				}
				return nil, err
			}
			if order.Version != item.Version || order.Status != item.Status {
				item.Version = 0
			}
			if result.Kind == requesterFollowupKind && item.Status == "closed" && item.LatestRepair != nil {
				item.RecurrenceExists, err = recurrenceExists(ctx, s.DB, item.LatestRepair.ID)
				if err != nil {
					return nil, err
				}
			}
		}
		return &result, nil
	}

	if (result.Kind == "candidates" || result.Kind == "worklist" || result.Kind == "pre_review" || result.Kind == reviewDiscussion) && u.Role != "admin" {
		return &BusinessResult{Kind: result.Kind, Unavailable: true, Items: []BusinessOrder{}, Statuses: map[string]int{}, At: result.At, Historical: true}, nil
	}
	if result.Kind == "pre_review" && displayAgent != 0 {
		_, skillErr := s.selectSkill(ctx, u, Agent{ID: displayAgent}, "", nil, MCPDispatch)
		if skillErr != nil || mcpAllowed(ctx, s.DB, mcpClaims{AgentID: displayAgent}, u, MCPQueryOrders) != nil {
			return &BusinessResult{Kind: result.Kind, Unavailable: true, Historical: true}, nil
		}
	}
	if result.Detail != nil {
		if _, e := s.getOrder(ctx, u, result.Detail.Order.ID); e != nil {
			var problem *Problem
			if errors.As(e, &problem) && problem.Status == 404 {
				return &BusinessResult{Kind: result.Kind, Unavailable: true, Items: []BusinessOrder{}, Statuses: map[string]int{}, At: result.At, Historical: true}, nil
			}
			return nil, e
		}
	}
	if result.Kind == "pre_review" || result.Natural || len(result.Additional) > 0 || result.Analysis != nil && len(result.Analysis.Related) > 0 {
		for i, item := range result.evidenceOrders() {
			order, err := s.getOrder(ctx, u, item.ID)
			if err != nil {
				var problem *Problem
				if errors.As(err, &problem) && problem.Status == 404 {
					return &BusinessResult{Kind: result.Kind, Unavailable: true, Items: []BusinessOrder{}, Statuses: map[string]int{}, At: result.At, Historical: true}, nil
				}
				return nil, err
			}
			if displayAgent != 0 && result.Kind == "pre_review" && i < len(result.Items) {
				result.Items[i].CurrentStatus = order.Status
				result.Items[i].CurrentVersion = order.Version
				result.Items[i].ReviewAddressMissing = needsReviewBuilding(order.Location)
			}
		}
		return &result, nil
	}
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
func (s *Server) lastBusinessResult(ctx context.Context, u User, cid int64) (*BusinessResult, error) {
	var messageID int64
	err := s.DB.QueryRowContext(ctx, "SELECT id FROM messages WHERE conversation_id=? AND role='assistant' AND status='complete' ORDER BY id DESC LIMIT 1", cid).Scan(&messageID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := s.businessHistory(ctx, u, cid, messageID, 0)
	if err != nil || result == nil || result.Unavailable {
		return nil, err
	}
	result.sourceMessage = messageID
	return result, err
}

// A complete, unqualified question can use the requester's current queue directly.
func requesterCountQuery(question string) OrderQuery {
	switch question {
	case "我的报修还有多少单未结束", "我的报修有多少单未结束", "我的报修还有几单未结束", "我的未结束工单有多少单":
		return OrderQuery{Queue: "active", Period: "all"}
	case "我的报修还有多少单待我处理", "我的报修有多少单待我处理", "我的报修还有几单待我处理", "我的报修还有多少单需要我处理":
		return OrderQuery{Queue: "action", Period: "all"}
	default:
		return OrderQuery{}
	}
}

func businessIntent(role, input string, previous *OrderQuery) (OrderQuery, bool) {
	q := strings.TrimSpace(input)
	query := OrderQuery{Queue: "all", Period: "all"}
	if role != "requester" && role != "technician" && role != "admin" {
		return query, false
	}
	if draftIntent(role, q, false) || containsAny(q, "哪些类别", "哪些报修类别", "哪些故障类别", "哪些报修类型") && !strings.Contains(q, "工单") {
		return query, false
	}
	if containsAny(q, "怎么", "如何", "流程", "制度", "规定", "安全", "资料", "时限", "多久", "几天", "多少时间", "多长时间", "多少钱") {
		return query, false
	}
	number := orderNumberPattern.FindString(q)
	businessNoun := containsAny(q, "工单", "报修", "任务", "待办", "单子", "哪些单", "几单", "多少单", "有哪些活")
	period := ""
	for _, item := range []struct {
		period string
		terms  []string
	}{
		{"today", []string{"今天", "今日"}}, {"yesterday", []string{"昨天", "昨日"}}, {"this_week", []string{"本周", "这周"}}, {"last_week", []string{"上周"}}, {"this_month", []string{"本月"}}, {"recent", []string{"最近一周", "近七天"}},
	} {
		if containsAny(q, item.terms...) {
			period = item.period
			break
		}
	}
	filterSignal := period != "" || containsAny(q, "待审核", "等我审核", "待我确认", "待我补充", "没修完", "未完成", "待派单", "待接单", "处理中", "待补充", "待确认", "未结束", "没处理完", "已结束", "已关闭", "已取消", "已驳回", "今天", "全部类别", "全部状态", "不限日期", "全部日期", "所有日期")
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
	if period != "" {
		query.Period = period
		query.DateFrom, query.DateTo = "", ""
	}
	if containsAny(q, "不限日期", "全部日期", "所有日期") {
		query.Period = "all"
		query.DateFrom, query.DateTo = "", ""
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
	if containsAny(input, "前天", "上个月") {
		return "请给出具体的起止日期，或选择昨天、本周、上周、本月、近七天等范围。"
	}
	categories := map[string]bool{}
	for _, part := range strings.FieldsFunc(input, func(r rune) bool { return strings.ContainsRune("和与及、，,或", r) }) {
		if cat := repairCategory(part); cat != "" {
			categories[cat] = true
		}
	}
	if len(categories) > 1 && containsAny(input, "多少", "几单", "哪些", "筛选", "查", "谁", "推荐") {
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

func (s *Server) messageOrderPage(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	cid, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.ownConversation(r.Context(), u, cid)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mid, err := strconv.ParseInt(r.PathValue("message"), 10, 64)
	if err != nil || mid < 1 {
		s.fail(w, r, invisible())
		return
	}
	page, _, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var raw []byte
	err = s.DB.QueryRowContext(r.Context(), "SELECT b.result FROM assistant_business_results b JOIN messages m ON m.id=b.message_id WHERE m.conversation_id=? AND m.id=? AND m.status='complete'", cid, mid).Scan(&raw)
	if err == sql.ErrNoRows {
		s.fail(w, r, invisible())
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var stored worklistSnapshot
	if err = json.Unmarshal(raw, &stored); err != nil {
		s.fail(w, r, err)
		return
	}
	result := stored.BusinessResult
	if n := r.URL.Query().Get("additional"); n != "" {
		index, e := strconv.Atoi(n)
		if e != nil || index < 0 || index >= len(result.Additional) {
			s.fail(w, r, invisible())
			return
		}
		result = result.Additional[index]
	}
	if (result.Kind != "orders" && result.Kind != "pre_review" && result.Kind != "worklist") || result.Query.Analysis != "" || result.At.IsZero() {
		s.fail(w, r, invisible())
		return
	}
	if err = mcpAllowed(r.Context(), s.DB, mcpClaims{AgentID: c.AgentID}, u, MCPQueryOrders); err != nil {
		s.fail(w, r, denied())
		return
	}
	var agent Agent
	var skill *SkillRun
	if result.Kind == "pre_review" || result.Kind == "worklist" {
		if u.Role != "admin" || result.Kind == "pre_review" && result.Query.Status != "submitted" {
			s.fail(w, r, denied())
			return
		}
		agent, err = s.agent(r.Context(), u, c.AgentID, false)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if skill, err = s.selectSkill(r.Context(), u, agent, "", nil, MCPDispatch); err != nil {
			s.fail(w, r, err)
			return
		}
		claims := mcpClaims{AgentID: c.AgentID, Skill: skill.ID, DefinitionVersion: skill.DefinitionVersion, SkillVersion: skill.ConfigVersion}
		if err = mcpAllowed(r.Context(), s.DB, claims, u, MCPDispatch); err != nil {
			s.fail(w, r, denied())
			return
		}
		if result.Kind == "worklist" {
			stored.BusinessResult = result
			items, total, number, supplemental, e := s.readWorklistPage(r.Context(), u, stored, page)
			if e != nil {
				s.fail(w, r, e)
				return
			}
			send(w, 200, map[string]any{"items": items, "total": total, "page": number, "supplemental": supplemental})
			return
		}
		snapshot, e := s.businessHistory(r.Context(), u, cid, mid, c.AgentID)
		if e != nil {
			s.fail(w, r, e)
			return
		}
		if snapshot == nil || snapshot.Unavailable {
			s.fail(w, r, invisible())
			return
		}
		if n := r.URL.Query().Get("additional"); n != "" {
			index, _ := strconv.Atoi(n)
			if index < 0 || index >= len(snapshot.Additional) {
				s.fail(w, r, invisible())
				return
			}
			snapshot = &snapshot.Additional[index]
		}
		if r.URL.Query().Get("refresh") == "1" {
			send(w, 200, snapshot)
			return
		}
		count := len(snapshot.Items)
		page = min(page, max(1, (count+4)/5))
		start := (page - 1) * 5
		send(w, 200, map[string]any{"items": snapshot.Items[start:min(start+5, count)], "total": count, "page": page})
		return
	}
	fresh, err := queryOrderPage(r.Context(), s.DB, u, result.Query, result.At, page, time.Time{})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": fresh.Items, "total": fresh.Total, "page": min(page, max(1, (fresh.Total+4)/5))})
}
