//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// diskpartPath is the DiskPart executable. It is a core Windows component
// present on all supported editions including Windows Home.
const diskpartPath = "diskpart.exe"

// diskpartTimeout bounds how long a single diskpart invocation may run.
const diskpartTimeout = 30 * time.Second

// DiskPartBackend implements VHDXBackend using Microsoft DiskPart.
//
// DiskPart supports:
//   - CREATE VDISK (fixed/expandable) and CREATE VDISK with PARENT
//     (differencing disks)
//   - SELECT / ATTACH / DETACH VDISK
//   - LIST VDISK and DETAIL VDISK (attached VHDs only)
//
// DiskPart does NOT provide metadata inspection for unattached VHDX files.
// InspectAttached only works on currently attached VHDs.
type DiskPartBackend struct{}

// NewDiskPartBackend returns a DiskPartBackend if diskpart.exe is available.
func NewDiskPartBackend() (*DiskPartBackend, error) {
	if _, err := exec.LookPath(diskpartPath); err != nil {
		return nil, fmt.Errorf("diskpart not found: %w", err)
	}
	return &DiskPartBackend{}, nil
}

// Name returns the backend name.
func (b *DiskPartBackend) Name() string { return "diskpart" }

// IsAvailable reports whether diskpart.exe is resolvable.
func (b *DiskPartBackend) IsAvailable() bool {
	_, err := exec.LookPath(diskpartPath)
	return err == nil
}

// run executes a DiskPart script and returns its combined output. The script
// commands are written by the caller and must not contain untrusted input.
func (b *DiskPartBackend) run(commands []string) (string, error) {
	script := strings.Join(commands, "\n") + "\n"
	tmp, err := os.CreateTemp("", "mkyboot-diskpart-*.txt")
	if err != nil {
		return "", fmt.Errorf("create diskpart script: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write diskpart script: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), diskpartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, diskpartPath, "/s", tmpName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("diskpart failed: %w", err)
	}
	return string(out), nil
}

// checkSuccess verifies that diskpart output indicates success for the given
// operation. DiskPart sometimes exits 0 even when a command fails, so we
// parse the output for explicit success/failure markers.
func checkSuccess(output, operation string) error {
	low := strings.ToLower(output)
	if strings.Contains(low, "error") || strings.Contains(low, "failed") || strings.Contains(low, "not valid") {
		return fmt.Errorf("diskpart %s failed: %s", operation, strings.TrimSpace(output))
	}
	if !strings.Contains(low, "successfully") {
		return fmt.Errorf("diskpart %s did not report success: %s", operation, strings.TrimSpace(output))
	}
	return nil
}

// CreateFixed creates a fixed-size VHDX at path.
func (b *DiskPartBackend) CreateFixed(path string, sizeMB int64) error {
	out, err := b.run([]string{
		fmt.Sprintf(`create vdisk file="%s" maximum=%d type=fixed`, path, sizeMB),
		"exit",
	})
	if err != nil {
		return err
	}
	return checkSuccess(out, "create fixed vdisk")
}

// CreateExpandable creates an expandable VHDX at path.
func (b *DiskPartBackend) CreateExpandable(path string, sizeMB int64) error {
	out, err := b.run([]string{
		fmt.Sprintf(`create vdisk file="%s" maximum=%d type=expandable`, path, sizeMB),
		"exit",
	})
	if err != nil {
		return err
	}
	return checkSuccess(out, "create expandable vdisk")
}

// CreateDifferencing creates a differencing VHDX at path with parentPath as
// its parent. The parent must already exist.
func (b *DiskPartBackend) CreateDifferencing(path string, parentPath string) error {
	out, err := b.run([]string{
		fmt.Sprintf(`create vdisk file="%s" parent="%s"`, path, parentPath),
		"exit",
	})
	if err != nil {
		return err
	}
	return checkSuccess(out, "create differencing vdisk")
}

// Attach attaches the VHDX at path. readOnly mounts it read-only.
func (b *DiskPartBackend) Attach(path string, readOnly bool) error {
	readonly := ""
	if readOnly {
		readonly = " readonly"
	}
	out, err := b.run([]string{
		fmt.Sprintf(`select vdisk file="%s"`, path),
		fmt.Sprintf("attach vdisk%s", readonly),
		"exit",
	})
	if err != nil {
		return err
	}
	return checkSuccess(out, "attach vdisk")
}

// Detach detaches the VHDX at path.
func (b *DiskPartBackend) Detach(path string) error {
	out, err := b.run([]string{
		fmt.Sprintf(`select vdisk file="%s"`, path),
		"detach vdisk",
		"exit",
	})
	if err != nil {
		return err
	}
	return checkSuccess(out, "detach vdisk")
}

// Remove deletes the VHDX file at path.
func (b *DiskPartBackend) Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove vhdx %q: %w", path, err)
	}
	return nil
}

// ListAttached returns the file paths of currently attached VHDs.
func (b *DiskPartBackend) ListAttached() ([]string, error) {
	out, err := b.run([]string{
		"list vdisk",
		"exit",
	})
	if err != nil {
		return nil, err
	}
	var paths []string
	re := regexp.MustCompile(`\*?\s+\d+\s+.*\s+(.+\.vhdx?)`)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		m := re.FindStringSubmatch(line)
		if len(m) == 2 {
			paths = append(paths, strings.TrimSpace(m[1]))
		}
	}
	return paths, nil
}

// InspectAttached returns details for an attached VHD.
func (b *DiskPartBackend) InspectAttached(path string) (*VHDXInfo, error) {
	out, err := b.run([]string{
		fmt.Sprintf(`select vdisk file="%s"`, path),
		"detail vdisk",
		"exit",
	})
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(out)
	if strings.Contains(low, "error") || strings.Contains(low, "not valid") {
		return nil, fmt.Errorf("diskpart inspect vdisk failed: %s", strings.TrimSpace(out))
	}

	info := &VHDXInfo{
		Filename: path,
		Attached: true,
	}

	reIsChild := regexp.MustCompile(`(?i)is child:\s*(yes|no)`)
	if m := reIsChild.FindStringSubmatch(out); len(m) == 2 {
		info.IsChild = strings.EqualFold(m[1], "yes")
	}

	reParent := regexp.MustCompile(`(?i)parent filename:\s*(.+)`)
	if m := reParent.FindStringSubmatch(out); len(m) == 2 {
		info.ParentFile = strings.TrimSpace(m[1])
	}

	reVirtual := regexp.MustCompile(`(?i)virtual size:\s*(\d+)\s*mb`)
	if m := reVirtual.FindStringSubmatch(out); len(m) == 2 {
		var v int64
		fmt.Sscanf(m[1], "%d", &v)
		info.VirtualSize = v * 1024 * 1024
	}

	rePhysical := regexp.MustCompile(`(?i)physical size:\s*(\d+)\s*kb`)
	if m := rePhysical.FindStringSubmatch(out); len(m) == 2 {
		var v int64
		fmt.Sscanf(m[1], "%d", &v)
		info.PhysicalSize = v * 1024
	}

	return info, nil
}

// detectBackend returns a DiskPartBackend if diskpart.exe is available.
func detectBackend() (VHDXBackend, error) {
	return NewDiskPartBackend()
}
