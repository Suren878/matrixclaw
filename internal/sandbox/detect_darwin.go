package sandbox

// detect uses sandbox-exec when it accepts the profiles this package writes.
func detect() Status {
	profile := seatbeltProfile(Policy{Mode: WorkspaceWrite, Scratch: []string{"/private/tmp"}, NoNetwork: true})
	if err := probe("/usr/bin/sandbox-exec", "-p", profile, "/usr/bin/true"); err != nil {
		return Status{Detail: "sandbox-exec does not work: " + err.Error()}
	}
	return Status{Mechanism: Seatbelt, Network: true, Detail: "macOS sandbox-exec"}
}
