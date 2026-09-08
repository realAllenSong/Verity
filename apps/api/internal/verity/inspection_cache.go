package verity

import (
	"encoding/json"
	"path/filepath"
)

type inspectionPair struct {
	before  any
	after   any
	outcome string
	reason  string
}

// Capture a bounded explanation sample while each operator already has its
// before and after values. UI reads never join or scan the full dataset.
type inspectionCollector struct {
	groups [4][]inspectionPair // removed, redacted, changed/routed, unchanged
}

func (c *inspectionCollector) Add(before, after any, outcome, reason string) {
	group := 2
	if outcome == "filtered" {
		group = 0
	}
	if left, ok := before.(pipelineRecord); ok {
		if right, ok := after.(pipelineRecord); ok && left.Content != right.Content {
			group = 1
		}
	}
	c.groups[group] = appendBounded(c.groups[group], inspectionPair{before, after, outcome, reason}, 20)
}

func (c *inspectionCollector) Commit(directory, stage string) error {
	groups := make([][]StageComparisonSample, len(c.groups))
	for index, pairs := range c.groups {
		for i := len(pairs) - 1; i >= 0; i-- {
			pair := pairs[i]
			sample := StageComparisonSample{Outcome: pair.outcome, Reason: pair.reason}
			var before, after artifactRow
			if pair.before != nil {
				raw, err := json.Marshal(pair.before)
				if err != nil {
					return err
				}
				before, err = decodeArtifactRow(raw)
				if err != nil {
					return err
				}
				sample.Before, sample.RecordID = before.raw, before.id
			}
			if pair.after != nil {
				raw, err := json.Marshal(pair.after)
				if err != nil {
					return err
				}
				after, err = decodeArtifactRow(raw)
				if err != nil {
					return err
				}
				sample.After, sample.RecordID = after.raw, after.id
			}
			if pair.before != nil && pair.after != nil {
				sample.ChangedFields = changedFields(before.data, after.data)
			}
			groups[index] = append(groups[index], sample)
		}
	}
	return writeJSONAtomic(filepath.Join(directory, stage+".comparison.json"), groups)
}

func selectInspection(groups [][]StageComparisonSample, limit int) []StageComparisonSample {
	result := make([]StageComparisonSample, 0, limit)
	if len(groups) != 4 {
		return result
	}
	removed := min(len(groups[0]), max(1, limit/2))
	result = append(result, groups[0][:removed]...)
	for _, group := range groups[1:] {
		result = append(result, group[:min(len(group), limit-len(result))]...)
	}
	if len(result) < limit {
		result = append(result, groups[0][removed:min(len(groups[0]), removed+limit-len(result))]...)
	}
	return result
}
