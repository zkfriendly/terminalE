package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zkfriendly/zone/internal/store"
)

const (
	paneProjects = 0
	paneTasks    = 1
)

const (
	modeNormal = iota
	modeNewProject
	modeNewTask
	modeRenameProject
	modeRenameTask
)

// dashboard manages projects and tasks and is the launch point for focus sessions.
type dashboard struct {
	store  *store.Store
	styles Styles

	width, height int

	projects []store.Project
	tasks    []store.Task
	workSecs map[int64]int

	selProj int
	selTask int
	pane    int
	mode    int
	input   textinput.Model

	// Standalone (non-pomodoro) tracking.
	tracking     bool
	trackTaskID  int64
	trackEntryID int64
	trackStart   time.Time

	// Background focus session (run by the daemon), if any.
	hasActive  bool
	activeTask string
	activeProj string

	// Most recent session that was ended early and can be resumed, if any.
	canResume   bool
	resumeLabel string
	resumeFocus int

	now time.Time
	err error
}

func newDashboard(st *store.Store, s Styles) *dashboard {
	ti := textinput.New()
	ti.Prompt = "  "
	ti.CharLimit = 80
	d := &dashboard{
		store:    st,
		styles:   s,
		input:    ti,
		workSecs: map[int64]int{},
		now:      time.Now(),
	}
	d.reload()
	return d
}

func (d *dashboard) reload() {
	projects, err := d.store.ListProjects(false)
	if err != nil {
		d.err = err
		return
	}
	d.projects = projects
	if d.selProj >= len(projects) {
		d.selProj = max(0, len(projects)-1)
	}
	d.reloadTasks()
	if ws, err := d.store.TaskWorkSeconds(); err == nil {
		d.workSecs = ws
	}
	d.hasActive = false
	if sess, ok, err := d.store.ActiveSession(); err == nil && ok {
		d.hasActive = true
		d.activeTask = "general focus"
		d.activeProj = ""
		if sess.TaskID != nil {
			if task, err := d.store.GetTask(*sess.TaskID); err == nil {
				d.activeTask = task.Title
				d.activeProj = task.ProjectName
			}
		}
	}

	// Offer to resume the last session only if it was ended early (abandoned)
	// and there isn't already one running.
	d.canResume = false
	if !d.hasActive {
		if sess, ok, err := d.store.LastEndedSession(); err == nil && ok && sess.Status == store.SessionAbandoned {
			d.canResume = true
			d.resumeLabel = "general focus"
			if sess.TaskID != nil {
				if task, err := d.store.GetTask(*sess.TaskID); err == nil {
					d.resumeLabel = task.Title
					if task.ProjectName != "" {
						d.resumeLabel += " · " + task.ProjectName
					}
				}
			}
			if rt, err := d.store.LoadRuntime(sess.ID); err == nil {
				d.resumeFocus = rt.Accrued
			}
		}
	}
}

func (d *dashboard) reloadTasks() {
	if len(d.projects) == 0 {
		d.tasks = nil
		d.selTask = 0
		return
	}
	tasks, err := d.store.ListTasks(d.projects[d.selProj].ID, false)
	if err != nil {
		d.err = err
		return
	}
	d.tasks = tasks
	if d.selTask >= len(tasks) {
		d.selTask = max(0, len(tasks)-1)
	}
}

func (d *dashboard) currentProject() (store.Project, bool) {
	if d.selProj < len(d.projects) {
		return d.projects[d.selProj], true
	}
	return store.Project{}, false
}

func (d *dashboard) currentTask() (store.Task, bool) {
	if d.selTask < len(d.tasks) {
		return d.tasks[d.selTask], true
	}
	return store.Task{}, false
}

func (d *dashboard) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tickMsg:
		d.now = time.Time(msg)
		return nil
	case tea.KeyPressMsg:
		if d.mode != modeNormal {
			return d.updateInput(msg)
		}
		return d.updateNormal(msg)
	}
	return nil
}

func (d *dashboard) updateInput(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		d.mode = modeNormal
		d.input.Blur()
		d.input.Reset()
		return nil
	case "enter":
		val := strings.TrimSpace(d.input.Value())
		mode := d.mode
		d.mode = modeNormal
		d.input.Blur()
		d.input.Reset()
		if val == "" {
			return nil
		}
		return d.commit(mode, val)
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	return cmd
}

func (d *dashboard) commit(mode int, val string) tea.Cmd {
	switch mode {
	case modeNewProject:
		if _, err := d.store.CreateProject(val, ""); err != nil {
			d.err = err
		}
		d.reload()
		d.selProj = len(d.projects) - 1
		d.reloadTasks()
	case modeNewTask:
		if p, ok := d.currentProject(); ok {
			if _, err := d.store.CreateTask(p.ID, val); err != nil {
				d.err = err
			}
			d.reloadTasks()
			d.selTask = len(d.tasks) - 1
		}
	case modeRenameProject:
		if p, ok := d.currentProject(); ok {
			_ = d.store.RenameProject(p.ID, val)
			d.reload()
		}
	case modeRenameTask:
		if t, ok := d.currentTask(); ok {
			_ = d.store.RenameTask(t.ID, val)
			d.reloadTasks()
		}
	}
	return nil
}

func (d *dashboard) startInput(mode int, placeholder, initial string) tea.Cmd {
	d.mode = mode
	d.input.Placeholder = placeholder
	d.input.SetValue(initial)
	return d.input.Focus()
}

func (d *dashboard) updateNormal(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "q":
		return func() tea.Msg { return quitMsg{} }
	case "s":
		return func() tea.Msg { return gotoStatsMsg{} }
	case "tab", "left", "right", "h", "l":
		if d.pane == paneProjects {
			d.pane = paneTasks
		} else {
			d.pane = paneProjects
		}
	case "up", "k":
		d.move(-1)
	case "down", "j":
		d.move(1)
	case "n":
		if d.pane == paneProjects {
			return d.startInput(modeNewProject, "project name", "")
		}
		if _, ok := d.currentProject(); ok {
			return d.startInput(modeNewTask, "task title", "")
		}
	case "e":
		if d.pane == paneProjects {
			if p, ok := d.currentProject(); ok {
				return d.startInput(modeRenameProject, "project name", p.Name)
			}
		} else if t, ok := d.currentTask(); ok {
			return d.startInput(modeRenameTask, "task title", t.Title)
		}
	case "d":
		d.archiveSelected()
	case "t":
		if t, ok := d.currentTask(); ok {
			d.toggleTracking(t)
		}
	case "x":
		if t, ok := d.currentTask(); ok {
			d.toggleDone(t)
		}
	case "enter", "f", " ", "space":
		// Resume a running background session if one exists. If the cursor is on
		// a task, switch the running session over to it on resume.
		if d.hasActive {
			var task *store.Task
			if d.pane == paneTasks {
				if t, ok := d.currentTask(); ok {
					task = &t
				}
			}
			return func() tea.Msg { return resumeSessionMsg{task: task} }
		}
		if d.pane == paneProjects && msg.String() != "f" {
			d.pane = paneTasks
			return nil
		}
		// Start a general focus session, defaulting the current task to the one
		// under the cursor (if any). You can switch tasks during the session.
		var task *store.Task
		if t, ok := d.currentTask(); ok {
			task = &t
		}
		return func() tea.Msg { return startSessionMsg{task: task} }
	case "R":
		if d.canResume {
			return func() tea.Msg { return resumeLastMsg{} }
		}
	case "r":
		d.reload()
	}
	return nil
}

func (d *dashboard) move(delta int) {
	if d.pane == paneProjects {
		if len(d.projects) == 0 {
			return
		}
		d.selProj = clampInt(d.selProj+delta, 0, len(d.projects)-1)
		d.selTask = 0
		d.reloadTasks()
		return
	}
	if len(d.tasks) == 0 {
		return
	}
	d.selTask = clampInt(d.selTask+delta, 0, len(d.tasks)-1)
}

func (d *dashboard) archiveSelected() {
	if d.pane == paneProjects {
		if p, ok := d.currentProject(); ok {
			_ = d.store.SetProjectArchived(p.ID, true)
			d.reload()
		}
		return
	}
	if t, ok := d.currentTask(); ok {
		if d.tracking && d.trackTaskID == t.ID {
			d.stopTracking()
		}
		_ = d.store.SetTaskArchived(t.ID, true)
		d.reloadTasks()
	}
}

func (d *dashboard) toggleDone(t store.Task) {
	status := "done"
	if t.Status == "done" {
		status = "open"
	}
	_ = d.store.SetTaskStatus(t.ID, status)
	d.reloadTasks()
}

func (d *dashboard) toggleTracking(t store.Task) {
	if d.tracking && d.trackTaskID == t.ID {
		d.stopTracking()
		return
	}
	d.stopTracking()
	id, err := d.store.StartEntry(t.ID, nil, store.KindWork)
	if err != nil {
		d.err = err
		return
	}
	d.tracking = true
	d.trackTaskID = t.ID
	d.trackEntryID = id
	d.trackStart = time.Now()
}

// stopTracking closes any open standalone entry.
func (d *dashboard) stopTracking() {
	if !d.tracking {
		return
	}
	_ = d.store.EndEntry(d.trackEntryID)
	d.tracking = false
	d.trackTaskID = 0
	d.trackEntryID = 0
	if ws, err := d.store.TaskWorkSeconds(); err == nil {
		d.workSecs = ws
	}
}

func (d *dashboard) render(width, height int) string {
	d.width, d.height = width, height
	if width == 0 {
		return "loading..."
	}

	header := d.styles.Title.Render("zone") + "  " +
		d.styles.Dim.Render("local-first time tracking + focus")
	if d.hasActive {
		label := d.activeTask
		if d.activeProj != "" {
			label += " · " + d.activeProj
		}
		banner := d.styles.Work.Render("● focus session running") + " " +
			d.styles.Dim.Render(label) + "  " +
			d.styles.HelpKey.Render("enter") + " " + d.styles.Help.Render("resume")
		header += "\n" + banner
	} else if d.canResume {
		label := d.resumeLabel
		if d.resumeFocus > 0 {
			label += "  " + d.styles.Dim.Render("("+formatDur(d.resumeFocus)+" focus)")
		}
		banner := d.styles.Break.Render("↻ last session ended early") + " " +
			d.styles.Dim.Render(label) + "  " +
			d.styles.HelpKey.Render("R") + " " + d.styles.Help.Render("resume") +
			d.styles.Dim.Render("  ·  ") +
			d.styles.HelpKey.Render("enter") + " " + d.styles.Help.Render("start new")
		header += "\n" + banner
	}

	paneW := (width - 6) / 2
	if paneW < 18 {
		paneW = 18
	}

	// Render the footer first so the body can be sized around its actual height
	// (the help bar may wrap to several lines on narrow terminals).
	footer := d.renderFooter()
	footerH := lipgloss.Height(footer)
	headerH := lipgloss.Height(header)

	// header + blank(1) + body(pane height + 2 border rows) + footer.
	bodyH := height - headerH - 1 - footerH - 2
	if bodyH < 5 {
		bodyH = 5
	}

	projects := d.renderProjects(paneW, bodyH)
	tasks := d.renderTasks(paneW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, projects, " ", tasks)

	return lipgloss.JoinVertical(lipgloss.Left, header, "", body, footer)
}

func (d *dashboard) renderProjects(w, h int) string {
	title := d.styles.PaneTitle.Render("Projects")
	var lines []string
	if len(d.projects) == 0 {
		lines = append(lines, d.styles.Dim.Render("no projects yet"))
		lines = append(lines, d.styles.Dim.Render("press n to add one"))
	}
	for i, p := range d.projects {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color)).Render("●")
		label := fmt.Sprintf("%s %s", dot, p.Name)
		if i == d.selProj && d.pane == paneProjects {
			label = d.styles.ItemSel.Render(" " + p.Name + " ")
			label = dot + " " + label
		}
		lines = append(lines, label)
	}
	if d.mode == modeNewProject || d.mode == modeRenameProject {
		lines = append(lines, "", d.input.View())
	}
	content := title + "\n\n" + strings.Join(lines, "\n")
	style := d.styles.PaneInactive
	if d.pane == paneProjects {
		style = d.styles.PaneActive
	}
	return style.Width(w).Height(h).Render(content)
}

func (d *dashboard) renderTasks(w, h int) string {
	heading := "Tasks"
	if p, ok := d.currentProject(); ok {
		heading = "Tasks · " + p.Name
	}
	title := d.styles.PaneTitle.Render(heading)

	var lines []string
	if len(d.tasks) == 0 {
		lines = append(lines, d.styles.Dim.Render("no tasks here"))
		if _, ok := d.currentProject(); ok {
			lines = append(lines, d.styles.Dim.Render("press n to add one"))
		} else {
			lines = append(lines, d.styles.Dim.Render("create a project first"))
		}
	}
	for i, t := range d.tasks {
		lines = append(lines, d.renderTaskLine(i, t, w))
	}
	if d.mode == modeNewTask || d.mode == modeRenameTask {
		lines = append(lines, "", d.input.View())
	}
	content := title + "\n\n" + strings.Join(lines, "\n")
	style := d.styles.PaneInactive
	if d.pane == paneTasks {
		style = d.styles.PaneActive
	}
	return style.Width(w).Height(h).Render(content)
}

func (d *dashboard) renderTaskLine(i int, t store.Task, w int) string {
	check := "○"
	if t.Status == "done" {
		check = "✓"
	}

	total := d.workSecs[t.ID]
	live := 0
	tracking := d.tracking && d.trackTaskID == t.ID
	if tracking {
		live = int(d.now.Sub(d.trackStart).Seconds())
	}
	dur := formatDur(total + live)

	name := t.Title
	prefix := check + " "
	line := prefix + name

	right := d.styles.Dim.Render(dur)
	if tracking {
		right = d.styles.Work.Render("● REC " + formatClock(live))
	}

	if i == d.selTask && d.pane == paneTasks {
		line = d.styles.ItemSel.Render(" " + prefix + name + " ")
	} else if t.Status == "done" {
		line = d.styles.Dim.Render(line)
	} else {
		line = d.styles.Item.Render(line)
	}
	return line + "  " + right
}

func (d *dashboard) renderFooter() string {
	s := d.styles
	var hints []string
	if d.mode != modeNormal {
		hints = []string{
			s.helpEntry("enter", "save"),
			s.helpEntry("esc", "cancel"),
		}
	} else {
		hints = []string{
			s.helpEntry("↑↓", "move"),
			s.helpEntry("tab", "pane"),
			s.helpEntry("enter/f", "focus zone"),
			s.helpEntry("t", "track"),
			s.helpEntry("n", "new"),
			s.helpEntry("e", "rename"),
			s.helpEntry("x", "done"),
			s.helpEntry("d", "archive"),
			s.helpEntry("s", "stats"),
			s.helpEntry("q", "quit"),
		}
		if d.canResume {
			hints = append(hints, s.helpEntry("R", "resume last"))
		}
	}
	if d.tracking {
		hints = append([]string{d.styles.Work.Render("tracking active")}, hints...)
	}
	help := wrapHints(hints, d.styles.Dim.Render("  ·  "), d.width)
	if d.err != nil {
		help = d.styles.Accent.Foreground(colRed).Render("error: "+d.err.Error()) + "\n" + help
	}
	return "\n" + help
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
