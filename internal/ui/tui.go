package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/StevenACoffman/codetospec/internal/llm"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TUISink forwards pipeline events to a running Bubble Tea program.
type TUISink struct {
	Program *tea.Program
}

// Emit implements Sink; tea.Program.Send is safe for concurrent use.
func (s TUISink) Emit(event any) {
	s.Program.Send(event)
}

// Palette — a dark "work queue" look: near-black ground, cream accent, and
// amber/violet/green stage colors that read as a pipeline.
const (
	cream  = lipgloss.Color("#fbf0df")
	amber  = lipgloss.Color("#fbbf24")
	violet = lipgloss.Color("#a78bfa")
	green  = lipgloss.Color("#4ade80")
	red    = lipgloss.Color("#f87171")
	ink    = lipgloss.Color("#e5e7eb")
	gray   = lipgloss.Color("#9ca3af")
	dim    = lipgloss.Color("#6b7280")
	line   = lipgloss.Color("#374151")
	ground = lipgloss.Color("#0b0c10")
)

// Pipeline phases, as PhaseChanged names them.
const (
	phaseExtract    = "extract"
	phaseMap        = "map"
	phaseReduce     = "reduce"
	phaseBuild      = "build"
	phaseCrosscheck = "crosscheck"
	phaseRender     = "render"
)

// Layout and pacing of the dashboard.
const (
	tickInterval    = 500 * time.Millisecond
	recentChecks    = 6  // verdicts listed in the stream
	journalLines    = 3  // log lines kept under the panels
	domainRows      = 7  // domains listed in the bars panel
	domainNameWidth = 16 // column of domain names
	barMinWidth     = 10
	barMaxWidth     = 46
	barMargin       = 14 // width beside a bar: name gap, count and borders
	panelMargin     = 6  // a panel's border and padding
	journalMargin   = 8  // a journal line's bullet and margins
	verdictMargin   = 16 // a verdict row's glyph and margins
	verdictMinWidth = 20
	gutter          = 2 // spaces between left and right header parts
)

// domainHue cycles per-domain bar colors, like the per-crate bars.
func domainHue(i int) lipgloss.Color {
	hues := [...]lipgloss.Color{
		"#f472b6", "#fb923c", "#f87171", "#34d399", "#38bdf8",
		"#fbbf24", "#a78bfa", "#22d3ee", "#a3e635", "#fb7185",
	}
	return hues[i%len(hues)]
}

// styles are the dashboard's text styles, built once per model.
type styles struct {
	header, brand, right         lipgloss.Style
	caption, heroNum, heroDone   lipgloss.Style
	heroSub, clock               lipgloss.Style
	dim, gray, ok, okBold, amber lipgloss.Style
	fail, panel, footer, track   lipgloss.Style
}

func newStyles() styles {
	return styles{
		header:   lipgloss.NewStyle().Bold(true).Foreground(cream).Background(ground).Padding(0, 1),
		brand:    lipgloss.NewStyle().Foreground(amber).Bold(true),
		right:    lipgloss.NewStyle().Foreground(gray),
		caption:  lipgloss.NewStyle().Foreground(dim).Bold(true),
		heroNum:  lipgloss.NewStyle().Foreground(cream).Bold(true),
		heroDone: lipgloss.NewStyle().Foreground(green).Bold(true),
		heroSub:  lipgloss.NewStyle().Foreground(gray),
		clock:    lipgloss.NewStyle().Foreground(cream),
		dim:      lipgloss.NewStyle().Foreground(dim),
		gray:     lipgloss.NewStyle().Foreground(gray),
		ok:       lipgloss.NewStyle().Foreground(green),
		okBold:   lipgloss.NewStyle().Foreground(green).Bold(true),
		amber:    lipgloss.NewStyle().Foreground(amber),
		fail:     lipgloss.NewStyle().Foreground(red).Bold(true),
		panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(line).
			Padding(0, 1),
		footer: lipgloss.NewStyle().Foreground(gray).Background(ground).Padding(0, 1),
		track:  lipgloss.NewStyle().Foreground(line),
	}
}

// stage is one cell of the pipeline strip.
type stage struct{ key, label string }

// stages drives both the pipeline strip and the phase ticks.
func stages() []stage {
	return []stage{
		{phaseExtract, "extract"},
		{phaseMap, "map"},
		{phaseReduce, "reduce"},
		{phaseCrosscheck, "check"},
		{phaseRender, "render"},
	}
}

type tickMsg time.Time

func tickEvery() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type checkItem struct {
	id      string
	verdict string
}

// Model is the full-screen run dashboard.
type Model struct {
	src, out, model string
	workers         int
	cancel          func()

	width, height int
	spinner       spinner.Model
	started       time.Time
	elapsed       time.Duration
	pulse         bool

	phase       string
	filesWalked int
	lastFile    string
	factsTotal  int
	factsByKind map[string]int
	extractors  []ExtractorFinished
	chunks      int

	mapDone, mapTotal, mapFailed int
	mapRules                     int
	mapByDomain                  map[string]int
	lastChunk                    string
	mapUsage                     llm.Usage

	reduceDone, reduceTotal, reduceFailed int
	reduceRules                           int
	reduceByDomain                        map[string]int
	lastDomain                            string
	reduceUsage                           llm.Usage

	checkDone, checkTotal                                   int
	checkSupported, checkRepaired, checkPartial, checkOther int
	recentChecks                                            []checkItem
	checkUsage                                              llm.Usage

	journal  []string
	finished bool
	quitting bool
	result   RunFinished
	st       styles
}

// NewModel builds the dashboard. cancel is invoked when the user quits so
// the pipeline shuts down cleanly (state saved, resumable).
func NewModel(src, out, model string, workers int, cancel func()) *Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(amber)
	return &Model{
		st:  newStyles(),
		src: src, out: out, model: model, workers: workers,
		cancel:         cancel,
		spinner:        sp,
		started:        time.Now(),
		factsByKind:    map[string]int{},
		mapByDomain:    map[string]int{},
		reduceByDomain: map[string]int{},
		phase:          phaseExtract,
	}
}

// Result returns the final run event once the program has finished.
func (m *Model) Result() (RunFinished, bool) {
	return m.result, m.finished
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickEvery())
}

// Update implements tea.Model: terminal and timer messages here, pipeline
// progress in record.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		cmd := m.key(msg.String())
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if !m.finished {
			m.elapsed = time.Since(m.started).Round(time.Second)
		}
		m.pulse = !m.pulse
		return m, tickEvery()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case RunFinished:
		m.finished = true
		m.result = msg
		return m, tea.Quit
	default:
		m.record(msg)
	}
	return m, nil
}

// key handles a key press: the first quit request cancels the run (state is
// saved, resumable), the next one, or one after the run, closes.
func (m *Model) key(key string) tea.Cmd {
	if key != "q" && key != "esc" && key != "ctrl+c" {
		return nil
	}
	if m.quitting || m.finished {
		return tea.Quit
	}
	m.quitting = true
	m.appendJournal("stopping, saving state...")
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

// record folds one pipeline progress event into the dashboard.
func (m *Model) record(msg tea.Msg) {
	switch msg := msg.(type) {
	case RunStarted:
		m.src, m.out, m.model, m.workers = msg.Src, msg.Out, msg.Model, msg.Workers
	case FileExtracted:
		m.filesWalked++
		m.lastFile = msg.Path
	case ExtractorFinished:
		m.extractors = append(m.extractors, msg)
	case FactsMerged:
		m.factsTotal = msg.Total
		m.factsByKind = msg.ByKind
	case Chunked:
		m.chunks = msg.Chunks
	case PhaseChanged:
		m.phase = msg.Phase
	case MapUnit:
		m.recordMap(&msg)
	case ReduceUnit:
		m.recordReduce(&msg)
	case CrosscheckUnit:
		m.recordCheck(&msg)
	case LogLine:
		m.appendJournal(msg.Message)
	}
}

func (m *Model) recordMap(msg *MapUnit) {
	m.mapDone, m.mapTotal = msg.Done, msg.Total
	if msg.Failed {
		m.mapFailed++
	}
	m.mapRules += msg.Rules
	if msg.Domain != "" {
		m.mapByDomain[msg.Domain] += msg.Rules
	}
	m.lastChunk = fmt.Sprintf("%s:%s", msg.Path, msg.Lines)
	m.mapUsage.Add(msg.Usage)
}

func (m *Model) recordReduce(msg *ReduceUnit) {
	m.reduceDone, m.reduceTotal = msg.Done, msg.Total
	if msg.Failed {
		m.reduceFailed++
	}
	m.reduceRules += msg.Rules
	if msg.Domain != "" {
		m.reduceByDomain[msg.Domain] += msg.Rules
	}
	m.lastDomain = msg.Domain
	m.reduceUsage.Add(msg.Usage)
}

func (m *Model) recordCheck(msg *CrosscheckUnit) {
	m.checkDone, m.checkTotal = msg.Done, msg.Total
	switch msg.Verdict {
	case "supported":
		m.checkSupported++
	case "repaired":
		m.checkRepaired++
	case "partial":
		m.checkPartial++
	default:
		m.checkOther++
	}
	m.recentChecks = append(m.recentChecks, checkItem{id: msg.RuleID, verdict: msg.Verdict})
	if len(m.recentChecks) > recentChecks {
		m.recentChecks = m.recentChecks[len(m.recentChecks)-recentChecks:]
	}
	m.checkUsage.Add(msg.Usage)
}

func (m *Model) appendJournal(line string) {
	m.journal = append(m.journal, line)
	if len(m.journal) > journalLines {
		m.journal = m.journal[len(m.journal)-journalLines:]
	}
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.width == 0 {
		return "starting..."
	}
	var b strings.Builder

	// Header
	left := m.st.brand.Render(
		"codetospec",
	) + m.st.right.Render(
		fmt.Sprintf("  %s → %s", m.src, m.out),
	)
	right := m.st.right.Render(fmt.Sprintf("%s · %d workers", m.model, m.workers))
	b.WriteString(m.headerBar(left, right) + "\n\n")

	// Hero metric + clock
	b.WriteString(m.hero() + "\n\n")

	// Pipeline stage strip
	b.WriteString(m.stageStrip() + "\n")

	// Per-domain bars — the "rules land per domain" panel.
	if bars := m.domainBars(); bars != "" {
		b.WriteString("\n" + bars + "\n")
	}

	// Verdict stream — rules flipping proven, like errors.txt → ✓.
	if len(m.recentChecks) > 0 {
		b.WriteString("\n" + m.verdictStream() + "\n")
	}

	// Journal
	if len(m.journal) > 0 {
		lines := make([]string, len(m.journal))
		for i, l := range m.journal {
			lines[i] = m.st.dim.Render("· " + truncateLine(l, m.width-journalMargin))
		}
		b.WriteString("\n" + strings.Join(lines, "\n") + "\n")
	}

	// Footer
	total := sumUsage(sumUsage(m.mapUsage, m.reduceUsage), m.checkUsage)
	footer := fmt.Sprintf("tokens  map %s · reduce %s · total %s      %s      %s",
		formatUsage(m.mapUsage), formatUsage(m.reduceUsage), formatUsage(total),
		m.elapsed, quitHint(m))
	b.WriteString("\n" + m.st.footer.Width(m.width).Render(footer))
	return b.String()
}

func quitHint(m *Model) string {
	if m.finished {
		return "done — [q] close"
	}
	if m.quitting {
		return "saving…"
	}
	return "[q] quit"
}

// hero renders the phase-appropriate headline metric, big and bright, with a
// live clock on the right — the "0 errors left" of the work-queue view.
func (m *Model) hero() string {
	var n int
	var caption, sub string
	numStyle := m.st.heroNum
	proven := m.checkSupported + m.checkRepaired
	toReview := m.checkPartial + m.checkOther

	switch {
	case m.checkTotal > 0:
		n, caption = toReview, "RULES TO REVIEW"
		sub = m.st.ok.Render(fmt.Sprintf("%d proven", proven)) +
			m.st.gray.Render(fmt.Sprintf("  ·  %d/%d checked", m.checkDone, m.checkTotal))
		if toReview == 0 && m.checkDone >= m.checkTotal && m.checkTotal > 0 {
			numStyle, caption = m.st.heroDone, "ALL RULES PROVEN"
		} else if toReview == 0 {
			numStyle = m.st.heroDone
		}
	case m.phase == phaseReduce || m.reduceRules > 0:
		n, caption = m.reduceRules, "RULES CONSOLIDATED"
		sub = m.st.gray.Render(fmt.Sprintf("%d/%d domains", m.reduceDone, m.reduceTotal))
	case m.phase == phaseMap || m.mapRules > 0:
		n, caption = m.mapRules, "CANDIDATE RULES"
		sub = m.st.gray.Render(fmt.Sprintf("%d/%d chunks mapped", m.mapDone, m.mapTotal))
	default:
		n, caption = m.factsTotal, "FACTS EXTRACTED"
		sub = m.st.gray.Render(fmt.Sprintf("%d files", m.filesWalked))
	}

	numTxt := numStyle.Render(bigNum(n))
	head := m.st.caption.Render(caption) + "   " + m.st.heroSub.Render(sub)
	left := head + "\n  " + numTxt

	clock := m.st.caption.Render(
		"ELAPSED",
	) + "\n" + m.st.clock.Render(
		fmt.Sprintf("%8s", m.elapsed),
	)
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(clock)-gutter, 1)
	rows := lipgloss.JoinHorizontal(lipgloss.Top, "  "+left, strings.Repeat(" ", gap), clock)
	return rows
}

// Number scales.
const (
	thousand   = 1_000
	million    = 1_000_000
	digitGroup = 3 // digits between thousands separators
)

// bigNum groups digits for readability (1,204).
func bigNum(n int) string {
	s := strconv.Itoa(n)
	if n < thousand {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%digitGroup == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// stageStrip renders the pipeline as colored cells that pulse on the active
// stage — extract ■ → map ■ → reduce ■ → check ▢ → render ▢.
func (m *Model) stageStrip() string {
	cur := phaseIndex(m.phase)
	var cells []string
	for _, stg := range stages() {
		ti := phaseIndex(stg.key)
		var sq, name lipgloss.Style
		var glyph string
		switch {
		case m.finished || cur > ti:
			glyph = "■"
			sq = lipgloss.NewStyle().Foreground(green)
			name = lipgloss.NewStyle().Foreground(ink)
		case cur == ti:
			glyph = "■"
			c := amber
			if !m.pulse {
				c = cream
			}
			sq = lipgloss.NewStyle().Foreground(c).Bold(true)
			name = lipgloss.NewStyle().Foreground(cream).Bold(true)
		default:
			glyph = "▢"
			sq = lipgloss.NewStyle().Foreground(dim)
			name = lipgloss.NewStyle().Foreground(dim)
		}
		cells = append(cells, sq.Render(glyph)+" "+name.Render(stg.label))
	}
	return "  " + strings.Join(cells, m.st.track.Render("  →  "))
}

// domainBars renders one horizontal bar per domain — the analog of the
// per-crate commit bars. Uses reduce counts once present, else map candidates.
func (m *Model) domainBars() string {
	src := m.reduceByDomain
	title := "RULES PER DOMAIN"
	if len(src) == 0 {
		src, title = m.mapByDomain, "CANDIDATES PER DOMAIN"
	}
	if len(src) == 0 {
		return ""
	}
	type dc struct {
		name string
		n    int
	}
	items := make([]dc, 0, len(src))
	maxN, total := 1, 0
	for k, v := range src {
		items = append(items, dc{k, v})
		total += v
		if v > maxN {
			maxN = v
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].n != items[j].n {
			return items[i].n > items[j].n
		}
		return items[i].name < items[j].name
	})

	nameW := domainNameWidth
	track := max(min(m.width-nameW-barMargin, barMaxWidth), barMinWidth)
	const rowsShown = domainRows
	var rows []string
	for i, it := range items {
		if i >= rowsShown {
			break
		}
		hue := domainHue(i)
		filled := max(it.n*track/maxN, 1)
		bar := lipgloss.NewStyle().Foreground(hue).Render(strings.Repeat("█", filled)) +
			m.st.track.Render(strings.Repeat("░", track-filled))
		name := m.st.gray.Render(fmt.Sprintf("%*s", nameW, truncateLine(it.name, nameW)))
		cnt := lipgloss.NewStyle().Foreground(ink).Render(fmt.Sprintf("%5s", bigNum(it.n)))
		rows = append(rows, fmt.Sprintf("%s  %s %s", name, bar, cnt))
	}
	head := m.st.caption.Render(title)
	if len(items) > rowsShown {
		head += m.st.gray.Render(
			fmt.Sprintf("   +%d more · %s total", len(items)-rowsShown, bigNum(total)),
		)
	} else {
		head += m.st.gray.Render(fmt.Sprintf("   %s total", bigNum(total)))
	}
	return m.st.panel.Width(m.width - panelMargin).Render(head + "\n" + strings.Join(rows, "\n"))
}

// verdictStream shows the latest reviewed rules flipping to a verdict, plus a
// colored tally — the errors.txt → ✓ column.
func (m *Model) verdictStream() string {
	var rows []string
	for _, c := range m.recentChecks {
		var glyph string
		var st lipgloss.Style
		switch c.verdict {
		case "supported", "repaired":
			glyph, st = "✓", m.st.ok
		case "partial":
			glyph, st = "≈", m.st.amber
		default:
			glyph, st = "✗", m.st.fail
		}
		id := truncateLine(c.id, max(m.width-verdictMargin, verdictMinWidth))
		rows = append(rows, st.Render(glyph)+" "+m.st.gray.Render(id))
	}
	tally := m.st.okBold.Render(fmt.Sprintf("%d proven", m.checkSupported+m.checkRepaired))
	if m.checkRepaired > 0 {
		tally += m.st.gray.Render(fmt.Sprintf(" (%d repaired)", m.checkRepaired))
	}
	if m.checkPartial > 0 {
		tally += m.st.amber.Render(fmt.Sprintf("  ·  %d partial", m.checkPartial))
	}
	if m.checkOther > 0 {
		tally += m.st.fail.Render(fmt.Sprintf("  ·  %d to review", m.checkOther))
	}
	head := m.st.caption.Render("LATEST VERDICTS") + "   " + tally
	return m.st.panel.Width(m.width - panelMargin).Render(head + "\n" + strings.Join(rows, "\n"))
}

func (m *Model) headerBar(left, right string) string {
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-gutter, 1)
	return m.st.header.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func phaseIndex(phase string) int {
	// map non-strip phases onto the strip: build folds into reduce's slot.
	if phase == phaseBuild {
		phase = phaseReduce
	}
	order := []string{phaseExtract, phaseMap, phaseReduce, phaseBuild, phaseCrosscheck, phaseRender}
	return max(slices.Index(order, phase), 0)
}

func formatTokens(n int) string {
	switch {
	case n >= million:
		return fmt.Sprintf("%.1fM", float64(n)/million)
	case n >= thousand:
		return fmt.Sprintf("%dk", n/thousand)
	default:
		return strconv.Itoa(n)
	}
}

func formatUsage(u llm.Usage) string {
	return formatTokens(u.PromptTokens) + "+" + formatTokens(u.CompletionTokens)
}

func sumUsage(a, b llm.Usage) llm.Usage {
	a.Add(b)
	return a
}

func truncateLine(s string, width int) string {
	if width <= 3 || len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}
