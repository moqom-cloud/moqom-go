# moqom-go

The server-side SDK for [MOQOM](https://moqom.cloud). For a backend, not a
participant: it mints the tokens your users join with, creates and closes rooms,
moderates them, and follows what happens in them.

It does not carry media. Nothing here speaks MOQT, and a Go process that wants to
receive frames — a recorder, a classifier — wants a MOQT subscriber instead. The
split is deliberate: media needs a QUIC stack, a codec, and a scheduler on the hot
path, and making every backend link all three to change somebody's grants would be
a poor trade.

```sh
go get github.com/moqom-cloud/moqom-go
```

```go
import moqom "github.com/moqom-cloud/moqom-go"
```

Documentation is at [moqom.dev](https://moqom.dev); the
[Go backend guide](https://moqom.dev/guides/backend-go/) covers everything below
in more depth.

## The call you will make most

Your users authenticate however they already do. You then ask for a token naming
that user, that device, that room, and exactly the rights they should have — MOQOM
never sees your password database.

```go
client, err := moqom.Dial("api.moqom.cloud:443",
    moqom.WithCredential(os.Getenv("MOQOM_SERVICE_KEY")),
    moqom.WithTenant("acme"))
if err != nil {
    return err
}
defer client.Close()

token, err := client.MintToken(ctx, moqom.TokenRequest{
    Username:         user.Name,
    DeviceThumbprint: request.Thumbprint, // from the client's device key
    Room:             "live/42",
    Publish:          moqom.PublishAudio | moqom.PublishVideo,
    Preset:           moqom.PresetNone,
})
```

## Moderation

Attribute the action to the human who asked for it. The service account comes from
the credentials on the connection and cannot be set per call, so an audit trail is
not something a caller can forge.

```go
room := client.Room("live/42")

_, err = room.Kick(ctx, moqom.KickRequest{
    Target: moqom.User("carol"),
    Code:   moqom.CodeHarassment,
    Actor:  moqom.Actor{OnBehalfOf: "jane", Reason: "repeated slurs in chat"},
})
```

Every call returns the moderation entry it produced, and its `Enforced` field is
the honest answer to "did that work": false means the record exists and a relay
holding an affected session has not confirmed the directive yet. Surface it. A
moderator told a kick succeeded who then watches the person keep talking will not
trust the tool again.

## Following a room

`Events` is your substitute for being in the room. Delivery is at-least-once and
ordered per room, and every event carries a sequence number contiguous within its
room — so persist it. A consumer that resumes from the last sequence it handled can
tell it *missed* something, which a timestamp cannot.

```go
stream, err := room.Events(ctx, moqom.EventQuery{After: cursor})
if err != nil {
    return err
}
for {
    event, err := stream.Recv()
    if err != nil {
        return err // reconnect from your cursor, not from the present
    }
    cursor = event.Sequence
}
```

## End-to-end encryption

Every room is end-to-end encrypted, with post-quantum keys and signatures, and
there is no way to turn it off: `Policy.E2EE` is always `E2EERequired`. A recorder
or classifier that needs the media joins the room's group as a disclosed member and
decrypts on your side.

## Over an overlay network

The admin API can also be served as a dark OpenZiti service that binds no public
port. This is an option for enterprise backends, not the default. A backend holding
an enrolled identity dials through the overlay with `WithDialer`; the overlay's
circuit is the encryption, so transport security is turned off explicitly:

```go
zctx, err := ziti.NewContext(identity)
if err != nil {
    return err
}
client, err := moqom.Dial("moqom-api",
    moqom.WithDialer(func(_ context.Context, service string) (net.Conn, error) {
        return zctx.Dial(service)
    }),
    moqom.WithoutTransportSecurity(),
    moqom.WithTenant("acme"),
    moqom.WithCredential(os.Getenv("MOQOM_SERVICE_KEY")))
```

The target is passed to the dialer as given, so `moqom-api` is a service name, not
a DNS name. Your MOQOM contact issues the identity.

## Identity

There are no numeric user IDs and nowhere to put one. A username is a string you
already have, and a device is derived from the device key's JWK thumbprint — so a
device ban and the thumbprint you passed to `MintToken` are the same fact. That is
what makes migrating from a platform with its own UID space a matter of pointing at
a new server rather than building a mapping table.

## Retries

Every mutating call carries an idempotency key, generated here when you do not
supply one, and a retry reuses it. A queue that redelivers "ban for 24 hours"
therefore does not extend the ban.

Supply your own key in `Actor` when the retry might come from a different process
than the first attempt — a key generated here cannot outlive it.

## Errors

Compare with `errors.Is` rather than importing gRPC to tell "no such room" from
"not allowed":

```go
if _, err := room.Kick(ctx, request); errors.Is(err, moqom.ErrDenied) {
    // the moderator lacks standing over this target
}
```

`*moqom.Error` names the operation, keeps the status code, and reports
`Retryable()` for a caller deciding whether to requeue.

## Contribute

Bug reports, fixes and examples are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md):
the first pull request asks you to sign the CLA, and `go test ./...` runs with no
account or network.

## Donate

The SDKs and samples are free. If they save you time, you can support the
open-source work at [elliottwave.dev/donate](https://elliottwave.dev/donate/).

## Licence

Apache-2.0. © 2026 Elliott Wave LLC.
