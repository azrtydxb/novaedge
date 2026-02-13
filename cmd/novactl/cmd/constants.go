package cmd

// Resource type alias constants used in switch cases across multiple commands.
const (
	resourceAliasGateways = "gateways"
	resourceAliasGateway  = "gateway"
	resourceAliasRoutes   = "routes"
	resourceAliasRoute    = "route"
	resourceAliasBackends = "backends"
	resourceAliasBackend  = "backend"
	resourceAliasPolicies = "policies"
	resourceAliasPolicy   = "policy"
	resourceAliasPol      = "pol"
	resourceAliasVIPs     = "vips"
	resourceAliasVIP      = "vip"
	resourceAliasIPPools  = "ippools"
	resourceAliasIPPool   = "ippool"

	resourceAliasTCPRoutes  = "tcproutes"
	resourceAliasTCPRoute   = "tcproute"
	resourceAliasTLSRoutes  = "tlsroutes"
	resourceAliasTLSRoute   = "tlsroute"
	resourceAliasGRPCRoutes = "grpcroutes"
	resourceAliasGRPCRoute  = "grpcroute"

	resourceAliasWASMPlugins = "wasmplugins"
	resourceAliasWASMPlugin  = "wasmplugin"
	resourceAliasWASM        = "wasm"

	resourceAliasCertificates      = "certificates"
	resourceAliasCertificate       = "certificate"
	resourceAliasCert              = "cert"
	resourceAliasProxyCertificates = "proxycertificates"

	statusYes     = "Yes"
	statusNo      = "No"
	statusUnknown = "Unknown"

	conditionAccepted = "Accepted"
	conditionTrue     = "True"
)
