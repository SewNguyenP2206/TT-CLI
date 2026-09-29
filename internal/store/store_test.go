package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tt-dev/tt/internal/core"
)

func storeTestRules() core.Rules {
	return core.Rules{
		IdleTimeoutMinutes: 5,
		XPPerActiveMinute:  10,
		FullRateMinutes:    360,
		HalfRateMinutes:    120,
		FocusBonus:         150,
		QuestBonus:         300,
	}
}

func TestBeatDeduplicatesAndBackfillsWithinIdleWindow(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tt.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 29, 12, 0, 10, 0, time.Local)
	rules := storeTestRules()

	first, err := db.Beat(ctx, now, "vscode", "go", rules)
	if err != nil { t.Fatal(err) }
	if first.AddedMinutes != 1 || first.Earned != 10 { t.Fatalf("first beat = %+v", first) }

	duplicate, err := db.Beat(ctx, now.Add(20*time.Second), "shell", "", rules)
	if err != nil { t.Fatal(err) }
	if duplicate.AddedMinutes != 0 || duplicate.TotalXP != 10 { t.Fatalf("duplicate beat = %+v", duplicate) }

	backfilled, err := db.Beat(ctx, now.Add(2*time.Minute), "shell", "", rules)
	if err != nil { t.Fatal(err) }
	if backfilled.AddedMinutes != 2 || backfilled.Earned != 20 || backfilled.TotalXP != 30 {
		t.Fatalf("backfilled beat = %+v", backfilled)
	}
}

func TestCompletedFocusAwardsFocusAndDailyQuestOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tt.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx := context.Background()
	start := time.Date(2026, 9, 29, 8, 0, 0, 0, time.Local)
	rules := storeTestRules()
	session, err := db.StartFocus(ctx, start, 25*60)
	if err != nil { t.Fatal(err) }
	award, quest, err := db.FinishFocus(ctx, session, start.Add(25*time.Minute), true, rules)
	if err != nil { t.Fatal(err) }
	if award != 450 || !quest { t.Fatalf("focus award = %d, quest = %t", award, quest) }

	second, err := db.StartFocus(ctx, start.Add(time.Hour), 25*60)
	if err != nil { t.Fatal(err) }
	award, quest, err = db.FinishFocus(ctx, second, start.Add(85*time.Minute), true, rules)
	if err != nil { t.Fatal(err) }
	if award != 150 || quest { t.Fatalf("second focus award = %d, quest = %t", award, quest) }
}