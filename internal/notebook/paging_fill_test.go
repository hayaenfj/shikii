package notebook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Measure the whole envelope, including JSON escaped a second time in MCP text.
func sourceFillSize(host string, value any) int {
	if host == "JSON" {
		return encodedSize(value)
	}
	data, _ := json.Marshal(value)
	return encodedSize(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(data)}}, "structuredContent": value,
	}})
}

func TestSourcePartsFillMeasuredBudget(t *testing.T) {
	for _, host := range []string{"JSON", "MCP"} {
		for _, kind := range []string{"observation", "original"} {
			t.Run(host+"/"+kind, func(t *testing.T) {
				b := Book{Root: t.TempDir(), Scope: "/project"}
				content := strings.Repeat("abc\"\n<>&\u2028\u20ac\U0001f331", 1800) + " END_OWNER_UNRESOLVED"
				id, want := "0000000000000091", content
				if kind == "observation" {
					o, err := b.capture(Observation{Kind: "test", Title: "Page capacity", Content: content, Reference: "synthetic", Version: "v1", ObservedAt: time.Unix(1, 0)})
					if err != nil {
						t.Fatal(err)
					}
					id = o.ID
				} else {
					originals := []Original{{TraceID: "0000000000000092", Inputs: map[string]string{"request": content}}}
					archiveConversation(t, b, id, originals)
					raw, _ := json.Marshal(originals)
					want = string(raw)
				}
				const budget = 8192
				cursor, joined, calls, underfilled, bytes := "", "", 0, 0, 0
				for {
					page, err := b.ReadSourcesPageMeasured([]string{id}, cursor, budget, func(v any) int { return sourceFillSize(host, v) })
					if err != nil {
						t.Fatal(err)
					}
					used := sourceFillSize(host, page)
					calls++
					bytes += used
					if used > budget {
						t.Fatalf("response exceeds the host budget: %d", used)
					}
					wire, _ := json.Marshal(page)
					var got struct {
						Observations []Observation `json:"observations"`
						OriginalPart *struct {
							From, To, Bytes int
							JSON            string `json:"json"`
						} `json:"original_part"`
						ObservationPart *struct{ From, To, Bytes int } `json:"observation_part"`
						Next            string                         `json:"next_cursor"`
					}
					if err := json.Unmarshal(wire, &got); err != nil {
						t.Fatal(err)
					}
					piece, from, to, total := "", 0, 0, 0
					if kind == "observation" {
						if len(got.Observations) != 1 || got.ObservationPart == nil {
							t.Fatal("split observations retain part provenance")
						}
						piece = got.Observations[0].Content
						from, to, total = got.ObservationPart.From, got.ObservationPart.To, got.ObservationPart.Bytes
					} else {
						if got.OriginalPart == nil {
							t.Fatal("split originals retain part provenance")
						}
						piece = got.OriginalPart.JSON
						from, to, total = got.OriginalPart.From, got.OriginalPart.To, got.OriginalPart.Bytes
					}
					if !utf8.ValidString(piece) || from != len(joined) || to != from+len(piece) || total != len(want) {
						t.Fatal("parts must cover contiguous UTF-8 bytes of the exact original")
					}
					joined += piece
					if got.Next == "" {
						break
					}
					// One more rune and cursor digits need at most a few dozen host bytes.
					if budget-used > 64 {
						underfilled++
					}
					if got.Next == cursor || calls > 100 {
						t.Fatal("source cursor must advance")
					}
					cursor = got.Next
				}
				if joined != want {
					t.Fatal("every original byte and final condition must survive paging")
				}
				t.Logf("pages=%d host_bytes=%d underfilled=%d", calls, bytes, underfilled)
				if underfilled > 0 {
					t.Fatalf("%d nonfinal pages left usable host capacity", underfilled)
				}
			})
		}
	}
}

func BenchmarkSourcePartsReadAll(b *testing.B) {
	for _, host := range []string{"JSON", "MCP"} {
		b.Run(host, func(b *testing.B) {
			book := Book{Root: b.TempDir(), Scope: "/project"}
			o, err := book.capture(Observation{Kind: "test", Title: "Page capacity", Content: strings.Repeat("abc\"\n<>&\u2028\u20ac\U0001f331", 6000), Reference: "synthetic", Version: "v1", ObservedAt: time.Unix(1, 0)})
			if err != nil {
				b.Fatal(err)
			}
			pages, bytes := 0, 0
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				cursor := ""
				for {
					page, err := book.ReadSourcesPageMeasured([]string{o.ID}, cursor, 8192, func(v any) int { return sourceFillSize(host, v) })
					if err != nil {
						b.Fatal(err)
					}
					pages++
					bytes += sourceFillSize(host, page)
					next, _ := page["next_cursor"].(string)
					if next == "" {
						break
					}
					if next == cursor {
						b.Fatal("source cursor did not advance")
					}
					cursor = next
				}
			}
			b.ReportMetric(float64(pages)/float64(b.N), "pages/op")
			b.ReportMetric(float64(bytes)/float64(b.N), "host-bytes/op")
		})
	}
}

// A host's compact presentation can fit when its shorter JSON form does not.
func TestSourcePartPresentationSwitchKeepsKnownFit(t *testing.T) {
	text := strings.Repeat("x", 512)
	end := 0
	err := fitSourcePart(text, func(n int) bool { end = n; return n >= 240 && n <= 300 })
	if err != nil || end < 256 || end > 300 {
		t.Fatal("retain the known fitting prefix across a host presentation switch", end, err)
	}
	err = fitSourcePart(text, func(n int) bool { end = n; return n == len(text) })
	if err != nil || end != len(text) {
		t.Fatal("a complete tail can fit without a continuation", end, err)
	}
	if err = fitSourcePart(text, func(int) bool { return false }); err == nil {
		t.Fatal("unusable source metadata must fail explicitly")
	}
}
