package memory

type Status struct {
	Path             string        `json:"path"`
	Backend          string        `json:"backend"`
	MemoryCount      int           `json:"memory_count"`
	PendingEmbedding int           `json:"pending_embedding"`
	Embedder         EmbedderProbe `json:"embedder"`
}

type EmbedderProbe struct {
	Configured bool `json:"configured"`
	OK         bool `json:"ok"`
	Dimension  int  `json:"dimension"`
}
