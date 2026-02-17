// SD-WAN types for the WebUI dashboard

export interface WANLink {
  name: string
  site: string
  provider: string
  role: string
  bandwidth: string
  latencyMs: number
  jitterMs: number
  packetLossPercent: number
  score: number
  healthy: boolean
}

export interface SDWANSite {
  name: string
  region: string
  overlayAddr: string
}

export interface SDWANLink {
  from: string
  to: string
  linkName: string
  latencyMs: number
  healthy: boolean
}

export interface SDWANTopology {
  sites: SDWANSite[]
  links: SDWANLink[]
}

export interface WANPolicy {
  name: string
  strategy: string
  matchHosts: string[]
  dscpClass: string
  selections: number
}

export interface SDWANEvent {
  timestamp: string
  type: string
  fromLink: string
  toLink: string
  reason: string
  policy: string
}
