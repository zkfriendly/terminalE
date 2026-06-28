package tui

import (
	"strings"
	"time"

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
	prompt  panelInput

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
	d := &dashboard{
		store:    st,
		styles:   s,
		prompt:   newPanelInput("  ", "", 80),
		workSecs: map[int64]int{},
		pane:     paneTasks,
		now:      time.Now(),
	}
	d.reload()
	return d
}

func (d *dashboard) reload() {
	tasks, err := d.store.ListTaskTree(false)
	if err != nil {
		d.err = err
		return
	}
	d.tasks = tasks
	if d.selTask >= len(tasks) {
		d.selTask = max(0, len(tasks)-1)
	}
	d.pane = paneTasks
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
	tasks, err := d.store.ListTaskTree(false)
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
		if d.prompt.IsFocused() {
			return d.updateInput(msg)
		}
		return d.updateNormal(msg)
	case tea.PasteMsg:
		if d.prompt.IsFocused() {
			return d.updateInput(msg)
		}
	}
	return nil
}

func (d *dashboard) updateInput(msg tea.Msg) tea.Cmd {
	cmd, act := d.prompt.handleMsg(msg)
	switch act {
	case panelInputCancel:
		d.mode = modeNormal
		d.prompt.Reset()
		return cmd
	case panelInputSubmit:
		val := strings.TrimSpace(d.prompt.Value())
		mode := d.mode
		d.mode = modeNormal
		d.prompt.Reset()
		if val == "" {
			return cmd
		}
		return tea.Batch(cmd, d.commit(mode, val))
	}
	return cmd
}

func (d *dashboard) commit(mode int, val string) tea.Cmd {
	var selectID int64
	switch mode {
	case modeNewProject:
		t, err := d.store.CreateRootTask(val)
		if err != nil {
			d.err = err
		} else {
			selectID = t.ID
		}
	case modeNewTask:
		if t, ok := d.currentTask(); ok {
			child, err := d.store.CreateChildTask(t.ID, val)
			if err != nil {
				d.err = err
			} else {
				selectID = child.ID
			}
		} else {
			t, err := d.store.CreateRootTask(val)
			if err != nil {
				d.err = err
			} else {
				selectID = t.ID
			}
		}
	case modeRenameProject:
		if t, ok := d.currentTask(); ok {
			_ = d.store.RenameTask(t.ID, val)
			selectID = t.ID
		}
	case modeRenameTask:
		if t, ok := d.currentTask(); ok {
			_ = d.store.RenameTask(t.ID, val)
			selectID = t.ID
		}
	}
	d.reload()
	if selectID != 0 {
		d.selectTask(selectID)
	}
	return nil
}

func (d *dashboard) startInput(mode int, placeholder, initial string) tea.Cmd {
	d.mode = mode
	d.prompt.SetPlaceholder(placeholder)
	d.prompt.SetValue(initial)
	return d.prompt.focusCmd()
}

func (d *dashboard) updateNormal(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "tab":
		return shellToggleFocusCmd()
	case "left", "h":
		d.moveToParent()
	case "right", "l":
		d.moveToFirstChild()
	case "up", "k":
		d.move(-1)
	case "down", "j":
		d.move(1)
	case "N":
		return d.startInput(modeNewProject, "top-level task", "")
	case "n":
		if t, ok := d.currentTask(); ok {
			return d.startInput(modeNewTask, "child of "+t.Title, "")
		}
		return d.startInput(modeNewProject, "top-level task", "")
	case "e":
		if t, ok := d.currentTask(); ok {
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
			if t, ok := d.currentTask(); ok {
				task = &t
			}
			return func() tea.Msg { return resumeSessionMsg{task: task} }
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
	t, ok := d.currentTask()
	if !ok {
		return
	}
	siblings := d.siblingTasks(t.ParentID)
	if len(siblings) == 0 {
		return
	}
	idx := 0
	for i, sibling := range siblings {
		if sibling.ID == t.ID {
			idx = i
			break
		}
	}
	idx = clampInt(idx+delta, 0, len(siblings)-1)
	d.selectTask(siblings[idx].ID)
}

func (d *dashboard) moveToParent() {
	t, ok := d.currentTask()
	if !ok || t.ParentID == nil {
		return
	}
	d.selectTask(*t.ParentID)
}

func (d *dashboard) moveToFirstChild() {
	t, ok := d.currentTask()
	if !ok {
		return
	}
	children := d.childTasks(t.ID)
	if len(children) == 0 {
		return
	}
	d.selectTask(children[0].ID)
}

func (d *dashboard) archiveSelected() {
	if t, ok := d.currentTask(); ok {
		if d.tracking && d.trackTaskID == t.ID {
			d.stopTracking()
		}
		_ = d.store.SetTaskArchived(t.ID, true)
		d.reload()
	}
}

func (d *dashboard) toggleDone(t store.Task) {
	status := "done"
	if t.Status == "done" {
		status = "open"
	}
	_ = d.store.SetTaskStatus(t.ID, status)
	d.reload()
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

	body := d.renderTasks(width, height)

	out := body
	if d.err != nil {
		out = d.styles.Accent.Foreground(colRed).Render("error: "+d.err.Error()) + "\n" + out
	}
	return out
}

func (d *dashboard) actionHints() []string {
	if d.prompt.IsFocused() {
		return []string{
			d.styles.helpEntry("enter", "save"),
			d.styles.helpEntry("esc", "cancel"),
		}
	}
	hints := []string{
		d.styles.helpEntry("↑↓", "move"),
		d.styles.helpEntry("←→", "level"),
		d.styles.helpEntry("enter/f", "focus"),
		d.styles.helpEntry("t", "track"),
		d.styles.helpEntry("n", "child"),
		d.styles.helpEntry("N", "top task"),
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
	if t, ok := d.currentTask(); ok {
		line := s.Dim.Render("task") + " " + s.StatValue.Render(t.Title)
		if t.ProjectName != "" {
			line += s.Dim.Render(" · ") + s.Subtitle.Render(t.ProjectName)
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
			label = d.styles.ItemSel.Render(" " + p.Name + " ")
		} else if d.pane != paneProjects {
			label = d.styles.Dim.Render(label)
		} else {
			label = d.styles.Item.Render(label)
		}
		lines = append(lines, dot+" "+label)
	}
	if d.mode == modeNewProject || d.mode == modeRenameProject {
		lines = append(lines, "", d.prompt.View())
	}
	content := title + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1, 0, 0).Render(content)
}

func (d *dashboard) renderTasks(w, h int) string {
	columns := d.taskColumns()
	if len(columns) == 0 {
		title := d.renderColumnTitle("Tasks", true)
		content := title + "\n" + strings.Join([]string{
			d.styles.Dim.Render("no tasks"),
			d.styles.Dim.Render("N to create a top-level task"),
		}, "\n")
		if d.mode == modeNewProject || d.mode == modeNewTask || d.mode == modeRenameProject || d.mode == modeRenameTask {
			content += "\n\n" + d.prompt.View()
		}
		return lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1).Render(content)
	}

	colW := w
	if len(columns) > 1 {
		colW = w / len(columns)
		if colW > 30 {
			colW = 30
		}
	}
	if colW < 18 {
		colW = 18
	}

	var rendered []string
	for i, col := range columns {
		rendered = append(rendered, d.renderTaskColumn(i, colW, h, col))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

type taskColumn struct {
	title string
	tasks []store.Task
	depth int
}

func (d *dashboard) taskColumns() []taskColumn {
	if len(d.tasks) == 0 {
		return nil
	}
	path := d.selectedPath()
	if len(path) == 0 {
		return []taskColumn{{title: "Tasks", tasks: d.siblingTasks(nil), depth: 0}}
	}

	var cols []taskColumn
	var parentID *int64
	for depth, task := range path {
		title := "Tasks"
		if depth > 0 {
			title = path[depth-1].Title
		}
		cols = append(cols, taskColumn{
			title: title,
			tasks: d.siblingTasks(parentID),
			depth: depth,
		})
		id := task.ID
		parentID = &id
	}

	if children := d.childTasks(path[len(path)-1].ID); len(children) > 0 {
		cols = append(cols, taskColumn{
			title: path[len(path)-1].Title,
			tasks: children,
			depth: len(path),
		})
	}
	return cols
}

func (d *dashboard) renderTaskColumn(idx, w, h int, col taskColumn) string {
	active := d.activeColumnDepth() == col.depth
	title := d.renderColumnTitle(col.title, active)

	var lines []string
	if len(col.tasks) == 0 {
		lines = append(lines, d.styles.Dim.Render("no tasks"))
		lines = append(lines, d.styles.Dim.Render("N to create a top-level task"))
	}
	for _, t := range col.tasks {
		lines = append(lines, d.renderTaskLine(t, w, active))
	}
	if active && (d.mode == modeNewProject || d.mode == modeNewTask || d.mode == modeRenameProject || d.mode == modeRenameTask) {
		lines = append(lines, "", d.prompt.View())
	}
	content := title + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().Width(w).Height(h).Padding(0, 1, 0, 0).Render(content)
}

func (d *dashboard) renderColumnTitle(label string, active bool) string {
	if active {
		return d.styles.PaneTitle.Render(label)
	}
	return d.styles.Dim.Render(label)
}

func (d *dashboard) renderTaskLine(t store.Task, w int, active bool) string {
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
	if t.HasChildren {
		name += " ›"
	}
	prefix := check + " "
	line := prefix + name

	right := d.styles.Dim.Render(dur)
	if tracking {
		right = d.styles.Work.Render("● REC " + formatClock(live))
	}

	if active && d.isSelected(t.ID) {
		line = d.styles.ItemSel.Render(" " + prefix + name + " ")
	} else if t.Status == "done" {
		line = d.styles.Dim.Render(line)
	} else {
		line = d.styles.Item.Render(line)
	}
	return line + "  " + right
}

func (d *dashboard) activeColumnDepth() int {
	if t, ok := d.currentTask(); ok {
		return t.Depth
	}
	return 0
}

func (d *dashboard) selectedPath() []store.Task {
	t, ok := d.currentTask()
	if !ok {
		return nil
	}
	byID := map[int64]store.Task{}
	for _, task := range d.tasks {
		byID[task.ID] = task
	}
	var reversed []store.Task
	for {
		reversed = append(reversed, t)
		if t.ParentID == nil {
			break
		}
		parent, ok := byID[*t.ParentID]
		if !ok {
			break
		}
		t = parent
	}
	path := make([]store.Task, len(reversed))
	for i := range reversed {
		path[len(reversed)-1-i] = reversed[i]
	}
	return path
}

func (d *dashboard) siblingTasks(parentID *int64) []store.Task {
	var out []store.Task
	for _, t := range d.tasks {
		if sameTaskParent(t.ParentID, parentID) {
			out = append(out, t)
		}
	}
	return out
}

func (d *dashboard) childTasks(parentID int64) []store.Task {
	var out []store.Task
	for _, t := range d.tasks {
		if t.ParentID != nil && *t.ParentID == parentID {
			out = append(out, t)
		}
	}
	return out
}

func (d *dashboard) selectTask(id int64) {
	for i, t := range d.tasks {
		if t.ID == id {
			d.selTask = i
			return
		}
	}
}

func (d *dashboard) isSelected(id int64) bool {
	t, ok := d.currentTask()
	return ok && t.ID == id
}

func sameTaskParent(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (d *dashboard) escIsLocal() bool {
	return d.prompt.IsFocused()
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
