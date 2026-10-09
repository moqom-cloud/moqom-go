package moqom

import (
	"context"
	"time"

	"github.com/moqom-cloud/moqom-go/admin/adminpb"
)

// AppleSignIn is a tenant's settings for Apple sign-in hosted by MOQOM: for an
// app with no backend, the control plane checks App Attest and Sign in with
// Apple itself and mints for the rooms named here.
type AppleSignIn struct {
	// TeamID is the Apple team the apps are signed by.
	TeamID string
	// BundleIDs are the apps. Empty means Apple sign-in is off.
	BundleIDs []string
	// Rooms are the existing rooms a signed-in person may be minted for. It
	// never creates a room.
	Rooms []string
	// Publish is what a signed-in person may publish there. It never grants
	// moderation or ownership.
	Publish PublishScope
	// AllowDevelopment accepts App Attest keys from Apple's development
	// environment, for builds run from Xcode. Leave off in production.
	AllowDevelopment bool

	// Read only.
	UpdatedAt  time.Time
	SignInPath string
	TokenPath  string
}

// SetAppleSignIn turns Apple sign-in on, changes it, or, with no bundle IDs,
// turns it off. It needs an owner key.
func (c *Client) SetAppleSignIn(ctx context.Context, settings AppleSignIn) (AppleSignIn, error) {
	response, err := call(ctx, c, "SetAppleSignIn", func(ctx context.Context) (*adminpb.AppleSignIn, error) {
		return c.tokens.SetAppleSignIn(ctx, &adminpb.SetAppleSignInRequest{
			Tenant:           c.tenant,
			TeamId:           settings.TeamID,
			BundleIds:        settings.BundleIDs,
			Rooms:            settings.Rooms,
			Publish:          publishToProto(settings.Publish),
			AllowDevelopment: settings.AllowDevelopment,
		})
	})
	if err != nil {
		return AppleSignIn{}, err
	}
	return appleSignInFromProto(response), nil
}

// AppleSignInSettings reads the tenant's Apple sign-in settings.
func (c *Client) AppleSignInSettings(ctx context.Context) (AppleSignIn, error) {
	response, err := call(ctx, c, "AppleSignInSettings", func(ctx context.Context) (*adminpb.AppleSignIn, error) {
		return c.tokens.GetAppleSignIn(ctx, &adminpb.GetAppleSignInRequest{Tenant: c.tenant})
	})
	if err != nil {
		return AppleSignIn{}, err
	}
	return appleSignInFromProto(response), nil
}

func appleSignInFromProto(wire *adminpb.AppleSignIn) AppleSignIn {
	return AppleSignIn{
		TeamID:           wire.GetTeamId(),
		BundleIDs:        wire.GetBundleIds(),
		Rooms:            wire.GetRooms(),
		Publish:          publishFromProto(wire.GetPublish()),
		AllowDevelopment: wire.GetAllowDevelopment(),
		UpdatedAt:        timeFromProto(wire.GetUpdatedAt()),
		SignInPath:       wire.GetSignInPath(),
		TokenPath:        wire.GetTokenPath(),
	}
}
