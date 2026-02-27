package dataplane

import (
	configpb "github.com/piwi3910/novaedge/internal/proto/gen"

	pb "github.com/piwi3910/novaedge/api/proto/dataplane"
)

// TranslateSnapshot converts a ConfigSnapshot (from the control-plane config
// proto) into an ApplyConfigRequest suitable for pushing to the Rust dataplane.
//
// This is a stub implementation for Phase 1.4. The full translation logic
// mapping every field will be implemented in Phase 6.
func TranslateSnapshot(snapshot *configpb.ConfigSnapshot) *pb.ApplyConfigRequest {
	if snapshot == nil {
		return &pb.ApplyConfigRequest{}
	}

	req := &pb.ApplyConfigRequest{
		Version: snapshot.GetVersion(),
	}

	// Translate gateways.
	for _, gw := range snapshot.GetGateways() {
		for _, lis := range gw.GetListeners() {
			req.Gateways = append(req.Gateways, &pb.GatewayConfig{
				Name:        gw.GetName() + "/" + lis.GetName(),
				Port:        uint32(lis.GetPort()), //nolint:gosec // proto field conversion
				BindAddress: "0.0.0.0",
			})
		}
	}

	// Translate routes (stub — maps route names only).
	for _, rt := range snapshot.GetRoutes() {
		req.Routes = append(req.Routes, &pb.RouteConfig{
			Name:      rt.GetName(),
			Hostnames: rt.GetHostnames(),
		})
	}

	// Translate clusters (stub — maps cluster names and basic LB).
	for _, cl := range snapshot.GetClusters() {
		req.Clusters = append(req.Clusters, &pb.ClusterConfig{
			Name: cl.GetName(),
		})
	}

	// Translate VIP assignments.
	for _, vip := range snapshot.GetVipAssignments() {
		req.Vips = append(req.Vips, &pb.VIPConfig{
			Name:    vip.GetVipName(),
			Address: vip.GetAddress(),
		})
	}

	// Translate L4 listeners.
	for _, l4 := range snapshot.GetL4Listeners() {
		req.L4Listeners = append(req.L4Listeners, &pb.L4ListenerConfig{
			Name: l4.GetName(),
			Port: uint32(l4.GetPort()), //nolint:gosec // proto field conversion
		})
	}

	// Translate policies (stub — maps policy names and types).
	for _, pol := range snapshot.GetPolicies() {
		req.Policies = append(req.Policies, &pb.PolicyConfig{
			Name: pol.GetName(),
		})
	}

	// Translate WAN links.
	for _, wl := range snapshot.GetWanLinks() {
		req.WanLinks = append(req.WanLinks, &pb.WANLinkConfig{
			Name:      wl.GetName(),
			Interface: wl.GetIface(),
			Provider:  wl.GetProvider(),
		})
	}

	return req
}
