package verity

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

// ContentBlock is an excerpt from an actual input field, never an LLM summary.
// SourcePath and ID survive normalization so the reader can align the same turn.
type ContentBlock struct {
	ID          string `json:"id"`
	Role        string `json:"role"`
	Text        string `json:"text"`
	SourcePath  string `json:"source_path"`
	ReplyTo     string `json:"reply_to,omitempty"`
	Interaction string `json:"interaction,omitempty"`
	Tool        string `json:"tool,omitempty"`
	Format      string `json:"format,omitempty"`
}

type ReadingDocument struct {
	Title     string         `json:"title,omitempty"`
	Source    string         `json:"source,omitempty"`
	Blocks    []ContentBlock `json:"blocks"`
	Synthetic bool           `json:"synthetic,omitempty"`
	Protected bool           `json:"protected,omitempty"`
}

func literal(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := values[key].(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// Display extraction is deliberately conservative: only explicit text fields,
// message arrays and before/after bodies are read. No timestamp-based pairing.
func contentBlocks(body map[string]any) []ContentBlock {
	if encoded, ok := body["content_blocks"]; ok {
		data, _ := json.Marshal(encoded)
		var blocks []ContentBlock
		if json.Unmarshal(data, &blocks) == nil {
			return blocks
		}
	}
	blocks := []ContentBlock{}
	var visit func(any, string, string, string, string, string, string)
	visit = func(value any, path, role, id, reply, interaction, tool string) {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				blocks = append(blocks, ContentBlock{ID: defaultString(id, path), Role: defaultString(role, "text"), Text: v, SourcePath: path, ReplyTo: reply, Interaction: interaction, Tool: tool})
			}
		case []any:
			for i, entry := range v {
				visit(entry, fmt.Sprintf("%s[%d]", path, i), role, id, reply, interaction, tool)
			}
		case map[string]any:
			id = defaultString(literal(v, "id", "message_id", "call_id"), id)
			role = defaultString(literal(v, "role"), role)
			typ := literal(v, "type")
			if typ == "tool_use" || typ == "function_call" {
				role = "tool_call"
			}
			if typ == "tool_result" || typ == "function_call_output" {
				role = "tool_result"
			}
			reply = defaultString(literal(v, "reply_to", "parent_id", "tool_use_id"), reply)
			interaction = defaultString(literal(v, "interaction", "event"), interaction)
			tool = defaultString(literal(v, "tool", "name"), tool)
			if privateObject(v) {
				visit("[Private content withheld]", path, role, id, reply, interaction, tool)
				return
			}
			start := len(blocks)
			for _, key := range []string{"text", "content", "body", "message", "output"} {
				if child, ok := v[key]; ok {
					visit(child, path+"."+key, role, id, reply, interaction, tool)
					break
				}
			}
			if len(blocks) == start && role == "tool_call" {
				for _, key := range []string{"arguments", "input"} {
					if child, ok := v[key]; ok {
						data, _ := json.MarshalIndent(protectValue(child), "", "  ")
						text := string(data)
						if s, ok := child.(string); ok {
							text = protectText(s)
						}
						visit(text, path+"."+key, role, id, reply, interaction, tool)
						break
					}
				}
			}
			if literal(v, "contentType") == "html" {
				for i := start; i < len(blocks); i++ {
					blocks[i].Format = "html"
				}
			}
			if calls, ok := v["tool_calls"]; ok {
				visit(calls, path+".tool_calls", "tool_call", "", id, "", "")
			}
			if fn, ok := v["function"]; ok && role == "tool_call" {
				visit(fn, path+".function", role, id, reply, interaction, tool)
			}
		}
	}
	for _, key := range []string{"messages", "turns", "comments"} {
		if values, ok := body[key].([]any); ok {
			visit(values, key, "message", "", "", "", "")
			break
		}
	}
	// A ticket or PR can have both a description and comments. Neither replaces
	// the other. Conversation content is kept in the source's explicit order.
	if len(blocks) > 0 {
		if value, ok := body["description"]; ok {
			visit(value, "description", "description", "", "", "", "")
		}
	}
	if len(blocks) == 0 {
		for _, item := range []struct{ key, role string }{{"prompt", "user"}, {"response", "assistant"}, {"before", "before"}, {"after", "after"}} {
			if value, ok := body[item.key]; ok {
				visit(value, item.key, item.role, "", "", "", "")
			}
		}
	}
	if len(blocks) == 0 {
		role := defaultString(literal(body, "role"), "text")
		kind := strings.ToLower(literal(body, "kind", "type", "event_type"))
		if strings.Contains(kind, "email") {
			role = "email"
		} else if strings.Contains(kind, "comment") || strings.Contains(kind, "slack") {
			role = "message"
		}
		visit(body, "", role, "", "", "", "")
		// Descriptions matter for tickets, calendar entries and documents too.
		if len(blocks) == 0 {
			for _, key := range []string{"description", "notes", "summary"} {
				if value, ok := body[key]; ok {
					visit(value, key, role, "", "", "", "")
					break
				}
			}
		}
	}
	if len(blocks) == 0 {
		keys := make([]string, 0, len(body))
		for key := range body {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "title" || key == "subject" || key == "name" {
				continue
			}
			if text, ok := body[key].(string); ok && len(strings.Fields(text)) >= 6 {
				visit(text, key, "text", "", "", "", "")
			}
		}
	}
	seen := map[string]int{}
	for i := range blocks {
		blocks[i].SourcePath = strings.TrimPrefix(blocks[i].SourcePath, ".")
		if blocks[i].ID == "" || strings.HasPrefix(blocks[i].ID, ".") {
			blocks[i].ID = blocks[i].SourcePath
		}
		base := blocks[i].ID
		seen[base]++
		if seen[base] > 1 {
			blocks[i].ID = fmt.Sprintf("%s:%d", base, seen[base])
		}
	}
	return blocks
}

func normalizeText(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = footerPattern.ReplaceAllString(text, "")
	return strings.Trim(text, "\n") // preserve code indentation, paragraphs and lists
}

func readingDocument(raw json.RawMessage) *ReadingDocument {
	if len(raw) == 0 {
		return nil
	}
	var envelope map[string]any
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	body := tableBody(envelope)
	metadata, _ := envelope["metadata"].(map[string]any)
	blocks := contentBlocks(body)
	for i := range blocks {
		if blocks[i].Format == "html" {
			blocks[i].Text = protectText(htmlText(blocks[i].Text))
		}
	}
	return &ReadingDocument{
		Title:  literal(body, "title", "subject", "name"),
		Source: defaultString(literal(body, "source", "source_type", "provider"), literal(metadata, "origin")),
		Blocks: blocks, Synthetic: asBool(body["synthetic"]) || asBool(metadata["synthetic"]),
		Protected: asBool(body["sensitive_only"]) || asBool(metadata["sensitive_only"]) || literal(body, "visibility") == "private",
	}
}

// Parse, never execute, email HTML. The original body remains in protected JSON.
func htmlText(source string) string {
	z := html.NewTokenizer(strings.NewReader(source))
	var out strings.Builder
	hidden := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(out.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" {
				hidden++
			}
			if hidden == 0 && (tag == "p" || tag == "div" || tag == "br" || tag == "li") {
				out.WriteString("\n")
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if (tag == "script" || tag == "style") && hidden > 0 {
				hidden--
			}
			if hidden == 0 && (tag == "p" || tag == "div" || tag == "li") {
				out.WriteString("\n")
			}
		case html.TextToken:
			if hidden == 0 {
				out.Write(z.Text())
			}
		}
	}
}

func structuredContent(body map[string]any) bool {
	for _, key := range []string{"messages", "turns", "comments", "prompt", "response", "before", "after", "description", "notes", "summary", "content_blocks"} {
		if _, ok := body[key]; ok {
			return true
		}
	}
	for _, key := range []string{"content", "body", "message"} {
		if value, ok := body[key]; ok {
			if _, str := value.(string); !str {
				return true
			}
		}
	}
	return false
}
