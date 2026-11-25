package app

import (
	"strings"
	"testing"
)

func TestExpandedCategoryRouting(t *testing.T) {
	cases := map[string]string{
		"楼道照明故障": "lighting", "插座没电": "electrical", "水管漏水": "plumbing",
		"空调漏水": "hvac", "门锁打不开": "doors_windows", "课桌松动": "furniture",
		"WiFi 断网": "network", "墙面脱落": "building", "查询其他故障": "other",
	}
	for question, expected := range cases {
		if got := repairCategory(question); got != expected || !categoryOK(got) {
			t.Errorf("repairCategory(%q) = %q, want %q", question, got, expected)
		}
	}
	if categoryOK("all") || categoryOK("unsupported") || len(categoryCodes()) != len(cases) {
		t.Fatal("category list permits an invalid code or is incomplete")
	}
}

func TestAssistantCapabilityParity(t *testing.T) {
	note := assistantContextNote("requester", "有哪些报修类别", nil, assistantKnowledge{State: "empty"})
	for _, cat := range repairCategories {
		if !strings.Contains(note, cat.label) {
			t.Errorf("missing %s", cat.code)
		}
	}
	for _, input := range []string{"空调漏水有哪些工单", "上周校园网有几单"} {
		if hint := queryClarification("admin", input, false); hint != "" {
			t.Errorf("incorrect clarification: %s", hint)
		}
	}
	q, ok := businessIntent("admin", "上周校园网有几单", nil)
	if !ok || q.Period != "last_week" || q.Category != "network" {
		t.Fatalf("wrong filter: %+v", q)
	}
	if !knowledgeQuestion("门锁打不开") || !draftIntent("requester", "门锁打不开怎么办", false) {
		t.Error("new category routing missing")
	}
}
