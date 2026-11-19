.PHONY: verify verify-full

verify:
	./scripts/verify.sh

verify-full:
	./scripts/verify.sh --full
