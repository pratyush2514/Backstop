.PHONY: test gateway-test sync-test python-test node-test compose-check e2e

test: gateway-test sync-test python-test node-test compose-check

gateway-test:
	cd gateway && CGO_ENABLED=0 go test ./... -count=1

sync-test:
	cd sync && CGO_ENABLED=0 go test ./... -count=1

python-test:
	cd sdk/python && python -m pytest tests -q

node-test:
	npm test

compose-check:
	docker compose -f deploy/docker-compose.yml config --quiet

e2e:
	node scripts/e2e.mjs
