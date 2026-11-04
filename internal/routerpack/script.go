package routerpack

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ScriptConfig is what the router setup script is generated from: the settings
// plus the RADIUS ports this process listens on.
type ScriptConfig struct {
	PublicBaseURL  string // settings.public_base_url; may be empty
	Secret         string // settings.radius_shared_secret
	AuthPort       int
	AcctPort       int
	CoaPort        int    // settings.coa_port
	InterimSeconds int    // settings.accounting_interval_seconds
	HotspotProfile string // profile name; empty = DefaultHotspotProfile
}

const (
	placeholderIP   = "<SERVER-IP>"
	placeholderPort = "<PUBLIC-PORT>"
	// DefaultHotspotProfile is the RouterOS default name of the first hotspot profile.
	DefaultHotspotProfile = "hsprof1"
)

// serverAddr splits public_base_url into host and port; both are empty when it is unset or unusable.
func serverAddr(raw string) (host, port string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", ""
	}
	port = u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return u.Hostname(), port
}

// routerosString quotes s as a RouterOS string literal.
func routerosString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, `?`, `\?`, "\n", `\n`, "\r", `\r`).Replace(s) + `"`
}

// SetupScript is the RouterOS script shown on /admin/router (PLAN 9.4). Every
// command is preceded by a comment, and every command is marked
// VERIFY-ON-HARDWARE: the syntax follows the plan and the legacy setup script
// and has not been run on a real router yet (PLAN 13).
func SetupScript(c ScriptConfig) string {
	host, port := serverAddr(c.PublicBaseURL)
	ip := placeholderIP
	if net.ParseIP(host) != nil {
		ip = host
	}
	if port == "" {
		port = placeholderPort
	}
	profile := c.HotspotProfile
	if profile == "" {
		profile = DefaultHotspotProfile
	}
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	// command prints a RouterOS command; with a placeholder in it the command is
	// printed commented out, so that a blind paste does not half-apply the setup.
	command := func(incomplete bool, format string, a ...any) {
		if incomplete {
			line("# EDIT THEN UNCOMMENT (replace the <...> parts first):")
			line("# "+format, a...)
			return
		}
		line(format, a...)
	}

	line("# HotspotPlusLight router setup (RouterOS). Paste into a RouterOS terminal, or save as setup.rsc and /import it.")
	line("# Every command below is VERIFY-ON-HARDWARE: it follows the plan and has not been run on a real router yet.")
	if ip == placeholderIP {
		line("# Replace %s with the IP address of this server (a RADIUS address cannot be a host name).", placeholderIP)
	}
	if host == "" {
		line("# Set the public base URL in Settings first: the walled garden and the login page need it.")
	}
	line("")
	line("# 1. RADIUS server (PAP login and accounting).")
	line("# VERIFY-ON-HARDWARE: /radius add syntax, timeout=5s.")
	command(ip == placeholderIP, "/radius add service=hotspot address=%s secret=%s authentication-port=%d accounting-port=%d timeout=5s", ip, routerosString(c.Secret), c.AuthPort, c.AcctPort)
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
	line("# 4. Walled garden: let not-yet-logged-in clients reach this server's public port only (never the admin port).")
	line("# VERIFY-ON-HARDWARE: walled-garden syntax; test the login page from an unauthenticated phone.")
	if ip == placeholderIP && host != "" {
		command(port == placeholderPort, "/ip hotspot walled-garden add dst-host=%s dst-port=%s action=allow", routerosString(host), port)
	} else {
		command(ip == placeholderIP || port == placeholderPort, "/ip hotspot walled-garden ip add dst-address=%s protocol=tcp dst-port=%s action=accept", ip, port)
	}
	line("")
	line("# 5. Login page: download files.zip from the Router page of this admin panel, unzip it, edit hotspot/login.html,")
	line("# then upload the content of the hotspot folder to the router (Files menu in WinBox, or FTP) into a folder named hotspot.")
	line("# VERIFY-ON-HARDWARE: html-directory and that the profile serves the uploaded login.html.")
	line("/ip hotspot profile set [find name=%s] html-directory=hotspot", profile)
	return b.String()
}
