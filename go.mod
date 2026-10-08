// The module path is the repository. `go get` resolves a module by fetching its
// path over HTTPS, so a Go SDK is only consumable from a public repository --
// the same forcing function that moved the Swift SDK out of the monorepo, for
// the same reason.
module github.com/moqom-cloud/moqom-go

go 1.25.0

require (
	google.golang.org/grpc v1.83.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)
