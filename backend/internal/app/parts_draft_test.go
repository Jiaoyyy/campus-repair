package app

import (
	"testing"
	"time"
)

func TestPartsResumeTime(t *testing.T) {
	now := time.Date(2026, 11, 20, 23, 50, 0, 0, businessTZ)
	for _, test := range []struct{ input, want string }{
		{"明天下午三点", "2026-11-21T15:00:00+08:00"},
		{"后天上午9点半", "2026-11-22T09:30:00+08:00"},
		{"2026-11-21 18:45", "2026-11-21T18:45:00+08:00"},
		{"2026年12月1日晚上八点", "2026-12-01T20:00:00+08:00"},
		{"今天下午三点", ""},
		{"这两天", ""},
		{"明天下午", ""},
		{"明天25:00", ""},
		{"2026-11-31 08:00", ""},
	} {
		actual, ok := partsResumeTime(test.input, now)
		if ok != (test.want != "") || ok && actual.Format(time.RFC3339) != test.want {
			t.Errorf("%q => %v (%v), want %q", test.input, actual, ok, test.want)
		}
	}
}
