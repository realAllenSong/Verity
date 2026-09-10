package verity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTableHundredThousandRows(t *testing.T) {
	if os.Getenv("VERITY_TABLE_LOAD") != "1" {
		t.Skip("set VERITY_TABLE_LOAD=1 for 100k-row table verification")
	}
	s, _, dir := tableFixture(t)
	raw, err := newAtomicJSONL(filepath.Join(dir, "raw.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Abort()
	out, err := newAtomicJSONL(filepath.Join(dir, "normalize.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Abort()
	for i := 0; i < 100_000; i++ {
		id := fmt.Sprintf("load_%06d", i)
		if err := raw.Write(map[string]any{"record_id": id, "payload": map[string]any{"text": " raw value ", "value": i}}); err != nil {
			t.Fatal(err)
		}
		if i%10 != 0 {
			if err := out.Write(map[string]any{"event_id": id, "content": "raw value", "value": i}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := raw.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := out.Commit(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	first, err := s.Table(ctx, "normalize", "", "", "all", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	cold := time.Since(start)
	if first.Rows != 100_000 || first.Counts["removed"] != 10_000 || first.Counts["result"] != 90_000 || len(first.Records) != 100 {
		t.Fatalf("bad 100k counts %+v", first.TableMeta)
	}
	start = time.Now()
	second, err := s.Table(ctx, "normalize", "", first.NextCursor, "all", "", 100)
	if err != nil || len(second.Records) != 100 || second.Records[0].Ordinal != 101 {
		t.Fatalf("bad seek page %v", err)
	}
	warm := time.Since(start)
	cursor, found, pages := "", 0, 0
	for {
		page, err := s.Table(ctx, "normalize", "", cursor, "all", "load_099999", 100)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		found += len(page.Records)
		if page.Scanned > 20_000 {
			t.Fatal("search exceeded bounded scan")
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if found != 1 || pages != 5 {
		t.Fatalf("sparse search missed later rows: %d matches in %d pages", found, pages)
	}
	t.Logf("100,000 rows: cold indexed page %s; cached next page %s; sparse search reached last record in %d bounded requests", cold, warm, pages)
}
