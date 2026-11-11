package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

type vmDeleteRunner func(uri string, args ...string) (string, error)

type vmDeleteVolume struct{ Path, Pool string }
type vmDeletionPlan struct {
	Name       string
	Volumes    []vmDeleteVolume
	NVRAM, TPM bool
}

type vmDeleteDomain struct {
	XMLName xml.Name `xml:"domain"`
	OS      struct {
		NVRAM *struct {
			Path   string `xml:",chardata"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"nvram"`
	} `xml:"os"`
	Devices struct {
		Disks []struct {
			Type   string `xml:"type,attr"`
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			ReadOnly  *struct{} `xml:"readonly"`
			Shareable *struct{} `xml:"shareable"`
		} `xml:"disk"`
		TPMs []struct {
			Backend struct {
				Type string `xml:"type,attr"`
			} `xml:"backend"`
		} `xml:"tpm"`
	} `xml:"devices"`
}

// Use the actual domain disk sources, never a filename guessed from its name.
func vmTopDiskPaths(document string) ([]string, bool, bool, error) {
	var domain vmDeleteDomain
	if err := xml.Unmarshal([]byte(document), &domain); err != nil {
		return nil, false, false, err
	}
	paths, seen := []string{}, map[string]bool{}
	writableDisks := 0
	for _, disk := range domain.Devices.Disks {
		if disk.Device != "disk" || disk.ReadOnly != nil {
			continue
		}
		writableDisks++
		if writableDisks > 1 {
			return nil, false, false, fmt.Errorf("multiple writable disks require manual ownership review; nothing deleted")
		}
		if disk.Type != "file" || disk.Source.File == "" || disk.Shareable != nil {
			return nil, false, false, fmt.Errorf("only exclusive writable file disks can be deleted automatically")
		}
		path, err := canonicalVMDiskPath(disk.Source.File)
		if err != nil {
			return nil, false, false, err
		}
		if !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}
	tpm := false
	for _, device := range domain.Devices.TPMs {
		tpm = tpm || device.Backend.Type == "emulator"
	}
	return paths, domain.OS.NVRAM != nil, tpm, nil
}

func canonicalVMDiskPath(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return "", fmt.Errorf("invalid VM disk path %q", path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("cannot verify disk %s: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("VM disk must be an existing regular file: %s", path)
	}
	return filepath.Clean(resolved), nil
}

func runVMDeleteCommand(uri string, args ...string) (string, error) {
	cmd := exec.Command("virsh", append([]string{"--connect", uri}, args...)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("virsh %s: %w (%s)", args[0], err, text)
	}
	if args[0] == "vol-list" {
		return strings.Trim(string(output), "\r\n"), nil
	}
	return text, nil
}

type vmStorageVolumeRow struct{ Name, Path string }

// virsh vol-list has no --name/JSON option. Its C.UTF-8 header defines
// display-cell boundaries; filenames may contain spaces and wide characters.
func parseVMVolumeTable(table string) ([]vmStorageVolumeRow, error) {
	if !utf8.ValidString(table) {
		return nil, fmt.Errorf("malformed libvirt volume table: invalid UTF-8")
	}
	lines := strings.Split(strings.Trim(table, "\n"), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("malformed libvirt volume table: missing header")
	}
	header := lines[0]
	nameColumn, pathColumn := strings.Index(header, "Name"), strings.Index(header, "Path")
	if nameColumn < 0 || pathColumn <= nameColumn+4 || strings.TrimSpace(header[:pathColumn]) != "Name" || strings.TrimSpace(header[pathColumn:]) != "Path" {
		return nil, fmt.Errorf("malformed libvirt volume table: expected Name/Path columns")
	}
	separator := strings.TrimSpace(lines[1])
	if len(separator) < pathColumn+4 || strings.Trim(separator, "-") != "" {
		return nil, fmt.Errorf("malformed libvirt volume table: missing separator")
	}
	rows, seen := []vmStorageVolumeRow{}, map[string]bool{}
	for _, line := range lines[2:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, r := range line {
			if unicode.IsControl(r) {
				return nil, fmt.Errorf("malformed libvirt volume table: control character")
			}
		}
		if ansi.StringWidth(line) <= pathColumn || strings.TrimSpace(ansi.Cut(line, 0, nameColumn)) != "" {
			return nil, fmt.Errorf("malformed libvirt volume table: misaligned row")
		}
		nameField, pathField := ansi.Cut(line, nameColumn, pathColumn), ansi.Cut(line, pathColumn, ansi.StringWidth(line))
		name, path := strings.TrimSpace(nameField), strings.TrimRight(pathField, " ")
		if name == "" || path == "" || strings.HasPrefix(path, " ") || !strings.HasSuffix(nameField, "  ") || seen[name] {
			return nil, fmt.Errorf("malformed libvirt volume table: ambiguous row")
		}
		seen[name] = true
		rows = append(rows, vmStorageVolumeRow{Name: name, Path: path})
	}
	return rows, nil
}

func vmDeleteLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Collect file/device sources, volume sources and NVRAM, including backingStore
// sources. CD-ROM sources protect other guests' media but are never deletion targets.
func vmXMLFileReferences(document, uri string, run vmDeleteRunner) ([]string, error) {
	decoder := xml.NewDecoder(strings.NewReader(document))
	var refs, stack []string
	var volume struct {
		Path string `xml:"target>path"`
	}
	if err := xml.Unmarshal([]byte(document), &volume); err != nil {
		return nil, err
	}
	appendReference := func(path string) error {
		if !filepath.IsAbs(path) {
			if !filepath.IsAbs(volume.Path) {
				return fmt.Errorf("cannot verify relative storage reference %q", path)
			}
			path = filepath.Join(filepath.Dir(volume.Path), path)
		}
		refs = append(refs, filepath.Clean(path))
		return nil
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return refs, nil
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			stack = append(stack, value.Name.Local)
			if value.Name.Local == "source" {
				storage := false
				for _, parent := range stack[:len(stack)-1] {
					if parent == "disk" || parent == "backingStore" || parent == "nvram" {
						storage = true
						break
					}
				}
				if !storage {
					continue
				}
				attrs := map[string]string{}
				for _, attr := range value.Attr {
					attrs[attr.Name.Local] = attr.Value
				}
				for _, key := range []string{"file", "dev"} {
					if attrs[key] != "" {
						if err := appendReference(attrs[key]); err != nil {
							return nil, err
						}
					}
				}
				if attrs["pool"] != "" && attrs["volume"] != "" {
					path, err := run(uri, "vol-path", attrs["volume"], "--pool", attrs["pool"])
					if err != nil {
						return nil, err
					}
					if err := appendReference(path); err != nil {
						return nil, err
					}
				}
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			last, text := stack[len(stack)-1], strings.TrimSpace(string(value))
			if text == "" {
				continue
			}
			if last == "nvram" {
				if err := appendReference(text); err != nil {
					return nil, err
				}
			}
			if last == "path" {
				for _, parent := range stack[:len(stack)-1] {
					if parent == "backingStore" {
						if err := appendReference(text); err != nil {
							return nil, err
						}
						break
					}
				}
			}
		}
	}
}

func vmReferenceMatches(path string, candidates map[string]bool) bool {
	if candidates[filepath.Clean(path)] {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && candidates[filepath.Clean(resolved)]
}

func planVMDeletion(name string, run vmDeleteRunner) (vmDeletionPlan, error) {
	plan := vmDeletionPlan{Name: name}
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
		return plan, fmt.Errorf("invalid VM name")
	}
	document, err := run(libvirtURI, "dumpxml", name, "--inactive")
	if err != nil {
		return plan, fmt.Errorf("cannot inspect VM before deletion: %w", err)
	}
	paths, nvram, tpm, err := vmTopDiskPaths(document)
	if err != nil {
		return plan, err
	}
	live, err := run(libvirtURI, "dumpxml", name)
	if err != nil {
		return plan, fmt.Errorf("cannot inspect current VM disks: %w", err)
	}
	livePaths, _, _, err := vmTopDiskPaths(live)
	if err != nil {
		return plan, err
	}
	liveSet := map[string]bool{}
	for _, path := range livePaths {
		liveSet[path] = true
	}
	if len(paths) != len(livePaths) {
		return plan, fmt.Errorf("active and inactive disk sources differ; resolve them before deleting VM")
	}
	for _, path := range paths {
		if !liveSet[path] {
			return plan, fmt.Errorf("active and inactive disk sources differ; resolve them before deleting VM")
		}
	}
	plan.NVRAM, plan.TPM = nvram, tpm
	protected := map[string]bool{}
	for _, path := range paths {
		pool, err := run(libvirtURI, "vol-pool", path)
		if err != nil || strings.TrimSpace(pool) == "" {
			return plan, fmt.Errorf("disk is not a managed libvirt volume: %s", path)
		}
		volume, err := run(libvirtURI, "vol-dumpxml", path, "--pool", pool)
		if err != nil {
			return plan, err
		}
		var meta struct {
			Path string `xml:"target>path"`
		}
		if err := xml.Unmarshal([]byte(volume), &meta); err != nil {
			return plan, err
		}
		actual, err := canonicalVMDiskPath(meta.Path)
		if err != nil || actual != path {
			return plan, fmt.Errorf("libvirt volume does not match domain disk %s", path)
		}
		plan.Volumes = append(plan.Volumes, vmDeleteVolume{Path: path, Pool: pool})
		protected[path] = true
	}
	// Only NVRAM is removed by undefine; backing disks are intentionally retained.
	if nvram {
		var data vmDeleteDomain
		if err := xml.Unmarshal([]byte(document), &data); err != nil {
			return plan, err
		}
		path := strings.TrimSpace(data.OS.NVRAM.Path)
		if path == "" {
			path = data.OS.NVRAM.Source.File
		}
		if path == "" {
			return plan, fmt.Errorf("cannot verify NVRAM storage path")
		}
		protected[filepath.Clean(path)] = true
	}
	for _, uri := range []string{libvirtURI, "qemu:///session"} {
		names, err := run(uri, "list", "--all", "--name")
		if err != nil {
			return plan, fmt.Errorf("cannot check shared guest storage in %s: %w", uri, err)
		}
		for _, other := range vmDeleteLines(names) {
			if uri == libvirtURI && other == name {
				continue
			}
			for _, options := range [][]string{{"dumpxml", other}, {"dumpxml", other, "--inactive"}} {
				otherXML, err := run(uri, options...)
				if err != nil {
					return plan, fmt.Errorf("cannot check storage used by %s/%s: %w", uri, other, err)
				}
				refs, err := vmXMLFileReferences(otherXML, uri, run)
				if err != nil {
					return plan, err
				}
				for _, ref := range refs {
					if vmReferenceMatches(ref, protected) {
						return plan, fmt.Errorf("refusing deletion: %s is also referenced by %s/%s", ref, uri, other)
					}
				}
			}
			snapshots, err := run(uri, "snapshot-list", other, "--name")
			if err != nil {
				return plan, fmt.Errorf("cannot inspect snapshots for %s/%s: %w", uri, other, err)
			}
			for _, snapshot := range vmDeleteLines(snapshots) {
				document, err := run(uri, "snapshot-dumpxml", other, snapshot)
				if err != nil {
					return plan, err
				}
				refs, err := vmXMLFileReferences(document, uri, run)
				if err != nil {
					return plan, err
				}
				for _, ref := range refs {
					if vmReferenceMatches(ref, protected) {
						return plan, fmt.Errorf("refusing deletion: %s is referenced by snapshot %s of %s/%s", ref, snapshot, uri, other)
					}
				}
			}
		}
		pools, err := run(uri, "pool-list", "--all", "--name")
		if err != nil {
			return plan, fmt.Errorf("cannot check backing volumes in %s: %w", uri, err)
		}
		for _, pool := range vmDeleteLines(pools) {
			table, err := run(uri, "vol-list", "--pool", pool)
			if err != nil {
				return plan, fmt.Errorf("cannot inspect storage pool %s: %w", pool, err)
			}
			volumes, err := parseVMVolumeTable(table)
			if err != nil {
				return plan, fmt.Errorf("cannot inspect storage pool %s: %w", pool, err)
			}
			for _, volume := range volumes {
				xmlText, err := run(uri, "vol-dumpxml", volume.Name, "--pool", pool)
				if err != nil {
					return plan, err
				}
				refs, err := vmXMLFileReferences(xmlText, uri, run)
				if err != nil {
					return plan, err
				}
				for _, ref := range refs {
					if vmReferenceMatches(ref, protected) {
						return plan, fmt.Errorf("refusing deletion: %s backs another volume %s/%s", ref, pool, volume.Name)
					}
				}
			}
		}
	}
	return plan, nil
}

func deleteVMWithRunner(name string, run vmDeleteRunner) error {
	plan, err := planVMDeletion(name, run)
	if err != nil {
		return err
	}
	state, err := run(libvirtURI, "domstate", name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(state) != "shut off" {
		if _, err := run(libvirtURI, "destroy", name); err != nil {
			return fmt.Errorf("could not stop VM; nothing deleted: %w", err)
		}
	}
	args := []string{"undefine", name, "--snapshots-metadata"}
	if plan.NVRAM {
		args = append(args, "--nvram")
	}
	if plan.TPM {
		args = append(args, "--tpm")
	}
	if _, err := run(libvirtURI, args...); err != nil {
		return fmt.Errorf("could not undefine VM; disks retained: %w", err)
	}
	for index, volume := range plan.Volumes {
		if _, err := run(libvirtURI, "vol-delete", volume.Path, "--pool", volume.Pool); err != nil {
			var retained []string
			for _, remaining := range plan.Volumes[index:] {
				retained = append(retained, fmt.Sprintf("%s (pool %s)", remaining.Path, remaining.Pool))
			}
			return fmt.Errorf("VM %q undefined; %d/%d disk volumes deleted; retained %s: %w", name, index, len(plan.Volumes), strings.Join(retained, ", "), err)
		}
	}
	return nil
}
