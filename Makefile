.PHONY: build frontend backend test dev-backend dev-frontend docker clean

BINARY := mikroview

build: frontend backend

frontend:
	cd frontend && npm install && npm run build
	rm -rf web/dist
	mkdir -p web/dist
	cp -r frontend/dist/. web/dist/
	# Restore the one tracked file in here. rm -rf above is deliberate --
	# it stops a renamed bundle from leaving its predecessor behind -- but
	# it also takes .gitkeep, which is what keeps go:embed compiling on a
	# clone with no build (#353). Without this line every build leaves a
	# deleted tracked file in `git status`.
	touch web/dist/.gitkeep

backend:
	go build -trimpath -o $(BINARY) .

test:
	go test ./...
	cd frontend && npx svelte-check --tsconfig ./tsconfig.app.json

dev-backend:
	go run . -http :8080

dev-frontend:
	cd frontend && npm run dev

docker:
	docker build -t mikroview .

clean:
	rm -f $(BINARY)
	rm -rf frontend/dist
	# Empty web/dist back to the one tracked file. Nothing to restore
	# from git any more -- .gitkeep is empty and is the only thing in
	# here that is not build output (#353).
	find web/dist -mindepth 1 ! -name .gitkeep -delete 2>/dev/null || true
	touch web/dist/.gitkeep

# live-check: stand the real thing up and drive it in a real browser.
#
# Not the test suite. Nearly every defect worth finding in this project
# was found by running it -- recovery keys reaching the container log, CLI
# commands writing files nothing read, a filter that broke only once
# matching events arrived. None were visible from the code or `go test`.
#
# Add a scenario per change: frontend/scripts/live-<thing>.mjs, importing
# the helpers from live-browser.mjs. live-smoke.mjs is the baseline every
# change runs.
# Two phases, because the checks come in two shapes. The browser
# scenarios drive one shared instance; the standalone scripts each stand
# up and tear down their own server on fixed ports, so they cannot share
# that instance and run after it is down.
#
# Both phases find their checks by glob. That is the point: adding
# frontend/scripts/live-<thing>.mjs or scripts/live-<thing>.sh is
# sufficient, and there is no second edit to forget. Three standalone
# scripts had no runner at all and rotted into being unable to start a
# server, silently, for months (#595, #624).
#
# The trap is load-bearing (#660). `live-env.sh up` detaches its instance
# deliberately, and `down` is the only thing that stops it -- so without
# a trap, a run interrupted before the `down` line (Ctrl-C, a killed
# agent, a session ending mid-scenario) leaves that server holding its
# slot's HTTP port for good. One such leak walled the standalone phase
# for every checkout on the host until it was found by hand, because the
# standalone scripts bind ports from the same range.
#
# EXIT alone is not enough: bash runs an EXIT trap on a normal exit, but
# a SIGINT reaching this shell terminates it without one unless INT is
# trapped too. Trapping both, then calling `down` explicitly between the
# phases, means the instance goes away whether the run finishes, fails or
# is killed. `down` is idempotent, so running it twice costs nothing.
#
# The INT/TERM handler ends in `exit`, and that is not decoration: bash
# resumes the script where it left off once a signal handler returns, so
# a handler that only cleans up leaves Ctrl-C aborting the browser phase
# and then running the whole standalone phase anyway -- minutes of work
# after the operator asked it to stop, observed while verifying #660.
#
# Armed AFTER `up`, never before, and that ordering matters. `down` ends
# with `rm -rf "$$MV_DIR"`, and two checkouts that hash to the same slot
# share that directory. `up` refuses to start when something already
# holds the port precisely so it does not trample a stranger's instance
# -- arming the trap first would have this recipe do exactly that on the
# way out of the refusal it just respected.
# The same gate, on the second host, so it does not hold this machine for
# the better part of an hour. scripts/gate-remote.sh has the reasoning;
# AGENTS.md's "The second host live-check runs on" has the account.
live-check-remote:
	@scripts/gate-remote.sh $(if $(MV_BROWSER),--browser $(MV_BROWSER),)

live-check:
	@eval "$$(scripts/live-env.sh up)"; \
	  trap 'scripts/live-env.sh down >/dev/null 2>&1 || true' EXIT; \
	  trap 'scripts/live-env.sh down >/dev/null 2>&1 || true; exit 130' INT TERM; \
	  status=0; \
	  scripts/run-scenarios.sh || status=1; \
	  scripts/live-env.sh down; \
	  scripts/run-live-scripts.sh || status=1; \
	  exit $$status

.PHONY: live-check

# live-routeros: boot a real RouterOS CHR and point it at a real
# mikroview. Opt-in rather than part of live-check, because it boots a VM
# and only changes that touch RouterOS ingest need it -- see #186.
#
# The router is real MikroTik firmware, so it answers questions no test
# double can: it is what established that /tool fetch refuses a POST body
# over ~64KiB and that :serialize to=json does not exist before 7.13.
live-routeros:
	@eval "$$(MV_BIND=$$(scripts/live-routeros.sh host-addr) scripts/live-env.sh up)"; \
	  test -n "$$MV_URL" || { echo "live-env.sh up produced no MV_URL" >&2; exit 1; }; \
	  eval "$$(scripts/live-routeros.sh up)"; \
	  status=0; \
	  scripts/live-routeros.sh trust "$$MV_URL" || status=1; \
	  scripts/live-routeros.sh run '/system resource print' || status=1; \
	  scripts/live-routeros.sh down; \
	  scripts/live-env.sh down; \
	  exit $$status

.PHONY: live-routeros

# live-container: every live-check scenario, against the image as it
# ships rather than a locally built binary (#273 slice 1).
#
# live-check builds and runs `go build` output on loopback over plain
# HTTP. That leaves three things it structurally cannot exercise, each of
# which has its own failure mode:
#
#   - the distroless image, which has no shell, so anything shelling out
#     works locally and fails there;
#   - the hardening (read-only root, ALL capabilities dropped, pids and
#     memory limits), so a path writing outside its volume works locally
#     and fails there;
#   - TLS as served, which is why every scenario failed at page.goto the
#     first time they were pointed here.
#
# Same scenarios, different environment: MV_ENV_SCRIPT is the only thing
# that changes. A scenario needing to know which environment it is in
# would drift between them, and being the *same* scenarios is the point.
live-container:
	@MV_ENV_SCRIPT=scripts/live-container.sh; export MV_ENV_SCRIPT; \
	  eval "$$(scripts/live-container.sh up)" || exit 1; \
	  test -n "$$MV_URL" || { echo "live-container.sh up produced no MV_URL" >&2; exit 1; }; \
	  status=0; \
	  scripts/run-scenarios.sh || status=1; \
	  if [ $$status -ne 0 ]; then echo "== container log"; scripts/live-container.sh logs | tail -40; fi; \
	  scripts/live-container.sh down; \
	  exit $$status

.PHONY: live-container

# live-container-postgres: the same pass with Postgres behind it.
#
# Separate target rather than a flag on the one above because it is a
# genuinely different deployment, not a variation: #262 made the storage
# backend a fork in behaviour, and every persisted store -- accounts,
# tokens, flags, entities, the match log -- takes a different code path.
# Running the scenarios against only one of them proves half the product.
live-container-postgres:
	@MV_ENV_SCRIPT=scripts/live-container.sh MV_BACKEND=postgres; \
	  export MV_ENV_SCRIPT MV_BACKEND; \
	  eval "$$(scripts/live-container.sh up)" || exit 1; \
	  test -n "$$MV_URL" || { echo "live-container.sh up produced no MV_URL" >&2; exit 1; }; \
	  status=0; \
	  scripts/run-scenarios.sh || status=1; \
	  if [ $$status -ne 0 ]; then echo "== container log"; scripts/live-container.sh logs | tail -40; fi; \
	  scripts/live-container.sh down; \
	  exit $$status

.PHONY: live-container-postgres

# live-routeros-container: the shipped container and a real RouterOS CHR,
# together (#273 slice 2).
#
# live-container proves mikroview works as it ships; live-routeros proves
# a real router can reach it. Neither proves the thing #243's "Done when"
# actually asks for -- that its features work on data a real router
# produced -- because every scenario either half runs feeds synthetic
# syslog. Only frontend/scripts/live-routeros-real.mjs runs here, and it
# is excluded from the plain targets (scripts/run-scenarios.sh) since it
# needs the VM.
#
# MV_BIND is why the container half differs from live-container: the
# router reaches this host through QEMU's user-mode networking, which
# forwards to the QEMU container's stack rather than to this host's
# loopback, so the published ports and the generated certificate both
# have to be on the host's LAN address.
#
# Slow by the standards of the other targets: a CHR boots under TCG here
# (no usable /dev/kvm), and setup completes a real DHCP handshake.
#
# Each `up` is captured and then eval'd, rather than eval'd directly from
# a command substitution. `eval "$(cmd)"` throws the exit status away --
# a failing cmd produces no output and `eval ""` succeeds -- so a router
# that never booted read as a router that booted fine, and the recipe
# went on to drive its serial console. The operator then saw a Python
# traceback and "connection refused" as the top of the log, four errors
# below the line that actually mattered (#613). This target runs rarely,
# by someone without recent context, so it misreporting its own failure
# costs more than it would on a common one.
live-routeros-container:
	@MV_ENV_SCRIPT=scripts/live-container.sh; export MV_ENV_SCRIPT; \
	  MV_BIND=$$(scripts/live-routeros.sh host-addr); export MV_BIND; \
	  env_out=$$(scripts/live-container.sh up) || exit 1; \
	  eval "$$env_out"; \
	  test -n "$$MV_URL" || { echo "live-container.sh up produced no MV_URL" >&2; exit 1; }; \
	  chr_out=$$(scripts/live-routeros.sh up) || { \
	    echo "live-routeros.sh up failed -- stopping here rather than driving a router that never booted" >&2; \
	    scripts/live-container.sh down >/dev/null 2>&1 || true; \
	    exit 1; \
	  }; \
	  eval "$$chr_out"; \
	  status=0; \
	  scripts/live-routeros.sh setup "$$MV_URL" "$$MV_BIND" "$$MV_SYSLOG_TLS_PORT" || status=1; \
	  if [ $$status -eq 0 ]; then \
	    echo "== frontend/scripts/live-routeros-real.mjs"; \
	    ( cd frontend && node ../frontend/scripts/live-routeros-real.mjs ) || status=1; \
	  fi; \
	  if [ $$status -ne 0 ]; then echo "== container log"; scripts/live-container.sh logs | tail -40; fi; \
	  scripts/live-routeros.sh down; \
	  scripts/live-container.sh down; \
	  exit $$status

.PHONY: live-routeros-container

# The door's geometry in the engines the harness doesn't drive.
# The Playwright container ships Firefox and WebKit with their system
# libraries, which the host lacks and cannot install without root --
# run against an already-standing instance: make engines-check
# MV_URL=https://<host-lan>:<port>. The repo mounts read-only so a
# stray install inside the container can never rewrite the host's
# node_modules (see the environment notes' Orbit incident).
engines-check:
	test -n "$(MV_URL)" || { echo "MV_URL required -- an already-standing instance, reachable from a container (host LAN address, not 127.0.0.1)" >&2; exit 1; }
	docker run --rm -v $(CURDIR):/repo:ro -w /repo/frontend -e MV_URL=$(MV_URL) \
	  mcr.microsoft.com/playwright:v1.62.0-noble node scripts/live-door-engines.mjs

.PHONY: engines-check

# fidelity: photograph the built app and its ratified mockup at the same
# viewport and compare per pixel (#658, ported from Orbit). Catches the
# thing neither the suite nor live-check can see -- a surface that works
# but is not the one that was ratified.
#
# Needs a running instance and the design host. Both are overridable:
#   FIDELITY_APP=... FIDELITY_MOCKUPS=... make fidelity
# Baselines move only deliberately: UPDATE_BASELINE=1 make fidelity
fidelity:
	@cd frontend && node tests/fidelity/screens.mjs

.PHONY: fidelity
