package app

import (
	"strings"
	"testing"
)

func TestDraftContractAndGrounding(t *testing.T) {
	sources := []string{"宿舍灯坏了，帮我报修", "三号楼201", "灯完全不亮"}
	good := `{"category":"lighting","location":"三号楼201","description":"灯完全不亮"}`
	if d, e := parseDraft(good, sources); e != nil || d.Location != "三号楼201" {
		t.Fatal("grounded fields rejected", e)
	}
	for _, raw := range []string{
		`null`, `[]`, `{"category":"lighting"} {}`, "```json\n" + good + "\n```",
		`{"category":"lighting","org_id":2}`, `{"category":"other"}`,
		`{"category":"lighting","location":"九号楼999"}`,
		`{"category":"lighting","description":"灯坏了需要换镇流器"}`,
		`{"location":"` + strings.Repeat("房", 201) + `"}`,
	} {
		if _, e := parseDraft(raw, sources); e == nil {
			t.Fatalf("invalid/untrusted draft accepted: %s", raw)
		}
	}
	if _, e := parseDraft(`{"category":"lighting","location":"","description":"宿舍灯坏了"}`, sources); e != nil {
		t.Fatal("partial draft rejected")
	}
}
func TestDraftIntentBoundary(t *testing.T) {
	for _, tt := range []struct {
		role, text   string
		active, want bool
	}{
		{"requester", "宿舍灯坏了，帮我报修", false, true},
		{"admin", "帮我报修", false, false}, {"technician", "帮我报修", true, false},
		{"requester", "三号楼201", true, true}, {"requester", "三号楼201", false, false},
		{"requester", "谢谢", true, false}, {"requester", "学校规定多久维修？", true, false},
	} {
		if draftIntent(tt.role, tt.text, tt.active) != tt.want {
			t.Fatalf("intent %v", tt)
		}
	}
	if !strings.Contains(draftReply(Input{Category: "lighting", Description: "灯坏了"}), "具体位置") {
		t.Fatal("missing location not requested")
	}
	if strings.Contains(draftReply(Input{Category: "lighting", Location: "三号楼201", Description: "灯坏了"}), "请补充") {
		t.Fatal("complete fields still requested")
	}
}

func TestGenericLocationNeedsFollowup(t *testing.T) {
	d, e := parseDraft(`{"category":"lighting","location":"宿舍","description":"灯坏了"}`, []string{"宿舍灯坏了"})
	if e != nil || d.Location != "" {
		t.Fatal("generic location treated as complete", e)
	}
}
