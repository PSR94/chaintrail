## Summary

Describe the user/operational problem and the chosen approach.

## Integrity / compatibility impact

- [ ] No durable format or hash-domain change
- [ ] Durable format/hash behavior changed and `docs/FORMAT.md` + ADR/changelog were updated
- [ ] Security/trust behavior changed and `docs/SECURITY.md` was updated
- [ ] Recovery behavior changed and refusal/mutation tests cover it

Explain any checked impact:

## Validation

- [ ] `make fmt-check`
- [ ] `make vet`
- [ ] `make test`
- [ ] `make race`
- [ ] `make build`
- [ ] Relevant failure-path/regression tests added

## Evidence safety

- [ ] No private keys, credentials, production journals, generated binaries, or coverage files in the diff
- [ ] Documentation matches implemented behavior
- [ ] No automatic repair/rewrite path can mutate evidence before integrity is established
