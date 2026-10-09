// The module path is the repository. `go get` resolves a module by fetching its
// path over HTTPS, so a Go SDK is only consumable from a public repository --
// the same forcing function that moved the Swift SDK out of the monorepo, for
// the same reason.
module github.com/moqom-cloud/moqom-go

go 1.26.0

require (
	google.golang.org/grpc v1.84.0
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
)
