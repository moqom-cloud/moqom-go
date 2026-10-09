package moqom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/moqom-cloud/moqom-go/admin/adminpb"
)

// A relay pool is a set of relays that carries only this tenant's traffic. Make
// the tenant private on one with SetTenantNetwork and every token minted for it
// names the pool: the pool's relays refuse every other token, the shared fleet
// refuses these, and Token.Relays says where to dial. There is no fallback to
// the shared fleet, so a private tenant whose pool has no relays yet is refused
// tokens rather than given ones that would go elsewhere.
//
// Every call here needs the tenant's owner (backend) key.

// PoolAccess is how a pool's relays are reached.
type PoolAccess string

const (
	// PoolAccessPublic is static public addresses, only this tenant's.
	PoolAccessPublic PoolAccess = "public"
	// PoolAccessPrivateServiceConnect is no public address: a Private Service
	// Connect attachment you create an endpoint for in your own VPC.
	PoolAccessPrivateServiceConnect PoolAccess = "private_service_connect"
	// PoolAccessOverlay is no public address: reachable only over the
	// zero-trust overlay, by devices with an identity you enrolled.
	PoolAccessOverlay PoolAccess = "overlay"
)

// PoolState is where a pool is in its life.
type PoolState string

const (
	// PoolProvisioning is relays starting. A private tenant is minted no
	// tokens until the pool is ready.
	PoolProvisioning PoolState = "provisioning"
	PoolReady        PoolState = "ready"
	// PoolDraining is relays finishing their sessions and taking no new ones.
	PoolDraining PoolState = "draining"
	PoolDeleted  PoolState = "deleted"
)

// PoolRelayState is where one relay of a pool is.
type PoolRelayState string

const (
	PoolRelayStarting PoolRelayState = "starting"
	PoolRelayReady    PoolRelayState = "ready"
	PoolRelayDraining PoolRelayState = "draining"
)

// NetworkMode is whether a tenant runs on the shared fleet or its own pool.
type NetworkMode string

const (
	// NetworkShared is MOQOM Cloud's shared relay fleet, the default.
	NetworkShared NetworkMode = "shared"
	// NetworkPrivate is the tenant's own pool, and nothing else.
	NetworkPrivate NetworkMode = "private"
)

// PoolRegion is how many relays a pool runs in one region.
type PoolRegion struct {
	// Region is a MOQOM Cloud region by its Google Cloud name, such as
	// "us-west1".
	Region string

	// Relays is how many to run: at least two for a pool MOQOM runs, so one can
	// restart without dropping the region, and at least one for a self-hosted
	// pool.
	Relays uint32

	// MaxRelays is the most autoscaling may add up to. Zero means Relays,
	// which is autoscaling off.
	MaxRelays uint32
}

// PoolRelay is one relay of a pool.
type PoolRelay struct {
	// Address is where clients dial it, host:port. Static for the life of the
	// pool, so it can be allow-listed.
	Address string
	Region  string
	State   PoolRelayState
}

// RelayPool is one of the tenant's pools.
type RelayPool struct {
	ID         string
	Name       string
	Regions    []PoolRegion
	SelfHosted bool
	Access     PoolAccess
	State      PoolState
	Relays     []PoolRelay
	CreatedAt  time.Time
}

// NewRelayPool is the request to create a pool.
type NewRelayPool struct {
	// Name is your label for the pool.
	Name string

	// Regions is where MOQOM runs the relays. Required unless SelfHosted.
	Regions []PoolRegion

	// SelfHosted makes a pool of relays you run and enrol into it, rather than
	// relays MOQOM runs. It is ready at once.
	SelfHosted bool
}

// TenantNetwork is which relays the tenant's tokens are good for.
type TenantNetwork struct {
	// Pool is the pool, for NetworkPrivate.
	Pool string
	Mode NetworkMode
}

// CreateRelayPool creates a pool. A pool MOQOM runs is PoolProvisioning until
// every region has its relays; make the tenant private on it only once
// RelayPool reports PoolReady.
//
// Not retried on an ambiguous failure, because a second attempt of a create
// that did land would be a second pool: list the pools and look.
func (c *Client) CreateRelayPool(ctx context.Context, request NewRelayPool) (RelayPool, error) {
	if request.Name == "" {
		return RelayPool{}, errors.New("moqom: CreateRelayPool needs a name")
	}
	wire := &adminpb.CreateRelayPoolRequest{
		Tenant:     c.tenant,
		Name:       request.Name,
		SelfHosted: request.SelfHosted,
	}
	for _, region := range request.Regions {
		wire.Regions = append(wire.Regions, poolRegionToProto(region))
	}
	pool, err := c.pools.CreateRelayPool(ctx, wire)
	if err != nil {
		return RelayPool{}, wrap("CreateRelayPool", err)
	}
	return poolFromProto(pool), nil
}

// GetRelayPool reads one pool, with each relay's address and state.
func (c *Client) GetRelayPool(ctx context.Context, id string) (RelayPool, error) {
	pool, err := call(ctx, c, "GetRelayPool", func(ctx context.Context) (*adminpb.RelayPool, error) {
		return c.pools.GetRelayPool(ctx, &adminpb.GetRelayPoolRequest{Tenant: c.tenant, PoolId: id})
	})
	return poolFromProto(pool), err
}

// ListRelayPools lists every pool in the tenant, deleted ones included, oldest
// first.
func (c *Client) ListRelayPools(ctx context.Context) ([]RelayPool, error) {
	response, err := call(ctx, c, "ListRelayPools", func(ctx context.Context) (*adminpb.ListRelayPoolsResponse, error) {
		return c.pools.ListRelayPools(ctx, &adminpb.ListRelayPoolsRequest{Tenant: c.tenant})
	})
	if err != nil {
		return nil, err
	}
	pools := make([]RelayPool, 0, len(response.GetPools()))
	for _, pool := range response.GetPools() {
		pools = append(pools, poolFromProto(pool))
	}
	return pools, nil
}

// ResizeRelayPool sets one region's relay count, adding the region if the
// pool has none there. A MaxRelays above Relays turns on autoscaling up to it.
// Removed relays drain rather than stop.
func (c *Client) ResizeRelayPool(ctx context.Context, id string, region PoolRegion) (RelayPool, error) {
	if region.Region == "" {
		return RelayPool{}, errors.New("moqom: ResizeRelayPool needs a region")
	}
	wire := &adminpb.ResizeRelayPoolRequest{Tenant: c.tenant, PoolId: id, Region: poolRegionToProto(region)}
	pool, err := call(ctx, c, "ResizeRelayPool", func(ctx context.Context) (*adminpb.RelayPool, error) {
		return c.pools.ResizeRelayPool(ctx, wire)
	})
	return poolFromProto(pool), err
}

// DeleteRelayPool deletes a pool. Refused while the tenant is private on it:
// SetTenantNetwork to shared, or to another pool, first.
func (c *Client) DeleteRelayPool(ctx context.Context, id string) (RelayPool, error) {
	pool, err := call(ctx, c, "DeleteRelayPool", func(ctx context.Context) (*adminpb.RelayPool, error) {
		return c.pools.DeleteRelayPool(ctx, &adminpb.DeleteRelayPoolRequest{Tenant: c.tenant, PoolId: id})
	})
	return poolFromProto(pool), err
}

// SetRelayPoolAccess sets how the pool's relays are reached.
func (c *Client) SetRelayPoolAccess(ctx context.Context, id string, access PoolAccess) (RelayPool, error) {
	wire, err := poolAccessToProto(access)
	if err != nil {
		return RelayPool{}, err
	}
	pool, err := call(ctx, c, "SetRelayPoolAccess", func(ctx context.Context) (*adminpb.RelayPool, error) {
		return c.pools.SetRelayPoolAccess(ctx, &adminpb.SetRelayPoolAccessRequest{Tenant: c.tenant, PoolId: id, Access: wire})
	})
	return poolFromProto(pool), err
}

// SetTenantNetwork confines the tenant to a pool, or returns it to the shared
// fleet. It takes effect at the next token minted; tokens already minted keep
// the network they were minted for until they expire.
func (c *Client) SetTenantNetwork(ctx context.Context, network TenantNetwork) error {
	wire := &adminpb.TenantNetwork{Pool: network.Pool}
	switch network.Mode {
	case NetworkShared:
		wire.Mode = adminpb.NetworkMode_NETWORK_MODE_SHARED
	case NetworkPrivate:
		wire.Mode = adminpb.NetworkMode_NETWORK_MODE_PRIVATE
	default:
		return fmt.Errorf("moqom: unknown network mode %q", network.Mode)
	}
	_, err := call(ctx, c, "SetTenantNetwork", func(ctx context.Context) (*adminpb.TenantNetwork, error) {
		return c.pools.SetTenantNetwork(ctx, &adminpb.SetTenantNetworkRequest{Tenant: c.tenant, Network: wire})
	})
	return err
}

// TenantNetwork reads which relays the tenant's tokens are good for.
func (c *Client) TenantNetwork(ctx context.Context) (TenantNetwork, error) {
	wire, err := call(ctx, c, "TenantNetwork", func(ctx context.Context) (*adminpb.TenantNetwork, error) {
		return c.pools.GetTenantNetwork(ctx, &adminpb.GetTenantNetworkRequest{Tenant: c.tenant})
	})
	if err != nil {
		return TenantNetwork{}, err
	}
	network := TenantNetwork{Pool: wire.GetPool(), Mode: NetworkShared}
	if wire.GetMode() == adminpb.NetworkMode_NETWORK_MODE_PRIVATE {
		network.Mode = NetworkPrivate
	}
	return network, nil
}

func poolRegionToProto(region PoolRegion) *adminpb.PoolRegion {
	return &adminpb.PoolRegion{Region: region.Region, Relays: region.Relays, MaxRelays: region.MaxRelays}
}

func poolAccessToProto(access PoolAccess) (adminpb.PoolAccess, error) {
	switch access {
	case PoolAccessPublic:
		return adminpb.PoolAccess_POOL_ACCESS_PUBLIC, nil
	case PoolAccessPrivateServiceConnect:
		return adminpb.PoolAccess_POOL_ACCESS_PRIVATE_SERVICE_CONNECT, nil
	case PoolAccessOverlay:
		return adminpb.PoolAccess_POOL_ACCESS_OVERLAY, nil
	default:
		return 0, fmt.Errorf("moqom: unknown pool access %q", access)
	}
}

func poolFromProto(wire *adminpb.RelayPool) RelayPool {
	if wire == nil {
		return RelayPool{}
	}
	pool := RelayPool{
		ID:         wire.GetId(),
		Name:       wire.GetName(),
		SelfHosted: wire.GetSelfHosted(),
		CreatedAt:  timeFromProto(wire.GetCreatedAt()),
	}
	switch wire.GetAccess() {
	case adminpb.PoolAccess_POOL_ACCESS_PUBLIC:
		pool.Access = PoolAccessPublic
	case adminpb.PoolAccess_POOL_ACCESS_PRIVATE_SERVICE_CONNECT:
		pool.Access = PoolAccessPrivateServiceConnect
	case adminpb.PoolAccess_POOL_ACCESS_OVERLAY:
		pool.Access = PoolAccessOverlay
	}
	switch wire.GetState() {
	case adminpb.PoolState_POOL_STATE_PROVISIONING:
		pool.State = PoolProvisioning
	case adminpb.PoolState_POOL_STATE_READY:
		pool.State = PoolReady
	case adminpb.PoolState_POOL_STATE_DRAINING:
		pool.State = PoolDraining
	case adminpb.PoolState_POOL_STATE_DELETED:
		pool.State = PoolDeleted
	}
	for _, region := range wire.GetRegions() {
		pool.Regions = append(pool.Regions, PoolRegion{
			Region: region.GetRegion(), Relays: region.GetRelays(), MaxRelays: region.GetMaxRelays(),
		})
	}
	for _, relay := range wire.GetRelays() {
		state := PoolRelayState("")
		switch relay.GetState() {
		case adminpb.PoolRelayState_POOL_RELAY_STATE_STARTING:
			state = PoolRelayStarting
		case adminpb.PoolRelayState_POOL_RELAY_STATE_READY:
			state = PoolRelayReady
		case adminpb.PoolRelayState_POOL_RELAY_STATE_DRAINING:
			state = PoolRelayDraining
		}
		pool.Relays = append(pool.Relays, PoolRelay{Address: relay.GetAddress(), Region: relay.GetRegion(), State: state})
	}
	return pool
}
