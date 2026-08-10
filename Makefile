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
