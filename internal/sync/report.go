package sync

// Failure 记录单个条目级失败。
type Failure struct {
	Item  Item   `json:"item"`
	Error string `json:"error"`
}

// Report 是一次单向同步的结果。
type Report struct {
	Resource     string    `json:"resource"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Mode         string    `json:"mode"`
	DryRun       bool      `json:"dry_run"`
	SourceCount  int       `json:"source_count"`
	TargetBefore int       `json:"target_before"`
	Added        []Item    `json:"added"`
	Removed      []Item    `json:"removed"`
	Skipped      int       `json:"skipped_already"`
	Failed       []Failure `json:"failed"`
}

// HasFailures 报告是否存在条目级失败。
func (r Report) HasFailures() bool { return len(r.Failed) > 0 }
