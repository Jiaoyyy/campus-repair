package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const worklistPageSize = 5

type worklistSnapshot struct {
	BusinessResult
	Rows []BusinessOrder `json:"worklist_rows,omitempty"`
}

func validWorklist(mode string) bool {
	return mode == "dispatch" || mode == "review" || mode == "recent_review" || mode == "pre_review"
}

// Match an instruction to start a review, leaving counts, explanations and writes to their own routes.
var startReviewRequest = regexp.MustCompile(`^(?:审核|初审)(?:一下)?(?:最近|近期)?(?:的)?(?:还没审核|未审核|待审核|待审)?(?:的)?(?:工单|单子)?$`)

// Common unqualified requests also work when the model is unavailable.
// Leave qualified questions to the semantic planner so filters are not dropped.
func adminWorklistIntent(role, input string) string {
	if role != "admin" {
		return ""
	}
	q := strings.Trim(strings.TrimSpace(input), "。？！?!")
	q = strings.TrimPrefix(q, "请")
	q = strings.TrimPrefix(q, "帮我")
	if startReviewRequest.MatchString(q) {
		return "pre_review"
	}
	switch q {
	case "初审", "审核工单", "重新初审待审核工单", "审核并派单", "审核后派单", "审核待审核工单", "初审待审核工单", "审核一下待审核的工单", "初审一下", "把待审核的工单处理一下", "初审待审核的工单", "审核这些工单", "把这些工单审核并派单":
		return "pre_review"
	case "把没派单的派单", "把没派单的派了", "给待派工单推荐维修人员", "看看待派工单应该安排给谁":
		return "dispatch"
	case "处理最近的工单", "把最近的工单处理一下", "最近哪些工单该先处理":
		return "recent_review"
	case "看看哪些工单该先处理", "哪些工单该先处理", "今天先处理什么", "哪些工单比较急", "哪些工单需要补充信息":
		return "review"
	}
	return ""
}

var urgencyFollowup = regexp.MustCompile(`^(?:那|那么)?(?:这几单里|这些工单里|这几单中)?(?:哪个|哪些|哪一个|哪张|哪一张|哪几张|哪一单|哪单)(?:工单|报修|单)?(?:比较|最|更)?(?:紧急|急|需要优先处理|该先处理)$`)

func asksUrgency(input string) bool {
	return urgencyFollowup.MatchString(strings.Trim(strings.TrimSpace(input), "。？?!！"))
}
func closeOrdersRequest(input string) bool {
	return containsAny(input, "把", "将", "帮我", "请", "全部", "批量") && containsAny(input, "工单", "报修", "单子") && containsAny(input, "都结束", "都关闭", "都结单", "全部结束", "全部关闭", "批量关闭")
}

var vagueLocation = regexp.MustCompile(`^(学校|校内|教学楼|宿舍|楼里|[一二三四五六七八九十0-9]+楼|不清楚|未知)$`)
var vagueFault = regexp.MustCompile(`^(坏了?|有问题|不能用|故障|不正常|需要维修)[。！! ]*$`)

func reviewAdvice(o BusinessOrder) string {
	switch o.Status {
	case "submitted":
		missing := []string{}
		if vagueLocation.MatchString(strings.TrimSpace(o.Location)) {
			missing = append(missing, "请补充具体楼栋、房间或就近标志。")
		}
		if vagueFault.MatchString(strings.TrimSpace(o.Description)) {
			missing = append(missing, "请说明哪个设施出现了什么现象。")
		}
		if len(missing) > 0 {
			return "建议退回补充：" + strings.Join(missing, "")
		}
		return "已有位置及故障描述，建议管理员核对现场信息后审核通过。"
	case "needs_info":
		return "已要求补充，等待报修人回复。"
	case "awaiting_confirmation":
		return "等待报修人核对维修结果。"
	case "assigned":
		return "已派单，等待维修人员接单。"
	case "in_progress":
		return "维修人员处理中，可在详情核对进展。"
	}
	return ""
}

func priorityClue(description string) string {
	for _, part := range strings.FieldsFunc(description, func(r rune) bool { return strings.ContainsRune("，。；！？,;\n", r) }) {
		if containsAny(part, "没有", "未见", "无", "未发现", "已排除", "不再", "没再", "不是", "未冒", "未漏", "没冒", "未起", "不漏", "已消除") {
			continue
		}
		if containsAny(part, "冒烟", "火花", "漏电", "触电", "起火", "电线裸露") || containsAny(part, "水", "积水") && containsAny(part, "插座", "配电箱") {
			return "优先核实报修描述：“" + strings.TrimSpace(part) + "”"
		}
	}
	return ""
}

func recommendCandidate(people []BusinessCandidate, planned map[int64]int) (*BusinessCandidate, string) {
	if len(people) == 0 {
		return nil, "当前没有符合该类别的启用维修人员，请管理员协调。"
	}
	best := people[0]
	load := func(c BusinessCandidate) int { return c.Pending + c.Progress + planned[c.ID] }
	for _, c := range people[1:] {
		if load(c) < load(best) || load(c) == load(best) && c.ID < best.ID {
			best = c
		}
	}
	n := planned[best.ID]
	planned[best.ID]++
	return &best, fmt.Sprintf("建议 %s：负责%s，待接单 %d 单、处理中 %d 单，本次此前已建议 %d 单。出勤待核实。", best.Name, businessCategoryNames[best.Categories[0]], best.Pending, best.Progress, n)
}

func adminWorklist(ctx context.Context, db *sql.DB, u User, mode string, scope *OrderQuery) (BusinessResult, error) {
	if u.Role != "admin" || !validWorklist(mode) {
		return BusinessResult{}, denied()
	}
	q := OrderQuery{Queue: "active"}
	if mode == "pre_review" {
		return queryBusinessOrdersForReview(ctx, db, u, scope)
	}
	if mode == "dispatch" {
		q.Status = "unassigned"
	}
	if mode == "recent_review" {
		q.Period = "recent"
	}
	b, err := queryOrderPage(ctx, db, u, q, time.Now().UTC(), 0, time.Time{})
	if err != nil {
		return b, err
	}
	b.Kind = "worklist"
	return b, populateWorklist(ctx, db, u, b.Items)
}

func populateWorklist(ctx context.Context, db *sql.DB, u User, items []BusinessOrder) error {
	for i := range items {
		o := &items[i]
		o.Priority = priorityClue(o.Description)
		o.Advice = reviewAdvice(*o)
	}
	for i := 0; i < len(items); i += worklistPageSize {
		page := items[i:min(i+worklistPageSize, len(items))]
		sort.SliceStable(page, func(i, j int) bool { return page[i].Priority != "" && page[j].Priority == "" })
	}
	candidates := map[string][]BusinessCandidate{}
	planned := map[int64]int{}
	for i := range items {
		o := &items[i]
		if o.Status != "unassigned" {
			continue
		}
		people, ok := candidates[o.Category]
		if !ok {
			var err error
			people, err = dispatchCandidates(ctx, db, u, o.Category)
			if err != nil {
				return err
			}
			candidates[o.Category] = people
		}
		o.Recommended, o.Advice = recommendCandidate(people, planned)
	}
	return nil
}

func (s *Server) readWorklistPage(ctx context.Context, u User, snapshot worklistSnapshot, page int) ([]BusinessOrder, int, int, bool, error) {
	rows := snapshot.Rows
	total := len(rows)
	if rows == nil {
		rows = snapshot.Items
		total = snapshot.Total
		if page > 1 && len(rows) < total {
			fresh, err := queryOrderPage(ctx, s.DB, u, snapshot.Query, snapshot.At, 0, snapshot.At)
			if err != nil {
				return nil, 0, 0, false, err
			}
			seen := make(map[int64]bool, len(rows))
			for _, row := range rows {
				seen[row.ID] = true
			}
			extra := []BusinessOrder{}
			for _, row := range fresh.Items {
				if !seen[row.ID] {
					row.Version = 0
					extra = append(extra, row)
				}
			}
			if err = populateWorklist(ctx, s.DB, u, extra); err != nil {
				return nil, 0, 0, false, err
			}
			rows = append(rows, extra...)
			total = len(rows)
		}
	}
	page = min(page, max(1, (total+worklistPageSize-1)/worklistPageSize))
	start := (page - 1) * worklistPageSize
	visible := []BusinessOrder{}
	for _, row := range rows[start:min(start+worklistPageSize, len(rows))] {
		order, err := s.getOrder(ctx, u, row.ID)
		var problem *Problem
		if errors.As(err, &problem) && problem.Status == 404 {
			continue
		}
		if err != nil {
			return nil, 0, 0, false, err
		}
		row.CurrentStatus, row.CurrentVersion = order.Status, order.Version
		visible = append(visible, row)
	}
	return visible, total, page, snapshot.Rows == nil && page > 1, nil
}

func decodeWorklist(raw any, mode string, priorScope *OrderQuery) (*BusinessResult, string, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, "", err
	}
	var payload struct {
		Worklist BusinessResult `json:"worklist"`
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, "", err
	}
	b := payload.Worklist
	expected := OrderQuery{Queue: "active"}
	switch mode {
	case "pre_review":
		expected = pendingReviewScope(priorScope)
	case "dispatch":
		expected.Status = "unassigned"
	case "recent_review":
		expected.Period = "recent"
	case "review":
	default:
		return nil, "", errors.New("invalid worklist mode")
	}
	if b.Kind != "worklist" || b.Query != expected {
		return nil, "", errors.New("invalid worklist scope")
	}
	// Reuse the ordinary order snapshot validation with the appropriate count constraint.
	b.Kind = "orders"
	checked, err := decodeOrderSnapshot(b, mode == "pre_review" || len(b.Items) == b.Total)
	if err != nil {
		return nil, "", err
	}
	checked.Kind = "worklist"
	if mode == "pre_review" {
		for _, item := range checked.Items {
			if item.Status != "submitted" || item.Version < 1 {
				return nil, "", errors.New("invalid pending order")
			}
		}
		checked.Kind = "pre_review"
		return checked, fmt.Sprintf("%s，共 %d 单。", queryLabel("admin", checked.Query), checked.Total), nil
	}
	scope := "本组织未结工单"
	if mode == "dispatch" {
		scope = "本组织待派工单"
	}
	if mode == "recent_review" {
		scope = "本组织近七天创建的未结工单"
	}
	for _, o := range checked.Items {
		if o.Advice == "" || finished(o.Status) || mode == "dispatch" && o.Status != "unassigned" {
			return nil, "", errors.New("invalid worklist item")
		}
	}
	if len(checked.Items) > worklistPageSize {
		checked.worklistRows = checked.Items
		checked.Items = checked.Items[:worklistPageSize]
	}
	answer := fmt.Sprintf("%s共 %d 单。", scope, b.Total)
	if b.Total > 0 {
		answer += "下方可逐页查看，优先核实项排在每页前面。"
	}
	return checked, answer, nil
}
