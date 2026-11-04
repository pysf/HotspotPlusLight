package routerpack

import (
	"fmt"
	"strings"
)

// ScriptConfig holds the values the router setup script is made from.
type ScriptConfig struct {
	Secret         string // settings.radius_shared_secret
	AuthPort       int    // RADIUS auth port this server listens on
	AcctPort       int    // RADIUS accounting port this server listens on
	CoaPort        int    // settings.coa_port
	InterimSeconds int    // settings.accounting_interval_seconds
	HotspotProfile string // profile name; empty = DefaultHotspotProfile
}

// placeholderIP stands for the server IP, which the admin must fill in.
const (
	placeholderIP = "<SERVER-IP>"
	// DefaultHotspotProfile is the RouterOS default name of the first hotspot profile.
	DefaultHotspotProfile = "hsprof1"
)

// routerosString quotes s as a RouterOS string literal.
func routerosString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, `?`, `\?`, "\n", `\n`, "\r", `\r`).Replace(s) + `"`
}

// SetupScript returns the RouterOS script shown on /admin/router (PLAN 9.4).
// VERIFY-ON-HARDWARE: the syntax follows the plan and the legacy script and has
// not been run on a real router yet (PLAN 13).
func SetupScript(c ScriptConfig) string {
	profile := c.HotspotProfile
	if profile == "" {
		profile = DefaultHotspotProfile
	}
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	line("# HotspotPlus Lite router setup (RouterOS). Paste into a RouterOS terminal, or save as setup.rsc and /import it.")
	line("# Every command below is VERIFY-ON-HARDWARE: it follows the plan and has not been run on a real router yet.")
	line("")
	line("# 1. RADIUS server (PAP login and accounting).")
	line("# VERIFY-ON-HARDWARE: /radius add syntax, timeout=5s.")
	line("# EDIT THEN UNCOMMENT: %s = the IP of this server as the router sees it.", placeholderIP)
	line("# /radius add service=hotspot address=%s secret=%s authentication-port=%d accounting-port=%d timeout=5s", placeholderIP, routerosString(c.Secret), c.AuthPort, c.AcctPort)
	line("")
	line("# 2. Accept Disconnect requests (the Disconnect button and automatic disconnects).")
	line("# RouterOS listens on port 1700 by default; we set the port from the settings (default 3799) so both sides match.")
	line("# VERIFY-ON-HARDWARE: /radius incoming syntax and that RouterOS accepts the Disconnect attribute set.")
	line("/radius incoming set accept=yes port=%d", c.CoaPort)
	line("")
	interim := ""
	if c.InterimSeconds > 0 {
		interim = fmt.Sprintf(" radius-interim-update=%ds", c.InterimSeconds)
		line("# 3. Hotspot profile: log in with PAP against RADIUS, send accounting every %d s.", c.InterimSeconds)
	} else {
		line("# 3. Hotspot profile: log in with PAP against RADIUS.")
	}
	line("# Replace %s with the name of your hotspot profile (see: /ip hotspot profile print).", profile)
	line("# VERIFY-ON-HARDWARE: login-by=http-pap (PAP, not CHAP, must reach RADIUS) and radius-interim-update.")
	line("/ip hotspot profile set [find name=%s] use-radius=yes login-by=http-pap radius-accounting=yes%s", profile, interim)
	line("")
	line("# 4. Login page: download a page design from the Router page of this admin panel and unzip it.")
	line("# Upload the extracted hotspot folder to the router (Files menu in WinBox, or FTP) and replace the default hotspot folder.")
	line("# On routers that keep files on flash storage, the folder is flash/hotspot.")
	return b.String()
}
