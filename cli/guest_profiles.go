package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

const (
	defaultGuestProfileID  = "kali"
	fallbackGuestProfileID = "unassigned"
	guestMetadataURI       = "https://pomdock.dev/xmlns/domain/1.0"
)

// GuestProfile describes how pomdock connects to and resets a VM. Provisioning
// support is added per profile; profiles are also useful for existing VMs.
type GuestProfile struct {
	ID             string
	Label          string
	Family         string
	Provisioner    string
	SSHUser        string
	SSHKey         string
	RDPUser        string
	Snapshot       string
	ImageURL       string
	ChecksumURL    string
	ChecksumType   string
	ImageFile      string
	AdminGroup     string
	SupportsSSH    bool
	SupportsRDP    bool
	SupportsWhonix bool
}

type VMProvisionOptions struct {
	ProfileID string
	Name      string
	ISO       string
	VirtioISO string
}

var guestProfiles = map[string]GuestProfile{
	"unassigned": {
		ID: "unassigned", Label: "Unassigned", Family: "unknown", SSHUser: "kali", SSHKey: "kali",
		RDPUser: "kali", Snapshot: snapshotName, SupportsSSH: true, SupportsRDP: true, SupportsWhonix: true,
	},
	"kali": {
		ID: "kali", Label: "Kali", Family: "linux", Provisioner: "kali", SSHUser: "kali", SSHKey: "kali",
		RDPUser: "kali", Snapshot: snapshotName, SupportsSSH: true, SupportsRDP: true, SupportsWhonix: true,
	},
	"ubuntu-lts": {
		ID: "ubuntu-lts", Label: "Ubuntu 24.04 LTS", Family: "linux", Provisioner: "cloud-image",
		SSHUser: "ubuntu", SSHKey: "pomdock", Snapshot: snapshotName, SupportsSSH: true, AdminGroup: "sudo",
		ImageURL:     "https://cloud-images.ubuntu.com/releases/noble/release/ubuntu-24.04-server-cloudimg-amd64.img",
		ChecksumURL:  "https://cloud-images.ubuntu.com/releases/noble/release/SHA256SUMS",
		ChecksumType: "sha256", ImageFile: "ubuntu-24.04-server-cloudimg-amd64.img",
	},
	"debian-stable": {
		ID: "debian-stable", Label: "Debian 13", Family: "linux", Provisioner: "cloud-image",
		SSHUser: "debian", SSHKey: "pomdock", Snapshot: snapshotName, SupportsSSH: true, AdminGroup: "sudo",
		ImageURL:     "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-generic-amd64.qcow2",
		ChecksumURL:  "https://cloud.debian.org/images/cloud/trixie/latest/SHA512SUMS",
		ChecksumType: "sha512", ImageFile: "debian-13-generic-amd64.qcow2",
	},
	"rocky-9": {
		ID: "rocky-9", Label: "Rocky Linux 9", Family: "linux", Provisioner: "cloud-image",
		SSHUser: "rocky", SSHKey: "pomdock", Snapshot: snapshotName, SupportsSSH: true, AdminGroup: "wheel",
		ImageURL:     "https://download.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",
		ChecksumURL:  "https://download.rockylinux.org/pub/rocky/9/images/x86_64/Rocky-9-GenericCloud-Base.latest.x86_64.qcow2.CHECKSUM",
		ChecksumType: "sha256", ImageFile: "Rocky-9-GenericCloud-Base.latest.x86_64.qcow2",
	},
	"windows-server-2025": {
		ID: "windows-server-2025", Label: "Windows Server 2025", Family: "windows", Provisioner: "windows-iso", RDPUser: "Administrator",
		Snapshot: snapshotName, SupportsRDP: true,
	},
	"windows-11-enterprise": {
		ID: "windows-11-enterprise", Label: "Windows 11 Enterprise", Family: "windows", Provisioner: "windows-iso", RDPUser: "pomdock",
		Snapshot: snapshotName, SupportsRDP: true,
	},
}

var provisionableGuestProfileIDs = []string{
	"kali", "ubuntu-lts", "debian-stable", "rocky-9",
	"windows-server-2025", "windows-11-enterprise",
}

func ProvisionableGuestProfiles() []GuestProfile {
	profiles := make([]GuestProfile, 0, len(provisionableGuestProfileIDs))
	for _, id := range provisionableGuestProfileIDs {
		profiles = append(profiles, guestProfiles[id])
	}
	return profiles
}

type guestMetadata struct {
	XMLName xml.Name `xml:"guest"`
	Profile string   `xml:"profile,attr"`
}

func GuestProfileByID(id string) GuestProfile {
	if profile, ok := guestProfiles[id]; ok {
		return profile
	}
	return guestProfiles[fallbackGuestProfileID]
}

func GuestProfileIDs() []string {
	ids := make([]string, 0, len(guestProfiles))
	for id := range guestProfiles {
		if id == fallbackGuestProfileID {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func GetVMProfileID(name string) string {
	out, err := virsh("metadata", name, guestMetadataURI, "--config")
	if err != nil {
		return fallbackGuestProfileID
	}
	var metadata guestMetadata
	if xml.Unmarshal([]byte(out), &metadata) != nil {
		return fallbackGuestProfileID
	}
	if _, ok := guestProfiles[metadata.Profile]; !ok {
		return fallbackGuestProfileID
	}
	return metadata.Profile
}

func SetVMProfileID(name, profileID string) error {
	if profileID == fallbackGuestProfileID {
		return fmt.Errorf("%q is a display fallback, not a guest profile", profileID)
	}
	if _, ok := guestProfiles[profileID]; !ok {
		return fmt.Errorf("unknown profile %q", profileID)
	}
	payload := fmt.Sprintf(`<guest xmlns="%s" profile="%s"/>`, guestMetadataURI, profileID)
	out, err := virsh("metadata", name, guestMetadataURI, "--config", "--key", "pomdock", "--set", payload)
	if err != nil {
		return fmt.Errorf("set profile metadata on %s: %w: %s", name, err, out)
	}
	return nil
}

func CopyVMProfileID(source, destination string) error {
	profileID := GetVMProfileID(source)
	if profileID == fallbackGuestProfileID {
		return nil
	}
	return SetVMProfileID(destination, profileID)
}

func profileSSHKey(profile GuestProfile) string {
	if profile.SSHKey == "" {
		return ""
	}
	return filepath.Join(os.Getenv("HOME"), ".ssh", profile.SSHKey)
}

func PrepareVMProvision(opts VMProvisionOptions) (*exec.Cmd, GuestProfile, error) {
	profile, ok := guestProfiles[opts.ProfileID]
	if !ok || profile.Provisioner == "" {
		return nil, GuestProfile{}, fmt.Errorf("profile %q does not support creation", opts.ProfileID)
	}
	if profile.Provisioner == "kali" {
		script := vmScript("kali-libvirt-setup.sh")
		if _, err := os.Stat(script); err != nil {
			return nil, GuestProfile{}, fmt.Errorf("Kali setup script is not installed")
		}
		if opts.ISO != "" {
			return nil, GuestProfile{}, fmt.Errorf("--iso only applies to Windows profiles")
		}
		return exec.Command("bash", script, opts.Name), profile, nil
	}

	if profile.Provisioner == "windows-iso" {
		if opts.ISO == "" {
			return nil, GuestProfile{}, fmt.Errorf("%s requires an official installation ISO", profile.Label)
		}
		iso, err := existingAbsoluteFile(opts.ISO)
		if err != nil {
			return nil, GuestProfile{}, fmt.Errorf("Windows ISO: %w", err)
		}
		virtioISO := opts.VirtioISO
		if virtioISO == "" {
			virtioISO = defaultVirtioISO()
		}
		if virtioISO != "" {
			virtioISO, err = existingAbsoluteFile(virtioISO)
			if err != nil {
				return nil, GuestProfile{}, fmt.Errorf("VirtIO ISO: %w", err)
			}
		}
		script := filepath.Join(repoRoot, "vm-profiles", "windows-iso-setup.sh")
		if _, err := os.Stat(script); err != nil {
			return nil, GuestProfile{}, fmt.Errorf("Windows ISO setup script is not installed")
		}
		return exec.Command("bash", script, profile.ID, opts.Name, iso, virtioISO), profile, nil
	}

	if opts.ISO != "" {
		return nil, GuestProfile{}, fmt.Errorf("--iso only applies to Windows profiles")
	}
	script := filepath.Join(repoRoot, "vm-profiles", "linux-cloud-setup.sh")
	if _, err := os.Stat(script); err != nil {
		return nil, GuestProfile{}, fmt.Errorf("Linux cloud setup script is not installed")
	}
	cmd := exec.Command("bash", script,
		profile.ID, opts.Name, profile.ImageURL, profile.ChecksumURL, profile.ChecksumType,
		profile.ImageFile, profile.SSHUser, profile.AdminGroup)
	return cmd, profile, nil
}

func existingAbsoluteFile(path string) (string, error) {
	path = os.ExpandEnv(path)
	if len(path) >= 2 && path[:2] == "~/" {
		path = filepath.Join(os.Getenv("HOME"), path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", abs)
	}
	return abs, nil
}

func defaultVirtioISO() string {
	if configured := os.Getenv("POMDOCK_VIRTIO_ISO"); configured != "" {
		return configured
	}
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "pcm.virt", "virtio-win*.iso"),
		"/usr/share/virtio-win/virtio-win.iso",
	}
	for _, candidate := range candidates {
		matches, _ := filepath.Glob(candidate)
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1]
		}
	}
	return ""
}
