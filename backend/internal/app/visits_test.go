package app

import (
	"testing"
	"time"
)

func TestVisitWindowAndStages(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	for _, tt := range []struct {
		name       string
		start, end *time.Time
		ok         bool
	}{
		{"minimum", at(time.Hour), at(90 * time.Minute), true}, {"maximum", at(time.Hour), at(5 * time.Hour), true}, {"horizon", at(14 * 24 * time.Hour), at(14*24*time.Hour + time.Hour), true},
		{"past", at(-time.Hour), at(time.Hour), false}, {"now", at(0), at(time.Hour), false}, {"too short", at(time.Hour), at(89 * time.Minute), false}, {"too long", at(time.Hour), at(6 * time.Hour), false}, {"too far", at(14*24*time.Hour + time.Second), at(14*24*time.Hour + time.Hour), false}, {"missing", nil, at(time.Hour), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if (validVisitWindow(tt.start, tt.end, now) == nil) != tt.ok {
				t.Fatal("window mismatch")
			}
		})
	}
	id := int64(10)
	o := Order{ID: 1, RequesterID: 20, AssigneeID: &id, Status: "in_progress", Version: 2}
	tech := User{ID: id, Role: "technician"}
	owner := User{ID: 20, Role: "requester"}
	v := VisitContext{Start: at(time.Hour), End: at(4 * time.Hour)}
	a := visitInput{Version: 2, Start: at(2 * time.Hour), End: at(3 * time.Hour)}
	if err := validateVisitAction(o, tech, v, "schedule", a, now); err != nil {
		t.Fatal(err)
	}
	v.Exception = "no_show"
	v.NeedsAvailability = true
	if containsAction(visitActions(o, tech, v, now), "schedule") {
		t.Fatal("no-show must obtain new availability")
	}
	v.NeedsAvailability = false
	if !containsAction(visitActions(o, tech, v, now), "schedule") {
		t.Fatal("new availability can be scheduled")
	}
	v.Visit = &Visit{ID: 1, Stage: "scheduled", End: now}
	if !containsAction(visitActions(o, tech, v, now), "depart") {
		t.Fatal("end equality allowed")
	}
	if containsAction(visitActions(o, tech, v, now.Add(time.Nanosecond)), "depart") {
		t.Fatal("past end refused")
	}
	v.Visit.Stage = "departed"
	if containsAction(visitActions(o, owner, v, now), "availability") {
		t.Fatal("cannot change while departed")
	}
	if !containsAction(visitActions(o, tech, v, now.Add(24*time.Hour)), "arrive") {
		t.Fatal("late arrival allowed")
	}
	v.Visit.Stage = "arrived"
	if !containsAction(visitActions(o, tech, v, now), "no-show") {
		t.Fatal("arrived can record absence")
	}
	v.Exception = "waiting_parts"
	actions := visitActions(o, tech, v, now)
	if len(actions) != 1 || actions[0] != "resume" {
		t.Fatal(actions)
	}
	a.Reason = "备件到货"
	if err := validateVisitAction(o, tech, v, "resume", a, now); err != nil {
		t.Fatal(err)
	}
	a.Version = 1
	if err := validateVisitAction(o, tech, v, "resume", a, now); err == nil {
		t.Fatal("stale accepted")
	}
	o.Status = "closed"
	if len(visitActions(o, tech, v, now)) != 0 {
		t.Fatal("terminal writable")
	}
}
func TestVisitMetricDates(t *testing.T) {
	now := time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC)
	start, end, err := visitMetricRange("", "", now)
	if err != nil || start.In(businessTZ).Format("2006-01-02") != "2026-10-17" || end.In(businessTZ).Format("2006-01-02") != "2026-10-24" {
		t.Fatal(start, end, err)
	}
	for _, tt := range []struct {
		a, b string
		ok   bool
	}{{"2024-01-01", "2024-12-31", true}, {"2024-01-01", "2025-01-01", false}, {"2026-10-23", "2026-10-22", false}, {"bad", "2026-10-23", false}, {"2026-10-23", "", false}} {
		_, _, err := visitMetricRange(tt.a, tt.b, now)
		if (err == nil) != tt.ok {
			t.Fatal(tt, err)
		}
	}
	if _, _, err = visitQueue("requester", "today", now); err == nil {
		t.Fatal("requester queue allowed")
	}
	if _, _, err = visitQueue("technician", "overdue", now); err == nil {
		t.Fatal("admin queue allowed")
	}
}
