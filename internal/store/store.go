package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"github.com/tt-dev/tt/internal/core"
)

type Store struct{ db *sql.DB }

type BeatResult struct {
	AddedMinutes int
	Earned       int
	TotalXP      int
}

type FocusSession struct {
	ID         int64
	Start      time.Time
	PlannedSec int
}

type DayStat struct {
	Date        string `json:"date"`
	Active      int    `json:"active_minutes"`
	XP          int    `json:"xp"`
}

type Snapshot struct {
	TotalXP  int
	Realm    core.Progress
	Today    DayStat
	Week     []DayStat
	Titles   []string
	QuestDone bool
	DaoTitle string
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS minutes (
  ts_minute INTEGER PRIMARY KEY,
  source TEXT NOT NULL,
  language TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS ledger (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('active','focus_bonus','quest','breakthrough')),
  delta INTEGER NOT NULL,
  ref TEXT NOT NULL,
  UNIQUE(kind, ref)
);
CREATE INDEX IF NOT EXISTS ledger_ts_idx ON ledger(ts);
CREATE TABLE IF NOT EXISTS focus_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  start INTEGER NOT NULL,
  planned_sec INTEGER NOT NULL,
	"end" INTEGER,
  completed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS quests (
  date TEXT NOT NULL,
  key TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(date, key)
);
CREATE TABLE IF NOT EXISTS titles (
  key TEXT PRIMARY KEY,
  unlocked_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT OR IGNORE INTO meta(key,value) VALUES ('schema_version','1'),('realm_index','0'),('accrue_cursor',''),('active_streak','0'),('cutoff','09:00'),('dao_title','');
`)
	return err
}

func (s *Store) Beat(ctx context.Context, now time.Time, source, language string, rules core.Rules) (BeatResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BeatResult{}, err
	}
	defer tx.Rollback()

	current := core.MinuteBucket(now)
	var previous sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(ts_minute) FROM minutes`).Scan(&previous); err != nil {
		return BeatResult{}, err
	}
	first := current
	if previous.Valid && current > previous.Int64 && current-previous.Int64 <= int64(rules.IdleTimeoutMinutes) {
		first = previous.Int64 + 1
	}
	var activeStreak int
	streakValue, streakErr := metaTx(ctx, tx, "active_streak")
	if streakErr == nil { activeStreak, _ = strconv.Atoi(streakValue) } else if !errors.Is(streakErr, sql.ErrNoRows) { return BeatResult{}, streakErr }
	lastActiveBucket := current - 1
	if previous.Valid { lastActiveBucket = previous.Int64 }

	activeByDate := map[string]int{}
	result := BeatResult{}
	for bucket := first; bucket <= current; bucket++ {
		insert, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO minutes(ts_minute,source,language) VALUES (?,?,?)`, bucket, source, language)
		if err != nil {
			return BeatResult{}, err
		}
		inserted, err := insert.RowsAffected()
		if err != nil {
			return BeatResult{}, err
		}
		if inserted == 0 {
			continue
		}
		if bucket == lastActiveBucket+1 { activeStreak++ } else { activeStreak = 1 }
		lastActiveBucket = bucket
		bucketTime := time.Unix(bucket*60, 0).In(time.Local)
		date := bucketTime.Format("2006-01-02")
		todayCount, exists := activeByDate[date]
		if !exists {
			dayStart := core.LocalDayStart(bucketTime)
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM minutes WHERE ts_minute >= ? AND ts_minute < ?`, core.MinuteBucket(dayStart), bucket).Scan(&todayCount); err != nil {
				return BeatResult{}, err
			}
			activeByDate[date] = todayCount
		}
		delta := core.ActiveMinuteXP(todayCount, rules.XPPerActiveMinute, rules)
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ledger(ts,kind,delta,ref) VALUES (?,?,?,?)`, bucket*60, "active", delta, strconv.FormatInt(bucket, 10))
		if err != nil {
			return BeatResult{}, err
		}
		result.AddedMinutes++
		result.Earned += delta
		activeByDate[date]++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('last_heartbeat',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.FormatInt(now.Unix(), 10)); err != nil { return BeatResult{}, err }
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('last_heartbeat_source',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, source); err != nil { return BeatResult{}, err }
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('accrue_cursor',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.FormatInt(current, 10)); err != nil { return BeatResult{}, err }
	if result.AddedMinutes > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('active_streak',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(activeStreak)); err != nil { return BeatResult{}, err }
	}
	if source == "vscode" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('last_vscode_heartbeat',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.FormatInt(now.Unix(), 10)); err != nil { return BeatResult{}, err }
	}
	if err := tx.Commit(); err != nil {
		return BeatResult{}, err
	}
	result.TotalXP, err = s.TotalXP(ctx)
	return result, err
}

func (s *Store) TotalXP(ctx context.Context) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM ledger`).Scan(&total)
	return total, err
}

func (s *Store) RealmIndex(ctx context.Context) (int, error) {
	value, err := s.Meta(ctx, "realm_index")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(value)
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	return value, err
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *Store) StartFocus(ctx context.Context, now time.Time, plannedSec int) (FocusSession, error) {
	if plannedSec < 60 {
		return FocusSession{}, errors.New("thời lượng Bế Quan phải ít nhất 1 phút")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO focus_sessions(start,planned_sec) VALUES(?,?)`, now.Unix(), plannedSec)
	if err != nil {
		return FocusSession{}, err
	}
	id, err := result.LastInsertId()
	return FocusSession{ID: id, Start: now, PlannedSec: plannedSec}, err
}

func (s *Store) FinishFocus(ctx context.Context, session FocusSession, now time.Time, completed bool, rules core.Rules) (int, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var existingEnd sql.NullInt64
	var existingCompleted int
	if err := tx.QueryRowContext(ctx, `SELECT "end",completed FROM focus_sessions WHERE id=?`, session.ID).Scan(&existingEnd, &existingCompleted); err != nil {
		return 0, false, err
	}
	if existingEnd.Valid {
		return 0, false, errors.New("phiên Bế Quan đã kết thúc")
	}
	if completed && now.Sub(session.Start) < time.Duration(session.PlannedSec)*time.Second {
		return 0, false, errors.New("phiên Bế Quan chưa đủ thời lượng")
	}
	completedInt := 0
	if completed {
		completedInt = 1
	}
	if _, err := tx.ExecContext(ctx, `UPDATE focus_sessions SET "end"=?,completed=? WHERE id=? AND "end" IS NULL`, now.Unix(), completedInt, session.ID); err != nil {
		return 0, false, err
	}
	if !completed {
		return 0, false, tx.Commit()
	}

	award := 0
	if session.PlannedSec >= 25*60 {
		award += rules.FocusBonus
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ledger(ts,kind,delta,ref) VALUES(?,?,?,?)`, now.Unix(), "focus_bonus", rules.FocusBonus, strconv.FormatInt(session.ID, 10))
		if err != nil {
			return 0, false, err
		}
	}
	date := now.In(time.Local).Format("2006-01-02")
	cutoff, err := metaTx(ctx, tx, "cutoff")
	if err != nil {
		return 0, false, err
	}
	deadline, parseErr := time.ParseInLocation("15:04", cutoff, time.Local)
	if parseErr != nil {
		deadline, _ = time.ParseInLocation("15:04", "09:00", time.Local)
	}
	localNow := now.In(time.Local)
	deadline = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), deadline.Hour(), deadline.Minute(), 0, 0, time.Local)
	questDone := false
	if localNow.Before(deadline) {
		quest, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO quests(date,key,done) VALUES(?,?,1)`, date, "thai_am_tho_nap")
		if err != nil {
			return 0, false, err
		}
		inserted, _ := quest.RowsAffected()
		if inserted > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO ledger(ts,kind,delta,ref) VALUES(?,?,?,?)`, now.Unix(), "quest", rules.QuestBonus, date+":thai_am_tho_nap")
			if err != nil {
				return 0, false, err
			}
			award += rules.QuestBonus
			questDone = true
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return award, questDone, nil
}

func metaTx(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	return value, err
}

func (s *Store) AdvanceRealm(ctx context.Context, now time.Time, nextIndex int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger(ts,kind,delta,ref) VALUES(?,?,0,?)`, now.Unix(), "breakthrough", strconv.Itoa(nextIndex)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('realm_index',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(nextIndex)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('breakthrough_failed_at','') ON CONFLICT(key) DO UPDATE SET value=''`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordFailedBreakthrough(ctx context.Context, now time.Time) error {
	return s.SetMeta(ctx, "breakthrough_failed_at", strconv.FormatInt(now.Unix(), 10))
}

func (s *Store) BreakthroughRetryAfter(ctx context.Context) (time.Time, error) {
	value, err := s.Meta(ctx, "breakthrough_failed_at")
	if errors.Is(err, sql.ErrNoRows) || value == "" { return time.Time{}, nil }
	if err != nil { return time.Time{}, err }
	timestamp, err := strconv.ParseInt(value, 10, 64)
	if err != nil { return time.Time{}, err }
	return time.Unix(timestamp, 0).Add(time.Hour), nil
}

func (s *Store) Snapshot(ctx context.Context, now time.Time, rules core.Rules) (Snapshot, error) {
	total, err := s.TotalXP(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	realmIndex, err := s.RealmIndex(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	today := core.LocalDayStart(now)
	result := Snapshot{TotalXP: total, Realm: core.ProgressAt(total, realmIndex, rules)}
	result.Today, err = s.dayStat(ctx, today)
	if err != nil {
		return Snapshot{}, err
	}
	for i := 6; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		stat, err := s.dayStat(ctx, day)
		if err != nil {
			return Snapshot{}, err
		}
		result.Week = append(result.Week, stat)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM titles ORDER BY unlocked_at`)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return Snapshot{}, err
		}
		result.Titles = append(result.Titles, key)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM quests WHERE date=? AND key='thai_am_tho_nap' AND done=1)`, today.Format("2006-01-02")).Scan(&result.QuestDone); err != nil {
		return Snapshot{}, err
	}
	result.DaoTitle, err = s.Meta(ctx, "dao_title")
	if err != nil && !errors.Is(err, sql.ErrNoRows) { return Snapshot{}, err }
	return result, rows.Err()
}

func (s *Store) dayStat(ctx context.Context, day time.Time) (DayStat, error) {
	start := core.MinuteBucket(day)
	end := core.MinuteBucket(day.AddDate(0, 0, 1))
	stat := DayStat{Date: day.Format("2006-01-02")}
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE((SELECT SUM(delta) FROM ledger WHERE ts >= ? AND ts < ?),0) FROM minutes WHERE ts_minute >= ? AND ts_minute < ?`, day.Unix(), day.AddDate(0, 0, 1).Unix(), start, end).Scan(&stat.Active, &stat.XP)
	return stat, err
}

func (s *Store) UnlockTitle(ctx context.Context, key string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO titles(key,unlocked_at) VALUES(?,?)`, key, now.Unix())
	return err
}

func (s *Store) RefreshTitles(ctx context.Context, now time.Time, rules core.Rules) error {
	byDate := map[string]int{}
	firstDay := core.LocalDayStart(now).AddDate(0, 0, -29)
	rows, err := s.db.QueryContext(ctx, `SELECT date(datetime(ts_minute*60,'unixepoch','localtime')) AS day, COUNT(*) FROM minutes WHERE ts_minute >= ? GROUP BY day`, core.MinuteBucket(firstDay))
	if err != nil {
		return err
	}
	for rows.Next() {
		var day string
		var minutes int
		if err := rows.Scan(&day, &minutes); err != nil {
			rows.Close()
			return err
		}
		byDate[day] = minutes
	}
	if err := rows.Close(); err != nil {
		return err
	}
	streakValue, err := s.Meta(ctx, "active_streak")
	if err != nil { return err }
	activeStreak, _ := strconv.Atoi(streakValue)
	local := now.In(time.Local)
	weekStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	for weekStart.Weekday() != time.Monday { weekStart = weekStart.AddDate(0, 0, -1) }
	for _, title := range rules.Titles {
		if title.StreakDays > 0 && title.DailyActiveMinutes > 0 && core.StreakDays(byDate, title.DailyActiveMinutes, now) >= title.StreakDays {
			if err := s.UnlockTitle(ctx, title.Key, now); err != nil { return err }
		}
		if title.ConsecutiveActiveMinutes > 0 && activeStreak >= title.ConsecutiveActiveMinutes {
			if err := s.UnlockTitle(ctx, title.Key, now); err != nil { return err }
		}
		if title.WeeklyFocusSessions > 0 {
			var focusCount int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM focus_sessions WHERE completed=1 AND "end" >= ?`, weekStart.Unix()).Scan(&focusCount); err != nil { return err }
			if focusCount >= title.WeeklyFocusSessions {
				if err := s.UnlockTitle(ctx, title.Key, now); err != nil { return err }
			}
		}
	}
	return nil
}

func (s *Store) Doctor(ctx context.Context, now time.Time) (map[string]any, error) {
	var last string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_heartbeat'`).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) { return nil, err }
	lastSeconds, _ := strconv.ParseInt(last, 10, 64)
	source, _ := s.Meta(ctx, "last_heartbeat_source")
	vscodeLast, _ := s.Meta(ctx, "last_vscode_heartbeat")
	vscodeSeconds, _ := strconv.ParseInt(vscodeLast, 10, 64)
	writeErr := s.db.PingContext(ctx)
	if writeErr == nil {
		var tx *sql.Tx
		tx, writeErr = s.db.BeginTx(ctx, nil)
		if writeErr == nil {
			_, writeErr = tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES('doctor_write_check','ok') ON CONFLICT(key) DO UPDATE SET value='ok'`)
			_ = tx.Rollback()
		}
	}
	return map[string]any{
		"database_writable": writeErr == nil,
		"database_error": func() string { if writeErr != nil { return writeErr.Error() }; return "" }(),
		"last_heartbeat": func() any { if lastSeconds == 0 { return nil }; return time.Unix(lastSeconds, 0).Format(time.RFC3339) }(),
		"last_source": source,
		"vscode_recent": vscodeSeconds > 0 && now.Sub(time.Unix(vscodeSeconds, 0)) <= 10*time.Minute,
		"last_vscode_heartbeat": func() any { if vscodeSeconds == 0 { return nil }; return time.Unix(vscodeSeconds, 0).Format(time.RFC3339) }(),
		"shell_hook_installed": shellHookPresent(),
	}, nil
}

func shellHookPresent() bool {
	home, err := os.UserHomeDir()
	if err != nil { return false }
	for _, name := range []string{".bashrc", ".zshrc", ".config/fish/config.fish"} {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err == nil && strings.Contains(string(data), "# tt heartbeat hook") { return true }
	}
	return false
}

func (s *Store) Export(ctx context.Context) ([]byte, error) {
	data := map[string]any{}
	queries := map[string]string{
		"minutes": `SELECT ts_minute,source,language FROM minutes ORDER BY ts_minute`,
		"ledger": `SELECT id,ts,kind,delta,ref FROM ledger ORDER BY id`,
		"focus_sessions": `SELECT id,start,planned_sec,"end",completed FROM focus_sessions ORDER BY id`,
		"quests": `SELECT date,key,done FROM quests ORDER BY date,key`,
		"titles": `SELECT key,unlocked_at FROM titles ORDER BY key`,
		"meta": `SELECT key,value FROM meta ORDER BY key`,
	}
	for table, query := range queries {
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil { return nil, err }
		columns, err := rows.Columns()
		if err != nil { rows.Close(); return nil, err }
		var records []map[string]any
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values { pointers[i] = &values[i] }
			if err := rows.Scan(pointers...); err != nil { rows.Close(); return nil, err }
			record := map[string]any{}
			for i, column := range columns {
				switch value := values[i].(type) {
				case []byte: record[column] = string(value)
				default: record[column] = value
				}
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil { rows.Close(); return nil, err }
		rows.Close()
		data[table] = records
	}
	return json.MarshalIndent(data, "", "  ")
}

func (s *Store) SetCutoff(ctx context.Context, cutoff string) error {
	if _, err := time.Parse("15:04", cutoff); err != nil { return fmt.Errorf("giờ mốc không hợp lệ: %w", err) }
	return s.SetMeta(ctx, "cutoff", cutoff)
}