package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const insightAgedDays = 7

type InsightGroup struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}
type InsightFact struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Count     int    `json:"count"`
	TimeBasis string `json:"time_basis,omitempty"`
}
type InsightNote struct {
	Evidence       []string `json:"evidence"`
	Recommendation string   `json:"recommendation"`
}
type BusinessAnalysis struct {
	Related     map[string]InsightOrders `json:"related,omitempty"`
	Title       string                   `json:"title"`
	Scope       string                   `json:"scope"`
	Facts       []InsightFact            `json:"facts"`
	Categories  []InsightGroup           `json:"categories"`
	Locations   []InsightGroup           `json:"locations"`
	Symptoms    []InsightGroup           `json:"symptoms"`
	Notes       []InsightNote            `json:"notes"`
	ModelStatus string                   `json:"model_status"`
	Warning     string                   `json:"warning,omitempty"`
}

// These narrow, explicit intents are also grounded after model validation. General
// location/date language continues through the native planner rather than regex SQL.
func analysisIntent(input string) string {
	if containsAny(input, "白跑", "到场无人", "到场没人", "上门没人") {
		return "no_show"
	}
	if containsAny(input, "上门异常", "上门超时", "等待备件", "需要重新约时间") {
		return "exceptions"
	}
	if containsAny(input, "高发", "最多的问题", "最多的故障", "最常见", "频繁", "重复出现") {
		return "frequent"
	}
	if containsAny(input, "总结", "分析一下", "分析问题", "发现问题", "有什么值得关注", "优先处理什么", "优先关注什么", "积压情况") {
		return "overview"
	}
	return ""
}
func groundAnalysisQuery(q OrderQuery, input string, previous *BusinessResult) OrderQuery {
	explicit := analysisIntent(input)
	if explicit != "" {
		q.Analysis = explicit
	}
	if q.Analysis == "" {
		return q
	}
	if explicit != "" && (q.Keyword == "白跑" || q.Keyword == "上门异常" || q.Keyword == "问题" || q.Keyword == "故障" || q.Keyword == "高发") {
		q.Keyword = ""
	}
	if q.Analysis == "overview" && !queryTimeMention.MatchString(input) && !(strings.HasPrefix(strings.TrimSpace(input), "那") && previous != nil) {
		q.Period, q.DateFrom, q.DateTo = "all", "", ""
		if q.Status == "" && (q.Queue == "" || q.Queue == "all") {
			q.Queue = "active"
		}
		return q
	}
	if q.DateFrom == "" && (q.Period == "" || q.Period == "all") {
		switch {
		case containsAny(input, "今天", "今日"):
			q.Period = "today"
		case containsAny(input, "昨天", "昨日"):
			q.Period = "yesterday"
		case containsAny(input, "上周"):
			q.Period = "last_week"
		case containsAny(input, "本周", "这周"):
			q.Period = "this_week"
		case strings.Contains(input, "本月"):
			q.Period = "this_month"
		default:
			q.Period = "recent"
		}
	}
	if !queryTimeMention.MatchString(input) {
		if strings.HasPrefix(strings.TrimSpace(input), "那") && previous != nil {
			q.Period, q.DateFrom, q.DateTo = previous.Query.Period, previous.Query.DateFrom, previous.Query.DateTo
		} else if q.DateFrom == "" {
			q.Period = "recent"
		}
	}
	if containsAny(input, "不限日期", "不限时间", "全部时间", "所有日期") {
		q.Period, q.DateFrom, q.DateTo = "all", "", ""
	}
	return q
}
func insightClip(s string, limit int) string {
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
func insightScope(role string) string {
	return map[string]string{"admin": "本组织", "requester": "本人报修", "technician": "当前指派给本人"}[role]
}
func analysisBase(u User, q OrderQuery, now time.Time) (string, []any, error) {
	q.Period, q.DateFrom, q.DateTo = "all", "", ""
	where, args, _, err := businessQuerySQL(u, q, now)
	return where, args, err
}
func noShowWindow(q OrderQuery, now time.Time) (string, []any, string) {
	if (q.Period == "" || q.Period == "all") && q.DateFrom == "" {
		return "v.no_show_at IS NOT NULL", nil, "白跑按到场无人记录时间，不限日期"
	}
	start, end := queryDates(q, now)
	return "v.no_show_at>=? AND v.no_show_at<?", []any{start, end}, "白跑记录时间（北京时间）：" + start.In(businessTZ).Format("2006-01-02") + " 至 " + end.Add(-time.Second).In(businessTZ).Format("2006-01-02")
}
func queryBusinessAnalysis(ctx context.Context, db *sql.DB, u User, q OrderQuery) (BusinessResult, error) {
	now := time.Now().UTC()
	b := BusinessResult{Kind: "analysis", Query: q, At: now, Items: []BusinessOrder{}, Statuses: map[string]int{}}
	a := &BusinessAnalysis{Title: map[string]string{"overview": "问题总结", "frequent": "高发问题分析", "no_show": "白跑记录分析", "exceptions": "上门异常分析"}[q.Analysis], Scope: insightScope(u.Role), Facts: []InsightFact{}, Categories: []InsightGroup{}, Locations: []InsightGroup{}, Symptoms: []InsightGroup{}, Notes: []InsightNote{}, ModelStatus: "facts_only"}
	b.Analysis = a
	where, args, basis, err := businessQuerySQL(u, q, now)
	if err != nil {
		return b, err
	}
	base, baseArgs, err := analysisBase(u, q, now)
	if err != nil {
		return b, err
	}
	noShowSQL, noShowArgs, noShowBasis := noShowWindow(q, now)
	if q.Analysis == "no_show" {
		where = base + " AND EXISTS (SELECT 1 FROM work_order_visits v WHERE v.work_order_id=w.id AND " + noShowSQL + ")"
		args = append(append([]any{}, baseArgs...), noShowArgs...)
		basis = noShowBasis
	}
	if q.Analysis == "overview" && q.Queue == "active" && basis == "" {
		basis = "当前未结束工单，不限创建日期"
	}
	if q.Analysis == "exceptions" {
		where, args = base, append([]any{}, baseArgs...)
		basis = "当前上门异常快照，不按工单创建日期排除旧工单"
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	add := func(id, title, text string, count int) {
		scope := basis
		switch id {
		case "overdue", "reschedule", "parts", "exception_total", "aged":
			scope = "当前快照，不限工单创建日期"
		case "no_show", "no_show_orders":
			scope = noShowBasis
		}
		a.Facts = append(a.Facts, InsightFact{ID: id, Title: title, Text: text, Count: count, TimeBasis: scope})
	}
	load := func(metric, where string, args []any, count int) error {
		if q.Metric != "" && q.Metric != metric || q.Metric == "" && (count == 0 || q.Analysis == "frequent" || (q.Analysis == "no_show" && metric != "no_show_orders") || (q.Analysis != "no_show" && metric == "no_show_orders")) {
			return nil
		}
		page, err := readInsightOrders(ctx, tx, u, q, metric, where, args, count)
		if err != nil {
			return err
		}
		if a.Related == nil {
			a.Related = map[string]InsightOrders{}
		}
		a.Related[metric] = page
		return nil
	}
	// All counters share one repeatable-read snapshot; query values remain bound.
	exceptionParts := []string{}
	exceptionArgs := []any{}
	for _, key := range []string{"overdue", "reschedule", "parts"} {
		f, values, _ := visitQueue("admin", key, now)
		f = strings.ReplaceAll(f, "work_orders.", "w.")
		exceptionParts = append(exceptionParts, "("+f+")")
		exceptionArgs = append(exceptionArgs, values...)
		var n int
		err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_orders w WHERE "+base+" AND "+f, append(append([]any{}, baseArgs...), values...)...).Scan(&n)
		if err != nil {
			return b, err
		}
		titles := map[string]string{"overdue": "上门超时", "reschedule": "需要重新约时间", "parts": "等待备件"}
		texts := map[string]string{"overdue": "已超过预约结束时间，尚未记录到场；由当前维修人员核实进展。", "reschedule": "到场无人后尚未形成新预约；核对报修人方便时间与维修人员确认。", "parts": "仍在等待备件；由当前维修人员跟进到位与恢复处理。"}
		add(key, titles[key], fmt.Sprintf("当前 %d 单。%s", n, texts[key]), n)
		if err := load(key, base+" AND "+f, append(append([]any{}, baseArgs...), values...), n); err != nil {
			return b, err
		}
	}
	exceptionWhere := base + " AND (" + strings.Join(exceptionParts, " OR ") + ")"
	allExceptionArgs := append(append([]any{}, baseArgs...), exceptionArgs...)
	var exceptions int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_orders w WHERE "+exceptionWhere, allExceptionArgs...).Scan(&exceptions); err != nil {
		return b, err
	}
	add("exception_total", "上门异常合计", fmt.Sprintf("当前去重后 %d 单；同一工单可能属于多类，不能直接相加。", exceptions), exceptions)
	if err := load("exception_total", exceptionWhere, allExceptionArgs, exceptions); err != nil {
		return b, err
	}
	if q.Analysis == "exceptions" {
		where, args = exceptionWhere, allExceptionArgs
	}
	b.TimeBasis = basis
	if b.TimeBasis == "" {
		b.TimeBasis = "工单范围不限日期"
	}
	var noShowCount, noShowOrders int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*),COUNT(DISTINCT w.id) FROM work_order_visits v JOIN work_orders w ON w.id=v.work_order_id WHERE "+base+" AND "+noShowSQL, append(append([]any{}, baseArgs...), noShowArgs...)...).Scan(&noShowCount, &noShowOrders)
	if err != nil {
		return b, err
	}
	add("no_show", "已记录白跑", fmt.Sprintf("%d 次，涉及 %d 单。%s；不等同于白跑下降率。", noShowCount, noShowOrders, noShowBasis), noShowCount)
	add("no_show_orders", "白跑涉及工单", fmt.Sprintf("按工单去重 %d 单，记录次数可能多于工单数。", noShowOrders), noShowOrders)
	if err := load("no_show_orders", base+" AND EXISTS (SELECT 1 FROM work_order_visits v WHERE v.work_order_id=w.id AND "+noShowSQL+")", append(append([]any{}, baseArgs...), noShowArgs...), noShowOrders); err != nil {
		return b, err
	}
	var aged int
	agedWhere := base + " AND status NOT IN ('closed','rejected','cancelled') AND created_at<=?"
	agedArgs := append(append([]any{}, baseArgs...), now.AddDate(0, 0, -insightAgedDays))
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_orders w WHERE "+agedWhere, agedArgs...).Scan(&aged)
	if err != nil {
		return b, err
	}
	add("aged", "未结束超过七天", fmt.Sprintf("当前 %d 单创建已满%d天且未结束；这是关注线索，不代表违反校方维修时限。", aged, insightAgedDays), aged)
	if err := load("aged", agedWhere, agedArgs, aged); err != nil {
		return b, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT status,COUNT(*) FROM work_orders w WHERE "+where+" GROUP BY status", args...)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var status string
		var n int
		if err = rows.Scan(&status, &n); err != nil {
			break
		}
		b.Statuses[status] = n
		b.Total += n
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return b, err
	}
	add("total", "本次分析范围", fmt.Sprintf("%s，共 %d 单；%s。", a.Scope, b.Total, b.TimeBasis), b.Total)
	for _, group := range []struct {
		column, id, title string
		target            *[]InsightGroup
	}{{"category", "category", "报修最多的类别", &a.Categories}, {"location", "location", "报修最多的准确位置", &a.Locations}, {"description", "symptom", "出现最多的相同描述", &a.Symptoms}} {
		rows, err = tx.QueryContext(ctx, "SELECT "+group.column+",COUNT(*) AS n FROM work_orders w WHERE "+where+" GROUP BY "+group.column+" ORDER BY n DESC,"+group.column+" ASC LIMIT 5", args...)
		if err != nil {
			return b, err
		}
		for rows.Next() {
			var g InsightGroup
			if err = rows.Scan(&g.Label, &g.Count); err != nil {
				break
			}
			if group.id == "category" {
				g.Label = businessCategoryNames[g.Label]
			}
			*group.target = append(*group.target, g)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return b, err
		}
		if len(*group.target) > 0 {
			top := (*group.target)[0]
			ties := []string{}
			for _, g := range *group.target {
				if g.Count == top.Count {
					ties = append(ties, insightClip(g.Label, 70))
				}
			}
			qualifier := ""
			if len(ties) > 1 {
				qualifier = "（展示项并列；前5项之外可能还有同数项）"
			}
			add(group.id+":top", group.title, fmt.Sprintf("%s：%s，各 %d 单%s；按全量记录统计。", group.title, strings.Join(ties, "、"), top.Count, qualifier), top.Count)
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,number,status,category,location,description,version FROM work_orders w WHERE "+where+" ORDER BY updated_at DESC,id DESC LIMIT 5", args...)
	if err != nil {
		return b, err
	}
	for rows.Next() {
		var o BusinessOrder
		if err = rows.Scan(&o.ID, &o.Number, &o.Status, &o.Category, &o.Location, &o.Description, &o.Version); err != nil {
			break
		}
		b.Items = append(b.Items, o)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return b, err
	}
	if err = tx.Commit(); err != nil {
		return b, err
	}
	return b, nil
}

func analysisFactsValid(a *BusinessAnalysis) bool {
	if a == nil || a.Title == "" || a.Scope == "" || len(a.Facts) < 8 || len(a.Facts) > 11 || a.ModelStatus != "facts_only" || len(a.Notes) != 0 {
		return false
	}
	ids := map[string]bool{}
	for _, f := range a.Facts {
		if f.ID == "" || ids[f.ID] || f.Count < 0 || f.Title == "" || f.Text == "" {
			return false
		}
		ids[f.ID] = true
	}
	for _, groups := range [][]InsightGroup{a.Categories, a.Locations, a.Symptoms} {
		if len(groups) > 5 {
			return false
		}
		for _, g := range groups {
			if g.Label == "" || g.Count < 1 {
				return false
			}
		}
	}
	return true
}

func formatAnalysis(b BusinessResult) string {
	a := b.Analysis
	out := a.Title + "\n" + a.Scope + " · " + b.TimeBasis
	order := []string{"category:top", "symptom:top", "location:top", "exception_total", "aged"}
	if b.Query.Analysis == "no_show" {
		order = []string{"no_show", "reschedule", "exception_total"}
	}
	if b.Query.Analysis == "exceptions" {
		order = []string{"exception_total", "overdue", "reschedule", "parts"}
	}
	for _, id := range order {
		for _, f := range a.Facts {
			if f.ID == id {
				out += "\n" + f.Text
			}
		}
	}
	if b.Total == 0 {
		out += "\n本次范围没有匹配工单；不据此判断其他范围也没有问题。"
	}
	if a.ModelStatus == "ready" {
		out += "\n建议（待核实）："
		for _, n := range a.Notes {
			out += "\n" + n.Recommendation
		}
	} else {
		out += "\n" + a.Warning
	}
	return out
}

// Preserve an explicitly named, currently authorized exact location when the
// planner omits it. Candidate names come from scoped records, never the model.
func chooseAnalysisLocation(candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", nil
	}
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if !strings.HasPrefix(chosen, candidate) {
			return "", bad("请每次分析一个位置范围，分别说明要分析的位置")
		}
	}
	return chosen, nil
}
func (s *Server) groundAnalysisLocation(ctx context.Context, u User, input string, q OrderQuery) (OrderQuery, error) {
	if q.Analysis == "" {
		return q, nil
	}
	where, args := orderFilter(u)
	args = append(args, input)
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT location FROM work_orders WHERE "+where+" AND location<>'' AND LOCATE(location,?)>0 ORDER BY CHAR_LENGTH(location) DESC,location LIMIT 51", args...)
	if err != nil {
		return q, err
	}
	defer rows.Close()
	candidates := []string{}
	for rows.Next() {
		var location string
		if err = rows.Scan(&location); err != nil {
			return q, err
		}
		candidates = append(candidates, location)
	}
	if err = rows.Err(); err != nil {
		return q, err
	}
	if len(candidates) > 50 {
		return q, bad("位置范围较多，请明确一个要分析的位置")
	}
	location, err := chooseAnalysisLocation(candidates)
	if err != nil {
		return q, err
	}
	if location != "" {
		q.Location = location
	}
	return q, nil
}
