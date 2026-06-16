package explore

import (
	"context"
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type Options struct {
	Limit int
}

type Store interface {
	List(context.Context) ([]memory.Record, error)
	Search(context.Context, memory.SearchRequest) ([]memory.SearchResult, error)
	Get(context.Context, string) (memory.Record, error)
}

func Run(ctx context.Context, store Store, opts Options) error {
	model, err := NewModel(ctx, store, opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(model).Run()
	return err
}

type item struct {
	Record memory.Record
	Score  float64
}

type Model struct {
	ctx      context.Context
	store    Store
	records  map[string]memory.Record
	items    []item
	input    textinput.Model
	selected int
	offset   int
	width    int
	height   int
	limit    int
	isDark   bool
	theme    theme
	err      error
}

func NewModel(ctx context.Context, store Store, opts Options) (Model, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "search memories by keyword…"
	input.CharLimit = 240
	input.Focus()
	model := Model{
		ctx:    ctx,
		store:  store,
		input:  input,
		width:  100,
		height: 30,
		limit:  opts.Limit,
	}
	model.applyTheme(true)
	if err := model.loadRecords(); err != nil {
		return Model{}, err
	}
	model.refresh()
	return model, nil
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, requestBackground)
}

func requestBackground() tea.Msg { return tea.RequestBackgroundColor() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.applyTheme(msg.IsDark())
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(max(20, msg.Width-8))
		m.ensureSelectionVisible()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "ctrl+p":
			m.moveSelection(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveSelection(1)
			return m, nil
		case "pgup":
			m.moveSelection(-m.listCapacity())
			return m, nil
		case "pgdown":
			m.moveSelection(m.listCapacity())
			return m, nil
		case "home":
			m.selected = 0
			m.ensureSelectionVisible()
			return m, nil
		case "end":
			if len(m.items) > 0 {
				m.selected = len(m.items) - 1
			}
			m.ensureSelectionVisible()
			return m, nil
		}
	}

	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.refresh()
	}
	return m, cmd
}

func (m Model) View() tea.View {
	if m.width <= 0 {
		m.width = 100
	}
	if m.height <= 0 {
		m.height = 30
	}
	t := m.theme

	header := lipgloss.NewStyle().Padding(0, 1).Render(
		t.headerAccent.Render("◆ ") + t.headerBrand.Render("agent-memoryd") + t.headerSub.Render("   memory explorer"),
	)
	search := lipgloss.NewStyle().Padding(0, 1).Render(
		t.searchIcon.Render("⌕  ") + m.input.View(),
	)
	rule := t.rule.Render(strings.Repeat("─", max(1, m.width)))
	status := m.statusBar()

	bodyHeight := max(4, m.height-lipgloss.Height(header)-lipgloss.Height(search)-lipgloss.Height(rule)-lipgloss.Height(status))
	body := m.bodyView(bodyHeight)

	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, search, rule, body, status))
	view.AltScreen = true
	return view
}

func (m *Model) applyTheme(isDark bool) {
	m.isDark = isDark
	m.theme = newTheme(isDark)
	st := textinput.DefaultStyles(isDark)
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(m.theme.faint)
	st.Focused.Text = lipgloss.NewStyle().Foreground(m.theme.fg)
	m.input.SetStyles(st)
}

func (m *Model) loadRecords() error {
	records, err := m.store.List(m.ctx)
	if err != nil {
		return fmt.Errorf("load memories: %w", err)
	}
	m.records = make(map[string]memory.Record, len(records))
	for _, record := range records {
		m.records[record.ID] = record
	}
	return nil
}

func (m *Model) refresh() {
	query := strings.TrimSpace(m.input.Value())
	m.err = nil
	if query == "" {
		m.items = recentItems(m.records, m.limit)
		m.selected = clamp(m.selected, 0, len(m.items)-1)
		m.ensureSelectionVisible()
		return
	}
	results, err := m.store.Search(m.ctx, memory.SearchRequest{Query: query, Limit: m.limit})
	if err != nil {
		m.err = err
		m.items = nil
		m.selected = 0
		m.offset = 0
		return
	}
	items := make([]item, 0, len(results))
	for _, result := range results {
		record, ok := m.records[result.ID]
		if !ok {
			record, err = m.store.Get(m.ctx, result.ID)
			if err != nil {
				m.err = err
				continue
			}
			m.records[result.ID] = record
		}
		items = append(items, item{Record: record, Score: result.Score})
	}
	m.items = items
	m.selected = clamp(m.selected, 0, len(m.items)-1)
	m.ensureSelectionVisible()
}

func (m Model) searching() bool { return strings.TrimSpace(m.input.Value()) != "" }

func recentItems(records map[string]memory.Record, limit int) []item {
	items := make([]item, 0, len(records))
	for _, record := range records {
		items = append(items, item{Record: record})
	}
	sort.Slice(items, func(i, j int) bool {
		left := mostRecent(items[i].Record)
		right := mostRecent(items[j].Record)
		if left.Equal(right) {
			return items[i].Record.ID < items[j].Record.ID
		}
		return left.After(right)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func mostRecent(record memory.Record) time.Time {
	if !record.UpdatedAt.IsZero() {
		return record.UpdatedAt
	}
	return record.CreatedAt
}

func (m *Model) moveSelection(delta int) {
	if len(m.items) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	m.selected = clamp(m.selected+delta, 0, len(m.items)-1)
	m.ensureSelectionVisible()
}

func (m *Model) ensureSelectionVisible() {
	if len(m.items) == 0 {
		m.selected = 0
		m.offset = 0
		return
	}
	height := m.listCapacity()
	if height <= 0 {
		height = 1
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+height {
		m.offset = m.selected - height + 1
	}
	maxOffset := max(0, len(m.items)-height)
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

// Layout math. The chrome around the body is the header, search line, rule, and
// status bar (4 lines); listCapacity must agree with what bodyView renders so
// scrolling stays in sync with the visible rows.

const chromeHeight = 4

func (m Model) narrow() bool { return m.width < 76 }

func (m Model) bodyHeight() int { return max(4, m.height-chromeHeight) }

func (m Model) listCapacity() int {
	bh := m.bodyHeight()
	if m.narrow() {
		return max(1, bh/2-2)
	}
	return max(1, bh-2)
}

// bodyView renders the list and detail panels. The lipgloss box model treats
// .Width()/.Height() as totals that include the rounded border (2 cols/rows) and
// horizontal padding (2 cols): usable text width = total-4, content rows =
// total-2. Panel totals are sized so list+gap+detail spans the full width and
// each panel fills the full body height, and content is rendered to those usable
// dimensions so nothing wraps inside the panel.
func (m Model) bodyView(height int) string {
	t := m.theme
	if m.narrow() {
		topTotal := height / 2
		botTotal := height - topTotal
		list := t.listPanel.Width(m.width).Height(topTotal).Render(m.listView(m.width-4, max(1, topTotal-2)))
		detail := t.detailPanel.Width(m.width).Height(botTotal).Render(m.detailView(m.width-4, max(1, botTotal-2)))
		return lipgloss.JoinVertical(lipgloss.Left, list, detail)
	}

	gap := 1
	listTotal := clamp(m.width/3, 32, 50)
	if listTotal > m.width-gap-32 {
		listTotal = max(24, m.width-gap-32)
	}
	detailTotal := m.width - gap - listTotal
	rows := max(1, height-2)

	list := t.listPanel.Width(listTotal).Height(height).Render(m.listView(listTotal-4, rows))
	detail := t.detailPanel.Width(detailTotal).Height(height).Render(m.detailView(detailTotal-4, rows))
	return lipgloss.JoinHorizontal(lipgloss.Top, list, " ", detail)
}

func (m Model) listView(width, height int) string {
	t := m.theme
	if m.err != nil {
		return t.errStyle.Render(wrap(m.err.Error(), width))
	}
	if len(m.items) == 0 {
		return t.faintStyle.Render("No memories yet")
	}
	searching := m.searching()
	end := min(len(m.items), m.offset+height)
	lines := make([]string, 0, max(0, end-m.offset))
	for idx := m.offset; idx < end; idx++ {
		lines = append(lines, m.renderRow(m.items[idx], idx == m.selected, searching, width))
	}
	return strings.Join(lines, "\n")
}

// renderRow lays out a single list row as: a left selection bar, a kind dot, the
// title (truncated to one line), and a right-aligned date or match score. When
// selected, every segment carries the selection background so the highlight is
// contiguous across the full row width.
func (m Model) renderRow(it item, selected, searching bool, width int) string {
	t := m.theme
	rec := it.Record

	right := relTime(mostRecent(rec))
	if searching {
		right = fmt.Sprintf("%d%%", int(it.Score*100+0.5))
	}
	rightW := lipgloss.Width(right)

	title := rec.Summary
	if title == "" {
		title = memory.Summarize(rec.Body, 160)
	}
	// gutter(1) + space(1) + dot(1) + space(1) + … + space(1) + right
	titleAvail := max(4, width-4-1-rightW)
	title = truncate(title, titleAvail)
	titleW := lipgloss.Width(title)
	pad := max(1, width-4-titleW-rightW)

	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(t.selBg)
		}
		return s
	}

	gutterChar := " "
	if selected {
		gutterChar = "▌"
	}
	titleStyle := lipgloss.NewStyle().Foreground(t.fg)
	if selected {
		titleStyle = lipgloss.NewStyle().Foreground(t.selFg).Bold(true)
	}

	var b strings.Builder
	b.WriteString(bg(lipgloss.NewStyle().Foreground(t.accent)).Render(gutterChar))
	b.WriteString(bg(lipgloss.NewStyle()).Render(" "))
	b.WriteString(bg(lipgloss.NewStyle().Foreground(t.kindColor(rec.Kind))).Render("●"))
	b.WriteString(bg(lipgloss.NewStyle()).Render(" "))
	b.WriteString(bg(titleStyle).Render(title))
	b.WriteString(bg(lipgloss.NewStyle()).Render(strings.Repeat(" ", pad)))
	b.WriteString(bg(lipgloss.NewStyle().Foreground(t.faint)).Render(right))
	return b.String()
}

func (m Model) detailView(width, height int) string {
	t := m.theme
	if m.err != nil {
		return t.errStyle.Render(wrap(m.err.Error(), width))
	}
	if len(m.items) == 0 {
		return t.faintStyle.Render("Select a memory to inspect")
	}
	it := m.items[m.selected]
	rec := it.Record

	title := rec.Summary
	if title == "" {
		title = memory.Summarize(rec.Body, 200)
	}

	lines := []string{t.detailTitle.Render(wrap(title, width))}

	meta := []string{t.chip(rec.Kind)}
	if rec.Project != "" {
		meta = append(meta, t.value.Render(rec.Project))
	}
	if ts := mostRecent(rec); !ts.IsZero() {
		meta = append(meta, t.faintStyle.Render(ts.Local().Format("Jan 2, 2006 · 3:04pm")))
	}
	if m.searching() {
		meta = append(meta, t.faintStyle.Render(fmt.Sprintf("%d%% match", int(it.Score*100+0.5))))
	}
	lines = append(lines, "", strings.Join(meta, "  "))

	lines = append(lines, t.field("id", rec.ID, width))
	if rec.Source != "" {
		lines = append(lines, t.field("source", rec.Source, width))
	}

	lines = append(lines, t.rule.Render(strings.Repeat("─", max(1, width))), "")
	lines = append(lines, t.value.Render(wrap(rec.Body, width)))

	return clampLines(strings.Join(lines, "\n"), height, t.faintStyle)
}

func (m Model) statusBar() string {
	t := m.theme
	mode := "RECENT"
	if m.searching() {
		mode = "SEARCH"
	}
	left := t.modeChip.Render(mode) + t.statusText.Render(fmt.Sprintf("  %d memories", len(m.items)))
	if len(m.items) > 0 {
		left += t.statusText.Render(fmt.Sprintf("  ·  %d/%d", m.selected+1, len(m.items)))
	}
	var keys string
	if m.width < 72 {
		keys = t.statusKey.Render("↑↓") + t.statusText.Render(" move   ") +
			t.statusKey.Render("esc") + t.statusText.Render(" quit")
	} else {
		keys = t.statusKey.Render("↑↓") + t.statusText.Render(" navigate   ") +
			t.statusKey.Render("pgup/pgdn") + t.statusText.Render(" page   ") +
			t.statusKey.Render("esc") + t.statusText.Render(" quit")
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(layoutLR(left, keys, max(10, m.width-2)))
}

// layoutLR places left and right strings on a single line, filling the gap so
// right hugs the far edge. Falls back to a single space when space is tight.
func layoutLR(left, right string, width int) string {
	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return t.Local().Format("Jan 2")
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Local().Format("Jan 2")
	}
}

func wrap(text string, width int) string {
	width = max(8, width)
	var out []string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimRight(rawLine, " \t")
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(line)
		current := ""
		for _, word := range words {
			if current == "" {
				current = word
				continue
			}
			if lipgloss.Width(current)+1+lipgloss.Width(word) > width {
				out = append(out, current)
				current = word
				continue
			}
			current += " " + word
		}
		if current != "" {
			out = append(out, current)
		}
	}
	return strings.Join(out, "\n")
}

func clampLines(text string, height int, more lipgloss.Style) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= height {
		return text
	}
	if height <= 1 {
		return more.Render("…")
	}
	hidden := len(lines) - (height - 1)
	lines = lines[:height-1]
	lines = append(lines, more.Render(fmt.Sprintf("…  %d more lines", hidden)))
	return strings.Join(lines, "\n")
}

func truncate(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 1 {
		return string([]rune(text)[:max(0, width)])
	}
	var out strings.Builder
	for _, r := range text {
		if lipgloss.Width(out.String()+string(r)+"…") > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "…"
}

func clamp(value, low, high int) int {
	if high < low {
		return 0
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// theme holds resolved colors and prebuilt styles for the current light/dark
// terminal background.
type theme struct {
	accent   color.Color
	fg       color.Color
	faint    color.Color
	selBg    color.Color
	selFg    color.Color
	chipText color.Color
	kinds    map[string]color.Color
	defKind  color.Color

	headerBrand  lipgloss.Style
	headerAccent lipgloss.Style
	headerSub    lipgloss.Style
	searchIcon   lipgloss.Style
	rule         lipgloss.Style
	faintStyle   lipgloss.Style
	errStyle     lipgloss.Style
	value        lipgloss.Style
	label        lipgloss.Style
	detailTitle  lipgloss.Style
	listPanel    lipgloss.Style
	detailPanel  lipgloss.Style
	statusKey    lipgloss.Style
	statusText   lipgloss.Style
	modeChip     lipgloss.Style
}

func newTheme(isDark bool) theme {
	ld := lipgloss.LightDark(isDark)
	accent := ld(lipgloss.Color("#7C3AED"), lipgloss.Color("#B79CFF"))
	fg := ld(lipgloss.Color("#1F2430"), lipgloss.Color("#E6E6F0"))
	muted := ld(lipgloss.Color("#5B6172"), lipgloss.Color("#9CA0B4"))
	faint := ld(lipgloss.Color("#9AA0AE"), lipgloss.Color("#6B7088"))
	border := ld(lipgloss.Color("#D7D9E0"), lipgloss.Color("#34384C"))
	selBg := ld(lipgloss.Color("#ECE6FF"), lipgloss.Color("#2D2A45"))
	selFg := ld(lipgloss.Color("#3D1D9E"), lipgloss.Color("#F4EFFF"))
	chipText := lipgloss.Color("#16121F")

	t := theme{
		accent:   accent,
		fg:       fg,
		faint:    faint,
		selBg:    selBg,
		selFg:    selFg,
		chipText: chipText,
		defKind:  lipgloss.Color("#9CA3AF"),
		kinds: map[string]color.Color{
			"user":      lipgloss.Color("#60A5FA"),
			"feedback":  lipgloss.Color("#FBBF24"),
			"project":   lipgloss.Color("#34D399"),
			"reference": lipgloss.Color("#22D3EE"),
			"note":      lipgloss.Color("#A78BFA"),
			"fact":      lipgloss.Color("#9CA3AF"),
		},
	}

	t.headerBrand = lipgloss.NewStyle().Bold(true).Foreground(fg)
	t.headerAccent = lipgloss.NewStyle().Foreground(accent)
	t.headerSub = lipgloss.NewStyle().Foreground(faint)
	t.searchIcon = lipgloss.NewStyle().Foreground(accent)
	t.rule = lipgloss.NewStyle().Foreground(border)
	t.faintStyle = lipgloss.NewStyle().Foreground(faint)
	t.errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171"))
	t.value = lipgloss.NewStyle().Foreground(fg)
	t.label = lipgloss.NewStyle().Foreground(faint)
	t.detailTitle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	t.listPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
	t.detailPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1)
	t.statusKey = lipgloss.NewStyle().Foreground(accent).Bold(true)
	t.statusText = lipgloss.NewStyle().Foreground(muted)
	t.modeChip = lipgloss.NewStyle().Background(accent).Foreground(chipText).Bold(true).Padding(0, 1)
	return t
}

func (t theme) kindColor(kind string) color.Color {
	if c, ok := t.kinds[strings.ToLower(strings.TrimSpace(kind))]; ok {
		return c
	}
	return t.defKind
}

func (t theme) chip(kind string) string {
	k := strings.TrimSpace(kind)
	if k == "" {
		k = "fact"
	}
	return lipgloss.NewStyle().
		Background(t.kindColor(k)).
		Foreground(t.chipText).
		Bold(true).
		Padding(0, 1).
		Render(strings.ToUpper(k))
}

func (t theme) field(label, value string, width int) string {
	const labelW = 7
	valWidth := max(8, width-labelW-1)
	return t.label.Width(labelW).Render(label) + " " + t.value.Render(truncate(value, valWidth))
}
