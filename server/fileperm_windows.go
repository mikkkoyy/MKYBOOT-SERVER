//go:build windows

package main

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// restrictFileToOwner replaces the file's DACL with one that grants full
// access only to the current user and is protected (non-inheriting). This is
// the correct way to achieve "owner-only" permissions on Windows, where
// os.Chmod only toggles the read-only attribute and cannot express 0600.
func restrictFileToOwner(path string) error {
	tok := windows.GetCurrentProcessToken()
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return fmt.Errorf("get token user: %w", err)
	}
	sid := u.User.Sid.String()
	// Protected DACL (D:P) with a single ACCESS_ALLOWED ACE granting full
	// access (FA) to the owner SID. No inheritance from the parent directory.
	sddl := "O:" + sid + "G:" + sid + "D:P(A;;FA;;;" + sid + ")"
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("descriptor owner: %w", err)
	}
	group, _, err := sd.Group()
	if err != nil {
		return fmt.Errorf("descriptor group: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("descriptor dacl: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner,
		group,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("set file security: %w", err)
	}
	return nil
}

// fileIsOwnerOnly reports whether the file's DACL is protected and grants no
// access to well-known non-owner principals (Everyone "WD", Users "BU",
// Authenticated Users "AU", Anonymous "AN"). It is the Windows counterpart to
// checking a 0600 mode on Unix.
func fileIsOwnerOnly(path string) (bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateFile(p, windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	s := sd.String()
	if !strings.Contains(s, "D:P") {
		return false, nil // inheritable / not protected
	}
	for _, bad := range []string{"WD", "BU", "AU", "AN"} {
		if strings.Contains(s, ";;;"+bad+")") {
			return false, nil // grants access to a broad group
		}
	}
	return true, nil
}
