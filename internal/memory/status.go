package memory

type Status struct {
	Path        string `json:"path"`
	Index       string `json:"index"`
	MemoryCount int    `json:"memory_count"`
}
