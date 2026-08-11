.PHONY: build frontend backend test dev-backend dev-frontend docker clean

BINARY := mikroview

build: frontend backend

frontend:
	cd frontend && npm install && npm run build
	rm -rf web/dist
	mkdir -p web/dist
	cp -r frontend/dist/. web/dist/

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
	git checkout -- web/dist/index.html 2>/dev/null || true
	find web/dist -mindepth 1 ! -name index.html -delete 2>/dev/null || true

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
live-check:
	@eval "$$(scripts/live-env.sh up)"; \
	  status=0; \
	  for scenario in frontend/scripts/live-*.mjs; do \
	    case "$$scenario" in *live-browser.mjs) continue;; esac; \
	    echo "== $$scenario"; \
	    ( cd frontend && node "../$$scenario" ) || status=1; \
	  done; \
	  scripts/live-env.sh down; \
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
	  for scenario in frontend/scripts/live-*.mjs; do \
	    case "$$scenario" in *live-browser.mjs) continue;; esac; \
	    echo "== $$scenario"; \
	    ( cd frontend && node "../$$scenario" ) || status=1; \
	  done; \
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
	  for scenario in frontend/scripts/live-*.mjs; do \
	    case "$$scenario" in *live-browser.mjs) continue;; esac; \
	    echo "== $$scenario"; \
	    ( cd frontend && node "../$$scenario" ) || status=1; \
	  done; \
	  if [ $$status -ne 0 ]; then echo "== container log"; scripts/live-container.sh logs | tail -40; fi; \
	  scripts/live-container.sh down; \
	  exit $$status

.PHONY: live-container-postgres
