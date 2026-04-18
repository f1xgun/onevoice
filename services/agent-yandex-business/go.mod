module github.com/f1xgun/onevoice/services/agent-yandex-business

go 1.25.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
	github.com/f1xgun/onevoice/pkg v0.0.0
	github.com/nats-io/nats.go v1.49.0
	github.com/playwright-community/playwright-go v0.5001.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/go-jose/go-jose/v3 v3.0.3 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
