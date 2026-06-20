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
	case "tab":
		return shellToggleFocusCmd()
	case "left", "h":
		d.pane = paneProjects
	case "right", "l":
		d.pane = paneTasks
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

func (d *dashboard) renderBody(width, height int) string {
	d.width, d.height = width, height
	if width == 0 {
		return "loading..."
	}

	projW := width / 4
	if projW < 16 {
		projW = 16
	}
	if projW > 28 {
		projW = 28
	}
	taskW := width - projW - 1
	if taskW < 20 {
		taskW = 20
	}

	projects := d.renderProjects(projW, height)
	tasks := d.renderTasks(taskW, height)
	body := lipgloss.JoinHorizontal(lipgloss.Top, projects, tasks)

	out := body
	if d.err != nil {
		out = d.styles.Accent.Foreground(colRed).Render("error: "+d.err.Error()) + "\n" + out
	}
	return out
}

func (d *dashboard) actionHints() []string {
	if d.mode != modeNormal {
		return []string{
			d.styles.helpEntry("enter", "save"),
			d.styles.helpEntry("esc", "cancel"),
		}
	}
	hints := []string{
		d.styles.helpEntry("↑↓", "move"),
		d.styles.helpEntry("←→", "column"),
		d.styles.helpEntry("enter/f", "focus"),
		d.styles.helpEntry("t", "track"),
		d.styles.helpEntry("n", "new"),
		d.styles.helpEntry("e", "rename"),
		d.styles.helpEntry("x", "done"),
		d.styles.helpEntry("d", "archive"),
	}
	if d.canResume {
		hints = append(hints, d.styles.helpEntry("R", "resume last"))
	}
	return hints
}

func (d *dashboard) infoHints() []string {
	s := d.styles
	var hints []string
	if d.hasActive {
		label := d.activeTask
		if d.activeProj != "" {
			label += " · " + d.activeProj
		}
		hints = append(hints, s.Work.Render("● focus session")+" "+s.Dim.Render(label))
	} else if d.canResume {
		label := d.resumeLabel
		if d.resumeFocus > 0 {
			label += " (" + formatDur(d.resumeFocus) + " focus)"
		}
		hints = append(hints, s.Break.Render("↻ resume available")+" "+s.Dim.Render(label))
	}
	if d.pane == paneProjects {
		if p, ok := d.currentProject(); ok {
			hints = append(hints, s.Dim.Render("project")+" "+s.StatValue.Render(p.Name)+
				s.Dim.Render(fmt.Sprintf(" · %d tasks", len(d.tasks))))
		}
	} else if t, ok := d.currentTask(); ok {
		line := s.Dim.Render("task") + " " + s.StatValue.Render(t.Title)
		if p, ok := d.currentProject(); ok {
			line += s.Dim.Render(" · ") + s.Subtitle.Render(p.Name)
		}
		total := d.workSecs[t.ID]
		if d.tracking && d.trackTaskID == t.ID {
			live := int(d.now.Sub(d.trackStart).Seconds())
			line += s.Dim.Render(" · ") + s.Work.Render("● REC "+formatClock(live))
		} else {
			line += s.Dim.Render(" · ") + s.Dim.Render(formatDur(total))
		}
		if t.Status == "done" {
			line += s.Dim.Render(" · done")
		}
		hints = append(hints, line)
	}
	return hints
}

func (d *dashboard) renderProjects(w, h int) string {
	title := d.renderColumnTitle("Projects", d.pane == paneProjects)
	var lines []string
	if len(d.projects) == 0 {
		lines = append(lines, d.styles.Dim.Render("no projects"))
		lines = append(lines, d.styles.Dim.Render("n to create"))
	}
	for i, p := range d.projects {
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color(p.Color)).Render("●")
		label := p.Name
		if i == d.selProj && d.pane == paneProjects {
			label = d.styles.ItemSel.Render(" "+p.Name+" ")
		} else if d.pane != paneProjects {
			label = d.styles.Dim.Render(label)
		} else {
			label = d.styles.Item.Render(label)
		}
		lines = append(lines, dot+" "+label)
	}
	if d.mode == modeNewProject || d.mode == modeRenameProject {
		lines = append(lines, "", d.input.View())
	}
	content := title + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1, 0, 0).Render(content)
}

func (d *dashboard) renderTasks(w, h int) string {
	heading := "Tasks"
	if p, ok := d.currentProject(); ok {
		heading = p.Name
	}
	title := d.renderColumnTitle(heading, d.pane == paneTasks)

	var lines []string
	if len(d.projects) == 0 {
		lines = append(lines, d.styles.Dim.Render("create a project first"))
	} else if len(d.tasks) == 0 {
		lines = append(lines, d.styles.Dim.Render("no tasks"))
		lines = append(lines, d.styles.Dim.Render("n to add"))
	}
	for i, t := range d.tasks {
		lines = append(lines, d.renderTaskLine(i, t, w))
	}
	if d.mode == modeNewTask || d.mode == modeRenameTask {
		lines = append(lines, "", d.input.View())
	}
	content := title + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1).Render(content)
}

func (d *dashboard) renderColumnTitle(label string, active bool) string {
	if active {
		return d.styles.PaneTitle.Render(label)
	}
	return d.styles.Dim.Render(label)
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

func (d *dashboard) escIsLocal() bool {
	return d.mode != modeNormal
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
