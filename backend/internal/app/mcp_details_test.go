package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExtendedToolArguments(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{
		{MCPOrderDetail, `{"number":"RP202611170001","include_history":true,"limit":20}`, true},
		{MCPOrderDetail, `{"number":"RP202611170001","number":"RP202611170002"}`, false},
		{MCPOrderDetail, `{"number":"RP202611170001","org_id":2}`, false},
		{MCPOrderDetail, `{"number":"RP202611170001","limit":51}`, false},
		{MCPOrderDetail, `{"number":"RP202611170001","include_history":"true"}`, false},
		{MCPOrderDetail, `{"number":"RP202611170001","cursor":"a"}`, false},
		{MCPOrderVisit, `{"number":"RP202611170001"}`, true},
		{MCPOrderVisit, `{"number":"RP202611170001","user_id":1}`, false},
		{MCPOrderVisit, `{"number":null}`, false},
		{MCPOrderVisit, `{"number":"RP202611170001 OR 1=1"}`, false},
		{MCPKnowledge, `{"query":"安全提醒","limit":10}`, true},
		{MCPKnowledge, `{"query":"安全提醒","limit":11}`, false},
		{MCPKnowledge, `{"query":"安全提醒","url":"http://example.com"}`, false},
		{MCPKnowledge, `{"query":"安全提醒","query":"另一组织"}`, false},
		{MCPQueryOrders, `{"queue":"action","include_next_steps":true}`, true},
		{MCPQueryOrders, `{"include_next_steps":true,"include_next_steps":false}`, false},
		{MCPQueryOrders, `{"include_next_steps":null}`, false},
		{MCPQueryOrders, `{"include_next_steps":"true"}`, false},
	} {
		if got := validMCPArguments(json.RawMessage(tc.raw), tc.name); got != tc.valid {
			t.Errorf("%s args=%s got=%v", tc.name, tc.raw, got)
		}
	}
}

func TestOrderNextStepPermissions(t *testing.T) {
	now := time.Now()
	technicianID := int64(2)
	requester := User{ID: 1, Role: "requester"}
	technician := User{ID: 2, Role: "technician"}
	order := Order{ID: 7, RequesterID: 1, AssigneeID: &technicianID, Version: 3}
	for status := range businessStatusNames {
		order.Status = status
		for _, user := range []User{requester, technician, {ID: 3, Role: "admin"}} {
			next := orderNextStep(order, user, VisitContext{}, now)
			if next.EvidenceID != "order:7:v3" || next.Reason == "" {
				t.Fatal("missing fact provenance", status)
			}
			if finished(status) && len(next.Actions) > 0 {
				t.Fatal("terminal order offers actions", status)
			}
		}
	}
	order.Status = "in_progress"
	next := orderNextStep(order, requester, VisitContext{NeedsAvailability: true}, now)
	if next.ResponsibleRole != "requester" || !containsAction(next.Actions, "visit_availability") {
		t.Fatal("reschedule did not identify requester")
	}
	next = orderNextStep(order, technician, VisitContext{NeedsAvailability: true}, now)
	if containsAction(next.Actions, "visit_availability") {
		t.Fatal("technician offered requester action")
	}
	next = orderNextStep(order, technician, VisitContext{Exception: "waiting_parts"}, now)
	if !containsAction(next.Actions, "order_visit") {
		t.Fatal("parts recovery entry missing")
	}
}

func TestBuiltinSkillRoles(t *testing.T) {
	if len(skillDefinitions) != 3 || skillRoleAllowed("dispatch_advice", "requester") || skillRoleAllowed("repair_intake", "admin") || skillRoleAllowed("unknown", "admin") {
		t.Fatal("invalid builtin skill role map")
	}
	for _, definition := range skillDefinitions {
		if definition.DefinitionVersion < 1 || definition.Instruction == "" || definition.Output == "" {
			t.Fatal("incomplete skill contract")
		}
	}
}
