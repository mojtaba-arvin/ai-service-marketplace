.PHONY: verify verify-full

verify:
	bash ./scripts/verify.sh

verify-full:
	bash ./scripts/verify.sh --full
