package verity

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateFixtureIsDeterministicAndReadable(t *testing.T) {
	formats := []FormatKind{FormatCSV, FormatTSV, FormatJSON, FormatJSONL, FormatParquet}
	for _, format := range formats {
		t.Run(string(format), func(t *testing.T) {
			first := filepath.Join(t.TempDir(), "first."+fixtureExtension(format))
			second := filepath.Join(t.TempDir(), "second."+fixtureExtension(format))
			config := FixtureConfig{Rows: 1000, Format: format, Seed: 20260906, NoiseProfile: "mixed"}
			firstManifest, err := GenerateFixture(first, config)
			if err != nil {
				t.Fatal(err)
			}
			secondManifest, err := GenerateFixture(second, config)
			if err != nil {
				t.Fatal(err)
			}
			if firstManifest.RecordCount != 1000 || firstManifest.SHA256 != secondManifest.SHA256 {
				t.Fatalf("manifests are not deterministic: %#v %#v", firstManifest, secondManifest)
			}
			file, err := os.Open(first)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			stream, err := OpenRecordStream(file, InputFormat{Kind: format})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			count := 0
			for {
				_, err := stream.Next(t.Context())
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					t.Fatal(err)
				}
				count++
			}
			if count != 1000 {
				t.Fatalf("read %d rows, want 1000", count)
			}
		})
	}
}

func TestFixtureManifestRoundTripsAsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.csv")
	manifest, err := GenerateFixture(path, FixtureConfig{Rows: 14, Format: FormatCSV, Seed: 7, NoiseProfile: "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded FixtureManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Format != "csv" || decoded.RecordCount != 14 || decoded.Bytes < 1 {
		t.Fatalf("unexpected manifest: %#v", decoded)
	}
}
