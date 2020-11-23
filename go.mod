module github.com/raulk/trampoline

go 1.15

require (
	github.com/testground/sdk-go v0.2.6
	github.com/testground/sdk-go/virtual/1 v0.0.0
	github.com/testground/sdk-go/virtual/2 v0.0.0
)

replace github.com/testground/sdk-go/virtual/1 => github.com/testground/sdk-go v0.2.3

replace github.com/testground/sdk-go/virtual/2 => github.com/testground/sdk-go v0.2.1
