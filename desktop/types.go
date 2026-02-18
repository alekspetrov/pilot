package main

// MetricsData holds dashboard metrics for display in the desktop app.
type MetricsData struct {
	TotalTokens  int64   `json:"totalTokens"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	TotalCostUSD float64 `json:"totalCostUSD"`
	CostPerTask  float64 `json:"costPerTask"`
	TotalTasks   int     `json:"totalTasks"`
	Succeeded    int     `json:"succeeded"`
	Failed       int     `json:"failed"`
	TokenHistory []int64   `json:"tokenHistory"` // 7 days
	CostHistory  []float64 `json:"costHistory"`  // 7 days
	TaskHistory  []int     `json:"taskHistory"`  // 7 days
}

// TaskDisplay represents a task in the queue panel.
type TaskDisplay struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`   // pending, queued, running, completed, failed
	Phase    string `json:"phase"`
	Progress int    `json:"progress"` // 0-100
	Duration string `json:"duration"`
	IssueURL string `json:"issueURL"`
	PRURL    string `json:"prURL"`
}

// HistoryEntry represents a completed task in the history panel.
type HistoryEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Duration    string `json:"duration"`
	CompletedAt string `json:"completedAt"`
	ParentID    string `json:"parentID"`
	IsEpic      bool   `json:"isEpic"`
	TotalSubs   int    `json:"totalSubs"`
	DoneSubs    int    `json:"doneSubs"`
}

// ServerStatus reports whether the pilot gateway is running.
type ServerStatus struct {
	Running bool   `json:"running"`
	Version string `json:"version"`
}

// ConfigSummary holds non-sensitive config for display in the desktop app.
type ConfigSummary struct {
	GatewayPort int      `json:"gatewayPort"`
	Autopilot   string   `json:"autopilot"`
	Adapters    []string `json:"adapters"`
}
