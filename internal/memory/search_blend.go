package memory

type BlendWeights struct {
	FTS    float64
	Vector float64
}

type scoredResult struct {
	ID      string
	Kind    string
	Project string
	Source  string
	Summary string
	Score   float64
}

func normalize(scores []float64) []float64 {
	if len(scores) == 0 {
		return scores
	}
	min, max := scores[0], scores[0]
	for _, s := range scores[1:] {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	out := make([]float64, len(scores))
	span := max - min
	if span == 0 {
		for i := range out {
			out[i] = 1.0
		}
		return out
	}
	for i, s := range scores {
		out[i] = (s - min) / span
	}
	return out
}

func normalizeDistance(scores []float64) []float64 {
	out := normalize(scores)
	for i := range out {
		out[i] = 1 - out[i]
	}
	return out
}

func blend(fts, vec []SearchResult, w BlendWeights) []SearchResult {
	byID := make(map[string]*scoredResult)
	ftsScores := make([]float64, len(fts))
	for i, r := range fts {
		ftsScores[i] = r.Score
	}
	normFTS := normalize(ftsScores)
	for i, r := range fts {
		byID[r.ID] = &scoredResult{
			ID: r.ID, Kind: r.Kind, Project: r.Project, Source: r.Source, Summary: r.Summary,
			Score: normFTS[i] * w.FTS,
		}
	}
	vecScores := make([]float64, len(vec))
	for i, r := range vec {
		vecScores[i] = r.Score
	}
	normVec := normalizeDistance(vecScores)
	for i, r := range vec {
		if existing, ok := byID[r.ID]; ok {
			existing.Score += normVec[i] * w.Vector
		} else {
			byID[r.ID] = &scoredResult{
				ID: r.ID, Kind: r.Kind, Project: r.Project, Source: r.Source, Summary: r.Summary,
				Score: normVec[i] * w.Vector,
			}
		}
	}
	results := make([]SearchResult, 0, len(byID))
	for _, sr := range byID {
		results = append(results, SearchResult{
			ID: sr.ID, Kind: sr.Kind, Project: sr.Project, Source: sr.Source, Summary: sr.Summary,
			Score: sr.Score,
		})
	}
	sortByScoreDesc(results)
	return results
}

func sortByScoreDesc(results []SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score ||
				(results[j].Score == results[i].Score && results[j].ID < results[i].ID) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}
