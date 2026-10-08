package ui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"omakaiju/internal/config"
)

// OmarchyStyle builds a Chroma style from the Omarchy palette so code colours
// follow the active theme and update on hot-reload.
func OmarchyStyle(t config.Theme) *chroma.Style {
	sb := styles.Get("swapoff").Builder()

	base := t.FGMain
	subtle := t.Subtle
	primary := t.Primary
	accent := t.Accent
	bg := t.BGDark
	sel := t.SelectionBG

	// Helper closure keeps the palette references terse below.
	set := func(ttype chroma.TokenType, colour string, bold bool) {
		entry := chroma.StyleEntry{Colour: parseColour(colour)}
		if bold {
			entry.Bold = chroma.Yes
		}
		sb.AddEntry(ttype, entry)
	}

	set(chroma.Background, bg, false)
	set(chroma.Text, base, false)
	set(chroma.Error, accent, true)

	set(chroma.Comment, subtle, false)
	set(chroma.CommentPreproc, accent, false)
	set(chroma.CommentSingle, subtle, false)
	set(chroma.CommentMultiline, subtle, false)

	set(chroma.Keyword, primary, true)
	set(chroma.KeywordConstant, primary, false)
	set(chroma.KeywordDeclaration, primary, true)
	set(chroma.KeywordNamespace, primary, true)
	set(chroma.KeywordReserved, primary, true)
	set(chroma.KeywordType, primary, false)
	set(chroma.Operator, primary, false)
	set(chroma.Punctuation, base, false)

	set(chroma.Name, base, false)
	set(chroma.NameAttribute, accent, false)
	set(chroma.NameClass, accent, true)
	set(chroma.NameConstant, primary, false)
	set(chroma.NameDecorator, accent, false)
	set(chroma.NameException, accent, false)
	set(chroma.NameFunction, primary, true)
	set(chroma.NameKeyword, primary, false)
	set(chroma.NameLabel, accent, false)
	set(chroma.NameNamespace, primary, false)
	set(chroma.NameTag, accent, true)
	set(chroma.NameVariable, base, false)

	set(chroma.Literal, accent, false)
	set(chroma.LiteralDate, accent, false)
	set(chroma.LiteralNumber, primary, false)
	set(chroma.LiteralString, accent, false)
	set(chroma.LiteralStringChar, accent, false)
	set(chroma.LiteralStringDoc, base, false)
	set(chroma.LiteralStringEscape, primary, true)
	set(chroma.LiteralStringInterpol, accent, false)
	set(chroma.LiteralStringRegex, accent, false)
	set(chroma.LiteralStringSymbol, accent, false)

	set(chroma.GenericHeading, primary, true)
	set(chroma.GenericSubheading, primary, true)
	set(chroma.GenericDeleted, accent, false)
	set(chroma.GenericEmph, accent, true)
	set(chroma.GenericInserted, primary, false)
	set(chroma.GenericStrong, primary, true)
	set(chroma.GenericPrompt, sel, false)

	style, err := sb.Build()
	if err != nil {
		return styles.Fallback
	}
	return style
}

func parseColour(s string) chroma.Colour {
	return chroma.ParseColour(s)
}

// HighlightLines colourises source lines for the preview, returning nil when
// the content cannot be highlighted so callers can fall back to raw lines.
func HighlightLines(path string, lines []string, theme config.Theme) []string {
	if len(lines) == 0 {
		return nil
	}

	source := strings.Join(lines, "\n")
	highlighted := splitLines(Highlight(path, source, OmarchyStyle(theme)))
	if len(highlighted) == 0 {
		return nil
	}
	// Highlighting failed silently if the output is byte-identical to the
	// source; the preview renders raw lines in that case, so return nothing.
	if strings.Join(highlighted, "\n") == source {
		return nil
	}
	return highlighted
}

// lexerFor picks a lexer by filename, falling back to content analysis so
// extensionless files still get highlighted.
func lexerFor(filename, source string) chroma.Lexer {
	if lexer := lexers.Match(filename); lexer != nil {
		return lexer
	}
	if strings.TrimSpace(source) != "" {
		return lexers.Analyse(source)
	}
	return nil
}

// Highlight renders source with syntax colouring. It never fails the caller:
// any lexer or formatter error falls back to the original source so the preview
// always shows something readable.
func Highlight(filename, source string, style *chroma.Style) string {
	if strings.TrimSpace(source) == "" {
		return source
	}

	lexer := lexerFor(filename, source)
	if lexer == nil {
		return source
	}
	if style == nil {
		style = styles.Fallback
	}

	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		return source
	}

	var buf bytes.Buffer
	if err := formatters.TTY16m.Format(&buf, style, iterator); err != nil {
		return source
	}
	return buf.String()
}
