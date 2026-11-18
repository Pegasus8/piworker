//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Access-allowed entry types whose SID follows the access mask.
const (
	accessAllowedACE         = 0x0 // ACCESS_ALLOWED_ACE_TYPE
	accessAllowedObjectACE   = 0x5 // ACCESS_ALLOWED_OBJECT_ACE_TYPE
	accessAllowedCallbackACE = 0x9 // ACCESS_ALLOWED_CALLBACK_ACE_TYPE
	accessAllowedCbObjectACE = 0xB // ACCESS_ALLOWED_CALLBACK_OBJECT_ACE_TYPE
)

// checkTokenFilePermissions refuses a token file that accounts other than
// the current user, SYSTEM and Administrators may read or change, like the
// Unix check refuses group and others (see tokenFileACL.exposedTo).
func checkTokenFilePermissions(path string, _ os.FileInfo) error {
	acl, err := readTokenFileACL(path)
	if err != nil {
		return fmt.Errorf("cannot read the permissions of token file %s: %w", path, err)
	}
	me, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("cannot check the permissions of token file %s: %w", path, err)
	}
	exposed := acl.exposedTo(me.String())
	if len(exposed) == 0 {
		return nil
	}
	names := make([]string, 0, len(exposed))
	for _, sid := range exposed {
		names = append(names, accountName(sid))
	}
	user := "${env:USERNAME}"
	if account, domain, _, err := me.LookupAccount(""); err == nil {
		user = domain + `\` + account
	}
	return fmt.Errorf(`token file %s is accessible to other users (%s); restrict it to yourself in PowerShell with: icacls "%s" /inheritance:r /grant:r "%s:(R,W)"`,
		path, strings.Join(names, ", "), path, user)
}

func readTokenFileACL(path string) (tokenFileACL, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return tokenFileACL{}, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return tokenFileACL{}, err
	}
	acl := tokenFileACL{}
	if owner != nil {
		acl.Owner = owner.String()
	}
	dacl, _, err := sd.DACL()
	switch {
	case errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND), err == nil && dacl == nil:
		acl.NoDACL = true
		return acl, nil
	case err != nil:
		return tokenFileACL{}, err
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return tokenFileACL{}, err
		}
		e := aclEntry{
			Mask:        uint32(ace.Mask),
			InheritOnly: ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0,
		}
		switch ace.Header.AceType {
		case accessAllowedACE, accessAllowedCallbackACE:
			e.Allow = true
			e.SID = (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		case accessAllowedObjectACE, accessAllowedCbObjectACE:
			// Not used on files; its SID is not read, so it counts as a
			// stranger (SID ""), and icacls /inheritance:r /grant:r drops it.
			e.Allow = true
		default:
			continue // deny and audit entries grant nothing
		}
		acl.Entries = append(acl.Entries, e)
	}
	return acl, nil
}

func currentUserSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

// accountName names a SID for messages (DOMAIN\name), or returns the SID.
func accountName(sid string) string {
	if sid == "" {
		return "an access entry the bridge cannot read"
	}
	s, err := windows.StringToSid(sid)
	if err != nil {
		return sid
	}
	account, domain, _, err := s.LookupAccount("")
	if err != nil {
		return sid
	}
	if domain == "" {
		return account
	}
	return domain + `\` + account
}
