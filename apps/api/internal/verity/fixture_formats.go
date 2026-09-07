package verity

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/parquet-go/parquet-go"
)

type FixtureConfig struct {
	Rows         int        `json:"rows"`
	Format       FormatKind `json:"format"`
	Seed         int64      `json:"seed"`
	NoiseProfile string     `json:"noise_profile"`
}

type FixtureManifest struct {
	Path         string `json:"path"`
	Format       string `json:"format"`
	RecordCount  int    `json:"record_count"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256"`
	Seed         int64  `json:"seed"`
	NoiseProfile string `json:"noise_profile"`
}

type fixtureRow struct {
	RecordID     string `json:"record_id" parquet:"record_id"`
	Kind         string `json:"kind" parquet:"kind"`
	OccurredAt   string `json:"occurred_at" parquet:"occurred_at"`
	Actor        string `json:"actor" parquet:"actor"`
	ThreadID     string `json:"thread_id" parquet:"thread_id"`
	Title        string `json:"title" parquet:"title"`
	Content      string `json:"content" parquet:"content"`
	Status       string `json:"status" parquet:"status"`
	Evidence     int64  `json:"evidence_count" parquet:"evidence_count"`
	TokenInput   int64  `json:"token_input" parquet:"token_input"`
	TokenOutput  int64  `json:"token_output" parquet:"token_output"`
	Sensitive    bool   `json:"sensitive_only" parquet:"sensitive_only"`
	DuplicateKey string `json:"duplicate_key" parquet:"duplicate_key"`
}

var fixtureHeaders = []string{
	"record_id", "kind", "occurred_at", "actor", "thread_id", "title", "content",
	"status", "evidence_count", "token_input", "token_output", "sensitive_only", "duplicate_key",
}

func fixtureExtension(format FormatKind) string {
	switch format {
	case FormatTSV:
		return "tsv"
	case FormatJSON:
		return "json"
	case FormatJSONL:
		return "jsonl"
	case FormatParquet:
		return "parquet"
	default:
		return "csv"
	}
}

// GenerateFixture writes deterministic, deliberately imperfect workflow records.
// It writes incrementally so the same generator can create million-row fixtures
// without first materializing the dataset in memory.
func GenerateFixture(path string, config FixtureConfig) (FixtureManifest, error) {
	if config.Rows < 1 {
		return FixtureManifest{}, errors.New("fixture rows must be greater than zero")
	}
	if config.NoiseProfile == "" {
		config.NoiseProfile = "mixed"
	}
	switch config.Format {
	case FormatCSV, FormatTSV, FormatJSON, FormatJSONL, FormatParquet:
	default:
		return FixtureManifest{}, fmt.Errorf("unsupported fixture format %q", config.Format)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return FixtureManifest{}, fmt.Errorf("create fixture directory: %w", err)
	}
	file, err := os.Create(path)
	if err != nil {
		return FixtureManifest{}, fmt.Errorf("create fixture: %w", err)
	}
	writeErr := writeFixture(file, config)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return FixtureManifest{}, fmt.Errorf("write fixture: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return FixtureManifest{}, fmt.Errorf("stat fixture: %w", err)
	}
	hash, err := fileSHA256(path)
	if err != nil {
		return FixtureManifest{}, err
	}
	return FixtureManifest{
		Path: path, Format: string(config.Format), RecordCount: config.Rows, Bytes: info.Size(),
		SHA256: hash, Seed: config.Seed, NoiseProfile: config.NoiseProfile,
	}, nil
}

func writeFixture(output io.Writer, config FixtureConfig) error {
	rng := rand.New(rand.NewSource(config.Seed))
	rows := func(yield func(fixtureRow) error) error {
		for index := 0; index < config.Rows; index++ {
			if err := yield(buildFormatFixtureRow(index, config.Rows, config.NoiseProfile, rng)); err != nil {
				return err
			}
		}
		return nil
	}
	switch config.Format {
	case FormatCSV, FormatTSV:
		writer := csv.NewWriter(output)
		if config.Format == FormatTSV {
			writer.Comma = '\t'
		}
		if err := writer.Write(fixtureHeaders); err != nil {
			return err
		}
		err := rows(func(row fixtureRow) error { return writer.Write(row.delimited()) })
		writer.Flush()
		return errors.Join(err, writer.Error())
	case FormatJSON:
		buffered := bufio.NewWriterSize(output, 256*1024)
		if _, err := buffered.WriteString("[\n"); err != nil {
			return err
		}
		err := rows(func(row fixtureRow) error {
			if row.RecordID != "evt_00000000" {
				if _, err := buffered.WriteString(",\n"); err != nil {
					return err
				}
			}
			return json.NewEncoder(buffered).Encode(row)
		})
		if err == nil {
			_, err = buffered.WriteString("]\n")
		}
		return errors.Join(err, buffered.Flush())
	case FormatJSONL:
		buffered := bufio.NewWriterSize(output, 256*1024)
		encoder := json.NewEncoder(buffered)
		err := rows(func(row fixtureRow) error { return encoder.Encode(row) })
		return errors.Join(err, buffered.Flush())
	case FormatParquet:
		writer := parquet.NewGenericWriter[fixtureRow](output)
		batch := make([]fixtureRow, 0, 1024)
		err := rows(func(row fixtureRow) error {
			batch = append(batch, row)
			if len(batch) < cap(batch) {
				return nil
			}
			_, err := writer.Write(batch)
			batch = batch[:0]
			return err
		})
		if err == nil && len(batch) > 0 {
			_, err = writer.Write(batch)
		}
		return errors.Join(err, writer.Close())
	default:
		return fmt.Errorf("unsupported fixture format %q", config.Format)
	}
}

func buildFormatFixtureRow(index, total int, profile string, rng *rand.Rand) fixtureRow {
	signals := []string{"activity", "correction", "verification_gap", "context_repetition", "agent_steer", "delivery_risk"}
	content := []string{
		"Reviewed the existing implementation and verified the next change.",
		"User interrupted the agent and corrected the requested implementation path.",
		"PR review requested regression coverage before merge.",
		"Re-entered the same task context in another working session.",
		"Steered the agent toward the repository adapter pattern.",
		"Delivery may slip because an upstream schema changed.",
	}[index%6]
	row := fixtureRow{
		RecordID: fmt.Sprintf("evt_%08d", index), Kind: signals[index%len(signals)],
		OccurredAt: time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(index) * 11 * time.Minute).Format(time.RFC3339),
		Actor:      fixturePeople[index%len(fixturePeople)], ThreadID: fmt.Sprintf("thread_%08d", index/4),
		Title: fmt.Sprintf("Workflow event %d", index), Content: content, Status: []string{"open", "closed", "in_progress"}[index%3],
		Evidence: int64(1 + index%7), TokenInput: int64(400 + rng.Intn(1800)), TokenOutput: int64(100 + rng.Intn(900)),
		DuplicateKey: fmt.Sprintf("key_%08d", index),
	}
	if profile == "mixed" {
		switch {
		case index%97 == 0:
			row.Sensitive = true
			row.Content = "Bearer sk-demo-secret-should-be-redacted"
		case index%71 == 0:
			row.OccurredAt = "not-a-timestamp"
		case index%53 == 0:
			row.Title = ""
			row.Content = "  " + row.Content + "\n\nSent from mobile  "
		case index%41 == 0:
			row.Content = "Quoted field, with comma and a second line\nfor parser validation."
		}
		if index > 0 && index%43 == 0 {
			row.DuplicateKey = fmt.Sprintf("key_%08d", index-1)
		}
		if total > 10 && index == total-1 {
			row.Content = "Final row verifies that streaming readers do not truncate input."
		}
	}
	return row
}

func (row fixtureRow) delimited() []string {
	return []string{
		row.RecordID, row.Kind, row.OccurredAt, row.Actor, row.ThreadID, row.Title, row.Content, row.Status,
		strconv.FormatInt(row.Evidence, 10), strconv.FormatInt(row.TokenInput, 10), strconv.FormatInt(row.TokenOutput, 10),
		strconv.FormatBool(row.Sensitive), row.DuplicateKey,
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open fixture for checksum: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("checksum fixture: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
