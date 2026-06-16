package explore

import (
	"context"
	"fmt"
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
	err      error
}

func NewModel(ctx context.Context, store Store, opts Options) (Model, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}
	input := textinput.New()
	input.Prompt = "Search: "
	input.Placeholder = "type to search memories"
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
	if err := model.loadRecords(); err != nil {
		return Model{}, err
	}
	model.refresh()
	return model, nil
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(max(20, msg.Width-10))
		m.ensureSelectionVisible()
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			m.moveSelection(-1)
			return m, nil
		case "down", "j":
			m.moveSelection(1)
			return m, nil
		case "pgup":
			m.moveSelection(-m.listHeight())
			return m, nil
		case "pgdown":
			m.moveSelection(m.listHeight())
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
	header := titleStyle.Render("agent-memoryd explore")
	search := searchStyle.Width(max(20, m.width-2)).Render(m.input.View())
	status := m.statusLine()
	bodyHeight := max(6, m.height-lipgloss.Height(header)-lipgloss.Height(search)-lipgloss.Height(status)-2)
	body := m.bodyView(bodyHeight)
	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, search, body, status))
	view.AltScreen = true
	return view
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
	height := m.listHeight()
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

func (m Model) listHeight() int {
	return max(1, m.height-6)
}

func (m Model) bodyView(height int) string {
	if m.width < 72 {
		list := listStyle.Width(max(20, m.width-2)).Height(height / 2).Render(m.listView(max(3, height/2)))
		detail := detailStyle.Width(max(20, m.width-2)).Height(max(3, height-height/2)).Render(m.detailView(max(3, height-height/2), max(20, m.width-6)))
		return lipgloss.JoinVertical(lipgloss.Left, list, detail)
	}
	listWidth := clamp(m.width/3, 28, 48)
	detailWidth := max(30, m.width-listWidth-5)
	list := listStyle.Width(listWidth).Height(height).Render(m.listView(height))
	detail := detailStyle.Width(detailWidth).Height(height).Render(m.detailView(height, detailWidth-2))
	return lipgloss.JoinHorizontal(lipgloss.Top, list, detail)
}

func (m Model) listView(height int) string {
	if m.err != nil {
		return mutedStyle.Render(m.err.Error())
	}
	if len(m.items) == 0 {
		return mutedStyle.Render("No memories")
	}
	end := min(len(m.items), m.offset+height)
	lines := make([]string, 0, max(0, end-m.offset))
	for idx := m.offset; idx < end; idx++ {
		record := m.items[idx].Record
		prefix := "  "
		style := rowStyle
		if idx == m.selected {
			prefix = "> "
			style = selectedRowStyle
		}
		title := record.Summary
		if title == "" {
			title = memory.Summarize(record.Body, 90)
		}
		line := prefix + truncate(title, max(12, m.listWidth()-len(prefix)))
		lines = append(lines, style.Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m Model) listWidth() int {
	if m.width < 72 {
		return max(20, m.width-4)
	}
	return clamp(m.width/3, 28, 48) - 2
}

func (m Model) detailView(height, width int) string {
	if len(m.items) == 0 || m.err != nil {
		return mutedStyle.Render("Select a memory to inspect")
	}
	record := m.items[m.selected].Record
	lines := []string{
		detailTitleStyle.Render(wrap(record.Summary, width)),
		mutedStyle.Render("id: " + record.ID),
		mutedStyle.Render(compactMeta(record)),
	}
	if record.Source != "" {
		lines = append(lines, mutedStyle.Render(wrap("source: "+record.Source, width)))
	}
	lines = append(lines, "", wrap(record.Body, width))
	text := strings.Join(lines, "\n")
	return clampLines(text, height)
}

func (m Model) statusLine() string {
	query := strings.TrimSpace(m.input.Value())
	mode := "recent"
	if query != "" {
		mode = "search"
	}
	count := fmt.Sprintf("%s: %d memories", mode, len(m.items))
	if len(m.items) > 0 {
		count = fmt.Sprintf("%s %d/%d", count, m.selected+1, len(m.items))
	}
	return statusStyle.Width(max(20, m.width-2)).Render(count + "  up/down navigate  esc quit")
}

func compactMeta(record memory.Record) string {
	parts := []string{record.Kind}
	if record.Project != "" {
		parts = append(parts, record.Project)
	}
	if t := mostRecent(record); !t.IsZero() {
		parts = append(parts, t.Local().Format("2006-01-02 15:04"))
	}
	return strings.Join(parts, "  ")
}

func wrap(text string, width int) string {
	width = max(8, width)
	var out []string
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
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

func clampLines(text string, height int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= height {
		return text
	}
	if height <= 1 {
		return "..."
	}
	lines = lines[:height-1]
	lines = append(lines, mutedStyle.Render("..."))
	return strings.Join(lines, "\n")
}

func truncate(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 1 {
		return text[:width]
	}
	var out strings.Builder
	for _, r := range text {
		if lipgloss.Width(out.String()+string(r)+"...") > width {
			break
		}
		out.WriteRune(r)
	}
	return out.String() + "..."
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

var (
	titleStyle       = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	searchStyle      = lipgloss.NewStyle().Padding(0, 1)
	listStyle        = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	detailStyle      = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1)
	rowStyle         = lipgloss.NewStyle()
	selectedRowStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle       = lipgloss.NewStyle().Faint(true)
	detailTitleStyle = lipgloss.NewStyle().Bold(true)
	statusStyle      = lipgloss.NewStyle().Faint(true).Padding(0, 1)
)
