// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
)

// access is the level of caller a route requires once authentication is
// active (an account exists and auth has not been disabled). #653
// introduced the third tier: viewer, sitting below user, replacing what
// used to be the single "any authenticated session" level of that name.
// Tiers stack (gauntlet.Role.AtLeast): admin implies user implies viewer.
type access int

const (
	// accessPublic: reachable with no session at all. Every entry here
	// is a deliberate hole in the authentication wall and should be
	// scrutinised on sight.
	accessPublic access = iota
	// accessViewer: any authenticated session, regardless of role --
	// what accessUser used to mean before #653 split the non-admin space
	// in two. A viewer may see everything at this tier but change
	// nothing that affects the instance.
	accessViewer
	// accessUser: an authenticated session whose role is user or better
	// (i.e. user or admin) -- the operational tier #653 introduced for
	// writes that change what mikroview is watching or showing, without
	// touching the instance itself (accounts, tokens, config).
	accessUser
	// accessAdmin: an authenticated session whose role is admin.
	accessAdmin
)

func (a access) String() string {
	switch a {
	case accessPublic:
		return "public"
	case accessViewer:
		return "viewer"
	case accessUser:
		return "user"
	default:
		return "admin"
	}
}

// allowsRole reports whether access level a permits a caller holding
// role, per gauntlet.Role.AtLeast's stacked tiers. accessPublic permits
// every role including no session at all, which is why the anonymous
// caller in TestAuthorizationMatrixIsEnforced isn't driven through this
// method -- there's no gauntlet.Role to hold when there's no session.
func (a access) allowsRole(role gauntlet.Role) bool {
	switch a {
	case accessPublic:
		return true
	case accessViewer:
		return role.AtLeast(gauntlet.RoleViewer)
	case accessUser:
		return role.AtLeast(gauntlet.RoleUser)
	default:
		return role.AtLeast(gauntlet.RoleAdmin)
	}
}

// routeExpectation is one row of the authorization matrix: a route, the
// access level it must enforce, and why.
type routeExpectation struct {
	method string
	path   string
	want   access
	// why documents the intent, so a future change that flips a level
	// has to consciously rewrite the justification rather than quietly
	// edit a constant.
	why string
}

// authzMatrix is the single source of truth for who may reach what.
//
// This exists because per-endpoint spot-check tests only cover the
// endpoints somebody remembered to write a test for. A real permission
// gap shipped exactly that way: POST /api/flags/{id}/clear-permanent
// was reachable by any authenticated non-admin and could permanently
// suppress detection for a chosen target, unlogged, because no test
// asserted otherwise and the omission looked like a deliberate choice.
//
// TestEveryRouteIsInTheAuthorizationMatrix below fails when a route is
// added to Routes() without a row here, so the next endpoint cannot
// reach main without someone stating its access level on the record.
var authzMatrix = []routeExpectation{
	// -- Public by necessity -------------------------------------------
	{http.MethodGet, "/api/healthz", accessPublic,
		"liveness probe -- hit by Docker's HEALTHCHECK, which has no session"},

	{http.MethodGet, "/api/config/problems", accessAdmin,
		"config key names, filesystem paths, the OIDC issuer URL and SMTP hosts are an infrastructure map; a non-admin gets an empty list rather than a 403, since whether problems exist is itself information"},
	{http.MethodGet, "/api/persistence", accessAdmin,
		"reports which backend (a JSON store's directory, or Postgres) this deployment's persisted state actually uses (#677's settings persistence row) -- a filesystem path is the same infrastructure-map disclosure /api/config/problems above is admin-gated for, so this follows it rather than defaulting to viewer the way most of Settings' other reads do"},
	{http.MethodGet, "/api/config/upgrade", accessAdmin,
		"the \"N new settings are available\" notice (#1218) and its ready-to-paste YAML -- admin-only like the wizard's own writes, since there is no read-only wizard for a viewer to reach this alongside"},

	// The config editor (#1347). Every route admin-only: the config is
	// the instance itself, secrets included, and the editor's whole job
	// is producing the file that decides everything else. Setup-only
	// mode serves these same rows (setuponly.go).
	{http.MethodPost, "/api/config/editor/open", accessAdmin,
		"re-checks the caller's password and returns the running config's text (secrets masked) and path -- the " +
			"infrastructure map /api/config/problems is admin-gated for, and then some; the password is on top of the role, not instead of it"},
	{http.MethodGet, "/api/config/editor/summary", accessAdmin,
		"the Engine Room Config card's facts about the running config -- its path, header, schema and snapshot count, " +
			"no text and no secrets, so no password either; admin-only for the path, the same infrastructure-map disclosure as /api/config/problems"},
	{http.MethodGet, "/api/config/editor/reveal", accessAdmin,
		"hands back the config's secret values in the clear, within the fifteen-minute unlock the password bought -- the most sensitive read here"},
	{http.MethodPost, "/api/config/validate", accessAdmin,
		"runs a config text through start-up's checks; the answers name keys, paths and hosts, the same disclosure as /api/config/problems"},
	{http.MethodPost, "/api/config/carry-forward", accessAdmin,
		"rewrites a config text for this build and snapshots the old one, writing to the snapshot store and the audit log"},
	{http.MethodPost, "/api/config/download", accessAdmin,
		"returns the whole config file with its secrets put back, within the unlock window -- a config file is the instance's keys"},
	{http.MethodGet, "/api/config/snapshots", accessAdmin,
		"lists the kept config snapshots (who took them, when, why); the texts themselves are behind the unlock on the route below"},
	{http.MethodPost, "/api/config/snapshots", accessAdmin,
		"keeps a whole config file, secrets included, in the snapshot store, and writes the audit log"},
	{http.MethodGet, "/api/config/snapshots/{id}", accessAdmin,
		"returns one kept config file (secrets masked, their values released to this session), within the unlock window"},
	{http.MethodDelete, "/api/config/snapshots/{id}", accessAdmin,
		"deletes a kept config file, possibly the copy a rollback needs, and writes the audit log"},

	{http.MethodGet, "/api/upgrade", accessViewer,
		"the upgrade notice's facts (#1240): the version this data directory last ran, this one, and how many of " +
			"the operator's declared routers are still on the old setup. Deliberately a tier below /api/config/upgrade " +
			"above, which is admin-gated for the config keys and paths it carries: this discloses two version strings " +
			"(the current one is already on every session's header) and a count of routers a viewer can see listed " +
			"anyway. The notice it feeds is still an admin's -- only an admin can paste a script or press done, and " +
			"UpgradeNotice.svelte draws it for nobody else"},
	{http.MethodPost, "/api/upgrade/acknowledge", accessAdmin,
		"the notice's `done` (#1240) -- an instance-wide, persisted statement that the routers have been dealt with, " +
			"so it is the admin's to make, same tier as POST /api/setup/mark and /api/setup/address, and " +
			"audited as upgrade.acknowledged with the admin's name"},

	{http.MethodGet, "/api/router-backups", accessAdmin,
		"lists every router's kept generations and missed-push count (#394) -- admin-only like the disk group's " +
			"own state/key rows beside it in Settings; a viewer never sees this group at all"},
	{http.MethodGet, "/api/router-backups/{device}/{generation}/{kind}", accessAdmin,
		"downloads one generation's .backup or .rsc -- a router's whole configuration, credentials included, so " +
			"this is admin-only and every call writes an audit entry with the admin's name (#394)"},

	{http.MethodGet, "/api/router-backups/{device}/{generation}/text", accessAdmin,
		"reads one generation's stored export on screen (#895) -- the same configuration the download beside it " +
			"hands over as a file, so it sits at the same tier, behind the same vault passphrase gate, and writes " +
			"the same kind of audit entry: reading it in a browser is not a lesser act than saving it"},
	{http.MethodGet, "/api/router-backups/{device}/diff", accessAdmin,
		"compares two of a router's stored exports (#895) -- it opens both, so it can disclose no less than the " +
			"read above and is gated identically; one audit entry names the pair"},

	{http.MethodPost, "/api/router-backups/{device}/{generation}/protect", accessAdmin,
		"marks one stored backup as kept, with a comment saying why (#1126) -- admin-only like the list and the " +
			"download beside it: it is a decision about what this instance holds on its own disk, and the comment " +
			"is an operator's note about their own network"},
	{http.MethodDelete, "/api/router-backups/{device}/{generation}/protect", accessAdmin,
		"releases a kept backup back into the cycling ten, where the oldest may then go -- same tier as keeping " +
			"one, deliberately: this is the only control in the group that can cost the operator a stored " +
			"configuration"},
	{http.MethodPatch, "/api/router-backups/{device}/{generation}/protect", accessAdmin,
		"rewrites a kept backup's comment (#1126) -- same tier as the two above, since it edits the same " +
			"sealed index entry and nothing else reaches it"},

	{http.MethodPost, "/api/router-backups/unlock", accessAdmin,
		"opens the vault's optional passphrase lock for the calling session (#956) -- admin-only for the same " +
			"reason the download is, and the unlock is bound to the session that made this call, so the same " +
			"admin's other sign-ins still see a locked vault"},
	{http.MethodPost, "/api/router-backups/lock", accessAdmin,
		"drops the vault's private key again (#956) -- admin-only, and deliberately allowed from any admin " +
			"session rather than only the one holding the unlock: locking is never the dangerous direction"},
	{http.MethodPost, "/api/router-backups/passphrase", accessAdmin,
		"sets the vault passphrase and re-seals what is already stored (#956) -- admin-only, and irreversible " +
			"without the passphrase from that moment on, mikroview's own operator included"},
	{http.MethodDelete, "/api/router-backups/passphrase", accessAdmin,
		"removes the vault passphrase, which requires the current one (#956): an admin who cannot open the " +
			"vault cannot decide to stop protecting it"},
	{http.MethodPut, "/api/router-backups/passphrase", accessAdmin,
		"changes the vault passphrase in one atomic step, given the current one (#1222): admin-only for the " +
			"same reason set and remove are, and no stored backup is touched since the key pair itself does not " +
			"change"},

	{http.MethodGet, "/api/droplist", accessAdmin,
		"lists every drop-list entry and the pull key's own status (#1224) -- admin-only, matching the router-" +
			"backups group beside it: this is an enforcement list an admin authored by hand (#461), not a read " +
			"any signed-in tier gets by default"},
	{http.MethodPost, "/api/droplist", accessAdmin,
		"adds an entry to block -- admin-only, since #461 settled that a droplist entry is always operator-" +
			"authored and never written by anything else, including a lesser-tier caller"},
	{http.MethodDelete, "/api/droplist/{cidr...}", accessAdmin,
		"removes an entry, CIDR taken from the path as a trailing wildcard -- same tier as adding one, and " +
			"the only removal route: the bodied DELETE /api/droplist added during the v0.6.0 audit reached no " +
			"caller and is gone"},
	{http.MethodPost, "/api/droplist/key", accessAdmin,
		"mints (or rotates) the droplist-pull key a router's own scheduled fetch presents at GET " +
			"/api/droplist.rsc -- admin-only, mirroring POST /api/tokens: minting a bearer credential is a setup " +
			"task, not day-to-day product use. Since #1447 the admin tier is the floor: the admin's password is " +
			"re-checked too, in the enrolment re-check budget"},
	{http.MethodDelete, "/api/droplist/key", accessAdmin,
		"revokes the droplist-pull key -- same tier as minting it"},
	{http.MethodGet, "/api/blocklist/builder", accessAdmin,
		"the blocklist builder's page (#1360): the catalogue, the router's version and what it holds of each " +
			"list -- admin-only beside the drop list, since the page exists to write enforcement onto a router"},
	{http.MethodPost, "/api/blocklist/builder/commands", accessAdmin,
		"renders the blocklist block (#1360), which re-sets the router's push script with an ingest token and " +
			"adds raw drop rules -- a setup task in the same tier as minting the drop list's key"},

	{http.MethodPut, "/api/settings/store", accessAdmin,
		"sets the event buffer's size on the running instance (#796). Admin rather than user tier for two " +
			"separate reasons, either sufficient: it spends the host's memory, which is an instance-wide cost " +
			"nobody else can undo from inside the app, and shrinking it destroys held history -- the only route " +
			"in mikroview by which a caller can discard evidence a viewer or user was relying on. The read half " +
			"is deliberately not here: the figure and its bounds ride GET /api/stats, so a viewer sees the bar " +
			"and the number without being able to move it"},

	{http.MethodGet, "/api/settings/history", accessAdmin,
		"reports the on-disk event history's switch, caps and the window actually held (#910). Admin for the read " +
			"as well, unlike the memory group directly above: the answer names how much custody data this " +
			"deployment keeps and how far back it reaches, which is the same infrastructure disclosure " +
			"/api/persistence is gated for, and the group is drawn on an admin surface so no other tier has " +
			"anywhere to put it"},
	{http.MethodPut, "/api/settings/history", accessAdmin,
		"turns the on-disk event history on or off and sets its two caps (#910). Admin for the memory slider's " +
			"reasons and one of its own: a smaller day count or cap deletes the oldest retained days at once. " +
			"Turning it off keeps the files (#1354); deleting them is the route below"},
	{http.MethodDelete, "/api/settings/history/files", accessAdmin,
		"deletes every retained history file an off history left on disk (#1354) -- the largest single act of " +
			"evidence destruction available anywhere in mikroview, so admin-only and behind a password re-check " +
			"on top of the session"},

	{http.MethodPut, "/api/settings/router-backups", accessAdmin,
		"opens or closes the router-backup SFTP drop box (#1361) -- a second listening port a stolen session " +
			"could otherwise open, so admin-only and password-gated on the way open (closing needs no password: " +
			"it is the safe direction). Same tier as the history switch above, for the same reason: an on/off " +
			"control with an instance-wide, hard-to-undo cost"},

	{http.MethodGet, "/api/settings/geo", accessAdmin,
		"the Engine Room's \"Country and network owner\" card (#1352): which country source is live and each " +
			"source's fetch state, plus whether an API key is set and by whom -- never the key itself. Admin for " +
			"the read too: the card sits in the admin-only Engine Room grid, and who configured which provider " +
			"account is instance configuration, not something the stream needs (healthz carries the one fact it " +
			"does need, geoSource)"},
	{http.MethodPut, "/api/settings/geo/ipinfo", accessAdmin,
		"stores an IPinfo Lite token, sealed under the retention key (#1352) -- entering a credential is a setup " +
			"task, the same tier as minting the droplist pull key"},
	{http.MethodDelete, "/api/settings/geo/ipinfo", accessAdmin,
		"removes the IPinfo token -- same tier as setting it"},
	{http.MethodPut, "/api/settings/geo/maxmind", accessAdmin,
		"stores a MaxMind account ID and licence key, sealed under the retention key (#1352) -- same tier as " +
			"the IPinfo token"},
	{http.MethodDelete, "/api/settings/geo/maxmind", accessAdmin,
		"removes the MaxMind credentials -- same tier as setting them"},
	{http.MethodGet, "/api/geo/lookup", accessViewer,
		"on-demand country and network owner for one public address (#1352), for the IP popover and host " +
			"dossier -- any signed-in caller, like /api/lookup/ip/{ip}: it reveals nothing about an address the " +
			"stream is not already showing, and no key or provider detail reaches the browser"},

	// -- Any authenticated session (viewer tier) ------------------------
	{http.MethodGet, "/api/events", accessViewer,
		"core read: the live firewall event feed"},
	{http.MethodGet, "/api/devices", accessViewer, "core read"},
	{http.MethodGet, "/api/devices/macs", accessViewer,
		"core read, same tier as /api/devices which it complements -- the persisted MAC-registry history " +
			"(first/last-seen, last-paired IP) backing the Entities page's named-things table (#675). No more " +
			"sensitive than the source IPs a viewer already reads off /api/events; it's a LAN client's MAC, not " +
			"credentials or config"},
	{http.MethodGet, "/api/matches", accessViewer,
		"a read over already-collected evidence, same tier as events/flags/stats/devices above -- also reachable via a read-only API token (readOnlyRoutes), since birdcage-style external correlation by source is the reason internal/matchlog exists. Renamed from /api/watchlist/matches by #407 when the watchlist noun was retired; the access decision is unchanged. " +
			"WIDENED by #586, and the widening is the part to scrutinise: entries=all serves the most recent matches across every entry, so a caller no longer needs to know a mac or ip to read from this log, and that includes a read-only token holder. Kept on this tier deliberately rather than promoted to admin, for three reasons. " +
			"The gate the issue asks for is the same gate the existing query carries. The mode learns nothing new in kind: a token that already reaches GET /api/events reads the live feed for every device, and a bounded page of matches is a strictly smaller view of the same traffic. And it is bounded by construction, not by the caller -- matchlog.RecentQuery clamps the limit to 5000 (100 by default) before either backend runs, which is the property that stops an all-entries read on an unrate-limited route being an arbitrarily large response. " +
			"What did NOT change: the per-identity path still refuses an empty identity (matchlog.ErrEmptyIdentity), and entries=all refuses to be combined with mac/ip, so 'no identity' can never silently mean 'every device'"},
	{http.MethodGet, "/api/rules", accessViewer, "core read"},
	{http.MethodGet, "/api/networks", accessViewer,
		"the configured home networks (networks.home, #1481), so the browser classifies inside/outside the " +
			"way the server does -- any signed-in caller, like /api/devices: it is a deployment fact the events " +
			"already imply, and carries no credential"},
	{http.MethodGet, "/api/me/preferences", accessViewer,
		"reads the caller's own preferences record (#1283) -- same reasoning as gauntlet's /api/auth/password: it acts " +
			"only on the session's own account, there is no id in the request that could point it at someone else's, " +
			"and even the lowest tier must be able to see its own presets, widgets and layout"},
	{http.MethodPatch, "/api/me/preferences", accessViewer,
		"merges changed keys into the caller's own preferences record (#1283) -- same tier and same reasoning as " +
			"the GET beside it and as /api/auth/password: a viewer changing their own accent colour or column " +
			"widths is not an operational decision about what mikroview is watching, it is a personal setting " +
			"following them to whichever browser they sign into"},
	{http.MethodGet, "/api/third-party-notices", accessViewer,
		"licence compliance: the copyright/licence texts of everything statically linked into this binary, which MIT/BSD/ISC/Apache-2.0 all require to accompany a binary distribution. Session-gated rather than public only because it is also a precise dependency-and-version inventory -- it withholds nothing, since the same file is in the public repo and the image"},
	{http.MethodGet, "/api/stats", accessViewer, "core read"},
	{http.MethodGet, "/api/stats/tops", accessViewer,
		"#644 round 21's per-minute top-port/top-talker columns -- the same tier as /api/stats above, since it is " +
			"a per-minute breakdown of data that endpoint already exposes in aggregate (byAction), not a new class " +
			"of read. #653's viewer tier took /api/stats down with it, and this follows for the same reason. " +
			"Deliberately NOT in readOnlyRoutes: HourTops' backward scan is heavier than anything else a " +
			"bearer token can already trigger on this tier, and nothing asked for that to be token-reachable"},
	{http.MethodGet, "/api/ws", accessViewer,
		"live tail; additionally same-origin checked (see checkOrigin)"},
	{http.MethodGet, "/api/lookup/ip/{ip}", accessViewer,
		"on-demand reputation lookup, proxied so no API key reaches the browser"},
	{http.MethodGet, "/api/routeros/{device}/rules", accessViewer,
		"the pushed firewall rule table (#186 step 4) -- same tier as the event stream it annotates: rule comments/chains are already visible in events, and the lookup button is a user-facing affordance"},
	{http.MethodGet, "/api/routeros/{device}/nat", accessViewer,
		"the pushed NAT table, same reasoning as the rules row above"},
	{http.MethodGet, "/api/routeros/{device}/addresses", accessViewer,
		"the pushed /ip/address table (#627), same tier as the rules/NAT rows above"},
	{http.MethodGet, "/api/routeros/{device}/services", accessViewer,
		"the pushed /ip/service table (#1329) -- the router's own management services, same tier as the rules/NAT/addresses rows above"},
	{http.MethodGet, "/api/routeros/{device}/wireguard", accessViewer,
		"the pushed WireGuard tables with derived per-tunnel state (#874), same tier as the rules/NAT/addresses rows above -- display data annotating what a viewer already sees on the topography"},
	{http.MethodGet, "/api/routeros/{device}/ppp-active", accessViewer,
		"the pushed /ppp/active table (#874), same tier and reasoning as the wireguard row above"},
	{http.MethodGet, "/api/flags", accessViewer, "core read"},
	{http.MethodGet, "/api/flags/expectations", accessViewer,
		"core read (#640's ledger): an expectation is the reason a firing is absent from the flags card above, so a caller who may read the flags but not the expectations behind them is reading half the story -- and reading the ledger changes nothing. Deliberately NOT in readOnlyRoutes: nothing asked for it to be token-reachable"},

	// -- Operational writes (user tier) ---------------------------------
	//
	// #653 introduced this tier below admin: these four flag actions used
	// to be open to any authenticated session (the old accessUser, now
	// called accessViewer above). The owner's ruling on #653 is that a
	// viewer -- who must not change anything that affects the instance --
	// may not make even a reversible change to what mikroview is
	// currently showing, so these tightened to require at least the user
	// role.
	{http.MethodPost, "/api/flags/clear-all", accessUser,
		"reversible: a cleared flag raises again on the next matching event, and a bulk clear records no expectation. Tightened from viewer to user tier by #653: reversible or not, this changes what mikroview is showing, which a viewer may not do"},
	{http.MethodPost, "/api/syslog/loss/clear", accessUser,
		"#1015: zeroes the ingest-loss counters GET /api/stats' syslog.loss reads (five since #1234 added duplicate-source detection). Same reasoning as " +
			"/api/flags/clear-all directly above -- reversible (a cleared counter starts a fresh episode on the " +
			"next occurrence), and a viewer may not change what mikroview is currently showing"},
	{http.MethodPost, "/api/flags/{id}/verdict", accessUser,
		"#640: the four verdicts, and the only way one flag leaves the inbox now that the plain clear and " +
			"the admin-only clear-permanent are gone. User tier for all four, per the ratified design: the " +
			"expectation an expected verdict records is bounded by the firing the operator just looked at " +
			"and withdrawn by the undo below, where the exclude-forever it replaces was unbounded and " +
			"admin-only. Audit-logged, so who decided a pair stops being flagged stays answerable. " +
			"Tightened from viewer to user tier by #653, same reasoning as clear-all above"},
	{http.MethodDelete, "/api/flags/verdict/{id}", accessUser,
		"#638's undo affordance for the row above, and #640's withdrawal of the expectation an expected " +
			"verdict recorded -- same tier as judging in the first place, since reversing a judgement is no " +
			"more dangerous than making one. Not \"/{id}/verdict\": see the registration comment in " +
			"server.go for why that shape can't be registered here. Tightened from viewer to user tier by " +
			"#653, same reasoning as clear-all above"},
	{http.MethodPut, "/api/flags/{id}/note", accessUser,
		"#1232: edits the note on a flag that already carries a verdict. Same tier as the verdict the note " +
			"explains, for the same #653 reason -- the note is part of the record a returning flag reads " +
			"back, so writing one changes what mikroview will show, which a viewer may not do. " +
			"Audit-logged as flag.note_edit, carrying no text"},
	{http.MethodDelete, "/api/flags/expectations/{id}", accessUser,
		"#640's Forget control on the ledger -- same tier as the verdict that records an expectation, since " +
			"the operator who can say \"expected\" can take it back, and an undo must not be harder to reach " +
			"than the thing it undoes. Forgetting only ever re-arms detection, which is the " +
			"safe direction"},

	// -- Admin only ----------------------------------------------------
	// The definitions surface (#407). #385 records the owner decision
	// that non-admins should eventually see settings surfaces read-only;
	// #490 was that phase 2's RBAC work, widening the list GET below one
	// row at a time while leaving every mutation here admin-only. #653
	// finished the job differently than #490 anticipated: rather than
	// widening each write to read-only-for-viewer, the owner's ruling
	// gave the whole surface -- reads and writes alike -- to the user
	// tier (the "watchers" bench gets full access), leaving only the
	// admin-only account/token/audit/config surfaces below untouched.
	{http.MethodGet, "/api/coverage/declarations", accessViewer,
		"a coverage-gap declaration (#630/#392) explains why a boundary-direction pair is intentionally quiet -- reading that explanation is the same viewer-tier read as GET /api/definitions below, not the user-tier write that authors one"},
	{http.MethodGet, "/api/definitions", accessViewer,
		"widened for the viewer-readable settings page (#490): a signed-in caller, even at the lowest tier, can see every definition's on/off state, scope and tuned params, same as an admin -- the design record's authz-matrix clause widens this GET deliberately. #653 went on to widen every write below it too, from admin to user tier, but left this one GET at viewer -- a viewer may see the whole surface, just not touch it"},
	{http.MethodGet, "/api/decommission", accessViewer,
		"the retiring segments the map draws, and the offers waiting to be answered (#460). Viewer tier for the same reason GET /api/definitions is: a viewer already sees the zones, the hosts on them and the watches over them, so a zone that is retiring is nothing new to them -- and hiding it would make the map misdescribe the network rather than protect anything"},
	{http.MethodPost, "/api/decommission/watches", accessUser,
		"answers a departure offer with yes, creating a watch over the retired range. Same tier and the same reasoning as POST /api/definitions below: this adds server-side traffic surveillance, which a viewer may not do"},
	{http.MethodPost, "/api/decommission/dismiss", accessUser,
		"answers a departure offer with no. User tier rather than viewer even though it creates nothing: declining drops the offer, so a viewer could otherwise silently spend the operator's one chance to watch a range that has just been retired"},
	{http.MethodPost, "/api/decommission/watches/{id}/force", accessUser,
		"forces a retiring segment off the map while its traffic persists -- a recorded override in the #385 pattern. User tier, not admin: it changes what the map shows and nothing about the instance, and the watch itself survives, so nothing is lost that an admin would need to authorise"},
	{http.MethodPost, "/api/decommission/watches/{id}/undo", accessUser,
		"takes a silent retirement back within the hour after it happened. Same tier as creating the watch, because that is what it does -- it puts the range back under surveillance the operator had stopped"},
	{http.MethodDelete, "/api/decommission/watches/{id}", accessUser,
		"abandons a decommission watch. Same tier as deleting a definition below -- it stops server-side surveillance the operator asked for"},
	{http.MethodPost, "/api/definitions", accessUser,
		"creates a definition. Was accessAdmin; #653's \"watchers\" bench ruling widened this to user tier -- a viewer still may not, since adding server-side traffic surveillance changes the instance"},
	{http.MethodGet, "/api/definitions/schema", accessUser,
		"the tunable knobs of every definition this deployment holds; same tier as the definitions themselves, and it enumerates the catalogue. Widened from admin to user tier by #653, same as the rest of this surface"},
	{http.MethodGet, "/api/definitions/{id}", accessUser,
		"one definition, same reasoning as the list. Widened from admin to user tier by #653"},
	{http.MethodPut, "/api/definitions/{id}", accessUser,
		"disabling a definition blinds the tool, and re-tuning one changes what fires. Was accessAdmin; #653's \"watchers\" bench ruling widened this to user tier"},
	{http.MethodDelete, "/api/definitions/{id}", accessUser,
		"removes an operator-authored definition entirely; same weight as creating it. Widened from admin to user tier by #653, same as POST above"},
	{http.MethodPost, "/api/definitions/{id}/clone", accessUser,
		"creates a definition, same reasoning as POST /api/definitions. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/reset", accessUser,
		"discards every param override in one call -- a configuration change, same tier as PUT. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/replay", accessUser,
		"re-runs a definition over the stored event corpus with candidate params: it reads every event in the ring and returns matching evidence, so it is at least as revealing as the definition list it belongs to. Widened from admin to user tier by #653, same as the rest of this surface"},
	{http.MethodPost, "/api/definitions/{id}/promote", accessUser,
		"changes what future traffic counts as expected for a device -- same weight as creating the definition. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/definitions/{id}/observing", accessUser,
		"same reasoning as promote. Widened from admin to user tier by #653"},
	{http.MethodGet, "/api/naming/provenance", accessUser,
		"says which layer supplies the name shown for one token, and whether a label saved here would be shadowed by a router-pushed one (#413). Was admin for two reasons: the editor it serves gave admins alone a pencil, so no non-admin ever called it; and the answer is a partial map of which router names which host, the same administrative metadata GET /api/entities is gated for. #653 widened both this and /api/entities to user tier together, so the reasoning now matches: a viewer still gets no pencil, but a user does, same as the entities surface it serves"},

	{http.MethodGet, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653's \"watchers\" bench ruling"},
	{http.MethodPost, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653"},
	{http.MethodDelete, "/api/entities", accessUser, "admin-managed labels/tags -- widened from admin to user tier by #653"},
	{http.MethodPut, "/api/coverage/declarations/{key}", accessUser,
		"declaring a boundary intentionally quiet is an on-record explanation, same weight as an entity label -- and #653 moved entity labels to the user tier, so this row followed the reasoning its own justification already rested on rather than staying admin beside a neighbour that moved"},
	{http.MethodDelete, "/api/coverage/declarations/{key}", accessUser,
		"undeclares a coverage gap, re-exposing it as unexplained -- same tier as creating it"},

	{http.MethodGet, "/api/hosts", accessViewer,
		"the host presence register (#1016): which hosts the feed has shown, when each was last heard from, and which quiet ones somebody has already explained. Same viewer-tier read as GET /api/coverage/declarations directly above, and for the same reason -- a non-admin looking at the map is exactly who needs to know whether a grey host is quiet on purpose. Deliberately not on readOnlyRoutes: it is a partial inventory of the operator's private address space, which no bearer token has ever been able to read"},
	{http.MethodPut, "/api/hosts/{key}/mark", accessUser,
		"marking a quiet host intended, or dismissing it, is an on-record statement about a silence -- the same weight as declaring a coverage gap intentionally quiet, so it takes the same user tier and the same audit line"},
	{http.MethodDelete, "/api/hosts/{key}/mark", accessUser,
		"withdraws that statement, putting the host back to whatever its own last-seen time says it is -- same tier as making it, exactly as DELETE /api/coverage/declarations/{key} sits at its sibling's tier"},

	{http.MethodGet, "/api/hosts/{ip}/dossier", accessViewer,
		"the device dossier (#410): everything already known about one address, assembled -- its traffic, MAC and vendor, names and their provenance, lease-versus-fixed, first/last seen, matched rules, and a suggested identity with its evidence. A read of data this instance already holds, at the same viewer tier as GET /api/hosts above and for the same reason: identifying an unknown host is exactly what a non-admin looking at the map needs to do. It changes nothing and it never touches the host it describes -- the probe command it prints is for the operator to run. Deliberately not on readOnlyRoutes, and more sharply than GET /api/hosts: this is one host's traffic, ports, peers and hardware address in a single response, which no bearer token has ever been able to read"},

	{http.MethodGet, "/api/baseline/off", accessViewer,
		"today's off-baseline lines (#1016 round 49): the source/destination/port/protocol lines seen today that are not on the established pattern, with the threshold that judged them. Same viewer-tier read as GET /api/hosts directly above and for the same reason -- a non-admin looking at the map is exactly who needs to see what is off pattern today. Deliberately not on readOnlyRoutes for the same reason either, and more sharply: this is the operator's private address space with destinations and ports attached, which no bearer token has ever been able to read. Established lines are unreachable through this endpoint by design, not by permission -- see handleBaselineOff"},
	{http.MethodGet, "/api/ports", accessViewer,
		"the port filter's answer (#1018 round 53): which boundaries and hosts carried a port in the window, and which pushed filter rules name it. Same viewer-tier read as GET /api/baseline/off directly above and for the same reason -- a non-admin looking at the map is exactly who asks where a port is used. Deliberately not on readOnlyRoutes for the same reason too: it is the operator's private address space with a port attached, which no bearer token has ever been able to read"},
	{http.MethodGet, "/api/trace", accessViewer,
		"one logged line's single hop through the router (#1018 round 53): the interfaces it came in and left on, the rule that decided, the NAT if any. Same viewer tier and the same off-readOnlyRoutes reasoning as GET /api/ports above -- it names both ends of one connection on the operator's own network"},
	{http.MethodGet, "/api/ports/serving", accessViewer,
		"the 'seen serving' lens's answer (#1320): every host the window saw actually answer, and what. Same viewer tier and the same off-readOnlyRoutes reasoning as GET /api/ports directly above, for the same reason -- it is the operator's own address space with the ports each host answered on, which no bearer token has ever been able to read"},
	{http.MethodGet, "/api/doors/internet", accessViewer,
		"the doors panel's answer (#1319): every pushed rule that lets the internet reach the router itself or a host behind it, and the pushed /ip service rows beside them. Same viewer tier and the same off-readOnlyRoutes reasoning as GET /api/ports directly above, for the same reason -- it is the operator's own address space with which hosts a WAN door leads to, which no bearer token has ever been able to read"},
	{http.MethodPut, "/api/baseline/{key}/expected", accessUser,
		"saying a line is expected is an on-record statement that traffic belongs, and it is the only way a line leaves the bright state early -- the same weight as marking a quiet host intended, so the same user tier and the same audit line. It also exempts the line from eviction, which is a second reason it is not a viewer's to make"},
	{http.MethodDelete, "/api/baseline/{key}/expected", accessUser,
		"withdraws that statement, putting the line back to whatever its own recurrence says it is -- same tier as making it, exactly as DELETE /api/hosts/{key}/mark sits at its sibling's tier"},

	{http.MethodGet, "/api/seen-values", accessViewer,
		"the values this instance has actually seen for the two filter fields with no list anywhere else -- protocol and interface (#1226). Same viewer-tier read as GET /api/hosts above and for a sharper version of the same reason: this is what a non-admin needs to set a filter at all, and refusing it would leave them with the free-text box the issue exists to replace. It records nothing and decides nothing. Deliberately not on readOnlyRoutes, same line as GET /api/hosts: the interface list is a partial inventory of the operator's network shape, which no bearer token has ever been able to read"},

	{http.MethodGet, "/api/suggestions", accessUser,
		"a suggestion's Justification names a specific rule/device -- same tier as the expectation definitions it can become. Widened from admin to user tier by #653, same as the definitions surface"},
	{http.MethodPost, "/api/suggestions/{id}/accept", accessUser,
		"creates a real expectation definition -- same reasoning as POST /api/definitions. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/{id}/hide", accessUser,
		"same tier as accept: declining a suggestion is the same class of decision. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/{id}/unhide", accessUser,
		"same reasoning as hide. Widened from admin to user tier by #653"},
	{http.MethodPost, "/api/suggestions/reset", accessUser,
		"destructively wipes every expectation definition -- the most dangerous single endpoint in this feature, and the one row in this whole surface the owner's #653 ruling considered keeping admin-only for that reason. It was widened to user tier anyway, on the view that the confirm:true body this handler requires is the real safeguard against an accidental call, not the role gate -- a safeguard user and admin are equally bound by"},

	{http.MethodPost, "/api/devices", accessAdmin,
		"declares a syslog-only router by name (issue #1281) -- admin-only, same tier as POST /api/tokens beside it: " +
			"this creates a new device identity, and the router it names has no ingest token to auto-discover it " +
			"through and no config.yaml sourceIp to declare it with"},
	{http.MethodDelete, "/api/devices/{id}", accessAdmin,
		"removes a device this registry itself created, clearing its enrolled address and any pending token with " +
			"it -- same tier as creating one. A config.yaml declaration refuses with 400 rather than reaching this " +
			"tier check meaningfully, since it would simply reappear on the next restart"},
	{http.MethodPost, "/api/devices/{id}/enrolment", accessAdmin,
		"mints (or rerolls) a device's enrolment token (#1281) -- a bearer credential that attributes syslog traffic " +
			"to a device, the same tier every other token-issuing endpoint in this API holds to (POST /api/tokens, " +
			"POST /api/droplist/key). Since #1291 the admin tier is the floor, not the whole check: this endpoint " +
			"also re-verifies the caller's password at the moment of minting, so a stolen session is not enough"},
	{http.MethodPost, "/api/devices/{id}/enrolment/address", accessAdmin,
		"points a pending enrolment window at a different address without touching the token (#1291, ruling 23a) -- " +
			"the one-click recovery when the operator named the wrong address and their router was turned away at " +
			"accept. No password re-proof, unlike minting: rebinding grants no acceptance (the token still has to " +
			"arrive from that address), and the registry only accepts an address already in the refused-senders " +
			"list, so it can open the window to somewhere that already reached the listener and nowhere else"},
	{http.MethodDelete, "/api/devices/{id}/enrolment", accessAdmin,
		"revokes a device's pending enrolment token before it is redeemed -- same tier as minting one"},
	{http.MethodGet, "/api/devices/refused", accessAdmin,
		"every syslog source address the listener gate has refused a line from (#1281) -- unlike GET /api/devices " +
			"above, these addresses have proven nothing about themselves, which is closer to GET /api/audit's " +
			"\"who has been probing this instance\" than to the fleet's own viewer-tier read"},

	{http.MethodGet, "/api/setup/status", accessViewer,
		"widened for the viewer-readable settings page (#490): a signed-in caller at any tier can see every device, source address and pushed table the setup wizard shows, same as an admin. It now also carries the ledger's marks (#487), for the same reason: an empty stream explains its own silence with the forced-past line that accounts for it, and a viewer looking at that stream needs the explanation as much as an admin does. The write side is a separate, admin-only route (POST /api/setup/mark)"},
	{http.MethodPost, "/api/setup/commands", accessViewer,
		"renders the wizard's RouterOS commands (#436) -- same tier as GET /api/setup/status beside it, deliberately: a signed-in caller at any tier can already see the routers, versions and pushed tables this endpoint reads, so it only re-renders that same evidence as copy-paste commands, changing nothing on the instance or the router"},
	{http.MethodPost, "/api/setup/mark", accessAdmin,
		"writes to the setup wizard's claim ledger and to the audit log (#487) -- #490 keeps \"Run setup…\" absent for viewers and there is no read-only wizard, so a viewer has neither a way to reach this nor any business recording a decision under their own name"},
	{http.MethodPost, "/api/setup/address", accessAdmin,
		"writes the wizard header field's answer -- what address a router can reach this instance on (#1213) -- persisted beside the claim ledger's own marks. Same tier as POST /api/setup/mark just above, for the same reason: no read-only wizard reaches this, and every RouterOS command the wizard renders downstream is written against whatever this stores"},
	{http.MethodPut, "/api/setup/backup-transport", accessAdmin,
		"chooses how step 6's router script delivers its backup -- over SFTP to the drop box, or in slices through the ingest channel for an HTTPS-only install (#955). Same tier as POST /api/setup/address just above and for the same reason: it is a property of the deployment, stored beside the address, and it decides what every operator is told to paste into their router"},
	{http.MethodPost, "/api/tune-logging/analyse", accessUser,
		"reads an uploaded RouterOS export and reports which filter rules cross a dark boundary (#435) -- user tier, same as the operational writes above: it changes nothing on the instance or the router, but a viewer may not act on what mikroview is watching, and choosing which rules to tune logging on is exactly that kind of operational decision, made concrete once the operator actually renders it below"},
	{http.MethodPost, "/api/tune-logging/render", accessUser,
		"renders logging switched on for the selected rules from an uploaded export, mechanically checked to differ only in logging (#435) -- same tier as analyse beside it. The output is a file the operator downloads and applies themselves; mikroview never connects to the router, so this is not a write to anything mikroview itself is exposed on -- but it is the same class of change-what-is-watched decision analyse already gates at user tier"},
	{http.MethodGet, "/api/audit", accessAdmin,
		"the admin action trail; also the record an attacker would want to read to see whether they were noticed"},

	// -- Test hooks (MV_TEST_HOOKS=1 only) -----------------------------
	// These two are registered only when the process was started with
	// that variable set, which no shipped image does -- so on a real
	// deployment they are not refused, they do not exist. The rows below
	// pin the tier they enforce on the harness instances where they *do*
	// exist; TestTestHookRoutesAreAbsentWithoutTheFlag pins the absence.
	{http.MethodPost, "/api/test/clock", accessAdmin,
		"moves this process's watch clock forward (#1063), so a scenario that needs a watch window to have closed " +
			"does not have to wait real minutes for it. Admin, matching the reset beside it: the flag is the real " +
			"gate, and there is no reason for the second lock to be weaker than the one on PUT /api/settings/store, " +
			"which changes far less"},
	{http.MethodPost, "/api/test/reset", accessAdmin,
		"erases every event, flag, match, pushed router table and definition on the instance (#1064) -- the single " +
			"most destructive call in this API, and the only one that discards evidence a viewer or user was " +
			"relying on without so much as a confirm body. Admin for the same reason PUT /api/settings/history is, " +
			"and gated behind MV_TEST_HOOKS on top of that"},
}

// TestEveryRouteIsInTheAuthorizationMatrix is the guard that makes the
// matrix meaningful: it walks the real mux's registered patterns and
// fails if any route lacks a row above. Without it, the matrix would
// silently describe an ever-smaller fraction of the API as routes get
// added.
func TestEveryRouteIsInTheAuthorizationMatrix(t *testing.T) {
	declared := make(map[string]bool, len(authzMatrix))
	for _, r := range authzMatrix {
		declared[r.method+" "+r.path] = true
	}

	for _, pattern := range registeredRoutePatterns(t) {
		if !declared[pattern] {
			t.Errorf("route %q is registered but has no row in authzMatrix.\n"+
				"Add one stating who may reach it and why -- a new endpoint must not ship without that decision "+
				"being made explicitly. (This check exists because clear-permanent shipped unguarded exactly this way.)",
				pattern)
		}
	}

	// The reverse direction: a stale row for a route that no longer
	// exists would give false confidence that something is still being
	// checked.
	registered := make(map[string]bool)
	for _, p := range registeredRoutePatterns(t) {
		registered[p] = true
	}
	for _, r := range authzMatrix {
		if !registered[r.method+" "+r.path] {
			t.Errorf("authzMatrix has a row for %s %s, but no such route is registered -- remove the stale row",
				r.method, r.path)
		}
	}
}

// retiredRoutes are routes this API used to serve and must not serve
// again. A retired route has no row in authzMatrix -- there is no tier
// to state -- so without this list nothing would notice one coming
// back. TestRetiredRoutesAreAbsent proves each is gone, not merely
// admin-only.
var retiredRoutes = []routeExpectation{
	{http.MethodPost, "/api/devices/{id}/registration", accessAdmin,
		"the ledger's old Register step (#1291), retired by #1493: a router is registered the moment its enrol " +
			"line is accepted, so no web request records a registration any more"},
}

// TestRetiredRoutesAreAbsent checks each retired route twice: it is not
// in the route table, and an admin -- who passes every tier check --
// gets 404 from the running server.
func TestRetiredRoutesAreAbsent(t *testing.T) {
	registered := make(map[string]bool)
	for _, p := range registeredRoutePatterns(t) {
		registered[p] = true
	}

	s := newAuthTestServer(t)
	s.TestHooks = true
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)

	for _, r := range retiredRoutes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			if registered[r.method+" "+r.path] {
				t.Errorf("%s %s is registered again, but it was retired: %s", r.method, r.path, r.why)
			}
			resp := doRouteRequest(t, admin, ts.URL, r)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("admin %s %s: status %d, want 404 for a retired route", r.method, r.path, resp.StatusCode)
			}
		})
	}
}

// TestAuthorizationMatrixIsEnforced drives every row against a running
// server in the state that actually matters -- auth active, with an
// admin and one account at each of the other two roles (#653) -- and
// asserts each of the four caller kinds gets what the matrix says.
//
// Every account holds a confirmed factor, so none is held at gauntlet's
// must-enrol-factor door: that door is gauntlet's subject, and this
// matrix is about mikroview's own role tiers. The /api/auth/* and
// /api/tokens routes are gauntlet's (#1202) and covered by its own
// suite, so they have no rows here.
func TestAuthorizationMatrixIsEnforced(t *testing.T) {
	s := newAuthTestServer(t)
	// The matrix covers the two MV_TEST_HOOKS routes, so the server it is
	// driven against has to be one that registers them. Turning the flag
	// on here rather than in newTestServer keeps every other test running
	// against the route table a shipped image serves.
	s.TestHooks = true
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	anon := &http.Client{}
	admin := registerAdmin(t, s, ts)
	user := createAccount(t, s, ts, "operator", fixtureUserPassword, gauntlet.RoleUser)
	viewer := createAccount(t, s, ts, "watcher", fixtureOtherPassword, gauntlet.RoleViewer)

	for _, r := range authzMatrix {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			assertAccess(t, anon, ts.URL, r, "anonymous", r.want == accessPublic)
			assertAccess(t, viewer, ts.URL, r, "viewer", r.want.allowsRole(gauntlet.RoleViewer))
			assertAccess(t, user, ts.URL, r, "user", r.want.allowsRole(gauntlet.RoleUser))
			assertAccess(t, admin, ts.URL, r, "admin", true)
		})
	}
}

// assertAccess issues one request and checks only whether it was let
// through the authorization layer -- not whether the handler behind it
// succeeded. A 404 for a placeholder ID, or a 400 for a body this test
// deliberately doesn't construct, both mean "you got past the gate",
// which is the only thing under test here. 401/403 mean refused.
func assertAccess(t *testing.T, c *http.Client, base string, r routeExpectation, who string, wantAllowed bool) {
	t.Helper()

	resp := doRouteRequest(t, c, base, r)
	defer resp.Body.Close()

	refused := resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden
	allowed := !refused

	if allowed != wantAllowed {
		verb := "was refused"
		if allowed {
			verb = "was allowed through"
		}
		t.Errorf("%s caller %s on %s %s (status %d), but the matrix says access is %q.\nReason on record: %s",
			who, verb, r.method, r.path, resp.StatusCode, r.want, r.why)
	}
}

// doRouteRequest turns a matrix row into a concrete request: wildcard
// segments get a placeholder that will not match real data (the handler
// behind it is expected to 404, which still proves the gate allowed the
// call through), and every non-GET carries the CSRF header so that
// check never masquerades as an authorization failure.
func doRouteRequest(t *testing.T, c *http.Client, base string, r routeExpectation) *http.Response {
	t.Helper()

	path := r.path
	for _, wildcard := range []string{"{id}", "{ip}", "{name}", "{device}"} {
		path = strings.Replace(path, wildcard, "matrix-probe-nonexistent", 1)
	}

	req, err := http.NewRequest(r.method, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.method != http.MethodGet {
		req.Header.Set(csrfHeaderName, csrfHeaderValue)
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", r.method, path, err)
	}
	return resp
}

// registeredRoutePatterns reads the real route table -- the reason
// Server.routes() exists as data (see server.go).
func registeredRoutePatterns(t *testing.T) []string {
	t.Helper()
	s, _ := newTestServer(t)
	// With the test hooks on, so the guard walks every route this binary
	// can serve rather than only the ones a shipped image does -- a
	// test-only route still has to state its access level, and a stale
	// row for one still has to be noticed.
	s.TestHooks = true
	out := make([]string, 0, len(s.routes()))
	for _, r := range s.routes() {
		out = append(out, r.method+" "+r.path)
	}
	return out
}

// bearerMuxRoutes pins what the two bearer-token muxes serve. The
// matrix guard above walks Server.routes(), which is the session-
// authenticated mux only -- readOnlyRoutes() and ingestRoutes() are
// deliberately separate ServeMuxes (that separation is what makes "a
// token cannot reach a write endpoint" structural rather than checked),
// and nothing failed if a route were added to either one.
//
// These are the muxes a stolen credential dispatches to, so their
// contents are exactly what the blast radius of a leaked token is.
// Adding a route here is a decision to widen that; this test makes it a
// deliberate one.
var bearerMuxRoutes = map[string][]string{
	"read-only": {
		"GET /api/events",
		"GET /api/flags",
		"GET /api/stats",
		"GET /api/devices",
		"GET /api/matches",
	},
	"ingest": {
		"POST /api/ingest/routeros",
		// #955's HTTPS route for a router backup. Widening the ingest
		// token's blast radius is the deliberate decision this list
		// exists to force, and what it buys is bounded: the token names
		// one device, a transfer belongs to the device that began it, and
		// the only thing a holder can do is push a file into that
		// device's own vault slot -- the same thing #394's SFTP
		// credential already does for the same router.
		"POST /api/ingest/router-backup",
	},
	"droplist-pull": {
		// #1224's third bearer mux, and the narrowest of the three: a
		// leaked pull key can read the generated .rsc drop-list feed and
		// nothing else -- not events, not flags, not any router's pushed
		// state, and no write of any kind.
		"GET /api/droplist.rsc",
	},
}

// TestBearerMuxesServeOnlyTheirDeclaredRoutes reads the two
// constructors' source rather than probing the muxes: net/http's
// ServeMux exposes no way to enumerate what was registered, and probing
// can only confirm the routes we already thought of -- it cannot notice
// one that was added. Source is where "a route was added" is visible.
func TestBearerMuxesServeOnlyTheirDeclaredRoutes(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gauntlet.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing gauntlet.go: %v", err)
	}

	found := map[string][]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		var name string
		switch fn.Name.Name {
		case "readOnlyRoutes":
			name = "read-only"
		case "ingestRoutes":
			name = "ingest"
		case "droplistPullRoutes":
			name = "droplist-pull"
		default:
			continue
		}
		found[name] = []string{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "HandleFunc" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s registers a non-literal pattern -- this guard can no longer read it", name)
				return true
			}
			pattern, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquoting %s: %v", lit.Value, err)
			}
			found[name] = append(found[name], pattern)
			return true
		})
	}

	for name, want := range bearerMuxRoutes {
		got, ok := found[name]
		if !ok {
			t.Errorf("could not find the %s mux constructor in gauntlet.go", name)
			continue
		}
		sort.Strings(got)
		expect := append([]string(nil), want...)
		sort.Strings(expect)
		if !reflect.DeepEqual(got, expect) {
			t.Errorf("the %s bearer mux serves %v, declared %v.\n"+
				"If this is intended, update bearerMuxRoutes -- and note that every route here is "+
				"reachable by anyone holding that kind of token, including a router-side script's "+
				"ingest token, which gauntlet's Token documents as readable by any RouterOS "+
				"'read' user.", name, got, expect)
		}
	}
}

// resetCodeSessionOpenPaths is every row of the matrix a session
// flagged MustChangePassword (#1251) may still reach: only the one that
// needs no session at all. The way out of that state, POST
// /api/auth/password, is gauntlet's and has no row here.
//
// Deliberately a literal list -- the point of this test is that widening
// what a half-authenticated session can touch has to be written down
// here as well as done in the gate.
var resetCodeSessionOpenPaths = map[string]bool{
	"/api/healthz": true,
}

// TestResetCodeSessionReachesNothingButTheChangePasswordRoute walks the
// whole authorization matrix with a session established by a one-time
// reset code, and requires a 403 from every row that is not on the short
// list above -- including the admin-tier ones, since the account here is
// an ordinary user, and including the plain reads a viewer could do.
//
// gauntlet's gate holds the door; what this pins is that it holds it in
// front of every one of mikroview's own routes, walked rather than
// sampled for the reason authzMatrix itself exists: a gate that is only
// checked on the endpoints somebody remembered would miss the next one
// added.
func TestResetCodeSessionReachesNothingButTheChangePasswordRoute(t *testing.T) {
	s := newAuthTestServer(t)
	s.TestHooks = true
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	registerAdmin(t, s, ts)
	bilbo, err := s.Auth.CreateUser("bilbo", fixtureUserPassword, gauntlet.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, code, err := s.Auth.IssueResetCode(bilbo.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	flagged := loggedInClient(t, ts.URL, "bilbo", code)

	for _, r := range authzMatrix {
		if resetCodeSessionOpenPaths[r.path] {
			continue
		}
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			resp := doRouteRequest(t, flagged, ts.URL, r)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("a session holding nothing but a reset code got %d from %s %s, want 403.\n"+
					"Until the person sets a password only they know, this session may reach POST /api/auth/password and nothing else.",
					resp.StatusCode, r.method, r.path)
			}
		})
	}
}
