package main

import "slices"

// tokenFileACL is a Windows file's owner and access list, as far as the token
// file check needs them. token_windows.go reads it; the rule itself is plain
// Go so that it is tested on every platform.
type tokenFileACL struct {
	Owner   string     // the owner's SID, e.g. "S-1-5-21-…-1001"
	NoDACL  bool       // the file has no access list: everyone has full access
	Entries []aclEntry // the access list, in order
}

// aclEntry is one entry of an access list.
type aclEntry struct {
	SID         string
	Allow       bool // an access-allowed entry; deny entries only take access away
	InheritOnly bool // applies to files created in a folder, not to this one
	Mask        uint32
}

// Windows access rights that expose the token, replace it, or let someone
// give themselves either.
const (
	accessReadData   = 0x00000001 // FILE_READ_DATA
	accessWriteData  = 0x00000002 // FILE_WRITE_DATA
	accessAppendData = 0x00000004 // FILE_APPEND_DATA
	accessWriteDAC   = 0x00040000 // WRITE_DAC
	accessWriteOwner = 0x00080000 // WRITE_OWNER
	accessGenericAll = 0x10000000 // GENERIC_ALL
	accessGenericW   = 0x40000000 // GENERIC_WRITE
	accessGenericR   = 0x80000000 // GENERIC_READ

	exposingAccess = accessReadData | accessWriteData | accessAppendData | accessWriteDAC |
		accessWriteOwner | accessGenericAll | accessGenericW | accessGenericR
)

const everyoneSID = "S-1-1-0"

// alwaysTrusted may access the token file: the local system and the
// administrators (who can read any file anyway), and OWNER RIGHTS, which
// stands for the owner (checked on its own).
var alwaysTrusted = []string{"S-1-5-18", "S-1-5-32-544", "S-1-3-4"}

// exposedTo lists the accounts other than me, the system and the
// administrators that can read or change the token file: its owner when that
// is someone else, and everyone an entry grants such access. A file without
// access list is open to Everyone (S-1-1-0).
func (a tokenFileACL) exposedTo(me string) []string {
	trusted := func(sid string) bool { return sid == me || slices.Contains(alwaysTrusted, sid) }
	var out []string
	add := func(sid string) {
		if !slices.Contains(out, sid) {
			out = append(out, sid)
		}
	}
	if !trusted(a.Owner) {
		add(a.Owner)
	}
	if a.NoDACL {
		add(everyoneSID)
		return out
	}
	for _, e := range a.Entries {
		if e.Allow && !e.InheritOnly && e.Mask&exposingAccess != 0 && !trusted(e.SID) {
			add(e.SID)
		}
	}
	return out
}
