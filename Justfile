proto: proto-lint
    buf generate

proto-lint:
    buf lint

proto-format:
    buf format -w

proto-breaking:
    buf breaking --against '.git#branch=main'
