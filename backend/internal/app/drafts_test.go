package app

import (
	"context"
	"strings"
	"testing"
)

func TestDraftContractAndGrounding(t *testing.T) {
	sources := []string{"宿舍灯坏了，帮我报修", "三号楼201", "灯完全不亮", "昨晚发现"}
	good := `{"category":"lighting","location":"三号楼201","observations":["灯完全不亮","昨晚发现"],"advice":"","question":""}`
	if d, e := parseDraft(good, sources, ""); e != nil || d.Fields.Location != "三号楼201" || d.Fields.Description != "灯完全不亮；昨晚发现" {
		t.Fatal("grounded fields rejected", e)
	}
	for _, raw := range []string{
		`null`, `[]`, good + ` {}`, "```json\n" + good + "\n```",
		strings.ReplaceAll(good, "三号楼201", "九号楼999"),
		strings.ReplaceAll(good, "灯完全不亮", "灯坏了需要换镇流器"),
		strings.ReplaceAll(good, "灯完全不亮", "宿舍灯坏了，帮我报修"),
		strings.ReplaceAll(good, "lighting", "unsupported"),
		strings.ReplaceAll(good, `"question":""`, `"org_id":1`),
		strings.ReplaceAll(good, `"question":""`, `"question":null`),
		strings.ReplaceAll(good, `"location":`, `"advice":"", "location":`),
	} {
		if _, e := parseDraft(raw, sources, ""); e == nil {
			t.Fatalf("invalid/untrusted draft accepted: %s", raw)
		}
	}
}

func TestDraftIntentBoundary(t *testing.T) {
	for _, tt := range []struct {
		role, text   string
		active, want bool
	}{
		{"requester", "宿舍灯坏了，帮我报修", false, true},
		{"requester", "我们宿舍灯坏了，咋整", false, true},
		{"requester", "灯坏了可以自己处理吗？", false, false},
		{"admin", "帮我报修", false, false}, {"technician", "帮我报修", true, false},
		{"requester", "三号楼201", true, true}, {"requester", "三号楼201", false, false},
		{"requester", "谢谢", true, false}, {"requester", "学校规定多久维修？", true, false},
	} {
		if draftIntent(tt.role, tt.text, tt.active) != tt.want {
			t.Fatalf("intent %v", tt)
		}
	}
}

func TestGenericLocationNeedsFollowup(t *testing.T) {
	d, e := parseDraft(`{"category":"lighting","location":"宿舍","observations":["灯坏了"],"advice":"","question":"你在哪栋宿舍、哪个房间？"}`, []string{"宿舍灯坏了"}, "")
	if e != nil || d.Fields.Location != "" {
		t.Fatal("generic location treated as complete", e)
	}
	if got := draftFollowUp(d); got != d.Question {
		t.Fatalf("natural follow-up lost: %q", got)
	}
	d.Fields.Location = "三号楼201"
	if got := draftFollowUp(d); !strings.Contains(got, "闪烁") {
		t.Fatalf("vague symptom not clarified: %q", got)
	}
	d.Fields.Description = "灯完全不亮"
	if got := draftFollowUp(d); got != "" {
		t.Fatalf("known location requested again: %q", got)
	}
}

func TestDraftAdviceRejectsUnverifiedOrUnsafeGuidance(t *testing.T) {
	s := &Server{}
	for _, advice := range []string{"学校规定两小时内修好。[999]", "打开配电箱并接线。", "试着换灯泡。", "已提交报修。"} {
		got, cites := s.draftAdvice(context.Background(), User{}, 1, draftResponse{Fields: Input{Category: "lighting"}, Advice: advice}, nil, true)
		if got != genericDraftAdvice("lighting") || len(cites) != 0 {
			t.Fatalf("unsafe or unverified guidance published: %q", got)
		}
	}
}

func TestDraftKeepsSpecificFactsAndLatestCorrection(t *testing.T) {
	raw := `{"category":"lighting","location":"三号楼201","observations":["灯坏了","灯一直闪","刚刚发现灯一直闪","现在完全不亮了"],"advice":"","question":""}`
	correction := "不是闪烁，现在完全不亮了"
	sources := []string{"宿舍灯坏了，刚刚发现灯一直闪", "三号楼201", correction}
	initial, err := parseDraft(raw, sources, "")
	if err != nil || initial.Fields.Description != "刚刚发现灯一直闪；现在完全不亮了" {
		t.Fatalf("redundant fragments retained: %q, %v", initial.Fields.Description, err)
	}
	updated, err := parseDraft(raw, sources, draftSymptomCorrection(correction))
	if err != nil || updated.Fields.Description != "现在完全不亮了" || updated.Fields.Location != "三号楼201" {
		t.Fatalf("wrong corrected draft: %+v, %v", updated.Fields, err)
	}
}
