package importmem

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomnagengast/agent-memoryd/internal/memory"
)

type Options struct {
	Path    string
	Project string
}

type Result struct {
	Path     string `json:"path"`
	Format   string `json:"format"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
}

func Import(ctx context.Context, store *memory.Store, opts Options) (Result, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return Result{}, fmt.Errorf("import path is empty")
	}
	path = expand(path)
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("stat import path: %w", err)
	}
	result := Result{Path: path}
	if info.IsDir() {
		result.Format = "directory"
		err := filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if shouldSkipDir(entry.Name()) && filePath != path {
					return filepath.SkipDir
				}
				return nil
			}
			if !isImportableFile(filePath) {
				return nil
			}
			fileResult, err := importFile(ctx, store, filePath, opts.Project)
			if err != nil {
				return err
			}
			result.Imported += fileResult.Imported
			result.Skipped += fileResult.Skipped
			return nil
		})
		if err != nil {
			return Result{}, fmt.Errorf("import directory: %w", err)
		}
		return result, nil
	}
	return importFile(ctx, store, path, opts.Project)
}

func importFile(ctx context.Context, store *memory.Store, path, project string) (Result, error) {
	result := Result{Path: path, Format: fileFormat(path)}
	switch result.Format {
	case "jsonl":
		imported, skipped, err := importJSONL(ctx, store, path, project)
		if err != nil {
			return Result{}, err
		}
		result.Imported = imported
		result.Skipped = skipped
		return result, nil
	case "markdown", "text":
		imported, skipped, err := importText(ctx, store, path, project)
		if err != nil {
			return Result{}, err
		}
		result.Imported = imported
		result.Skipped = skipped
		return result, nil
	default:
		result.Skipped = 1
		return result, nil
	}
}

func importJSONL(ctx context.Context, store *memory.Store, path, project string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open jsonl import: %w", err)
	}
	defer file.Close()

	imported := 0
	skipped := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			skipped++
			continue
		}
		var record memory.Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return imported, skipped, fmt.Errorf("decode jsonl import %s:%d: %w", path, lineNo, err)
		}
		if strings.TrimSpace(record.Body) == "" {
			skipped++
			continue
		}
		if record.ID == "" {
			record.ID = stableID(path, fmt.Sprintf("%d", lineNo))
		}
		if record.Source == "" {
			record.Source = fmt.Sprintf("%s:%d", path, lineNo)
		}
		if record.Project == "" {
			record.Project = strings.TrimSpace(project)
		}
		now := record.CreatedAt
		if now.IsZero() {
			now = time.Time{}
		}
		if _, err := store.Add(ctx, memory.AddRequest{
			ID:      record.ID,
			Kind:    record.Kind,
			Project: record.Project,
			Source:  record.Source,
			Summary: record.Summary,
			Body:    record.Body,
			Now:     now,
		}); err != nil {
			return imported, skipped, fmt.Errorf("import memory %s:%d: %w", path, lineNo, err)
		}
		imported++
	}
	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("read jsonl import: %w", err)
	}
	return imported, skipped, nil
}

func importText(ctx context.Context, store *memory.Store, path, project string) (int, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, fmt.Errorf("read text import: %w", err)
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return 0, 1, nil
	}
	summary := textSummary(body, path)
	if _, err := store.Add(ctx, memory.AddRequest{
		ID:      stableID(path, ""),
		Kind:    "note",
		Project: strings.TrimSpace(project),
		Source:  path,
		Summary: summary,
		Body:    body,
	}); err != nil {
		return 0, 0, fmt.Errorf("import text memory: %w", err)
	}
	return 1, 0, nil
}

func textSummary(body, path string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if line != "" {
			return memory.Summarize(line, 140)
		}
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func isImportableFile(path string) bool {
	switch fileFormat(path) {
	case "jsonl", "markdown", "text":
		return true
	default:
		return false
	}
}

func fileFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl":
		return "jsonl"
	case ".md", ".markdown":
		return "markdown"
	case ".txt":
		return "text"
	default:
		return "unknown"
	}
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".cache", ".DS_Store":
		return true
	default:
		return false
	}
}

func stableID(parts ...string) string {
	h := sha1.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "import:" + hex.EncodeToString(h.Sum(nil))[:16]
}

func expand(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return os.ExpandEnv(path)
}
