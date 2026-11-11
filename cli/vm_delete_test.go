package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

type vmDeleteTestCall struct {
	uri  string
	args []string
}
type vmDeleteTestFixture struct {
	domains                map[string]map[string]string
	volumes                map[string]map[string]map[string]string
	snapshots              map[string]map[string]map[string]string
	live                   string
	state                  string
	fail                   map[string]bool
	calls                  []vmDeleteTestCall
	disk, base, iso, nvram string
}

func newVMDeleteFixture(t *testing.T) *vmDeleteTestFixture {
	t.Helper()
	root := t.TempDir()
	fixture := &vmDeleteTestFixture{
		domains: map[string]map[string]string{libvirtURI: {}, "qemu:///session": {}},
		volumes: map[string]map[string]map[string]string{libvirtURI: {"default": {}}, "qemu:///session": {}},
		state:   "running", fail: map[string]bool{}, snapshots: map[string]map[string]map[string]string{},
		disk: filepath.Join(root, "actual primary.qcow2"), base: filepath.Join(root, "backing-base.qcow2"),
		iso: filepath.Join(root, "readonly-installer.iso"), nvram: filepath.Join(root, "selected_VARS.fd"),
	}
	for _, path := range []string{fixture.disk, fixture.base, fixture.iso, fixture.nvram} {
		if err := os.WriteFile(path, []byte("synthetic storage"), 0400); err != nil {
			t.Fatal(err)
		}
	}
	fixture.domains[libvirtURI]["selected"] = fixture.domain(fixture.disk)
	fixture.volumes[libvirtURI]["default"]["actual primary.qcow2"] = fmt.Sprintf("<volume><name>actual primary.qcow2</name><target><path>%s</path></target><backingStore><path>%s</path></backingStore></volume>", fixture.disk, fixture.base)
	fixture.volumes[libvirtURI]["default"]["backing-base.qcow2"] = fmt.Sprintf("<volume><target><path>%s</path></target></volume>", fixture.base)
	return fixture
}

func (f *vmDeleteTestFixture) domain(path string) string {
	return fmt.Sprintf(`<domain><os><nvram>%s</nvram></os><devices><disk type="file" device="disk"><source file="%s"/><backingStore><source file="%s"/></backingStore></disk><disk type="file" device="cdrom"><source file="%s"/><readonly/></disk><tpm><backend type="emulator"/></tpm></devices></domain>`, f.nvram, path, f.base, f.iso)
}

func (f *vmDeleteTestFixture) run(uri string, args ...string) (string, error) {
	f.calls = append(f.calls, vmDeleteTestCall{uri, append([]string(nil), args...)})
	if f.fail[uri+" "+args[0]] {
		return "", fmt.Errorf("synthetic permission failure")
	}
	sortedKeys := func(values map[string]string) string {
		keys := []string{}
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return strings.Join(keys, "\n")
	}
	switch args[0] {
	case "dumpxml":
		if uri == libvirtURI && args[1] == "selected" && len(args) == 2 && f.live != "" {
			return f.live, nil
		}
		doc, ok := f.domains[uri][args[1]]
		if !ok {
			return "", fmt.Errorf("unknown fixture guest")
		}
		return doc, nil
	case "list":
		return sortedKeys(f.domains[uri]), nil
	case "snapshot-list":
		return sortedKeys(f.snapshots[uri][args[1]]), nil
	case "snapshot-dumpxml":
		return f.snapshots[uri][args[1]][args[2]], nil
	case "domstate":
		return f.state, nil
	case "pool-list":
		pools := map[string]string{}
		for pool := range f.volumes[uri] {
			pools[pool] = ""
		}
		return sortedKeys(pools), nil
	case "vol-list":
		if slices.Contains(args, "--name") {
			return "", fmt.Errorf("unsupported option --name for vol-list")
		}
		var rows []vmStorageVolumeRow
		for _, name := range vmDeleteLines(sortedKeys(f.volumes[uri][args[2]])) {
			var meta struct {
				Path string `xml:"target>path"`
			}
			_ = xml.Unmarshal([]byte(f.volumes[uri][args[2]][name]), &meta)
			if meta.Path == "" {
				meta.Path = "/synthetic/" + name
			}
			rows = append(rows, vmStorageVolumeRow{Name: name, Path: meta.Path})
		}
		return vmVolumeTestTable(rows), nil
	case "vol-pool":
		for pool, volumes := range f.volumes[uri] {
			for _, document := range volumes {
				var data struct {
					Path string `xml:"target>path"`
				}
				_ = xml.Unmarshal([]byte(document), &data)
				if data.Path == args[1] {
					return pool, nil
				}
			}
		}
		return "", fmt.Errorf("not managed")
	case "vol-dumpxml":
		volumes := f.volumes[uri][args[3]]
		if doc, ok := volumes[args[1]]; ok {
			return doc, nil
		}
		for _, doc := range volumes {
			var data struct {
				Path string `xml:"target>path"`
			}
			_ = xml.Unmarshal([]byte(doc), &data)
			if data.Path == args[1] {
				return doc, nil
			}
		}
		return "", fmt.Errorf("unknown volume")
	case "destroy", "undefine", "vol-delete":
		return "", nil
	default:
		return "", fmt.Errorf("unexpected fixture command: %q", args)
	}
}

func (f *vmDeleteTestFixture) mutations() []vmDeleteTestCall {
	var result []vmDeleteTestCall
	for _, call := range f.calls {
		if slices.Contains([]string{"destroy", "undefine", "vol-delete"}, call.args[0]) {
			result = append(result, call)
		}
	}
	return result
}

func TestDeleteVMUsesExactManagedVolumeAndKeepsMediaAndBacking(t *testing.T) {
	f := newVMDeleteFixture(t)
	if err := deleteVMWithRunner("selected", f.run); err != nil {
		t.Fatal(err)
	}
	calls := f.mutations()
	if len(calls) != 3 || calls[0].args[0] != "destroy" || calls[1].args[0] != "undefine" || calls[2].args[0] != "vol-delete" {
		t.Fatalf("unsafe deletion order: %+v", calls)
	}
	for _, flag := range []string{"--snapshots-metadata", "--nvram", "--tpm"} {
		if !slices.Contains(calls[1].args, flag) {
			t.Fatalf("missing state cleanup %s: %+v", flag, calls)
		}
	}
	if !slices.Equal(calls[2].args, []string{"vol-delete", f.disk, "--pool", "default"}) {
		t.Fatalf("wrong disk deleted: %+v", calls)
	}
	for _, path := range []string{f.base, f.iso} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated storage changed: %v", err)
		}
	}
}

func TestDeleteVMRejectsReferencesFromOtherURIsAndBackingVolumes(t *testing.T) {
	for _, kind := range []string{"other-system-guest", "same-name-session-guest", "unattached-backing-volume", "symlink-reference", "shared-nvram", "snapshot-reference", "relative-backing-volume"} {
		t.Run(kind, func(t *testing.T) {
			f := newVMDeleteFixture(t)
			switch kind {
			case "other-system-guest":
				f.domains[libvirtURI]["other"] = f.domain(f.disk)
			case "same-name-session-guest":
				f.domains["qemu:///session"]["selected"] = f.domain(f.disk)
			case "unattached-backing-volume":
				f.volumes[libvirtURI]["default"]["unattached.qcow2"] = fmt.Sprintf("<volume><backingStore><path>%s</path></backingStore></volume>", f.disk)
			case "symlink-reference":
				alias := filepath.Join(t.TempDir(), "alias.qcow2")
				if err := os.Symlink(f.disk, alias); err != nil {
					t.Fatal(err)
				}
				f.domains["qemu:///session"]["other"] = fmt.Sprintf("<domain><devices><disk><source file=%q/></disk></devices></domain>", alias)
			case "snapshot-reference":
				f.domains["qemu:///session"]["other"] = "<domain><devices/></domain>"
				f.snapshots["qemu:///session"] = map[string]map[string]string{"other": {"old-state": fmt.Sprintf(`<domainsnapshot><domain><devices><disk><source file="%s"/></disk></devices></domain></domainsnapshot>`, f.disk)}}
			case "relative-backing-volume":
				f.volumes[libvirtURI]["default"]["relative.qcow2"] = fmt.Sprintf("<volume><target><path>%s</path></target><backingStore><path>%s</path></backingStore></volume>", filepath.Join(filepath.Dir(f.disk), "relative.qcow2"), filepath.Base(f.disk))
			case "shared-nvram":
				f.domains["qemu:///session"]["other"] = fmt.Sprintf("<domain><os><nvram>%s</nvram></os></domain>", f.nvram)
			}
			if err := deleteVMWithRunner("selected", f.run); err == nil {
				t.Fatal("shared storage accepted")
			}
			if len(f.mutations()) != 0 {
				t.Fatalf("mutated before shared storage validation: %+v", f.mutations())
			}
		})
	}
}

func TestDeleteVMValidationFailuresNeverMutate(t *testing.T) {
	for _, kind := range []string{"unmanaged-disk", "unreadable-session", "unreadable-pool", "shareable-disk", "non-file-disk", "active-disk-mismatch", "volume-path-mismatch", "multiple-writable-disks"} {
		t.Run(kind, func(t *testing.T) {
			f := newVMDeleteFixture(t)
			switch kind {
			case "unmanaged-disk":
				f.fail[libvirtURI+" vol-pool"] = true
			case "unreadable-session":
				f.fail["qemu:///session list"] = true
			case "unreadable-pool":
				f.fail[libvirtURI+" vol-list"] = true
			case "shareable-disk":
				f.domains[libvirtURI]["selected"] = strings.Replace(f.domain(f.disk), "<source", "<shareable/><source", 1)
			case "non-file-disk":
				f.domains[libvirtURI]["selected"] = strings.Replace(f.domain(f.disk), `type="file"`, `type="block"`, 1)
			case "active-disk-mismatch":
				f.live = f.domain(f.base)
			case "multiple-writable-disks":
				f.domains[libvirtURI]["selected"] = strings.Replace(f.domain(f.disk), "</devices>", fmt.Sprintf(`<disk type="file" device="disk"><source file="%s"/></disk></devices>`, f.base), 1)
			case "volume-path-mismatch":
				f.volumes[libvirtURI]["default"]["actual primary.qcow2"] = fmt.Sprintf("<volume><target><path>%s</path></target></volume>", f.base)
			}
			if err := deleteVMWithRunner("selected", f.run); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			if len(f.mutations()) != 0 {
				t.Fatalf("preflight mutated VM: %+v", f.mutations())
			}
		})
	}
}

func TestDeleteVMReportsPartialFailuresWithoutFilesystemFallback(t *testing.T) {
	for _, command := range []string{"destroy", "undefine", "vol-delete"} {
		t.Run(command, func(t *testing.T) {
			f := newVMDeleteFixture(t)
			f.fail[libvirtURI+" "+command] = true
			err := deleteVMWithRunner("selected", f.run)
			if err == nil {
				t.Fatal("backend failure ignored")
			}
			if command == "vol-delete" && (!strings.Contains(err.Error(), "undefined") || !strings.Contains(err.Error(), f.disk) || !strings.Contains(err.Error(), "pool default")) {
				t.Fatalf("unclear retained-storage error: %v", err)
			}
			mutations := f.mutations()
			for _, call := range mutations {
				if call.args[0] == "vol-delete" && command != "vol-delete" {
					t.Fatalf("disk deleted after failed %s: %+v", command, mutations)
				}
			}
			if _, statErr := os.Stat(f.disk); statErr != nil {
				t.Fatalf("local filesystem fallback removed disk: %v", statErr)
			}
		})
	}
}

func TestDeleteVMStoppedGuestDoesNotNeedDestroy(t *testing.T) {
	f := newVMDeleteFixture(t)
	f.state = "shut off"
	if err := deleteVMWithRunner("selected", f.run); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.mutations() {
		if call.args[0] == "destroy" {
			t.Fatal("destroy called for stopped guest")
		}
	}
}

func TestDeleteVMStorageInventoryIgnoresDirectNetworkInterfaces(t *testing.T) {
	f := newVMDeleteFixture(t)
	document := fmt.Sprintf(`<domain><devices><interface type="direct"><source dev="enp16s0" mode="bridge"/><model type="virtio"/></interface><disk type="file" device="disk"><source file="%s"/><target dev="vda"/></disk></devices></domain>`, f.base)
	refs, err := vmXMLFileReferences(document, libvirtURI, f.run)
	if err != nil || !slices.Equal(refs, []string{f.base}) {
		t.Fatalf("network source confused with storage: %q, %v", refs, err)
	}
	f.domains[libvirtURI]["unrelated-direct-interface"] = document
	if err := deleteVMWithRunner("selected", f.run); err != nil {
		t.Fatalf("unrelated interface blocked validated storage: %v", err)
	}
	for _, call := range f.mutations() {
		if call.args[0] == "vol-delete" && call.args[1] != f.disk {
			t.Fatalf("unrelated storage selected: %+v", call)
		}
	}
}

// Model virsh's supported two-column C-locale table, including display padding.
func vmVolumeTestTable(rows []vmStorageVolumeRow) string {
	width := 25
	for _, row := range rows {
		if size := ansi.StringWidth(row.Name); size > width {
			width = size
		}
	}
	result := " Name" + strings.Repeat(" ", width-4+2) + "Path\n" + strings.Repeat("-", width+80) + "\n"
	for _, row := range rows {
		result += " " + row.Name + strings.Repeat(" ", width-ansi.StringWidth(row.Name)+2) + row.Path + "\n"
	}
	return result
}

func TestVMVolumeTablePreservesSpacesAndUnicodeDisplayColumns(t *testing.T) {
	want := []vmStorageVolumeRow{
		{Name: "actual primary.qcow2", Path: "/var/lib/libvirt/images/actual primary.qcow2"},
		{Name: "Überblick 東京.qcow2", Path: "/srv/virtual machines/Überblick 東京.qcow2"},
		{Name: "an exceptionally long volume name.qcow2", Path: "/srv/virtual machines/other disk.qcow2"},
	}
	got, err := parseVMVolumeTable(vmVolumeTestTable(want))
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("volume table changed names/paths: %+v %v", got, err)
	}
	got, err = parseVMVolumeTable(vmVolumeTestTable(nil))
	if err != nil || len(got) != 0 {
		t.Fatalf("empty pool table rejected: %+v %v", got, err)
	}
}

func TestVMVolumeTableRejectsMalformedOrAmbiguousRows(t *testing.T) {
	valid := vmVolumeTestTable([]vmStorageVolumeRow{{Name: "disk", Path: "/srv/disk"}})
	for _, table := range []string{"", "Name Path\n", strings.Replace(valid, "Path", "Unexpected", 1), strings.Replace(valid, "----", "oops", 1), strings.Replace(valid, "/srv/disk", "", 1), strings.Replace(valid, "/srv/disk", " /srv/disk", 1), valid + " disk /bad/columns\n", valid + strings.Split(valid, "\n")[2] + "\n", strings.Replace(valid, "/srv/disk", "/srv/disk\tname", 1), strings.Replace(valid, "disk", "disk\xff", 1)} {
		if rows, err := parseVMVolumeTable(table); err == nil {
			t.Fatalf("malformed table accepted: %q -> %+v", table, rows)
		}
	}
}

func TestVMDeleteRunnerKeepsUTF8LocaleAndTablePadding(t *testing.T) {
	bin := t.TempDir()
	table := vmVolumeTestTable([]vmStorageVolumeRow{{Name: "Überblick 東京.qcow2", Path: "/srv/virtual machines/Überblick 東京.qcow2"}})
	t.Setenv("LC_ALL", "C")
	t.Setenv("POMDOCK_TEST_VOLUME_TABLE", table)
	t.Setenv("PATH", bin)
	script := `#!/bin/sh
[ "$LC_ALL" = C.UTF-8 ] || exit 91
for arg in "$@"; do [ "$arg" != --name ] || exit 92; done
printf '%s' "$POMDOCK_TEST_VOLUME_TABLE"
`
	if err := os.WriteFile(filepath.Join(bin, "virsh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	output, err := runVMDeleteCommand(libvirtURI, "vol-list", "--pool", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if output != strings.Trim(table, "\r\n") {
		t.Fatalf("table alignment was trimmed: %q", output)
	}
	rows, err := parseVMVolumeTable(output)
	if err != nil || len(rows) != 1 || rows[0].Name != "Überblick 東京.qcow2" {
		t.Fatalf("UTF-8 table was corrupted: %+v %v", rows, err)
	}
}
