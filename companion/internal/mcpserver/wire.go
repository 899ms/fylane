package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodCallTool is the only method whose result carries a body worth
// trimming; every other method passes through untouched.
const methodCallTool = "tools/call"

// A tool result carries the same body twice. The SDK fills structuredContent
// from the typed output and, for a tool that wrote no content blocks of its
// own, repeats that exact JSON as content[0].text — the spec's fallback for
// clients older than structured output. On the wire that doubles every answer
// we send, and the wire is what the platforms cap.
//
// Which copy is freight depends on who is asking, and the three platforms
// measured differently (04_TECH_STACK): Claude hands the model only the
// content blocks and never reads structuredContent; ChatGPT and Grok read
// both. So there is no one copy to drop. For a caller we do not recognise the
// answer is to drop neither — it may be a local client that reads structured
// output strictly, and half a body nobody can parse is worse than a whole one
// sent twice.
type wireTrim int

const (
	// keepBothCopies is the spec-complete shape, and what every unknown
	// caller keeps. Gemini is known by name but was never measured, so it
	// stays here too: a guess in this direction costs an answer.
	keepBothCopies wireTrim = iota
	// dropStructured keeps the content blocks and removes the structured
	// half, for a platform that never looks at it.
	dropStructured
	// dropContentCopy keeps structuredContent and removes the generated
	// JSON fallback, which leaves the tool's output schema contract intact.
	dropContentCopy
)

var wireTrims = map[string]wireTrim{
	"claude":  dropStructured,
	"chatgpt": dropContentCopy,
	"grok":    dropContentCopy,
}

// trimDuplicateBody removes the copy of the answer this caller does not read.
// It sits in middleware rather than in the tools because the duplicate is
// created after a handler returns, inside the SDK — this is the one place
// that sees every tool result, including tools added later.
func trimDuplicateBody(trim wireTrim) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || method != methodCallTool {
				return res, err
			}
			if call, ok := res.(*mcp.CallToolResult); ok {
				trimCallResult(call, trim)
			}
			return res, nil
		}
	}
}

// trimCallResult drops one copy of the body, and only where there provably is
// one. A result with no structured half has nothing duplicated: a call that
// failed carries its message in the content blocks alone, and taking those
// away would hand the model an empty answer instead of a reason.
func trimCallResult(res *mcp.CallToolResult, trim wireTrim) {
	if res == nil || res.StructuredContent == nil {
		return
	}
	switch trim {
	case dropStructured:
		// Only when something is left to read. A tool answering through the
		// structured half alone would otherwise say nothing at all.
		if len(res.Content) > 0 {
			res.StructuredContent = nil
		}
	case dropContentCopy:
		if generatedCopy(res) {
			// Empty rather than nil: content is a required field, and a
			// caller reading it should find an empty list, not null.
			res.Content = []mcp.Content{}
		}
	}
}

// generatedCopy reports whether the content blocks are exactly the fallback
// the SDK generated from the structured output. Tools that write their own
// text — the snapshot's dimensions, the probe's instruction — say things the
// structured half does not, and on Claude that text is all the model gets;
// mistaking one of those for a duplicate would delete the only sentence that
// explains the answer.
func generatedCopy(res *mcp.CallToolResult) bool {
	if len(res.Content) != 1 {
		return false
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return false
	}
	raw, ok := res.StructuredContent.(json.RawMessage)
	if !ok {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return false
		}
		raw = b
	}
	return bytes.Equal([]byte(text.Text), raw)
}
