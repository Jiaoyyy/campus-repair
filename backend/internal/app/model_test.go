package app

import (
	"errors"
	"testing"
)

func TestStateTransitions(t *testing.T) {
	techID := int64(3)
	users := map[string]User{"requester": {ID: 1, OrgID: 1, Role: "requester"}, "admin": {ID: 2, OrgID: 1, Role: "admin"}, "technician": {ID: 3, OrgID: 1, Role: "technician"}}
	cases := []struct {
		state, action, role, next string
		input                     Action
	}{
		{"submitted", "review", "admin", "unassigned", Action{Decision: "approve"}},
		{"submitted", "review", "admin", "needs_info", Action{Decision: "request_info", Reason: "请补充位置"}},
		{"submitted", "review", "admin", "rejected", Action{Decision: "reject", Reason: "重复"}},
		{"needs_info", "resubmit", "requester", "submitted", Action{Input: Input{Category: "lighting", Location: "三号楼", Description: "灯不亮"}}},
		{"unassigned", "assign", "admin", "assigned", Action{TechnicianID: 3}},
		{"assigned", "reassign", "admin", "assigned", Action{TechnicianID: 4, Reason: "调整任务"}},
		{"assigned", "accept", "technician", "in_progress", Action{}},
		{"in_progress", "records", "technician", "in_progress", Action{Measures: "检查", Result: "等待材料"}},
		{"in_progress", "complete", "technician", "awaiting_confirmation", Action{Measures: "更换灯具", Result: "正常"}},
		{"awaiting_confirmation", "confirm", "requester", "closed", Action{}},
		{"awaiting_confirmation", "reopen", "requester", "unassigned", Action{Reason: "还未恢复"}},
		{"submitted", "cancel", "requester", "cancelled", Action{Reason: "重复提交"}},
		{"needs_info", "cancel", "requester", "cancelled", Action{Reason: "重复提交"}},
		{"unassigned", "cancel", "requester", "cancelled", Action{Reason: "重复提交"}},
		{"assigned", "cancel", "requester", "cancelled", Action{Reason: "重复提交"}},
	}
	for _, tc := range cases {
		t.Run(tc.state+"/"+tc.action, func(t *testing.T) {
			o := Order{OrgID: 1, RequesterID: 1, AssigneeID: &techID, Version: 2, Status: tc.state}
			a := tc.input
			a.Version = 2
			next, err := transition(o, users[tc.role], tc.action, &a)
			if err != nil || next != tc.next {
				t.Fatalf("got %s %v", next, err)
			}
		})
	}
}
func TestForbiddenAndConflict(t *testing.T) {
	id := int64(3)
	o := Order{OrgID: 1, RequesterID: 1, AssigneeID: &id, Version: 2, Status: "assigned"}
	cases := []struct {
		user   User
		action string
		input  Action
		status int
	}{
		{User{ID: 1, OrgID: 1, Role: "requester"}, "assign", Action{Version: 2}, 403},
		{User{ID: 4, OrgID: 1, Role: "technician"}, "accept", Action{Version: 2}, 404},
		{User{ID: 3, OrgID: 2, Role: "technician"}, "accept", Action{Version: 2}, 404},
		{User{ID: 3, OrgID: 1, Role: "technician"}, "accept", Action{Version: 1}, 409},
		{User{ID: 2, OrgID: 1, Role: "admin"}, "reassign", Action{Version: 2}, 400},
	}
	for _, tc := range cases {
		_, err := transition(o, tc.user, tc.action, &tc.input)
		var p *Problem
		if !errors.As(err, &p) || p.Status != tc.status {
			t.Fatalf("got %v want %d", err, tc.status)
		}
	}
	for _, state := range []string{"closed", "cancelled", "rejected", "in_progress", "awaiting_confirmation"} {
		o.Status = state
		a := Action{Version: 2, Reason: "cancel"}
		_, err := transition(o, User{ID: 1, OrgID: 1, Role: "requester"}, "cancel", &a)
		var p *Problem
		if !errors.As(err, &p) || p.Status != 409 {
			t.Fatalf("cancel allowed in %s", state)
		}
	}
}
func TestInputLimits(t *testing.T) {
	for _, i := range []Input{{Category: "bad", Location: "x", Description: "y"}, {Category: "lighting", Location: " ", Description: "y"}, {Category: "lighting", Location: "x", Description: " "}} {
		if i.normalize() == nil {
			t.Fatal("invalid input accepted")
		}
	}
	i := Input{Category: "lighting", Location: "  三号楼  ", Description: " 灯不亮 "}
	if err := i.normalize(); err != nil || i.Location != "三号楼" {
		t.Fatal("normalization failed")
	}
}
