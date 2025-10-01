package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type EventType int

const (
	EvtStart EventType = iota
	EvtDone
)

type Event struct {
	Type     EventType
	Name     string
	URL      string
	Instance int
	Status   string // pass|fail|no-baseline|error
	Error    string // optional
}

type Model struct {
	total int

	active map[string]activeItem // key: name
	logs   []logItem             // ring buffer of last N
	maxLog int

	counts struct {
		running   int
		passed    int
		failed    int
		nobase    int
		errored   int
		completed int
	}

	styles struct {
		header lipgloss.Style
		ok     lipgloss.Style
		fail   lipgloss.Style
		warn   lipgloss.Style
		dim    lipgloss.Style
		tag    lipgloss.Style
	}
}

type activeItem struct {
	name     string
	instance int
	start    time.Time
	url      string
}
type logItem struct {
	name     string
	instance int
	status   string
	err      string
	dur      time.Duration
}

func New(total int, maxLog int) Model {
	m := Model{
		total:  total,
		active: make(map[string]activeItem),
		maxLog: maxLog,
	}
	m.styles.header = lipgloss.NewStyle().Bold(true)
	m.styles.ok = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	m.styles.fail = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	m.styles.warn = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	m.styles.dim = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	m.styles.tag = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Faint(true)
	return m
}

type tickMsg time.Time
type eventMsg Event

func (m Model) Init() tea.Cmd {
	return tea.Tick(time.Second/6, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Tick(time.Second/6, func(t time.Time) tea.Msg { return tickMsg(t) })

	case eventMsg:
		switch msg.Type {
		case EvtStart:
			if _, ok := m.active[msg.Name]; !ok {
				m.active[msg.Name] = activeItem{name: msg.Name, instance: msg.Instance, start: time.Now(), url: msg.URL}
				m.counts.running = len(m.active)
			}
		case EvtDone:
			if it, ok := m.active[msg.Name]; ok {
				delete(m.active, msg.Name)
				m.counts.running = len(m.active)
				li := logItem{
					name:     msg.Name,
					instance: it.instance,
					status:   msg.Status,
					err:      msg.Error,
					dur:      time.Since(it.start),
				}
				m.logs = append(m.logs, li)
				if len(m.logs) > m.maxLog {
					m.logs = m.logs[len(m.logs)-m.maxLog:]
				}
				m.counts.completed++
				switch msg.Status {
				case "pass":
					m.counts.passed++
				case "fail":
					m.counts.failed++
				case "no-baseline":
					m.counts.nobase++
				case "error":
					m.counts.errored++
				}
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	// Header
	fmt.Fprintf(&b, "%s  total:%d  running:%d  %s:%d  %s:%d  %s:%d  %s:%d\n",
		m.styles.header.Render("SNAPSHOTS"),
		m.total, m.counts.running,
		m.styles.ok.Render("pass"), m.counts.passed,
		m.styles.fail.Render("fail"), m.counts.failed,
		m.styles.warn.Render("no-base"), m.counts.nobase,
		m.styles.dim.Render("error"), m.counts.errored,
	)

	// Active (sorted by start time)
	if len(m.active) > 0 {
		b.WriteString("\nActive:\n")
		type row struct {
			name     string
			instance int
			age      time.Duration
		}
		rows := make([]row, 0, len(m.active))
		for _, v := range m.active {
			rows = append(rows, row{v.name, v.instance, time.Since(v.start)})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].age > rows[j].age })
		for _, r := range rows {
			line := fmt.Sprintf("  [%d] %-48s  %s",
				r.instance, truncate(r.name, 48), m.styles.dim.Render(r.age.Truncate(100*time.Millisecond).String()))
			fmt.Fprintln(&b, line)
		}
	}

	// Logs
	if len(m.logs) > 0 {
		b.WriteString("\nLast:\n")
		for i := len(m.logs) - 1; i >= 0; i-- {
			l := m.logs[i]
			status := l.status
			switch status {
			case "pass":
				status = m.styles.ok.Render("pass")
			case "fail":
				status = m.styles.fail.Render("fail")
			case "no-baseline":
				status = m.styles.warn.Render("no-base")
			case "error":
				status = m.styles.dim.Render("error")
			}
			errTxt := ""
			if l.err != "" {
				errTxt = " " + m.styles.dim.Render("("+truncate(l.err, 80)+")")
			}
			fmt.Fprintf(&b, "  [%d] %-48s  %6s  %s%s\n",
				l.instance, truncate(l.name, 48), l.dur.Truncate(10*time.Millisecond), status, errTxt)
			if (len(m.logs) - i) >= m.maxLog {
				break
			}
		}
	}

	// Footer
	done := fmt.Sprintf("%d/%d", m.counts.completed, m.total)
	b.WriteString("\n" + m.styles.tag.Render("press Ctrl+C to quit  •  done "+done) + "\n")
	return b.String()
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// --- Runner ---

// Run starts the TUI; returns a function to send events and a cancel to stop UI.
func Run(ctx context.Context, total int) (send func(Event), stop func()) {
	prog := tea.NewProgram(New(total, 12), tea.WithContext(ctx))
	events := make(chan Event, 1024)

	go func() {
		for {
			select {
			case <-ctx.Done():
				prog.Quit()
				return
			case ev := <-events:
				prog.Send(eventMsg(ev))
			}
		}
	}()

	go func() { _ = prog.Start() }()

	send = func(e Event) {
		select {
		case events <- e:
		default:
		}
	}
	stop = func() { prog.Quit() }
	return send, stop
}
