package verity

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

const (
	fixtureUniqueEvents    = 3611
	fixtureDuplicateEvents = 231
	fixturePolicyRows      = 335
	fixtureLowQualityRows  = 362
	fixtureSignalRows      = 1086
	fixtureReviewRows      = 42
)

var fixtureSources = []string{
	"codex", "claude_code", "github", "jira", "slack", "outlook",
	"calendar", "confluence", "powerpoint",
}

var fixturePeople = []string{
	"Mina Okafor", "Ravi Narayanan", "Elena Petrova",
	"Theo Martin", "Naomi Brooks", "Lucas Ferreira",
}

var fixtureSignals = []struct {
	id      string
	content string
}{
	{"correction", "User stopped a running refactor after type errors appeared and said to preserve the public API contract."},
	{"verification_gap", "PR review requested missing regression tests for null tenant IDs and error handling before merge."},
	{"context_repetition", "Re-entered the Jira requirements and acceptance criteria in another coding session for the same task."},
	{"dependency_blocker", "Jira issue moved to Blocked because an upstream schema dependency changed without a migration path."},
	{"knowledge_need", "Asked in Slack which approved internal API client should handle retries and tenant headers."},
	{"delivery_risk", "Email thread flags that the dataset delivery slipped after an incompatible schema revision."},
	{"coordination_overhead", "Calendar meeting moved to next week because the external dependency owner was unavailable."},
	{"agent_steer", "Developer steered the coding agent away from a new abstraction and toward the repository adapter pattern."},
}

var fixtureOrdinary = []string{
	"Updated implementation notes after the design review.",
	"Synced the branch and resolved a small merge conflict.",
	"Shared the weekly project status with the working group.",
	"Opened the repository and inspected the existing test layout.",
	"Added a source link to the delivery notes.",
	"Reviewed the ticket history before starting the next task.",
}

// GenerateNoisyFixtures creates deterministic, deliberately imperfect input batches
// using the same generic envelope accepted by the public staging API.
func GenerateNoisyFixtures(outputDir string) (map[string]int, error) {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return nil, fmt.Errorf("create fixture directory: %w", err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, fmt.Errorf("read fixture directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".jsonl" {
			if err := os.Remove(filepath.Join(outputDir, entry.Name())); err != nil {
				return nil, fmt.Errorf("replace fixture %s: %w", entry.Name(), err)
			}
		}
	}

	rng := rand.New(rand.NewSource(24_082_026))
	records := make([]dataRecord, 0, fixtureUniqueEvents+fixtureDuplicateEvents)
	for index := 0; index < fixtureUniqueEvents; index++ {
		records = append(records, buildFixtureRecord(index, rng))
	}
	for index := 0; index < fixtureDuplicateEvents; index++ {
		duplicate, err := cloneDataRecord(records[(index*13)%fixtureUniqueEvents])
		if err != nil {
			return nil, err
		}
		duplicate.Metadata["duplicate_ingest"] = true
		duplicate.Metadata["duplicate_at"] = time.Date(2026, 8, 29, 0, 0, index, 0, time.UTC).Format(time.RFC3339)
		records = append(records, duplicate)
	}
	rng.Shuffle(len(records), func(left, right int) {
		records[left], records[right] = records[right], records[left]
	})

	batchIDs := []string{
		"batch_2026_08_24_01", "batch_2026_08_25_01", "batch_2026_08_26_01",
		"batch_2026_08_27_01", "batch_2026_08_28_01", "batch_2026_08_29_01",
	}
	batches := make(map[string][]dataRecord, len(batchIDs))
	for index, record := range records {
		batchID := batchIDs[index%len(batchIDs)]
		record.BatchID = batchID
		record.IngestedAt = time.Date(2026, 8, 24+index%6, 9, 0, index, 0, time.UTC).Format(time.RFC3339)
		batches[batchID] = append(batches[batchID], record)
	}
	counts := make(map[string]int, len(batchIDs))
	for _, batchID := range batchIDs {
		path := filepath.Join(outputDir, batchID+".jsonl")
		if err := writeJSONLinesAtomic(path, batches[batchID]); err != nil {
			return nil, fmt.Errorf("write fixture batch: %w", err)
		}
		counts[batchID] = len(batches[batchID])
	}
	return counts, nil
}

func buildFixtureRecord(index int, rng *rand.Rand) dataRecord {
	source := fixtureSources[index%len(fixtureSources)]
	occurredAt := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(index*19) * time.Minute)
	signalPosition := index - fixturePolicyRows - fixtureLowQualityRows
	title := source + " activity"
	content := fixtureOrdinary[rng.Intn(len(fixtureOrdinary))]
	kind := "activity"
	occurred := occurredAt.Format(time.RFC3339)

	switch {
	case index < fixturePolicyRows:
		title = "Credential fragment captured without surrounding task context"
		content = "od_demo_secret_SHOULD_BE_REDACTED"
		kind = "unscoped_content"
	case index < fixturePolicyRows+fixtureLowQualityRows:
		title = ""
		content = ""
		kind = "unknown"
		if index%3 == 0 {
			occurred = ""
		} else {
			occurred = "not-a-timestamp"
		}
	case signalPosition < fixtureSignalRows:
		signal := fixtureSignals[signalPosition%len(fixtureSignals)]
		title = fmt.Sprintf("%s event related to DATA-%d", source, 1200+index%67)
		content = signal.content
		kind = signal.id
	}

	metadata := map[string]any{
		"origin": source, "workspace": "synthetic-lab", "synthetic": true,
		"evidence_count": 3 + index%5,
	}
	if signalPosition >= 0 && signalPosition < fixtureReviewRows {
		metadata["evidence_count"] = 1
	}
	if index < fixturePolicyRows {
		metadata["sensitive_only"] = true
	}
	if index%43 == 0 && index >= fixturePolicyRows+fixtureLowQualityRows {
		content += fmt.Sprintf(" Contact owner: developer%d@example.invalid.", index%17)
	}
	if index%71 == 0 && index >= fixturePolicyRows+fixtureLowQualityRows {
		content = "  " + content + "\n\nAutomated footer: sent from mobile  "
	}
	metadata["ticket"] = fmt.Sprintf("DATA-%d", 1200+index%67)
	metadata["interaction"] = []string{"interrupt", "steer", "queue", "complete"}[index%4]
	metadata["token_input"] = 820 + index%970
	metadata["token_output"] = 310 + index%530

	return dataRecord{
		RecordID: fmt.Sprintf("evt_%05d", index), DatasetID: "workflow-signals",
		Payload: map[string]any{
			"event_id": fmt.Sprintf("evt_%05d", index), "kind": kind,
			"occurred_at": occurred, "actor": fixturePeople[index%len(fixturePeople)],
			"thread_id": fmt.Sprintf("thread_%05d", index/4), "title": title,
			"content": content, "status": []string{"open", "closed", "in_progress"}[index%3],
		},
		Metadata: metadata,
	}
}

func cloneDataRecord(record dataRecord) (dataRecord, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return dataRecord{}, fmt.Errorf("encode duplicate fixture: %w", err)
	}
	var result dataRecord
	if err := json.Unmarshal(data, &result); err != nil {
		return dataRecord{}, fmt.Errorf("decode duplicate fixture: %w", err)
	}
	return result, nil
}
