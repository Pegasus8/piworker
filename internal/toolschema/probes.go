package toolschema

import (
	"regexp/syntax"
	"slices"
	"unicode"
)

// patternProbes lists, in corpus order, the probes a pattern accepts (see
// Probes), ignoring x-piworker-allow.
//
// Each probe is a small automaton over the characters of a value, and the
// pattern accepts the probe when some value goes from the program's start to
// its match while driving that automaton to "found": a reachability search
// over (instruction, automaton state) pairs of the compiled program, so it
// covers every value the pattern accepts, not a sample of them. Zero-width
// assertions (^, $, \b…) are assumed to hold, which can only add values: the
// search errs on the side of reporting a probe.
func patternProbes(pattern string) ([]string, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return nil, err
	}
	masks := classMasks(prog)
	var out []string
	for _, c := range probeCheckers {
		if c.reachable(prog, masks) {
			out = append(out, c.probe)
		}
	}
	return out, nil
}

// Character classes the checkers tell apart. Every whitespace character
// behaves the same in every checker, and so does every line break, so the
// search runs over these classes instead of characters.
const (
	classDash = iota
	classDot
	classSlash
	classSpace // whitespace other than line breaks
	classBreak // line breaks
	classDollar
	classSemicolon
	classPipe
	classBacktick
	classOther // any other character
	numClasses
)

var (
	lineBreaks = []rune{'\n', '\r', '\u0085', ' ', ' '}
	// spaces is every Unicode space (White_Space) that is not a line break.
	spaces = func() []rune {
		var out []rune
		for _, rng := range unicode.White_Space.R16 {
			for r := rune(rng.Lo); r <= rune(rng.Hi); r += rune(rng.Stride) {
				if !slices.Contains(lineBreaks, r) {
					out = append(out, r)
				}
			}
		}
		return out
	}()
	// classRunes lists the characters of each class but classOther.
	classRunes = [numClasses - 1][]rune{
		classDash:      {'-'},
		classDot:       {'.'},
		classSlash:     {'/'},
		classSpace:     spaces,
		classBreak:     lineBreaks,
		classDollar:    {'$'},
		classSemicolon: {';'},
		classPipe:      {'|'},
		classBacktick:  {'`'},
	}
	// interesting is every character of a class but classOther.
	interesting = func() []rune {
		var set []rune
		for _, runes := range classRunes {
			set = append(set, runes...)
		}
		slices.Sort(set)
		return slices.Compact(set)
	}()
)

// classMasks returns, per instruction, the classes it can consume (bit i for
// class i); 0 for instructions that consume nothing.
func classMasks(prog *syntax.Prog) []uint16 {
	masks := make([]uint16, len(prog.Inst))
	for pc := range prog.Inst {
		inst := &prog.Inst[pc]
		switch inst.Op {
		case syntax.InstRune, syntax.InstRune1, syntax.InstRuneAny, syntax.InstRuneAnyNotNL:
		default:
			continue
		}
		var m uint16
		for class, runes := range classRunes {
			for _, r := range runes {
				if inst.MatchRune(r) {
					m |= 1 << class
					break
				}
			}
		}
		if matchesOther(inst) {
			m |= 1 << classOther
		}
		masks[pc] = m
	}
	return masks
}

// Automaton states shared by every checker: found is the accepting sink and
// dead means the value can no longer be found (the search stops there).
const (
	found = -1
	dead  = -2
)

// probeChecker is the automaton of one probe. Its own states are 0..states-1
// and it starts in 0.
type probeChecker struct {
	probe  string
	states int
	// step moves on a character of the class.
	step func(state, class int) int
	// atEnd reports whether a value ending in state is found.
	atEnd func(state int) bool
}

var probeCheckers = []probeChecker{
	prefixChecker("-x", 1),
	prefixChecker("--a=b", 2),
	dotDotSegmentChecker(),
	containsChecker("/", classSlash),
	containsChecker(" ", classSpace),
	containsChecker("\n", classBreak),
	containsChecker("$(", classDollar),
	containsChecker(";", classSemicolon),
	containsChecker("|", classPipe),
	containsChecker("`", classBacktick),
}

// containsChecker finds a value containing a character of class.
func containsChecker(probe string, class int) probeChecker {
	return probeChecker{
		probe:  probe,
		states: 1,
		step: func(_, c int) int {
			if c == class {
				return found
			}
			return 0
		},
		atEnd: func(int) bool { return false },
	}
}

// prefixChecker finds a value starting with dashes - characters.
func prefixChecker(probe string, dashes int) probeChecker {
	return probeChecker{
		probe:  probe,
		states: dashes,
		step: func(state, c int) int {
			switch {
			case c != classDash:
				return dead
			case state+1 == dashes:
				return found
			default:
				return state + 1
			}
		},
		atEnd: func(int) bool { return false },
	}
}

// dotDotSegmentChecker finds a value with a .. path segment: .. preceded by
// the start of the value or a /, and followed by its end or a /.
func dotDotSegmentChecker() probeChecker {
	const (
		segmentStart = iota // at the start of the value or after a /
		oneDot              // segment so far: "."
		twoDots             // segment so far: ".."
		inSegment           // anything else
	)
	return probeChecker{
		probe:  "..",
		states: 4,
		step: func(state, c int) int {
			switch {
			case c == classSlash && state == twoDots:
				return found
			case c == classSlash:
				return segmentStart
			case c == classDot && state == segmentStart:
				return oneDot
			case c == classDot && state == oneDot:
				return twoDots
			default:
				return inSegment
			}
		},
		atEnd: func(state int) bool { return state == twoDots },
	}
}

// reachable reports whether some value accepted by prog is found by c.
// masks are prog's classMasks.
func (c probeChecker) reachable(prog *syntax.Prog, masks []uint16) bool {
	// found is stored as one more state so that visited stays a flat slice.
	width := c.states + 1
	index := func(state int) int {
		if state == found {
			return c.states
		}
		return state
	}
	visited := make([]bool, len(prog.Inst)*width)
	type item struct {
		pc    uint32
		state int
	}
	var stack []item
	push := func(pc uint32, state int) {
		if state == dead {
			return
		}
		if k := int(pc)*width + index(state); !visited[k] {
			visited[k] = true
			stack = append(stack, item{pc, state})
		}
	}
	push(uint32(prog.Start), 0)
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		inst := &prog.Inst[it.pc]
		switch inst.Op {
		case syntax.InstAlt, syntax.InstAltMatch:
			push(inst.Out, it.state)
			push(inst.Arg, it.state)
		case syntax.InstCapture, syntax.InstNop, syntax.InstEmptyWidth:
			push(inst.Out, it.state)
		case syntax.InstMatch:
			if it.state == found || c.atEnd(it.state) {
				return true
			}
		default:
			for class, m := 0, masks[it.pc]; m != 0; class, m = class+1, m>>1 {
				if m&1 == 0 {
					continue
				}
				if it.state == found {
					push(inst.Out, found)
				} else {
					push(inst.Out, c.step(it.state, class))
				}
			}
		}
	}
	return false
}

// matchesOther reports whether a rune instruction matches some character
// outside interesting.
func matchesOther(inst *syntax.Inst) bool {
	runes := inst.Rune
	if len(runes) == 1 {
		// A literal, with its case folds when FoldCase is set.
		r0 := runes[0]
		if !slices.Contains(interesting, r0) {
			return true
		}
		if syntax.Flags(inst.Arg)&syntax.FoldCase != 0 {
			for r := unicode.SimpleFold(r0); r != r0; r = unicode.SimpleFold(r) {
				if !slices.Contains(interesting, r) {
					return true
				}
			}
		}
		return false
	}
	// Ranges lo-hi in pairs (folding is already applied).
	for i := 0; i+1 < len(runes); i += 2 {
		lo, hi := runes[i], runes[i+1]
		inside := 0
		for _, r := range interesting {
			if r >= lo && r <= hi {
				inside++
			}
		}
		if int(hi-lo)+1 > inside {
			return true
		}
	}
	return false
}
