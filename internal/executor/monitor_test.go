package executor

import (
	"testing"
)

func TestNewMonitor(t *testing.T) {
	monitor := NewMonitor()

	if monitor == nil {
		t.Fatal("NewMonitor returned nil")
	}
	if monitor.tasks == nil {
		t.Error("tasks map not initialized")
	}
}

func TestMonitorRegister(t *testing.T) {
	monitor := NewMonitor()

	monitor.Register("task-1", "Test Task")

	state, ok := monitor.Get("task-1")
	if !ok {
		t.Fatal("Failed to get registered task")
	}
	if state.ID != "task-1" {
		t.Errorf("Expected ID 'task-1', got '%s'", state.ID)
	}
	if state.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got '%s'", state.Title)
	}
	if state.Status != StatusPending {
		t.Errorf("Expected status pending, got %s", state.Status)
	}
}

func TestMonitorStart(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task")

	monitor.Start("task-1")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusRunning {
		t.Errorf("Expected status running, got %s", state.Status)
	}
	if state.StartedAt == nil {
		t.Error("StartedAt not set")
	}
}

func TestMonitorUpdateProgress(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task")
	monitor.Start("task-1")

	monitor.UpdateProgress("task-1", "IMPL", 50, "Working...")

	state, _ := monitor.Get("task-1")
	if state.Phase != "IMPL" {
		t.Errorf("Expected phase 'IMPL', got '%s'", state.Phase)
	}
	if state.Progress != 50 {
		t.Errorf("Expected progress 50, got %d", state.Progress)
	}
	if state.Message != "Working..." {
		t.Errorf("Expected message 'Working...', got '%s'", state.Message)
	}
}

func TestMonitorComplete(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task")
	monitor.Start("task-1")

	monitor.Complete("task-1", "https://github.com/org/repo/pull/1")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusCompleted {
		t.Errorf("Expected status completed, got %s", state.Status)
	}
	if state.PRUrl != "https://github.com/org/repo/pull/1" {
		t.Errorf("Expected PR URL, got '%s'", state.PRUrl)
	}
	if state.CompletedAt == nil {
		t.Error("CompletedAt not set")
	}
}

func TestMonitorFail(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Test Task")
	monitor.Start("task-1")

	monitor.Fail("task-1", "Something went wrong")

	state, _ := monitor.Get("task-1")
	if state.Status != StatusFailed {
		t.Errorf("Expected status failed, got %s", state.Status)
	}
	if state.Error != "Something went wrong" {
		t.Errorf("Expected error message, got '%s'", state.Error)
	}
}

func TestMonitorGetAll(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1")
	monitor.Register("task-2", "Task 2")
	monitor.Register("task-3", "Task 3")

	all := monitor.GetAll()
	if len(all) != 3 {
		t.Errorf("Expected 3 tasks, got %d", len(all))
	}
}

func TestMonitorGetRunning(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1")
	monitor.Register("task-2", "Task 2")
	monitor.Start("task-1")

	running := monitor.GetRunning()
	if len(running) != 1 {
		t.Errorf("Expected 1 running task, got %d", len(running))
	}
	if running[0].ID != "task-1" {
		t.Errorf("Expected task-1, got %s", running[0].ID)
	}
}

func TestMonitorCount(t *testing.T) {
	monitor := NewMonitor()

	if monitor.Count() != 0 {
		t.Error("Expected count 0 for empty monitor")
	}

	monitor.Register("task-1", "Task 1")
	monitor.Register("task-2", "Task 2")

	if monitor.Count() != 2 {
		t.Errorf("Expected count 2, got %d", monitor.Count())
	}
}

func TestMonitorRemove(t *testing.T) {
	monitor := NewMonitor()
	monitor.Register("task-1", "Task 1")

	monitor.Remove("task-1")

	_, ok := monitor.Get("task-1")
	if ok {
		t.Error("Task should have been removed")
	}
}
