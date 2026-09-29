package core

import (
	"math"
	"time"
)

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type RealmRule struct {
	Name                string  `yaml:"name"`
	BaseCost            float64 `yaml:"base_cost"`
	BreakthroughMinutes int     `yaml:"breakthrough_minutes"`
	Art                 string  `yaml:"art"`
}

type TitleRule struct {
	Key                       string `yaml:"key"`
	Name                      string `yaml:"name"`
	StreakDays                int    `yaml:"streak_days"`
	DailyActiveMinutes        int    `yaml:"daily_active_minutes"`
	ConsecutiveActiveMinutes  int    `yaml:"consecutive_active_minutes"`
	WeeklyFocusSessions       int    `yaml:"weekly_focus_sessions"`
}

type Rules struct {
	IdleTimeoutMinutes int         `yaml:"idle_timeout_minutes"`
	XPPerActiveMinute  int         `yaml:"xp_per_active_minute"`
	GrowthRate         float64     `yaml:"growth_rate"`
	FullRateMinutes    int         `yaml:"full_rate_minutes"`
	HalfRateMinutes    int         `yaml:"half_rate_minutes"`
	FocusBonus         int         `yaml:"focus_bonus"`
	QuestBonus         int         `yaml:"quest_bonus"`
	Realms             []RealmRule `yaml:"realms"`
	Titles             []TitleRule `yaml:"titles"`
}

type Progress struct {
	RealmIndex int
	Realm      string
	Tier       int
	XPInTier   int
	NextCost   int
	AtBrink    bool
	Complete   bool
}

func ActiveMinuteXP(activeMinutesToday, baseXP int, rules Rules) int {
	switch {
	case activeMinutesToday < rules.FullRateMinutes:
		return baseXP
	case activeMinutesToday < rules.FullRateMinutes+rules.HalfRateMinutes:
		return baseXP / 2
	default:
		return 0
	}
}

func TierCost(base float64, tier int, growthRate float64) int {
	if tier < 1 {
		return 0
	}
	return int(math.Round(base * math.Pow(growthRate, float64(tier-1))))
}

func RealmCost(rule RealmRule, growthRate float64) int {
	total := 0
	for tier := 1; tier <= 9; tier++ {
		total += TierCost(rule.BaseCost, tier, growthRate)
	}
	return total
}

func ProgressAt(totalXP int, realmIndex int, rules Rules) Progress {
	if len(rules.Realms) == 0 || realmIndex >= len(rules.Realms) {
		return Progress{RealmIndex: len(rules.Realms), Complete: true}
	}
	if realmIndex < 0 {
		realmIndex = 0
	}

	spent := 0
	for i := 0; i < realmIndex; i++ {
		spent += RealmCost(rules.Realms[i], rules.GrowthRate)
	}
	remaining := totalXP - spent
	if remaining < 0 {
		remaining = 0
	}

	progress := Progress{RealmIndex: realmIndex, Realm: rules.Realms[realmIndex].Name, Tier: 1}
	for tier := 1; tier <= 9; tier++ {
		cost := TierCost(rules.Realms[realmIndex].BaseCost, tier, rules.GrowthRate)
		if remaining < cost {
			progress.Tier = tier
			progress.XPInTier = remaining
			progress.NextCost = cost
			return progress
		}
		remaining -= cost
	}
	progress.Tier = 9
	progress.XPInTier = TierCost(rules.Realms[realmIndex].BaseCost, 9, rules.GrowthRate)
	progress.NextCost = progress.XPInTier
	progress.AtBrink = true
	return progress
}

func CanBreakthrough(totalXP int, realmIndex int, rules Rules) bool {
	progress := ProgressAt(totalXP, realmIndex, rules)
	return progress.AtBrink && realmIndex+1 < len(rules.Realms)
}

func MinuteBucket(t time.Time) int64 { return t.Unix() / 60 }

func LocalDayStart(t time.Time) time.Time {
	local := t.In(time.Local)
	year, month, day := local.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func StreakDays(activeByDate map[string]int, minMinutes int, today time.Time) int {
	streak := 0
	date := LocalDayStart(today)
	for {
		if activeByDate[date.Format("2006-01-02")] < minMinutes {
			break
		}
		streak++
		date = date.AddDate(0, 0, -1)
	}
	return streak
}