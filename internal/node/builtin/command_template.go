package builtin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Pegasus8/piworker/internal/types"
	"mvdan.cc/sh/v3/syntax"
)

var commandPlaceholder = regexp.MustCompile(`\{\{(payload|topic|timestamp|meta\.[^{}\r\n]+)\}\}`)

// bindCommand never inserts message data into shell source. Only references to
// positional arguments are inserted, using the shell parser to determine the
// surrounding quotation context. Unsupported contexts fail before execution.
// Unless allowDash is set, a value starting with '-' is refused when it can
// begin its shell word: bound to an argument, most programs would read it as
// an option ("-rf", "--output=…"). Behind literal text in the same word
// ("temp={{payload}}", "--x={{payload}}", "/tmp/{{payload}}") it is allowed.
func bindCommand(command string, msg *types.Message, allowDash bool) (string, []string, error) {
	matches := commandPlaceholder.FindAllStringSubmatchIndex(command, -1)
	if len(matches) == 0 {
		return command, nil, nil
	}
	tree, err := syntax.NewParser().Parse(strings.NewReader(command), "command")
	if err != nil {
		return "", nil, fmt.Errorf("invalid command template: %w", err)
	}
	replacements := make([]string, len(matches))
	args := make([]string, 0, len(matches))
	var stack []syntax.Node
	var bindErr error
	syntax.Walk(tree, func(n syntax.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		quote := byte(0)
		switch node := n.(type) {
		case *syntax.Lit:
			if len(stack) > 1 {
				if _, ok := stack[len(stack)-2].(*syntax.DblQuoted); ok {
					quote = '"'
				}
			}
		case *syntax.SglQuoted:
			if node.Dollar {
				return true
			}
			quote = '\''
		default:
			return true
		}
		for i, m := range matches {
			if replacements[i] != "" || uint(m[0]) < n.Pos().Offset() || uint(m[1]) > n.End().Offset() {
				continue
			}
			for _, ancestor := range stack {
				switch node := ancestor.(type) {
				case *syntax.File, *syntax.Stmt, *syntax.CallExpr, *syntax.Word, *syntax.Lit,
					*syntax.SglQuoted, *syntax.BinaryCmd, *syntax.Block,
					*syntax.Subshell, *syntax.IfClause, *syntax.WhileClause, *syntax.ForClause,
					*syntax.WordIter, *syntax.CaseClause, *syntax.CaseItem, *syntax.FuncDecl:
				case *syntax.DblQuoted:
					if node.Dollar {
						bindErr = fmt.Errorf("placeholders are not supported in localized strings")
					}
				case *syntax.Assign:
					if node.Index != nil {
						bindErr = fmt.Errorf("placeholders are not supported in indexed assignments")
					}
				case *syntax.Redirect:
					if node.Op == syntax.Hdoc || node.Op == syntax.DashHdoc || node.Op == syntax.WordHdoc {
						bindErr = fmt.Errorf("placeholders are not supported in here-documents or here-strings")
					}
				default:
					bindErr = fmt.Errorf("placeholder %s must be in an ordinary shell argument, not a shell expansion", command[m[0]:m[1]])
				}
			}
			if m[0] > 0 && command[m[0]-1] == '\\' && quote != '\'' {
				bindErr = fmt.Errorf("do not escape command placeholders with a backslash")
			}
			key := command[m[2]:m[3]]
			value, exists := commandValue(key, msg)
			if !exists {
				// Preserve the historical missing-metadata behavior without re-expanding it.
				replacements[i] = command[m[0]:m[1]]
				continue
			}
			if strings.ContainsRune(value, 0) {
				bindErr = fmt.Errorf("command placeholder values cannot contain NUL bytes")
			}
			if !allowDash && strings.HasPrefix(value, "-") && mayBeginWord(command, stack, uint(m[0])) {
				bindErr = fmt.Errorf("the value of %s starts with \"-\" and could be read as a command option; "+
					"set allowDashArgs: true if the command expects such values (e.g. after \"--\")", command[m[0]:m[1]])
			}
			args = append(args, value)
			reference := "${" + strconv.Itoa(len(args)) + "}"
			switch quote {
			case '"':
				replacements[i] = reference
			case '\'':
				replacements[i] = "'\"" + reference + "\"'"
			default:
				replacements[i] = "\"" + reference + "\""
			}
		}
		return true
	})
	if bindErr != nil {
		return "", nil, bindErr
	}
	var result strings.Builder
	offset := 0
	for i, m := range matches {
		if replacements[i] == "" {
			return "", nil, fmt.Errorf("unsupported command placeholder context: %s", command[m[0]:m[1]])
		}
		result.WriteString(command[offset:m[0]])
		result.WriteString(replacements[i])
		offset = m[1]
	}
	result.WriteString(command[offset:])
	return result.String(), args, nil
}

// mayBeginWord reports whether the placeholder at offset off can begin the
// shell word that contains it (the innermost *syntax.Word on the walk stack):
// whether everything before it in that word may expand to "". Quotes, other
// placeholders (their values may be empty) and expansions count as possibly
// empty; so do the unquoted brace and glob characters, which can be expanded
// away ("{,}{{payload}}"). Without an enclosing word it answers true.
func mayBeginWord(command string, stack []syntax.Node, off uint) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		if w, ok := stack[i].(*syntax.Word); ok {
			return emptyBefore(command, w.Parts, off)
		}
	}
	return true
}

// emptyBefore reports whether the word parts before offset off may expand to "".
func emptyBefore(command string, parts []syntax.WordPart, off uint) bool {
	for _, part := range parts {
		start, end := part.Pos().Offset(), part.End().Offset()
		if start >= off {
			break
		}
		switch p := part.(type) {
		case *syntax.Lit:
			if hasLiteralText(command[start:min(end, off)], "{},*?[]") {
				return false
			}
		case *syntax.SglQuoted:
			inner := start + 1 // after the opening quote
			if p.Dollar {
				inner++
			}
			if stop := min(end-1, off); inner < stop && hasLiteralText(command[inner:stop], "") {
				return false
			}
		case *syntax.DblQuoted:
			if !emptyBefore(command, p.Parts, off) {
				return false
			}
		default:
			// Parameter, command and arithmetic expansions may be empty.
		}
	}
	return true
}

// hasLiteralText reports whether s, with command placeholders removed, has a
// character that is not in ignore.
func hasLiteralText(s, ignore string) bool {
	return strings.IndexFunc(commandPlaceholder.ReplaceAllString(s, ""), func(r rune) bool {
		return !strings.ContainsRune(ignore, r)
	}) >= 0
}

func commandValue(key string, msg *types.Message) (string, bool) {
	switch key {
	case "payload":
		switch value := msg.Payload.(type) {
		case string:
			return value, true
		case []byte:
			return string(value), true
		default:
			return fmt.Sprint(value), true
		}
	case "topic":
		return msg.Topic, true
	case "timestamp":
		return strconv.FormatInt(msg.Timestamp.UnixMilli(), 10), true
	default:
		value, ok := msg.Meta[strings.TrimPrefix(key, "meta.")]
		return fmt.Sprint(value), ok
	}
}
