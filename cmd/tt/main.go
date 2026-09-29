package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/tt-dev/tt/content"
	"github.com/tt-dev/tt/internal/core"
	"github.com/tt-dev/tt/internal/shell"
	"github.com/tt-dev/tt/internal/store"
	"github.com/tt-dev/tt/internal/tracker"
	"github.com/tt-dev/tt/internal/tui"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "tt:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	command := "dashboard"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	if command == "beat" {
		rules, err := content.Rules()
		if err != nil { return nil }
		_ = runBeat(ctx, args, rules)
		return nil
	}
	rules, err := content.Rules()
	if err != nil { return err }
	dbPath, err := databasePath()
	if err != nil { return err }
	db, err := store.Open(dbPath)
	if err != nil { return err }
	defer db.Close()
	now := core.SystemClock{}.Now()
	if command != "doctor" {
		if _, err := tracker.New(db, core.SystemClock{}, rules).Beat(ctx, "cli", ""); err != nil { return err }
	}

	switch command {
	case "init":
		return runInit(ctx, db)
	case "status":
		return runStatus(ctx, db, now, rules)
	case "focus":
		return runFocus(ctx, db, args, now, rules)
	case "breakthrough":
		return runBreakthrough(ctx, db, now, rules)
	case "stats":
		return runStats(ctx, db, args, now, rules)
	case "doctor":
		return runDoctor(ctx, db, now)
	case "export":
		return runExport(ctx, db)
	case "dashboard":
		snapshot, err := db.Snapshot(ctx, now, rules)
		if err != nil { return err }
		return tui.RunDashboard(snapshot, rules)
	default:
		return fmt.Errorf("lệnh không xác định: %s\nDùng: tt [init|status|focus|breakthrough|stats|beat|doctor|export]", command)
	}
}

func databasePath() (string, error) {
	if path := os.Getenv("TT_DB"); path != "" { return path, nil }
	config, err := os.UserConfigDir()
	if err != nil { return "", err }
	return filepath.Join(config, "tt", "tt.db"), nil
}

func runBeat(ctx context.Context, args []string, rules core.Rules) error {
	set := flag.NewFlagSet("beat", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	source := set.String("source", "shell", "heartbeat source")
	language := set.String("language", "", "editor language identifier")
	if err := set.Parse(args); err != nil { return err }
	path, err := databasePath()
	if err != nil { return err }
	db, err := store.Open(path)
	if err != nil { return err }
	defer db.Close()
	_, err = tracker.New(db, core.SystemClock{}, rules).Beat(ctx, *source, *language)
	return err
}

func runInit(ctx context.Context, db *store.Store) error {
	title := "Đạo hữu"
	cutoff := "09:00"
	installHook := runtime.GOOS != "windows"
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Đạo hiệu của bạn").Value(&title),
		huh.NewInput().Title("Giờ mốc nhiệm vụ (HH:MM)").Value(&cutoff),
		huh.NewConfirm().Title("Cài hook heartbeat cho shell hiện tại?").Value(&installHook),
	))
	if err := form.Run(); err != nil { return err }
	if _, err := time.Parse("15:04", cutoff); err != nil { return fmt.Errorf("giờ mốc không hợp lệ: cần HH:MM") }
	if err := db.SetMeta(ctx, "dao_title", title); err != nil { return err }
	if err := db.SetCutoff(ctx, cutoff); err != nil { return err }
	if installHook && runtime.GOOS != "windows" {
		shellName := filepath.Base(os.Getenv("SHELL"))
		if shellName == "." || shellName == "" { shellName = "bash" }
		path, err := shell.Install(shellName)
		if err != nil { return err }
		fmt.Printf("Đã thêm hook vào %s. Mở terminal mới để áp dụng.\n", path)
	}
	if runtime.GOOS == "windows" { fmt.Println("Shell hook chưa hỗ trợ Windows; có thể dùng extension VS Code.") }
	fmt.Println("Cài extension: mở extensions/vscode trong repo và chạy npm install, npm run compile.")
	fmt.Println("Với VS Code Remote/SSH, cài extension và binary tt trên máy remote.")
	return nil
}

func runStatus(ctx context.Context, db *store.Store, now time.Time, rules core.Rules) error {
	snapshot, err := db.Snapshot(ctx, now, rules)
	if err != nil { return err }
	if snapshot.Realm.RealmIndex < len(rules.Realms) { fmt.Println(strings.TrimRight(rules.Realms[snapshot.Realm.RealmIndex].Art, "\n")) }
	fmt.Printf("%s · Tầng %d\n", snapshot.Realm.Realm, snapshot.Realm.Tier)
	fmt.Printf("Linh Khí %d / %d  %s\n", snapshot.Realm.XPInTier, snapshot.Realm.NextCost, bar(snapshot.Realm.XPInTier, snapshot.Realm.NextCost))
	fmt.Printf("Tổng: %d LK · Hôm nay: %d phút active\n", snapshot.TotalXP, snapshot.Today.Active)
	if snapshot.Realm.AtBrink { fmt.Println("Bình Cảnh: dùng tt breakthrough để thử đột phá.") }
	return nil
}

func bar(value, maximum int) string {
	if maximum <= 0 { return "[                    ]" }
	filled := value * 20 / maximum
	if filled > 20 { filled = 20 }
	return "[" + strings.Repeat("=", filled) + strings.Repeat("·", 20-filled) + "]"
}

func runFocus(ctx context.Context, db *store.Store, args []string, now time.Time, rules core.Rules) error {
	set := flag.NewFlagSet("focus", flag.ContinueOnError)
	minutes := set.Int("min", 25, "thời lượng Bế Quan")
	if err := set.Parse(args); err != nil { return err }
	if *minutes < 1 || *minutes > 240 { return errors.New("--min phải trong khoảng 1 đến 240") }
	session, err := db.StartFocus(ctx, now, *minutes*60)
	if err != nil { return err }
	completed, runErr := tui.RunFocus(time.Duration(*minutes) * time.Minute)
	end := core.SystemClock{}.Now()
	award, quest, finishErr := db.FinishFocus(ctx, session, end, completed, rules)
	if runErr != nil { return runErr }
	if finishErr != nil { return finishErr }
	if completed { fmt.Printf("Bế Quan hoàn thành. Nhận %d Linh Khí.\n", award); if quest { fmt.Println("Nhiệm vụ Thái Âm Thổ Nạp hoàn thành.") } } else { fmt.Println("Bế Quan đã kết thúc; không thưởng, không phạt.") }
	return db.RefreshTitles(ctx, end, rules)
}

func runBreakthrough(ctx context.Context, db *store.Store, now time.Time, rules core.Rules) error {
	total, err := db.TotalXP(ctx)
	if err != nil { return err }
	realmIndex, err := db.RealmIndex(ctx)
	if err != nil { return err }
	if !core.CanBreakthrough(total, realmIndex, rules) { return errors.New("chưa ở Bình Cảnh hoặc đã đạt đại cảnh giới cuối") }
	retryAfter, err := db.BreakthroughRetryAfter(ctx)
	if err != nil { return err }
	if now.Before(retryAfter) { return fmt.Errorf("cần chờ thêm %s trước lần thử tiếp theo", retryAfter.Sub(now).Round(time.Minute)) }
	minutes := rules.Realms[realmIndex+1].BreakthroughMinutes
	fmt.Printf("Đột phá %s: cần một phiên Bế Quan liền mạch %d phút.\n", rules.Realms[realmIndex+1].Name, minutes)
	session, err := db.StartFocus(ctx, now, minutes*60)
	if err != nil { return err }
	completed, runErr := tui.RunFocus(time.Duration(minutes) * time.Minute)
	end := core.SystemClock{}.Now()
	_, _, finishErr := db.FinishFocus(ctx, session, end, completed, rules)
	if runErr != nil { return runErr }
	if finishErr != nil { return finishErr }
	if !completed {
		if err := db.RecordFailedBreakthrough(ctx, end); err != nil { return err }
		return errors.New("đột phá chưa thành; có thể thử lại sau 1 giờ, không mất Linh Khí")
	}
	if err := db.AdvanceRealm(ctx, end, realmIndex+1); err != nil { return err }
	if err := db.RefreshTitles(ctx, end, rules); err != nil { return err }
	fmt.Printf("Đột phá thành công: %s!\n", rules.Realms[realmIndex+1].Name)
	return nil
}

func runStats(ctx context.Context, db *store.Store, args []string, now time.Time, rules core.Rules) error {
	set := flag.NewFlagSet("stats", flag.ContinueOnError)
	week := set.Bool("week", false, "hiển thị 7 ngày")
	if err := set.Parse(args); err != nil { return err }
	snapshot, err := db.Snapshot(ctx, now, rules)
	if err != nil { return err }
	if *week {
		for _, day := range snapshot.Week { fmt.Printf("%s  %-24s %3d phút  %d LK\n", day.Date, strings.Repeat("█", min(day.Active/15, 24)), day.Active, day.XP) }
	} else {
		fmt.Printf("Hôm nay %s: %d phút active, %d Linh Khí\n", snapshot.Today.Date, snapshot.Today.Active, snapshot.Today.XP)
	}
	return nil
}

func runDoctor(ctx context.Context, db *store.Store, now time.Time) error {
	report, err := db.Doctor(ctx, now)
	if err != nil { return err }
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err == nil { fmt.Println(string(encoded)) }
	return err
}

func runExport(ctx context.Context, db *store.Store) error {
	data, err := db.Export(ctx)
	if err != nil { return err }
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}
