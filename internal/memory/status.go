package memory

type Status struct {
	Path        string `json:"path"`
	Backend     string `json:"backend"`
	MemoryCount int    `json:"memory_count"`
}
