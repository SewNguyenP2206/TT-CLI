package tui

import (
	"fmt"
	"strings"
	"time"

	 tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tt-dev/tt/internal/core"
	"github.com/tt-dev/tt/internal/store"
)

var (
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("#C34E32")).Bold(true)
	muted  = lipgloss.NewStyle().Foreground(lipgloss.Color("#69706B"))
	good   = lipgloss.NewStyle().Foreground(lipgloss.Color("#27785D"))
)

type dashboardModel struct {
	snapshot store.Snapshot
	rules    core.Rules
	tab      int
}

func (m dashboardModel) Init() tea.Cmd { return nil }

func (m dashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok { return m, nil }
	switch key.String() {
	case "q", "ctrl+c": return m, tea.Quit
	case "left", "h": m.tab = (m.tab + 3) % 4
	case "right", "l", "tab": m.tab = (m.tab + 1) % 4
	case "1": m.tab = 0
	case "2": m.tab = 1
	case "3": m.tab = 2
	case "4": m.tab = 3
	}
	return m, nil
}

func (m dashboardModel) View() string {
	tabs := []string{"Tu vi", "Nhiệm vụ", "Đạo hiệu", "Thống kê"}
	var nav []string
	for i, tab := range tabs {
		label := fmt.Sprintf(" %d %s ", i+1, tab)
		if i == m.tab { nav = append(nav, accent.Render(label)) } else { nav = append(nav, muted.Render(label)) }
	}
	header := strings.Join(nav, "  ")
	var body string
	switch m.tab {
	case 0:
		daoname := m.snapshot.DaoTitle
		if daoname == "" { daoname = "TT" }
		body = fmt.Sprintf("%s\n\nCảnh giới: %s  ·  Tầng %d\nLinh Khí: %d / %d\nHôm nay: %d phút hoạt động\n\n%s",
			accent.Render(daoname), m.snapshot.Realm.Realm, m.snapshot.Realm.Tier,
			m.snapshot.Realm.XPInTier, m.snapshot.Realm.NextCost, m.snapshot.Today.Active,
			xpSword(m.snapshot.Realm.XPInTier, m.snapshot.Realm.NextCost))
		if m.snapshot.Realm.AtBrink { body += "\n\n" + accent.Render("BÌNH CẢNH · tt breakthrough") }
	case 1:
		status := "Chưa hoàn thành"
		if m.snapshot.QuestDone { status = good.Render("Đã hoàn thành · +300 Linh Khí") }
		body = fmt.Sprintf("%s\n\nThái Âm Thổ Nạp\nHoàn thành Bế Quan đầu tiên trước giờ mốc.\n%s", accent.Render("NHIỆM VỤ HÔM NAY"), status)
	case 2:
		body = accent.Render("ĐẠO HIỆU ĐÃ MỞ") + "\n\n"
		if len(m.snapshot.Titles) == 0 {
			body += muted.Render("Chưa có đạo hiệu")
		} else {
			names := make(map[string]string, len(m.rules.Titles))
			for _, title := range m.rules.Titles { names[title.Key] = title.Name }
			var unlocked []string
			for _, key := range m.snapshot.Titles { if name, ok := names[key]; ok { unlocked = append(unlocked, name) } else { unlocked = append(unlocked, key) } }
			body += strings.Join(unlocked, "\n")
		}
	case 3:
		body = accent.Render("THỐNG KÊ 7 NGÀY") + "\n\n"
		for _, day := range m.snapshot.Week {
			bar := strings.Repeat("▮", min(day.Active/15, 24))
			body += fmt.Sprintf("%s  %-8s %3d phút  %s\n", day.Date[5:], bar, day.Active, muted.Render(fmt.Sprintf("%d LK", day.XP)))
		}
	}
	return "\n" + header + "\n\n" + body + "\n\n" + muted.Render("←/→ hoặc 1–4 chuyển mục · q thoát") + "\n"
}

func xpSword(current, total int) string {
	if total <= 0 { return "[====================]" }
	filled := current * 20 / total
	if filled > 20 { filled = 20 }
	return "[" + strings.Repeat("=", filled) + strings.Repeat("·", 20-filled) + "]──➤"
}

func RunDashboard(snapshot store.Snapshot, rules core.Rules) error {
	_, err := tea.NewProgram(dashboardModel{snapshot: snapshot, rules: rules}, tea.WithAltScreen()).Run()
	return err
}

type focusTick time.Time

type focusModel struct {
	duration time.Duration
	start    time.Time
	remaining time.Duration
	done     bool
}

func tick() tea.Cmd { return tea.Tick(time.Second, func(now time.Time) tea.Msg { return focusTick(now) }) }

func (m focusModel) Init() tea.Cmd { return tick() }

func (m focusModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case tea.KeyMsg:
		if value.String() == "ctrl+c" || value.String() == "q" { return m, tea.Quit }
	case focusTick:
		m.remaining = m.duration - time.Since(m.start)
		if m.remaining <= 0 { m.remaining = 0; m.done = true; return m, tea.Quit }
		return m, tick()
	}
	return m, nil
}

func (m focusModel) View() string {
	minutes := int(m.remaining.Seconds()) / 60
	seconds := int(m.remaining.Seconds()) % 60
	phase := "BẾ QUAN"
	if m.done { phase = "TĨNH TÂM · HOÀN THÀNH" }
	breathFrames := []string{"    .    ", "  .   .  ", " (     ) ", "  .   .  "}
	frame := int(time.Since(m.start).Seconds()/4) % len(breathFrames)
	return "\n" + accent.Render(phase) + "\n\n" + breathFrames[frame] + "\n" + fmt.Sprintf("%02d:%02d", minutes, seconds) + "\n\n" + muted.Render("Thở đều · q để kết thúc sớm") + "\n"
}

func RunFocus(duration time.Duration) (bool, error) {
	model := focusModel{duration: duration, start: time.Now(), remaining: duration}
	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil { return false, err }
	result, ok := final.(focusModel)
	return ok && result.done, nil
}