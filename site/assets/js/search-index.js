// Hand-made search index, built from the h2 sections of the docs pages (a test keeps it in step).
export default [
 {
  "page": "Introduction",
  "heading": "What it is",
  "url": "docs/#what-it-is",
  "keywords": "HotspotPlus Lite manages Wi-Fi accounts for one MikroTik hotspot router . You make plans (data, time, validity, speed, number of devices) and members , or you print a batch of vouchers . At every login the router asks the program, and the program says yes or n"
 },
 {
  "page": "Introduction",
  "heading": "How the parts fit",
  "url": "docs/#parts",
  "keywords": "The router runs the hotspot and stores the login page. It sends each login and each accounting packet to the server by RADIUS. HotspotPlus Lite runs on a computer on the same network. It answers RADIUS on UDP 1812 and 1813, serves the admin panel on TCP 8081, "
 },
 {
  "page": "Introduction",
  "heading": "What you need",
  "url": "docs/#requirements",
  "keywords": "A MikroTik router with the Hotspot feature already working. A computer that is always on, on the same network as the router: Linux, Windows or macOS (x86 or ARM). UDP 1812 and 1813 open from the router to that computer, and UDP 3799 open from that computer to "
 },
 {
  "page": "Introduction",
  "heading": "What it does not do",
  "url": "docs/#limits",
  "keywords": "One router and one admin account. No multi-tenant mode. MikroTik routers only, and PAP login only. If the router uses CHAP, the server rejects the login. No payments, SMS, self-signup or plan choice by members. The admin panel and all messages are in English. "
 },
 {
  "page": "Introduction",
  "heading": "Where to start",
  "url": "docs/#read-next",
  "keywords": "Install the program and start it once. Make it run as a service . Log in , set a password, and create a plan and a member. Follow the router setup and upload a login page . Keep backup and upgrade and troubleshooting close."
 },
 {
  "page": "Introduction",
  "heading": "Where it comes from",
  "url": "docs/#origin",
  "keywords": "HotspotPlus Lite is a rewrite of HotspotPlus . It keeps the member, plan and quota rules and removes the infrastructure and the multi-tenant product around them. It is open source under the AGPL-3.0, and the code is on GitHub ."
 },
 {
  "page": "Installation",
  "heading": "Download",
  "url": "docs/installation/#download",
  "keywords": "Every release has nine files. Each holds the program, README.md and LICENSE . The file names have no version, so these links always give the newest release. System Build File Linux x86 64-bit hotspotpluslight-linux-amd64.tar.gz Linux x86 32-bit hotspotplusligh"
 },
 {
  "page": "Installation",
  "heading": "Linux",
  "url": "docs/installation/#linux",
  "keywords": "Download, unpack and install the program: curl -LO  tar -xzf hotspotpluslight-linux-amd64.tar.gz sudo install -m 755 hotspotpluslight /usr/local/bin/ hotspotpluslight version Make a settings file with a new secret key. The key encrypts the member passwords, so"
 },
 {
  "page": "Installation",
  "heading": "macOS",
  "url": "docs/installation/#macos",
  "keywords": "Use darwin-arm64 on Apple silicon or darwin-amd64 on an Intel Mac: curl -LO  tar -xzf hotspotpluslight-darwin-arm64.tar.gz ./hotspotpluslight version Then create hsp.env and start the server as shown for Linux. If macOS blocks a file you downloaded in a browse"
 },
 {
  "page": "Installation",
  "heading": "Windows",
  "url": "docs/installation/#windows",
  "keywords": "Open PowerShell, then download and unpack the zip: curl.exe -LO  Expand-Archive hotspotpluslight-windows-amd64.zip -DestinationPath C:\\hotspotpluslight cd C:\\hotspotpluslight .\\hotspotpluslight.exe version To try it in the window, set a key and start it: $env:"
 },
 {
  "page": "Installation",
  "heading": "The first start",
  "url": "docs/installation/#first-run",
  "keywords": "On the first start the server creates the database, creates the admin account and prints this block. The log lines go to stderr and the block goes to stdout. This is the real output: time=2026-11-04T17:44:47.540+01:00 level=INFO msg=\"migrations applied\" count="
 },
 {
  "page": "Installation",
  "heading": "Build from source",
  "url": "docs/installation/#build",
  "keywords": "You need Go 1.26 (see go.mod ) and make. The build has no cgo and no Node step. git clone  cd HotspotPlusLight make build make build writes bin/hotspotpluslight . For a 64-bit Windows program use make build-windows , which writes bin/hotspotpluslight.exe . mak"
 },
 {
  "page": "Installation",
  "heading": "Ports",
  "url": "docs/installation/#ports",
  "keywords": "Port Direction Used for TCP 8081 to the server, from your computer The admin panel and /healthz (plain HTTP). UDP 1812 to the server, from the router RADIUS login requests. UDP 1813 to the server, from the router RADIUS accounting. UDP 3799 to the router, from"
 },
 {
  "page": "Run as a service",
  "heading": "Linux (systemd)",
  "url": "docs/running-as-a-service/#systemd",
  "keywords": "Create a user for the service, and a settings file that only root and that user can read. It holds the secret key. sudo useradd --system --no-create-home --shell /usr/sbin/nologin hsp printf 'HSP_SECRET_KEY=%s\\nHSP_DATA_DIR=/var/lib/hotspotpluslight\\n' \"$(hots"
 },
 {
  "page": "Run as a service",
  "heading": "Windows service",
  "url": "docs/running-as-a-service/#windows",
  "keywords": "The program can install itself as a Windows service. Open PowerShell as Administrator , go to the folder with hotspotpluslight.exe and run: cd C:\\hotspotpluslight .\\hotspotpluslight.exe service install .\\hotspotpluslight.exe service start service install does "
 },
 {
  "page": "Run as a service",
  "heading": "macOS (launchd)",
  "url": "docs/running-as-a-service/#macos",
  "keywords": "The service commands work only on Windows. On macOS, use launchd. Put the settings in /usr/local/etc/hotspotpluslight.env (same format as above, readable only by root) and create /Library/LaunchDaemons/local.hotspotpluslight.plist : <?xml version=\"1.0\" encodin"
 },
 {
  "page": "Run as a service",
  "heading": "Check that it runs",
  "url": "docs/running-as-a-service/#check",
  "keywords": "On the server, curl  prints ok . In a browser,  shows the login page. Then continue with First login ."
 },
 {
  "page": "Configuration",
  "heading": "Environment variables",
  "url": "docs/configuration/#env",
  "keywords": "The server reads these variables when it starts. Everything else is on the Settings page . Variable Default Meaning HSP_SECRET_KEY (required) Base64 of 32 random bytes. It is the AES-GCM key for the stored member passwords. The server will not start without it"
 },
 {
  "page": "Configuration",
  "heading": "The env file",
  "url": "docs/configuration/#env-file",
  "keywords": "Instead of setting variables, put them in a file and start with --env-file . The option works for serve and for admin set-password : # hsp.env HSP_SECRET_KEY=your-key-here HSP_DATA_DIR=/var/lib/hotspotpluslight HSP_LOG_LEVEL=info # a space and a hash start a c"
 },
 {
  "page": "Configuration",
  "heading": "Commands",
  "url": "docs/configuration/#commands",
  "keywords": "Command What it does hotspotpluslight serve Starts the server: the admin panel and both RADIUS listeners. hotspotpluslight gen-key Prints a new HSP_SECRET_KEY (base64 of 32 random bytes). It needs no database. hotspotpluslight admin set-password [--stdin] Sets"
 },
 {
  "page": "Configuration",
  "heading": "The Settings page",
  "url": "docs/configuration/#settings-page",
  "keywords": "These are saved in the database and edited on the Settings page of the admin panel. They apply without a restart. Setting Default Meaning Business name (empty) Your business name. It is printed at the top of every voucher and shown in the admin header and the browser tab. Leave it empty to hide it. Time zone UTC An IANA name such as E"
 },
 {
  "page": "First login",
  "heading": "Log in",
  "url": "docs/first-login/#login",
  "keywords": "Open  . On the first start the account is admin with the password admin (you can change the user name with HSP_ADMIN_USERNAME before the first start). The first-start block printed by the server says the same. Do this at once: until the password is changed, an"
 },
 {
  "page": "First login",
  "heading": "Set a new password",
  "url": "docs/first-login/#password",
  "keywords": "After you log in you are sent to the password page. No other page opens until you set a new password. The rules: At least 10 characters, and at most 72 bytes. Not your user name, not admin , and not a very common password. No other rules, so a long phrase is f"
 },
 {
  "page": "First login",
  "heading": "If you forget it",
  "url": "docs/first-login/#forgot",
  "keywords": "Reset it from the command line of the server. It asks twice without echo, uses the same rules as the page, and ends every open admin session: hotspotpluslight admin set-password With the source code, make set-password does the same for the ./data database. Use"
 },
 {
  "page": "First login",
  "heading": "Create a plan",
  "url": "docs/first-login/#plan",
  "keywords": "No plan exists after the first start, and every member needs a plan. Open Plans and choose New plan : Field Meaning Name Shown in lists. Names are unique, ignoring case. Validity (days) A whole number, at least 1. The member can use the plan this long, counted"
 },
 {
  "page": "First login",
  "heading": "Create a member",
  "url": "docs/first-login/#member",
  "keywords": "Open Members and choose New member . Pick a plan. A user name has English letters and digits only (up to 64) and is not case-sensitive; if you leave it empty, the mobile number is used. If you leave the password empty, a random 6-digit number is made. You can "
 },
 {
  "page": "First login",
  "heading": "Print vouchers",
  "url": "docs/first-login/#vouchers",
  "keywords": "Open Batches and choose a new batch. Choose a plan, how many vouchers (up to the maximum in Settings, 30 by default), and an optional label. Each voucher is a member with a user name made from the prefix and a number (for example wifi1360 ) and a random passwo"
 },
 {
  "page": "First login",
  "heading": "See who is online",
  "url": "docs/first-login/#online",
  "keywords": "The Online page lists the open sessions and refreshes every 10 seconds. Disconnect sends a RADIUS Disconnect request to the router. The Usage page shows traffic per day for 30 days and the top 10 members for 7, 30 or 90 days. Next: Router setup ."
 },
 {
  "page": "Router setup",
  "heading": "Overview",
  "url": "docs/router-setup/#overview",
  "keywords": "This page assumes the Hotspot already works on the router (a hotspot server, a hotspot profile and an address pool). You then change three things: the router asks HotspotPlus Lite for logins and accounting, it accepts Disconnect requests, and it uses a login p"
 },
 {
  "page": "Router setup",
  "heading": "Step 1: the shared secret",
  "url": "docs/router-setup/#settings",
  "keywords": "In the admin panel open Settings . Note the RADIUS shared secret (a random one was made at the first start, or the value of HSP_RADIUS_SHARED_SECRET ). You can change it. Note also the Disconnect (CoA) port , 3799 by default."
 },
 {
  "page": "Router setup",
  "heading": "Step 2: the setup script",
  "url": "docs/router-setup/#script",
  "keywords": "Open Router ( /admin/router ). It shows a setup script for RouterOS with your secret and ports filled in. It looks like this: # 1. RADIUS server. Replace <SERVER-IP>, and use the secret from the Settings page. /radius add service=hotspot address=<SERVER-IP> se"
 },
 {
  "page": "Router setup",
  "heading": "Step 3: the login page",
  "url": "docs/router-setup/#page",
  "keywords": "On the same Router page choose a design and press Download . You get a zip with a folder named hotspot . Unzip it, then upload the unzipped hotspot folder to the router (Files in WinBox, or FTP) and replace the old one. On routers that keep files on flash stor"
 },
 {
  "page": "Router setup",
  "heading": "Firewall",
  "url": "docs/router-setup/#firewall",
  "keywords": "These ports must be open. HotspotPlus Lite never talks to members, only to the router and to you: Where Rule On the server, from the router UDP 1812 (login) and UDP 1813 (accounting), inbound. On the server, from your computer TCP 8081 (admin panel), inbound. "
 },
 {
  "page": "Router setup",
  "heading": "Test it",
  "url": "docs/router-setup/#test",
  "keywords": "Make a member in the admin panel. On a phone, join the hotspot Wi-Fi, open any web page and log in. Then check the Online page. The Router page also has a link status panel: the time and source address of the last login request, the last accounting packet and "
 },
 {
  "page": "Hotspot login pages",
  "heading": "The 8 designs",
  "url": "docs/hotspot-login-pages/#designs",
  "keywords": "The Router page of the admin panel offers 8 downloads. Each is a complete hotspot folder for the router. Seven use one layout with a different color theme. You can open a preview of each one before you download it. Key Design Look mikrotik MikroTik default The"
 },
 {
  "page": "Hotspot login pages",
  "heading": "What is in the folder",
  "url": "docs/hotspot-login-pages/#files",
  "keywords": "The themed folders contain: login.html , alogin.html , status.html , logout.html , error.html : the pages members see. css/hotspot.css : the layout and the colors of the theme. js/busy.js : an optional spinner and double-submit guard; the pages work without it"
 },
 {
  "page": "Hotspot login pages",
  "heading": "Edit the heading, subtitle and logo",
  "url": "docs/hotspot-login-pages/#edit",
  "keywords": "Open login.html in a text editor. Comments in the file show where to change things: Heading : the text of the <h1> , \"Welcome\" by default. Subtitle : the text of the line below it, with the class sub . Logo : upload your logo to the hotspot folder as img/logo."
 },
 {
  "page": "Hotspot login pages",
  "heading": "Translate the messages",
  "url": "docs/hotspot-login-pages/#translate",
  "keywords": "When the server rejects a login it sends only a short code. login.html turns the code into text in a block called MESSAGES - translate here : $(if error == \"INVALID_CREDENTIALS\")Wrong username or password. $(elif error == \"INACTIVE_USER\")This account is disabl"
 },
 {
  "page": "Hotspot login pages",
  "heading": "Reason codes",
  "url": "docs/hotspot-login-pages/#codes",
  "keywords": "These are all the codes the server can send: Code Default text When INVALID_CREDENTIALS Wrong username or password. Unknown user or wrong password. INACTIVE_USER This account is disabled. The member is disabled. NO_PLAN No internet plan is assigned to this acc"
 },
 {
  "page": "Hotspot login pages",
  "heading": "After login",
  "url": "docs/hotspot-login-pages/#status",
  "keywords": "The status page shows \"Hi, USERNAME\", the time online, the time left and the data left when the router fills them in, a Continue link to the first page the member asked for, and a Log out button. The router decides which of these values it has, so some may be "
 },
 {
  "page": "How it works",
  "heading": "The login checks",
  "url": "docs/how-it-works/#login",
  "keywords": "When a member logs in, the router sends an Access-Request with the user name and the password (PAP). The server checks these in order and stops at the first one that fails. The failure becomes a reject with that code (see Reason codes ). # Check Code when it f"
 },
 {
  "page": "How it works",
  "heading": "What the router gets",
  "url": "docs/how-it-works/#accept",
  "keywords": "If every check passes, the server sends an Access-Accept with these attributes: Attribute Value Session-Timeout The smallest of: the time left in the plan, the time until the member's expiry, and the time until the end of the validity period. At least 1 second"
 },
 {
  "page": "How it works",
  "heading": "Accounting",
  "url": "docs/how-it-works/#accounting",
  "keywords": "The router sends Start, Interim (every accounting interval) and Stop packets with the totals of the session. The server turns the totals into usage: Download is Acct-Output-Octets and upload is Acct-Input-Octets , each with its Gigawords part. Time is Acct-Ses"
 },
 {
  "page": "How it works",
  "heading": "Two layers of limits",
  "url": "docs/how-it-works/#limits",
  "keywords": "Layer 1: the router. It gets Session-Timeout , the data limits and the rate limit at login, and ends the session or limits the speed by itself. Layer 2: the server. After each Start or Interim of an open session, the server checks the member again with the log"
 },
 {
  "page": "How it works",
  "heading": "Disconnect (kick)",
  "url": "docs/how-it-works/#kick",
  "keywords": "The Disconnect button on the Online page, and the automatic kicks above, send a RADIUS Disconnect-Request to the router on the Disconnect port (3799 by default), with the user name and the session details. If the router answers ACK, the session is marked ended"
 },
 {
  "page": "How it works",
  "heading": "After login",
  "url": "docs/how-it-works/#status",
  "keywords": "After a successful login the router opens alogin.html , which sends the member to the status page. The status page is on the router and shows what the router knows. It does not ask the server. For diagrams of these flows, see the README on GitHub ."
 },
 {
  "page": "Architecture",
  "heading": "The parts",
  "url": "docs/architecture/#parts",
  "keywords": "Part What it does RADIUS server Answers login requests on UDP 1812 and accounting on UDP 1813, and sends Disconnect requests to the router. Built on layeh.com/radius . Admin panel Server-rendered pages (chi router, templ components, htmx) with our own CSS and "
 },
 {
  "page": "Architecture",
  "heading": "One HTTP listener",
  "url": "docs/architecture/#listeners",
  "keywords": "The program has one HTTP listener, the admin panel on :8081 (with /healthz ), and two UDP listeners for RADIUS. There is no public web server and no API for the login page: the router hosts the login page by itself. The admin panel uses plain HTTP and must not"
 },
 {
  "page": "Architecture",
  "heading": "One SQLite file",
  "url": "docs/architecture/#sqlite",
  "keywords": "All state is in hsp.db in the data folder, in WAL mode. The program opens it as two pools: one write connection, so writers queue in Go and never fight over the lock, and eight read connections. Every write is a short transaction. Accounting packets are handle"
 },
 {
  "page": "Architecture",
  "heading": "Key decisions",
  "url": "docs/architecture/#decisions",
  "keywords": "Decision Reason One business, one admin, one router, MikroTik only It keeps the program small. The multi-tenant product of the original HotspotPlus is not needed here. PAP only The login page posts the password to the router, which passes it on. CHAP is reject"
 },
 {
  "page": "Architecture",
  "heading": "How it is tested",
  "url": "docs/architecture/#tests",
  "keywords": "End-to-end tests start the real binary and use only real interfaces: UDP RADIUS from a fake router, HTTP and the command line. Domain rules and services also have unit tests. A capacity test with a simulated router raised the number of online users step by ste"
 },
 {
  "page": "Backup and upgrade",
  "heading": "What to back up",
  "url": "docs/backup-and-upgrade/#what",
  "keywords": "The state is two things: The database , hsp.db in the data folder. The secret key , HSP_SECRET_KEY (in your env file, for example hsp.env ). Keep the key If the key is lost, the stored member passwords are unrecoverable , and the server will not start on that "
 },
 {
  "page": "Backup and upgrade",
  "heading": "Back up the database",
  "url": "docs/backup-and-upgrade/#backup",
  "keywords": "Use SQLite's own backup command. It makes a consistent copy while the server is running. The sqlite3 tool is separate from HotspotPlus Lite, so install it if you do not have it. sqlite3 hsp.db \".backup backup-$(date +%F).db\" Run it in the data folder, or give "
 },
 {
  "page": "Backup and upgrade",
  "heading": "Restore",
  "url": "docs/backup-and-upgrade/#restore",
  "keywords": "Stop the server. Put the backup file in the data folder as hsp.db , and delete old hsp.db-wal and hsp.db-shm files there. Start the server with the same HSP_SECRET_KEY that the database was made with. To move to a new machine, copy the database (from a backup)"
 },
 {
  "page": "Backup and upgrade",
  "heading": "Upgrade",
  "url": "docs/backup-and-upgrade/#upgrade",
  "keywords": "An upgrade replaces the program and restarts it. The database migrations run by themselves at start and only go forward. Make a backup first: you cannot go back to an older program on a migrated database, but you can restore the backup. Linux (systemd) curl -L"
 },
 {
  "page": "Backup and upgrade",
  "heading": "What is kept",
  "url": "docs/backup-and-upgrade/#retention",
  "keywords": "Finished sessions older than 90 days are removed (the setting Keep finished sessions (days) ; 0 keeps them forever). The daily usage history and the members are kept."
 },
 {
  "page": "Troubleshooting",
  "heading": "Logins fail",
  "url": "docs/troubleshooting/#login-problems",
  "keywords": "Nobody can log in, and the login page waits or fails Check in this order: Open the Router page and look at the link status panel. If no login request arrived, the router does not reach the server: check <SERVER-IP> in /radius and that UDP 1812 and 1813 are ope"
 },
 {
  "page": "Troubleshooting",
  "heading": "Router and usage",
  "url": "docs/troubleshooting/#router-problems",
  "keywords": "Disconnect does nothing, or the admin panel shows an error On the router, run /radius incoming set accept=yes port=3799 , with the same port as Disconnect (CoA) port on the Settings page. Check that the router accepts UDP 3799 from the server in its firewall. "
 },
 {
  "page": "Troubleshooting",
  "heading": "Admin panel, server and settings",
  "url": "docs/troubleshooting/#admin-problems",
  "keywords": "The times are wrong, or vouchers expire at the wrong hour The default time zone is UTC . Set yours on the Settings page, for example Europe/London . Dates, daily usage and voucher expiry follow it. I forgot the admin password Run hotspotpluslight admin set-pas"
 }
];
