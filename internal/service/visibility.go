package service

import "github.com/Pegasus8/piworker/internal/flow"

// List returns the running tools actor may call now, and the version of the
// runtime manager's tools snapshot they were read from (it changes on every
// deploy and undeploy, so callers can cache what they build from a listing).
//
// The rules are exactly those of Call's permission check, so a listed tool is
// callable and every other one is refused: an agent needs the call scope and
// a grant whose fingerprint matches the tool and is not suspended (or
// all_tools, which never covers privileged tools), plus the privileged flag
// for privileged tools. Unlike Call, List has no side effects: a grant whose
// tool changed is hidden, not suspended.
func (t *Tools) List(actor Actor) (version uint64, tools []flow.ToolInfo) {
	version, all := t.runtimes.Tools()
	for _, ti := range all {
		if t.checkAccess(actor, ti) == nil {
			tools = append(tools, ti)
		}
	}
	return version, tools
}
