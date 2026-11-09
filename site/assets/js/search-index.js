// Search index of the docs. Each entry is one h2 section: its heading and the first 260 characters of its text, with links removed. Rebuild it after you change a docs section; E13.15 checks that every entry still matches its section.
export default [
 {
  "page": "Introduction",
  "heading": "What it is",
  "url": "docs/#what-it-is",
  "keywords": "HotspotPlus Lite manages Wi-Fi accounts for one MikroTik hotspot router . You make plans (data limit, time limit, validity period, speed limit, simultaneous devices) and members , or you print a batch of vouchers . At every login, the router asks the server an"
 },
 {
  "page": "Introduction",
  "heading": "How the parts fit",
  "url": "docs/#parts",
  "keywords": "The router runs the hotspot and stores the login page. It sends each login and each accounting packet (usage updates during a session) to the server by RADIUS. HotspotPlus Lite runs on a computer on the same network. It answers RADIUS on UDP 1812 and UDP 1813 "
 },
 {
  "page": "Introduction",
  "heading": "What you need",
  "url": "docs/#requirements",
  "keywords": "A MikroTik router with the hotspot feature already working. A computer on the same network as the router that is always on. It can run Linux, Windows or macOS (x86 or ARM). UDP 1812 and UDP 1813 open from the router to that computer, and UDP 3799 open from tha"
 },
 {
  "page": "Introduction",
  "heading": "What it does not do",
  "url": "docs/#limits",
  "keywords": "One router and one admin account. No multi-tenant mode (one install for many businesses). MikroTik routers only, and PAP login only. If the router uses CHAP, the server rejects the login. No payments and no SMS. Members cannot sign up or choose a plan by thems"
 },
 {
  "page": "Introduction",
  "heading": "Where to start",
  "url": "docs/#read-next",
  "keywords": "Install the program and start it once (see Installation ). Set it up as a service (see Run as a service ). Log in, set a new password and create a plan and a member (see First login ). Set up the router (see Router setup ) and upload a login page (see Hotspot "
 },
 {
  "page": "Introduction",
  "heading": "Where it comes from",
  "url": "docs/#origin",
  "keywords": "HotspotPlus Lite is a rewrite of HotspotPlus . It keeps the rules for members, plans and limits. It removes the extra infrastructure and the multi-tenant product around them. It is open source under the AGPL-3.0. The code is on GitHub ."
 },
 {
  "page": "Installation",
  "heading": "Download",
  "url": "docs/installation/#download",
  "keywords": "Every release has 9 files. Each file holds the program, README.md and LICENSE . The file names have no version number, so these links always give you the newest release. System Build File Linux x86 64-bit hotspotpluslight-linux-amd64.tar.gz Linux x86 32-bit ho"
 },
 {
  "page": "Installation",
  "heading": "Linux",
  "url": "docs/installation/#linux",
  "keywords": "Download, unpack and install the program: curl -LO tar -xzf hotspotpluslight-linux-amd64.tar.gz sudo install -m 755 hotspotpluslight /usr/local/bin/ hotspotpluslight version Start the server from the same folder. At the first start, the server creates the secr"
 },
 {
  "page": "Installation",
  "heading": "macOS",
  "url": "docs/installation/#macos",
  "keywords": "Use darwin-arm64 on Apple silicon or darwin-amd64 on an Intel Mac: curl -LO tar -xzf hotspotpluslight-darwin-arm64.tar.gz ./hotspotpluslight version Then start the server as shown for Linux. If macOS blocks a file you downloaded in a browser, remove the quaran"
 },
 {
  "page": "Installation",
  "heading": "Windows",
  "url": "docs/installation/#windows",
  "keywords": "Open PowerShell, then download and unpack the zip: curl.exe -LO Expand-Archive hotspotpluslight-windows-amd64.zip -DestinationPath C:\\hotspotpluslight cd C:\\hotspotpluslight .\\hotspotpluslight.exe version To try it in the PowerShell window, start it: .\\hotspot"
 },
 {
  "page": "Installation",
  "heading": "The first start",
  "url": "docs/installation/#first-run",
  "keywords": "On the first start, the server creates the database and the admin account, and prints this block. The log lines go to stderr and the block goes to stdout. This is the real output: time=2026-11-04T17:44:47.540+01:00 level=INFO msg=\"migrations applied\" count=4 ="
 },
 {
  "page": "Installation",
  "heading": "Build from source",
  "url": "docs/installation/#build",
  "keywords": "You need Go 1.26 (see go.mod ) and make . The build needs no cgo and no Node.js step. git clone cd HotspotPlusLight make build make build writes bin/hotspotpluslight . For a 64-bit Windows program, use make build-windows , which writes bin/hotspotpluslight.exe"
 },
 {
  "page": "Installation",
  "heading": "Ports",
  "url": "docs/installation/#ports",
  "keywords": "The MikroTik router talks to the server with RADIUS (the protocol the router uses to ask the server about each login). Port Direction Used for TCP 8081 to the server, from your computer The admin panel and /healthz (plain HTTP). UDP 1812 to the server, from th"
 },
 {
  "page": "Run as a service",
  "heading": "Linux (systemd)",
  "url": "docs/running-as-a-service/#systemd",
  "keywords": "Create a user for the service, and an env file that only root and that user can read: sudo useradd --system --no-create-home --shell /usr/sbin/nologin hsp printf 'HSP_DATA_DIR=/var/lib/hotspotpluslight\\n' | sudo tee /etc/hotspotpluslight.env >/dev/null sudo ch"
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
  "keywords": "The service commands work only on Windows. On macOS, use launchd. Put variables such as HSP_DATA_DIR in the env file /usr/local/etc/hotspotpluslight.env (readable only by root). Then create /Library/LaunchDaemons/local.hotspotpluslight.plist : <?xml version=\"1"
 },
 {
  "page": "Run as a service",
  "heading": "Check that it runs",
  "url": "docs/running-as-a-service/#check",
  "keywords": "On the server, curl prints ok . In a browser, shows the admin login page. Then continue with First login ."
 },
 {
  "page": "Configuration",
  "heading": "Environment variables",
  "url": "docs/configuration/#env",
  "keywords": "These variables control how the server starts. Some of them are used only at the first start, to fill in the first values. After that, you change them only on the Settings page , and the variable is ignored. If a variable is set but differs from the saved sett"
 },
 {
  "page": "Configuration",
  "heading": "The env file",
  "url": "docs/configuration/#env-file",
  "keywords": "Instead of setting environment variables, you can put them in an env file and start the server with --env-file . This option works for serve and for admin set-password : # hsp.env HSP_DATA_DIR=/var/lib/hotspotpluslight HSP_LOG_LEVEL=info # a space and a hash s"
 },
 {
  "page": "Configuration",
  "heading": "Commands",
  "url": "docs/configuration/#commands",
  "keywords": "Command What it does hotspotpluslight serve Starts the server: the admin panel and both RADIUS listeners. hotspotpluslight gen-key Prints a new key (base64 of 32 random bytes). The server creates the key automatically at the first start. Use this command only "
 },
 {
  "page": "Configuration",
  "heading": "The Settings page",
  "url": "docs/configuration/#settings-page",
  "keywords": "These settings are saved in the database. You change them on the Settings page of the admin panel. The page has groups: Business, RADIUS, Members, Voucher defaults, Server addresses and Logs. One Save settings button at the bottom saves all of them. Next to th"
 },
 {
  "page": "First login",
  "heading": "Log in",
  "url": "docs/first-login/#login",
  "keywords": "Open . After you set your password, the admin panel opens on the Dashboard . At the first start, the user name is admin and the password is admin . You can set another user name with HSP_ADMIN_USERNAME before the first start, or change it later on the Password"
 },
 {
  "page": "First login",
  "heading": "Set a new password",
  "url": "docs/first-login/#password",
  "keywords": "After you log in, the server sends you to the password page. No other page opens until you set a new password. The page also has a Username field that shows the current name. Keep admin or type your own: 4 to 32 letters, numbers, dots or dashes. The server sav"
 },
 {
  "page": "First login",
  "heading": "If you forget it",
  "url": "docs/first-login/#forgot",
  "keywords": "Reset it on the command line of the server. The command asks for the new password twice and does not show what you type. It uses the same rules as the page and ends every open admin session. It does not change the user name: hotspotpluslight admin set-password"
 },
 {
  "page": "First login",
  "heading": "Create a plan",
  "url": "docs/first-login/#plan",
  "keywords": "After the first start there are no plans, and every member needs a plan. Open the Plans page and choose Add plan . A side panel opens: Field Meaning Name Shown in lists. Each name must be unique (case does not matter). Valid for (days) A whole number, at least"
 },
 {
  "page": "First login",
  "heading": "Create a member",
  "url": "docs/first-login/#member",
  "keywords": "Open the Members page and choose Add member . A side panel opens with a user name, a password and a plan. Under the plan, a short line shows what the plan gives: data, days, speed and devices. The Generate button puts a random 6-digit password in the field. A "
 },
 {
  "page": "First login",
  "heading": "Print vouchers",
  "url": "docs/first-login/#vouchers",
  "keywords": "Open the Vouchers page. The form is on the left and the list of batches is on the right. Choose a plan, the number of vouchers and an optional batch name. The number can be up to the maximum set on the Settings page (30 by default). Each voucher is a member. I"
 },
 {
  "page": "First login",
  "heading": "See who is online",
  "url": "docs/first-login/#online",
  "keywords": "The Dashboard shows how many members are online now, today's traffic and the Router link card. The Online now page lists the open sessions (user name, name, IP and MAC address, time online, data down and up) and refreshes every 10 seconds. The Kick button asks"
 },
 {
  "page": "Dashboard",
  "heading": "Open the Dashboard",
  "url": "docs/dashboard/#open",
  "keywords": "After you log in, the Dashboard is the first page you see. It is also the first item in the sidebar. The brand name in the top bar takes you back to it. The whole page refreshes by itself every 10 seconds, so you can leave it open. All times and days follow th"
 },
 {
  "page": "Dashboard",
  "heading": "The four tiles",
  "url": "docs/dashboard/#tiles",
  "keywords": "Tile What it shows Online now The open sessions that the router still reports. This is the same number as on the Online now page, so one member on 2 devices counts twice. Peak today The most sessions online at one moment since midnight. Traffic today Download "
 },
 {
  "page": "Dashboard",
  "heading": "Router link",
  "url": "docs/dashboard/#router-status",
  "keywords": "The Router link card shows what the MikroTik router last sent to the server: the time and source address of the last login request, the last accounting packet and the last Disconnect result. It also shows how many packets had a wrong shared secret. The label n"
 },
 {
  "page": "Dashboard",
  "heading": "Needs attention",
  "url": "docs/dashboard/#attention",
  "keywords": "This is one card with 2 short lists. Each list shows the full count in its title and at most 5 members. The label next to the card title shows the number of items, or All clear : Expiring in 3 days : active members whose expiry time is in the next 3 days. You "
 },
 {
  "page": "Dashboard",
  "heading": "The three charts",
  "url": "docs/dashboard/#charts",
  "keywords": "Traffic, last 14 days : download plus upload for all members, one bar per day. The Usage page has 7, 30 and 90 day ranges and the top members. Busy hours : for each hour of the day, the average number of people online over the last 14 days. Use it to see when "
 },
 {
  "page": "Dashboard",
  "heading": "How failed logins are counted",
  "url": "docs/dashboard/#failed-logins",
  "keywords": "A failed login is one refusal that the server sends back to the router, for any reason: wrong user name or password, expired account, no data left, no plan, too many devices and so on. The server adds 1 to a counter for that day and that reason. No personal da"
 },
 {
  "page": "Router setup",
  "heading": "Overview",
  "url": "docs/router-setup/#overview",
  "keywords": "This page assumes that the hotspot feature already works on your MikroTik router (a hotspot server, a hotspot profile and an address pool). You then change 3 things. The router asks HotspotPlus Lite about each login over RADIUS (the protocol the router uses to"
 },
 {
  "page": "Router setup",
  "heading": "Step 1: the shared secret",
  "url": "docs/router-setup/#settings",
  "keywords": "In the admin panel, open the Settings page. Note the RADIUS shared secret . The server made a random one at the first start, or it used the value of HSP_RADIUS_SHARED_SECRET . You can change it. Use letters and digits only (no spaces or symbols). Make it long "
 },
 {
  "page": "Router setup",
  "heading": "Step 2: the setup script",
  "url": "docs/router-setup/#script",
  "keywords": "Open the Router page ( /admin/router ). It shows a RouterOS setup script with your shared secret and ports filled in. It looks like this: # 1. RADIUS server. Replace <SERVER-IP>, and use the secret from the Settings page. /radius add service=hotspot address=<S"
 },
 {
  "page": "Router setup",
  "heading": "Step 3: the login page",
  "url": "docs/router-setup/#page",
  "keywords": "On the same Router page, under Sign-in pages , choose a design and click Download ( Open preview shows it first). You get a zip file with a folder named hotspot . Unzip it. Then upload the hotspot folder to the router (with Files in WinBox, or with FTP) and re"
 },
 {
  "page": "Router setup",
  "heading": "Firewall",
  "url": "docs/router-setup/#firewall",
  "keywords": "These ports must be open. The server never talks to members. It talks only to the router and to you: Where Rule On the server, from the router UDP 1812 (login) and UDP 1813 (accounting), inbound. On the server, from your computer TCP 8081 (admin panel), inboun"
 },
 {
  "page": "Router setup",
  "heading": "Test it",
  "url": "docs/router-setup/#test",
  "keywords": "Create a member in the admin panel. On a phone, join the hotspot Wi-Fi, open any web page and log in. Then check the Online now page. The Dashboard has a Router link card. It shows the time and source address of the last login request, the last accounting pack"
 },
 {
  "page": "Hotspot login pages",
  "heading": "The 8 designs",
  "url": "docs/hotspot-login-pages/#designs",
  "keywords": "The Router page of the admin panel offers 8 downloads. Each one is a complete hotspot folder for the router. The 7 themes share one layout, each with different colors. You can open a preview of each design before you download it. Key Design Look mikrotik Mikro"
 },
 {
  "page": "Hotspot login pages",
  "heading": "What is in the folder",
  "url": "docs/hotspot-login-pages/#files",
  "keywords": "The folders of the 7 themes contain: login.html , alogin.html , status.html , logout.html , error.html : the pages members see. css/hotspot.css : the layout and the colors of the theme. js/busy.js : an optional spinner that also stops a form from being sent tw"
 },
 {
  "page": "Hotspot login pages",
  "heading": "Edit the heading, subtitle and logo",
  "url": "docs/hotspot-login-pages/#edit",
  "keywords": "Open login.html in a text editor. Comments in the file show where to change things: Heading : the text of the <h1> tag. It is \"Welcome\" by default. Subtitle : the text of the line below the heading (the line with the class sub ). Logo : upload your logo to the"
 },
 {
  "page": "Hotspot login pages",
  "heading": "Translate the messages",
  "url": "docs/hotspot-login-pages/#translate",
  "keywords": "When a login fails, the server sends only a short reason code. login.html turns the code into text in a block called MESSAGES - translate here : $(if error == \"INVALID_CREDENTIALS\")Wrong username or password. $(elif error == \"INACTIVE_USER\")This account is dis"
 },
 {
  "page": "Hotspot login pages",
  "heading": "Reason codes",
  "url": "docs/hotspot-login-pages/#codes",
  "keywords": "These are all the codes the server can send: Code Default text When INVALID_CREDENTIALS Wrong username or password. Unknown user name or wrong password. INACTIVE_USER This account is disabled. The member is disabled. NO_PLAN No internet plan is assigned to thi"
 },
 {
  "page": "Hotspot login pages",
  "heading": "After login",
  "url": "docs/hotspot-login-pages/#status",
  "keywords": "The status page shows \"Hi, USERNAME\", the time online, the time left and the data left. It also has a Continue link to the first page the member asked for and a Log out button. The router fills in only the values it has, so some may be missing on your RouterOS"
 },
 {
  "page": "How it works",
  "heading": "The login checks",
  "url": "docs/how-it-works/#login",
  "keywords": "When a member logs in, the MikroTik router sends a RADIUS Access-Request with the user name and the password. RADIUS is the protocol the router uses to ask the server about each login. The login method is PAP: the router sends the password to the server. The s"
 },
 {
  "page": "How it works",
  "heading": "What the router gets",
  "url": "docs/how-it-works/#accept",
  "keywords": "If every check passes, the server sends an Access-Accept with these attributes: Attribute Value Session-Timeout The smallest of: the time left in the plan, the time until the member's expiry time and the time until the end of the validity period. At least 1 se"
 },
 {
  "page": "How it works",
  "heading": "Accounting",
  "url": "docs/how-it-works/#accounting",
  "keywords": "The router sends accounting packets (usage updates during a session): Start, Interim (every accounting interval) and Stop. Each packet holds the totals of the session. The server turns the totals into usage: Download is Acct-Output-Octets and upload is Acct-In"
 },
 {
  "page": "How it works",
  "heading": "Two layers of limits",
  "url": "docs/how-it-works/#limits",
  "keywords": "Layer 1: the router. At login, the router gets Session-Timeout , the data limits and the speed limit ( Mikrotik-Rate-Limit ). It ends the session or limits the speed by itself. Layer 2: the server. After each Start or Interim packet of an open session, the ser"
 },
 {
  "page": "How it works",
  "heading": "Disconnect (kick)",
  "url": "docs/how-it-works/#kick",
  "keywords": "The Kick button on the Online now page and the automatic disconnects above send a RADIUS Disconnect-Request to the router. The request goes to the Disconnect port (UDP 3799 by default) with the user name and the session details. If the router answers with Disc"
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
  "keywords": "Part What it does RADIUS server Answers login requests on UDP 1812 and accounting on UDP 1813, and sends Disconnect requests to the router. Built on layeh.com/radius . Admin panel Pages built on the server (chi router, templ components, htmx), with our own CSS"
 },
 {
  "page": "Architecture",
  "heading": "One HTTP listener",
  "url": "docs/architecture/#listeners",
  "keywords": "The server has one HTTP listener (the admin panel on :8081 , with /healthz ) and 2 UDP listeners for RADIUS. There is no public web server and no API for the login page. The router hosts the login page by itself. The admin panel uses plain HTTP, and it must no"
 },
 {
  "page": "Architecture",
  "heading": "One SQLite file",
  "url": "docs/architecture/#sqlite",
  "keywords": "All data is in hsp.db in the data folder, in WAL mode. The server opens it as 2 pools: 1 write connection and 8 read connections. With a single write connection, writers wait their turn in Go and never fight over the lock. Every write is a short transaction. A"
 },
 {
  "page": "Architecture",
  "heading": "Key decisions",
  "url": "docs/architecture/#decisions",
  "keywords": "Decision Reason One business, one admin, one router, MikroTik only It keeps the program small. HotspotPlus serves many businesses from one install, and that is not needed here. PAP only The login page sends the password to the router, and the router passes it "
 },
 {
  "page": "Architecture",
  "heading": "How it is tested",
  "url": "docs/architecture/#tests",
  "keywords": "End-to-end tests start the real program and use only real interfaces: UDP RADIUS from a fake router, HTTP and the command line. Domain rules and services also have unit tests. A capacity test with a simulated router raised the number of online members step by "
 },
 {
  "page": "Backup and upgrade",
  "heading": "What to back up",
  "url": "docs/backup-and-upgrade/#what",
  "keywords": "You need to back up 2 things: The database , hsp.db in the data folder. The secret key , hsp.key in the data folder (or HSP_SECRET_KEY in your env file, if you set the key yourself). Keep the key If you lose the key, the stored member passwords are unrecoverab"
 },
 {
  "page": "Backup and upgrade",
  "heading": "Back up the database",
  "url": "docs/backup-and-upgrade/#backup",
  "keywords": "Use the backup command of SQLite. It makes a complete, correct copy while the server is running. The sqlite3 tool is not part of HotspotPlus Lite, so install it if you do not have it. sqlite3 hsp.db \".backup backup-$(date +%F).db\" Run it in the data folder, or"
 },
 {
  "page": "Backup and upgrade",
  "heading": "Restore",
  "url": "docs/backup-and-upgrade/#restore",
  "keywords": "Stop the server. Put the backup file in the data folder as hsp.db , and delete any old hsp.db-wal and hsp.db-shm files there. Start the server with the data folder that holds both hsp.db and hsp.key . If you use an env file with HSP_SECRET_KEY , use the same k"
 },
 {
  "page": "Backup and upgrade",
  "heading": "Upgrade",
  "url": "docs/backup-and-upgrade/#upgrade",
  "keywords": "To upgrade, you replace the program and restart it. The database migrations (changes to the database layout) run by themselves at start and only go forward. Make a backup first. You cannot go back to an older program on a migrated database, but you can restore"
 },
 {
  "page": "Backup and upgrade",
  "heading": "What is kept",
  "url": "docs/backup-and-upgrade/#retention",
  "keywords": "Finished sessions older than 90 days are removed. You change this with the Keep finished sessions (days) setting on the Settings page; 0 keeps them forever. The daily usage history and the members are kept."
 },
 {
  "page": "Troubleshooting",
  "heading": "Logins fail",
  "url": "docs/troubleshooting/#login-problems",
  "keywords": "Nobody can log in, and the login page waits or fails Check in this order: Open the Dashboard and look at the Router link card. If no login request arrived, the router cannot reach the server. Check <SERVER-IP> in /radius , and check that UDP 1812 and UDP 1813 "
 },
 {
  "page": "Troubleshooting",
  "heading": "Router and usage",
  "url": "docs/troubleshooting/#router-problems",
  "keywords": "Kick does nothing, or the admin panel shows an error On the router, run /radius incoming set accept=yes port=3799 . Use the same port as the Disconnect (CoA) port setting on the Settings page. Check that the router firewall accepts UDP 3799 from the server. Th"
 },
 {
  "page": "Troubleshooting",
  "heading": "Admin panel, server and settings",
  "url": "docs/troubleshooting/#admin-problems",
  "keywords": "The times are wrong, or vouchers expire at the wrong hour The default time zone is UTC . Set your time zone on the Settings page, for example Europe/London . Dates, daily usage and voucher expiry times follow it. I forgot the admin password Run hotspotplusligh"
 }
];
