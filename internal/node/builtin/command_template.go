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
func bindCommand(command string, msg *types.Message) (string, []string, error) {
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
