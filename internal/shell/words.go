package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// word renders a word as shlex did in 0.6: quotes and escapes removed, and every expansion
// ($VAR, ${…}, $( ), backticks, $(( )), <( )) kept as its source text, so a path that the
// shell still has to expand stays recognisable by its `$` or backtick.
func (w *walker) word(word *syntax.Word) string {
	var text strings.Builder
	for _, part := range word.Parts {
		w.part(&text, part, false)
	}
	return text.String()
}

func (w *walker) part(text *strings.Builder, part syntax.WordPart, doubleQuoted bool) {
	switch node := part.(type) {
	case *syntax.Lit:
		if doubleQuoted {
			text.WriteString(unescapeDoubleQuoted(node.Value))
		} else {
			text.WriteString(unescape(node.Value))
		}
	case *syntax.SglQuoted:
		text.WriteString(node.Value)
	case *syntax.DblQuoted:
		for _, inner := range node.Parts {
			w.part(text, inner, true)
		}
	default:
		text.WriteString(w.source[node.Pos().Offset():node.End().Offset()])
	}
}

// assignment renders NAME=value; an array or indexed assignment keeps its source text.
func (w *walker) assignment(assign *syntax.Assign) string {
	if assign.Array != nil || assign.Index != nil {
		return w.source[assign.Pos().Offset():assign.End().Offset()]
	}
	if assign.Naked {
		if assign.Name != nil {
			return assign.Name.Value
		}
		return w.word(assign.Value)
	}
	operator := "="
	if assign.Append {
		operator = "+="
	}
	value := ""
	if assign.Value != nil {
		value = w.word(assign.Value)
	}
	return assign.Name.Value + operator + value
}

// unescape drops the backslash of every escaped character outside quotes.
func unescape(literal string) string {
	if !strings.Contains(literal, `\`) {
		return literal
	}
	var text strings.Builder
	for index := 0; index < len(literal); index++ {
		if literal[index] == '\\' && index+1 < len(literal) {
			index++
		}
		text.WriteByte(literal[index])
	}
	return text.String()
}

// unescapeDoubleQuoted drops the backslash in front of the characters it escapes inside
// double quotes: $, `, ", \ and a line break; any other backslash stays.
func unescapeDoubleQuoted(literal string) string {
	if !strings.Contains(literal, `\`) {
		return literal
	}
	var text strings.Builder
	for index := 0; index < len(literal); index++ {
		if literal[index] == '\\' && index+1 < len(literal) && strings.IndexByte("$`\"\\\n", literal[index+1]) >= 0 {
			index++
			if literal[index] == '\n' {
				continue
			}
		}
		text.WriteByte(literal[index])
	}
	return text.String()
}
