package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Windows SIDs used by the access-list tests.
const (
	sidMe            = "S-1-5-21-1004336348-1177238915-682003330-1001"
	sidOtherUser     = "S-1-5-21-1004336348-1177238915-682003330-1002"
	sidSystem        = "S-1-5-18"
	sidAdmins        = "S-1-5-32-544"
	sidUsers         = "S-1-5-32-545"
	sidEveryone      = "S-1-1-0"
	sidAuthenticated = "S-1-5-11"
	sidOwnerRights   = "S-1-3-4"
)

func allow(sid string, mask uint32) aclEntry { return aclEntry{SID: sid, Allow: true, Mask: mask} }

func TestTokenFileACL(t *testing.T) {
	const (
		fullControl = 0x1F01FF // FILE_ALL_ACCESS
		readWrite   = 0x12019F // icacls (R,W)
		readOnly    = 0x120089 // FILE_GENERIC_READ
		modify      = 0x1301BF
		// READ_CONTROL | SYNCHRONIZE | FILE_READ_ATTRIBUTES: cannot read the content.
		attributesOnly = 0x120080
	)
	// What a file in %USERPROFILE% inherits.
	profile := []aclEntry{allow(sidSystem, fullControl), allow(sidAdmins, fullControl), allow(sidMe, fullControl)}

	for name, tc := range map[string]struct {
		acl  tokenFileACL
		want []string
	}{
		"inherited from the profile folder":  {tokenFileACL{Owner: sidMe, Entries: profile}, nil},
		"restricted with icacls":             {tokenFileACL{Owner: sidMe, Entries: []aclEntry{allow(sidMe, readWrite)}}, nil},
		"owned by Administrators":            {tokenFileACL{Owner: sidAdmins, Entries: profile}, nil},
		"owner rights":                       {tokenFileACL{Owner: sidMe, Entries: []aclEntry{allow(sidOwnerRights, fullControl)}}, nil},
		"Users may read":                     {tokenFileACL{Owner: sidMe, Entries: append(profile, allow(sidUsers, readOnly))}, []string{sidUsers}},
		"Everyone generic read":              {tokenFileACL{Owner: sidMe, Entries: append(profile, allow(sidEveryone, 0x80000000))}, []string{sidEveryone}},
		"Authenticated Users may modify":     {tokenFileACL{Owner: sidMe, Entries: append(profile, allow(sidAuthenticated, modify))}, []string{sidAuthenticated}},
		"another user may change the access": {tokenFileACL{Owner: sidMe, Entries: append(profile, allow(sidOtherUser, 0x40000))}, []string{sidOtherUser}},
		"another user owns it":               {tokenFileACL{Owner: sidOtherUser, Entries: []aclEntry{allow(sidMe, readOnly)}}, []string{sidOtherUser}},
		"no access list":                     {tokenFileACL{Owner: sidMe, NoDACL: true}, []string{sidEveryone}},
		"attributes only":                    {tokenFileACL{Owner: sidMe, Entries: append(profile, allow(sidUsers, attributesOnly))}, nil},
		"deny entries take access away":      {tokenFileACL{Owner: sidMe, Entries: append(profile, aclEntry{SID: sidEveryone, Mask: fullControl})}, nil},
		"entries for children only":          {tokenFileACL{Owner: sidMe, Entries: append(profile, aclEntry{SID: sidUsers, Allow: true, InheritOnly: true, Mask: fullControl})}, nil},
		"each stranger once": {tokenFileACL{Owner: sidMe, Entries: append(profile,
			allow(sidUsers, readOnly), allow(sidUsers, modify), allow(sidEveryone, readOnly))}, []string{sidUsers, sidEveryone}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.acl.exposedTo(sidMe))
		})
	}
}
