package moqom_test

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	moqom "github.com/moqom-cloud/moqom-go"
	"github.com/moqom-cloud/moqom-go/admin/adminpb"
)

// poolStub is a RelayPools and Tokens service that records what arrived.
type poolStub struct {
	adminpb.UnimplementedRelayPoolsServer
	adminpb.UnimplementedTokensServer

	mu       sync.Mutex
	creates  int
	gets     int
	created  *adminpb.CreateRelayPoolRequest
	network  *adminpb.SetTenantNetworkRequest
	resized  *adminpb.ResizeRelayPoolRequest
	failWith error
}

var stubPool = &adminpb.RelayPool{
	Id: "pool-1", Tenant: "acme", Name: "production",
	Regions:   []*adminpb.PoolRegion{{Region: "us-west1", Relays: 2, MaxRelays: 8}},
	Access:    adminpb.PoolAccess_POOL_ACCESS_OVERLAY,
	State:     adminpb.PoolState_POOL_STATE_READY,
	Relays:    []*adminpb.PoolRelay{{Address: "198.51.100.1:443", Region: "us-west1", State: adminpb.PoolRelayState_POOL_RELAY_STATE_READY}},
	CreatedAt: timestamppb.New(time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)),
}

func (s *poolStub) CreateRelayPool(_ context.Context, request *adminpb.CreateRelayPoolRequest) (*adminpb.RelayPool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	s.created = request
	if s.failWith != nil {
		return nil, s.failWith
	}
	return stubPool, nil
}

func (s *poolStub) GetRelayPool(_ context.Context, request *adminpb.GetRelayPoolRequest) (*adminpb.RelayPool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.failWith != nil {
		return nil, s.failWith
	}
	return stubPool, nil
}

func (s *poolStub) ResizeRelayPool(_ context.Context, request *adminpb.ResizeRelayPoolRequest) (*adminpb.RelayPool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resized = request
	return stubPool, nil
}

func (s *poolStub) SetTenantNetwork(_ context.Context, request *adminpb.SetTenantNetworkRequest) (*adminpb.TenantNetwork, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.network = request
	return request.GetNetwork(), nil
}

func (s *poolStub) GetTenantNetwork(context.Context, *adminpb.GetTenantNetworkRequest) (*adminpb.TenantNetwork, error) {
	return &adminpb.TenantNetwork{Pool: "pool-1", Mode: adminpb.NetworkMode_NETWORK_MODE_PRIVATE}, nil
}

func (s *poolStub) MintClientToken(context.Context, *adminpb.MintClientTokenRequest) (*adminpb.MintClientTokenResponse, error) {
	return &adminpb.MintClientTokenResponse{Token: "t", Relays: []string{"198.51.100.1:443", "198.51.100.2:443"}}, nil
}

func servePools(t *testing.T, backing *poolStub) *moqom.Client {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	adminpb.RegisterRelayPoolsServer(server, backing)
	adminpb.RegisterTokensServer(server, backing)
	go func() { _ = server.Serve(listener) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(fakeCredential{}),
	)
	if err != nil {
		t.Fatalf("dialling the stub: %v", err)
	}
	client, err := moqom.Dial("ignored", moqom.WithTenant("acme"), moqom.WithConnection(conn),
		moqom.WithRetry(3, time.Millisecond))
	if err != nil {
		t.Fatalf("building the client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	})
	return client
}

// A create carries no idempotency key, so a retry of one that landed would
// be a second pool billed by the hour. Reads are retried as usual.
func TestCreatingARelayPoolIsNotRetried(t *testing.T) {
	backing := &poolStub{failWith: status.Error(codes.Unavailable, "try again")}
	client := servePools(t, backing)
	ctx := context.Background()

	if _, err := client.CreateRelayPool(ctx, moqom.NewRelayPool{Name: "production", SelfHosted: true}); err == nil {
		t.Fatal("a failed create reported success")
	}
	if backing.creates != 1 {
		t.Errorf("a create was attempted %d times, want once", backing.creates)
	}
	if _, err := client.GetRelayPool(ctx, "pool-1"); err == nil {
		t.Fatal("a failed read reported success")
	}
	if backing.gets != 3 {
		t.Errorf("a read was attempted %d times, want 3", backing.gets)
	}
}

func TestRelayPoolsAndNetworksSurviveTheWire(t *testing.T) {
	backing := &poolStub{}
	client := servePools(t, backing)
	ctx := context.Background()

	pool, err := client.CreateRelayPool(ctx, moqom.NewRelayPool{
		Name:    "production",
		Regions: []moqom.PoolRegion{{Region: "us-west1", Relays: 2, MaxRelays: 8}},
	})
	if err != nil {
		t.Fatalf("creating a pool: %v", err)
	}
	if backing.created.GetTenant() != "acme" || backing.created.GetRegions()[0].GetMaxRelays() != 8 {
		t.Errorf("the create arrived as %v", backing.created)
	}
	want := moqom.RelayPool{
		ID: "pool-1", Name: "production",
		Regions: []moqom.PoolRegion{{Region: "us-west1", Relays: 2, MaxRelays: 8}},
		Access:  moqom.PoolAccessOverlay, State: moqom.PoolReady,
		Relays:    []moqom.PoolRelay{{Address: "198.51.100.1:443", Region: "us-west1", State: moqom.PoolRelayReady}},
		CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
	}
	if !pool.CreatedAt.Equal(want.CreatedAt) || pool.ID != want.ID || pool.Access != want.Access || pool.State != want.State ||
		!slices.Equal(pool.Regions, want.Regions) || !slices.Equal(pool.Relays, want.Relays) {
		t.Errorf("the pool read back as %+v, want %+v", pool, want)
	}

	if _, err := client.ResizeRelayPool(ctx, "pool-1", moqom.PoolRegion{Region: "us-east4", Relays: 3}); err != nil {
		t.Fatalf("resizing: %v", err)
	}
	if backing.resized.GetPoolId() != "pool-1" || backing.resized.GetRegion().GetRelays() != 3 {
		t.Errorf("the resize arrived as %v", backing.resized)
	}

	if err := client.SetTenantNetwork(ctx, moqom.TenantNetwork{Pool: "pool-1", Mode: moqom.NetworkPrivate}); err != nil {
		t.Fatalf("setting the network: %v", err)
	}
	if backing.network.GetNetwork().GetMode() != adminpb.NetworkMode_NETWORK_MODE_PRIVATE || backing.network.GetTenant() != "acme" {
		t.Errorf("the network arrived as %v", backing.network)
	}
	network, err := client.TenantNetwork(ctx)
	if err != nil || network != (moqom.TenantNetwork{Pool: "pool-1", Mode: moqom.NetworkPrivate}) {
		t.Errorf("the network read back as %+v (%v)", network, err)
	}

	minted, err := client.MintToken(ctx, moqom.TokenRequest{Username: "bob", DeviceThumbprint: "thumb"})
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if !slices.Equal(minted.Relays, []string{"198.51.100.1:443", "198.51.100.2:443"}) {
		t.Errorf("the token lists relays %v", minted.Relays)
	}
}

func TestAnUnknownPoolAccessOrNetworkModeIsRefusedLocally(t *testing.T) {
	client := servePools(t, &poolStub{})
	ctx := context.Background()
	if _, err := client.SetRelayPoolAccess(ctx, "pool-1", "carrier-pigeon"); err == nil {
		t.Error("an unknown access was sent")
	}
	if err := client.SetTenantNetwork(ctx, moqom.TenantNetwork{Pool: "pool-1"}); err == nil {
		t.Error("a network with no mode was sent")
	}
	if _, err := client.CreateRelayPool(ctx, moqom.NewRelayPool{}); err == nil {
		t.Error("a pool with no name was sent")
	}
}
