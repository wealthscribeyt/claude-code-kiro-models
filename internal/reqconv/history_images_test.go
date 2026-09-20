package reqconv

import (
	"testing"

	"github.com/d-kuro/kirocc/internal/anthropic"
)

func imageBlock(data string) anthropic.ContentBlock {
	return anthropic.ContentBlock{
		Type:   anthropic.BlockTypeImage,
		Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: data},
	}
}

// A history user message keeps its images: history entries use the same
// userInputMessage shape as the current message, so an image pasted in an
// earlier turn stays visible to the model instead of being dropped.
func TestBuildHistoryCarriesImages(t *testing.T) {
	msgs := []anthropic.Message{
		{Role: "user", Content: anthropic.MessageContent{Blocks: []anthropic.ContentBlock{
			{Type: anthropic.BlockTypeText, Text: "look at this"},
			imageBlock("iVBORw0KGgo="),
		}}},
		{Role: "assistant", Content: anthropic.MessageContent{Text: "a cat"}},
	}

	history := buildHistory(msgs, NewToolNameMap(), nil)

	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	user := history[0].UserInputMessage
	if len(user.Images) != 1 {
		t.Fatalf("expected 1 image on the history entry, got %d", len(user.Images))
	}
	if user.Images[0].Format != "png" || user.Images[0].Source.Bytes != "iVBORw0KGgo=" {
		t.Errorf("image not carried through: %+v", user.Images[0])
	}
	if user.Content != "look at this" {
		t.Errorf("content = %q, want %q", user.Content, "look at this")
	}
}

// An image-only history message has no text once the image block is extracted.
// Kiro rejects a userInputMessage with no content at all ("Improperly formed
// request"), so the entry falls back to the "(empty)" placeholder.
func TestBuildHistoryImageOnlyMessageKeepsContent(t *testing.T) {
	msgs := []anthropic.Message{
		{Role: "user", Content: anthropic.MessageContent{Blocks: []anthropic.ContentBlock{imageBlock("AAAA")}}},
		{Role: "assistant", Content: anthropic.MessageContent{Text: "a cat"}},
	}

	history := buildHistory(msgs, NewToolNameMap(), nil)

	user := history[0].UserInputMessage
	if user.Content != syntheticEmpty {
		t.Errorf("content = %q, want %q", user.Content, syntheticEmpty)
	}
	if len(user.Images) != 1 {
		t.Fatalf("expected the image to survive, got %d", len(user.Images))
	}
}

// Images nested in a history tool_result are promoted to the entry's image
// list, the same way the current message promotes them.
func TestBuildHistoryPromotesToolResultImages(t *testing.T) {
	msgs := []anthropic.Message{
		{Role: "user", Content: anthropic.MessageContent{Blocks: []anthropic.ContentBlock{{
			Type:      anthropic.BlockTypeToolResult,
			ToolUseID: "toolu_read_png",
			Content:   anthropic.MessageContent{Blocks: []anthropic.ContentBlock{imageBlock("BBBB")}},
		}}}},
		{Role: "assistant", Content: anthropic.MessageContent{Text: "a chart"}},
	}

	history := buildHistory(msgs, NewToolNameMap(), nil)

	user := history[0].UserInputMessage
	if len(user.Images) != 1 || user.Images[0].Source.Bytes != "BBBB" {
		t.Fatalf("tool_result image not promoted: %+v", user.Images)
	}
	// A tool-result turn legitimately carries empty content; the placeholder
	// must not displace the observed kiro-cli continuation shape.
	if user.Content != "" {
		t.Errorf("content = %q, want empty for a tool-result turn", user.Content)
	}
	if user.UserInputMessageContext == nil || len(user.UserInputMessageContext.ToolResults) != 1 {
		t.Fatal("tool result missing from the history entry")
	}
}
