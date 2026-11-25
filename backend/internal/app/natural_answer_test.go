package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNaturalAnswerUsesVerifiedValuesInFreeProse(t *testing.T) {
	values := map[string]answerValue{
		"total":              {Label: "当前未结", Value: "2"},
		"order.100.location": {Label: "工单位置", Value: "3号楼201"},
		"order.100.status":   {Label: "工单状态", Value: "待审核"},
	}
	for _, body := range []string{
		"这两单仍需要跟进。\n\n- {{order.100.location}}的一条记录状态为{{order.100.status}}。",
		"|范围|数量|\n|---|---|\n|当前未结|{{total}}单|",
		"查到2单。\n\n1. 先看{{order.100.location}}。",
	} {
		raw, _ := json.Marshal(map[string]string{"body": body})
		got, err := renderNaturalAnswer(raw, values)
		if err != nil || strings.Contains(got, "{{") {
			t.Fatalf("%q: %s, %v", body, got, err)
		}
	}
}

func TestNaturalAnswerRejectsUnverifiedOutput(t *testing.T) {
	values := map[string]answerValue{"total": {Label: "当前未结", Value: "2"}}
	for _, body := range []string{
		"查到{{absent}}单。", "查到999单。", "查到十二单。", "查到102单。", "查到4.2单。",
		"工单RP123456789012。", "4号楼201。", "打开https://example.com。",
		"已经派单。", "根因是老化。", "拆开配电箱。",
	} {
		raw, _ := json.Marshal(map[string]string{"body": body})
		if _, err := renderNaturalAnswer(raw, values); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestNaturalAnswerDatesAreEvidenceRatherThanOpeningText(t *testing.T) {
	at := time.Date(2026, time.November, 19, 4, 0, 0, 0, time.UTC)
	current := BusinessResult{At: at, Total: 2, TimeBasis: "工单日期不限", Query: OrderQuery{Queue: "active"}}
	values := naturalValues(&current)
	for _, key := range []string{"scope", "window_start", "window_end"} {
		if _, ok := values[key]; ok {
			t.Fatalf("unexpected insertable value %q for an undated count", key)
		}
	}
	got, err := renderNaturalAnswer(json.RawMessage(`{"body":"目前还有{{total}}单没处理完。"}`), values)
	if err != nil || got != "目前还有2单没处理完。" {
		t.Fatalf("current count answer = %q, %v", got, err)
	}

	current.Query = OrderQuery{Period: "this_week"}
	current.Additional = []BusinessResult{{At: at, Total: 5, Query: OrderQuery{Period: "last_week"}}}
	values = naturalValues(&current)
	got, err = renderNaturalAnswer(json.RawMessage(`{"body":"本周截至{{window_end}}新增{{total}}单，上周（{{result2.window_start}}到{{result2.window_end}}）新增{{result2.total}}单。"}`), values)
	if err != nil || got != "本周截至2026年11月19日新增2单，上周（2026年11月9日到2026年11月15日）新增5单。" {
		t.Fatalf("comparison answer = %q, %v", got, err)
	}
}
