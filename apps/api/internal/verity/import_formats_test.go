package verity

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestDetectFormatUsesExtensionAndContent(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		header     string
		wantKind   FormatKind
		compressed bool
	}{
		{name: "csv", filename: "events.csv", header: "id,message\n1,hello\n", wantKind: FormatCSV},
		{name: "tsv", filename: "events.tsv", header: "id\tmessage\n1\thello\n", wantKind: FormatTSV},
		{name: "json", filename: "events.json", header: "[{\"id\":1}]", wantKind: FormatJSON},
		{name: "jsonl", filename: "events.jsonl", header: "{\"id\":1}\n{\"id\":2}\n", wantKind: FormatJSONL},
		{name: "ndjson", filename: "events.ndjson", header: "{\"id\":1}\n", wantKind: FormatJSONL},
		{name: "parquet magic", filename: "upload.bin", header: "PAR1ignored", wantKind: FormatParquet},
		{name: "gzip csv", filename: "events.csv.gz", header: string([]byte{0x1f, 0x8b, 0x08}), wantKind: FormatCSV, compressed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := DetectFormat(test.filename, []byte(test.header))
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != test.wantKind || got.Gzip != test.compressed {
				t.Fatalf("DetectFormat() = %#v, want kind=%q gzip=%v", got, test.wantKind, test.compressed)
			}
		})
	}
}

func TestCSVStreamPreservesQuotedAndMultilineFields(t *testing.T) {
	input := "id,message,note\r\n1,\"hello, world\",\"first line\nsecond line\"\r\n"
	rows := collectRecordStream(t, strings.NewReader(input), InputFormat{Kind: FormatCSV})
	want := []map[string]any{{"id": "1", "message": "hello, world", "note": "first line\nsecond line"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("CSV rows = %#v, want %#v", rows, want)
	}
}

func TestStructuredTextStreamsProduceEquivalentRows(t *testing.T) {
	want := []map[string]any{
		{"id": float64(1), "message": "alpha"},
		{"id": float64(2), "message": "beta"},
	}
	tests := []struct {
		name   string
		input  string
		format InputFormat
	}{
		{name: "json array", input: "[{\"id\":1,\"message\":\"alpha\"},{\"id\":2,\"message\":\"beta\"}]", format: InputFormat{Kind: FormatJSON}},
		{name: "jsonl", input: "{\"id\":1,\"message\":\"alpha\"}\n\n{\"id\":2,\"message\":\"beta\"}\n", format: InputFormat{Kind: FormatJSONL}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := collectRecordStream(t, strings.NewReader(test.input), test.format)
			if !reflect.DeepEqual(rows, want) {
				t.Fatalf("%s rows = %#v, want %#v", test.name, rows, want)
			}
		})
	}
}

func TestGzipStreamDecompressesBeforeParsing(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte("id,message\n1,hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rows := collectRecordStream(t, bytes.NewReader(compressed.Bytes()), InputFormat{Kind: FormatCSV, Gzip: true})
	if got := rows[0]["message"]; got != "hello" {
		t.Fatalf("message = %#v, want hello", got)
	}
}

type parquetFormatFixture struct {
	ID      int64  `parquet:"id"`
	Message string `parquet:"message"`
	Active  bool   `parquet:"active"`
}

func TestParquetStreamPreservesScalarTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.parquet")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := parquet.NewGenericWriter[parquetFormatFixture](file)
	if _, err := writer.Write([]parquetFormatFixture{{ID: 7, Message: "hello", Active: true}}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	rows := collectRecordStream(t, input, InputFormat{Kind: FormatParquet})
	want := map[string]any{"id": int64(7), "message": "hello", "active": true}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("Parquet row = %#v, want %#v", rows[0], want)
	}
}

func TestRecordStreamReportsMalformedJSONLLine(t *testing.T) {
	stream, err := OpenRecordStream(strings.NewReader("{\"id\":1}\nnot-json\n"), InputFormat{Kind: FormatJSONL})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(t.Context()); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("malformed JSONL error = %v, want line 2", err)
	}
}

func collectRecordStream(t *testing.T, reader io.Reader, format InputFormat) []map[string]any {
	t.Helper()
	stream, err := OpenRecordStream(reader, format)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	var rows []map[string]any
	for {
		row, err := stream.Next(t.Context())
		if err == io.EOF {
			return rows
		}
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
}
