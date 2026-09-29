package core

import (
	"testing"
	"time"
)

func testRules() Rules {
	return Rules{
		FullRateMinutes: 360, HalfRateMinutes: 120, GrowthRate: 1.2,
		Realms: []RealmRule{{Name: "Luyện Khí", BaseCost: 600}, {Name: "Trúc Cơ", BaseCost: 3000}},
	}
}

func TestActiveMinuteXPCap(t *testing.T) {
	rules := testRules()
	for _, test := range []struct{ minutes, want int }{{0, 10}, {359, 10}, {360, 5}, {479, 5}, {480, 0}} {
		if got := ActiveMinuteXP(test.minutes, 10, rules); got != test.want {
			t.Errorf("ActiveMinuteXP(%d) = %d, want %d", test.minutes, got, test.want)
		}
	}
}

func TestTierCostAndBreakthroughGate(t *testing.T) {
	rules := testRules()
	if got := TierCost(600, 2, 1.2); got != 720 {
		t.Fatalf("TierCost = %d, want 720", got)
	}
	if CanBreakthrough(RealmCost(rules.Realms[0], rules.GrowthRate)-1, 0, rules) {
		t.Fatal("breakthrough opened before tier 9 was full")
	}
	if !CanBreakthrough(RealmCost(rules.Realms[0], rules.GrowthRate), 0, rules) {
		t.Fatal("breakthrough should open at tier 9")
	}
}

func TestStreakDaysUsesLocalCalendarDays(t *testing.T) {
	today := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	active := map[string]int{"2026-09-29": 30, "2026-09-28": 45, "2026-09-27": 29}
	if got := StreakDays(active, 30, today); got != 2 {
		t.Fatalf("StreakDays = %d, want 2", got)
	}
}