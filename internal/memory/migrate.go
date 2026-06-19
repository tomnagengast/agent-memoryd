package memory

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	zvec "github.com/zvec-ai/zvec-go"
)

// migrateBatchSize is the maximum number of docs per zvec Upsert call.
// zvec rejects batches larger than 1024.
const migrateBatchSize = 512

// migrateIfNeeded imports a legacy memories.jsonl into an open zvec collection.
// It is idempotent: if the jsonl does not exist or the collection already has
// documents, it returns immediately. Runs single-owner inside Open (zvec holds
// an exclusive directory lock), so no additional locking is needed.
// On success the jsonl is renamed to jsonlPath+".migrated".
// Returns the number of records imported (0 if skipped).
func migrateIfNeeded(coll *zvec.Collection, jsonlPath string) (int, error) {
	if _, err := os.Stat(jsonlPath); os.IsNotExist(err) {
		return 0, nil
	}

	stats, err := coll.GetStats()
	if err != nil {
		return 0, fmt.Errorf("migrate get stats: %w", err)
	}
	if stats.DocCount > 0 {
		return 0, nil
	}

	records, err := parseJSONL(jsonlPath)
	if err != nil {
		return 0, fmt.Errorf("migrate parse jsonl: %w", err)
	}
	if len(records) == 0 {
		return 0, nil
	}

	// Upsert in batches: zvec rejects single batches larger than 1024 docs.
	for start := 0; start < len(records); start += migrateBatchSize {
		end := start + migrateBatchSize
		if end > len(records) {
			end = len(records)
		}
		batch := records[start:end]

		docs := make([]*zvec.Doc, 0, len(batch))
		for _, record := range batch {
			doc, docErr := recordToDoc(record, nil)
			if docErr != nil {
				zvec.FreeDocs(docs)
				return 0, fmt.Errorf("migrate build doc for %q: %w", record.ID, docErr)
			}
			docs = append(docs, doc)
		}

		if _, err := coll.Upsert(docs); err != nil {
			zvec.FreeDocs(docs)
			return 0, fmt.Errorf("migrate upsert batch [%d:%d]: %w", start, end, err)
		}
		zvec.FreeDocs(docs)
	}

	if err := coll.Flush(); err != nil {
		return 0, fmt.Errorf("migrate flush: %w", err)
	}

	// Optimize merges pending FTS index segments so the imported records are
	// durable and visible to FTS queries when the collection is reopened in a
	// future process. Without this, FTS data written in one process is invisible
	// after a close/reopen and cannot be recovered by a later Optimize call.
	if err := coll.Optimize(); err != nil {
		return 0, fmt.Errorf("migrate optimize: %w", err)
	}

	if err := os.Rename(jsonlPath, jsonlPath+".migrated"); err != nil {
		return 0, fmt.Errorf("migrate rename: %w", err)
	}

	return len(records), nil
}

func parseJSONL(path string) ([]Record, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open jsonl: %w", err)
	}
	defer file.Close()

	var records []Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode record: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read jsonl: %w", err)
	}
	return records, nil
}
