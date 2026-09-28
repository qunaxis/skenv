// Package mdscan finds fenced code blocks in Markdown text, for tests that
// check the examples in docs against real parsers and schemas.
package mdscan

import "strings"

// SkipDirective, alone on the line right before a fence, marks that block
// as an intentionally incomplete snippet: callers should skip it rather
// than treat it as a candidate to validate.
const SkipDirective = "<!-- docs-check: snippet -->"

// Block is one fenced code block.
type Block struct {
	File string // as passed to Blocks
	Line int    // 1-based line of the opening fence
	Lang string // the fence's language, e.g. "toml" (the info string's first word)
	Text string // the block's content, without the fences
	Skip bool   // SkipDirective precedes this block
}

// Blocks returns the fenced code blocks of data whose language is one of
// langs, in the order they appear. A block without a matching closing fence
// is ignored.
func Blocks(file, data string, langs ...string) []Block {
	want := make(map[string]bool, len(langs))
	for _, l := range langs {
		want[l] = true
	}
	lines := strings.Split(data, "\n")
	var blocks []Block
	for i := 0; i < len(lines); i++ {
		info, ok := strings.CutPrefix(lines[i], "```")
		if !ok {
			continue
		}
		// CommonMark's info string is the language plus arbitrary trailing
		// text (VitePress code-group titles: "```toml [GitHub]"); only the
		// first word is the language.
		lang, _, _ := strings.Cut(strings.TrimSpace(info), " ")
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				end = j
				break
			}
		}
		if end == -1 {
			break // unclosed fence: nothing after it is code
		}
		if want[lang] {
			var text string
			if body := lines[i+1 : end]; len(body) > 0 {
				text = strings.Join(body, "\n") + "\n"
			}
			blocks = append(blocks, Block{
				File: file,
				Line: i + 1,
				Lang: lang,
				Text: text,
				// The marker only counts right before the fence, with no
				// blank line in between: it must clearly belong to this
				// block, not to prose above an unrelated blank line.
				Skip: i > 0 && strings.TrimSpace(lines[i-1]) == SkipDirective,
			})
		}
		i = end
	}
	return blocks
}
