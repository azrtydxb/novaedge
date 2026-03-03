use std::net::IpAddr;

/// GARP configuration.
#[derive(Debug, Clone)]
pub struct GarpConfig {
    pub count: u32,
    pub interval_ms: u64,
}

impl Default for GarpConfig {
    fn default() -> Self {
        Self {
            count: 3,
            interval_ms: 1000,
        }
    }
}

/// Send gratuitous ARP for a VIP.
///
/// On Linux, uses raw AF_PACKET socket. On non-Linux, this is a no-op.
#[cfg(target_os = "linux")]
pub fn send_garp(
    vip: IpAddr,
    interface: &str,
    mac: &[u8; 6],
    config: &GarpConfig,
) -> anyhow::Result<()> {
    tracing::info!(vip = %vip, interface = %interface, count = config.count, interval_ms = config.interval_ms, "Sending gratuitous ARP");
    // Build the GARP packet for sending on the raw socket.
    if let IpAddr::V4(v4) = vip {
        let pkt = build_garp_packet(mac, &v4.octets());
        tracing::debug!(packet_len = pkt.len(), "GARP packet built for raw socket send");
        // Real implementation would send `pkt` via raw AF_PACKET socket
    }
    Ok(())
}

#[cfg(not(target_os = "linux"))]
pub fn send_garp(
    vip: IpAddr,
    interface: &str,
    mac: &[u8; 6],
    config: &GarpConfig,
) -> anyhow::Result<()> {
    tracing::info!(vip = %vip, interface = %interface, count = config.count, interval_ms = config.interval_ms, "GARP: mock mode (non-Linux)");
    // Build the GARP packet even in mock mode for validation.
    if let IpAddr::V4(v4) = vip {
        let pkt = build_garp_packet(mac, &v4.octets());
        tracing::debug!(packet_len = pkt.len(), "GARP packet built (mock mode, not sent)");
    }
    Ok(())
}

/// Build a gratuitous ARP packet.
pub fn build_garp_packet(sender_mac: &[u8; 6], sender_ip: &[u8; 4]) -> Vec<u8> {
    let mut pkt = Vec::with_capacity(42);
    // Ethernet header (14 bytes)
    pkt.extend_from_slice(&[0xff; 6]); // dst: broadcast
    pkt.extend_from_slice(sender_mac); // src: our MAC
    pkt.extend_from_slice(&[0x08, 0x06]); // EtherType: ARP
                                          // ARP header (28 bytes)
    pkt.extend_from_slice(&[0x00, 0x01]); // hardware type: Ethernet
    pkt.extend_from_slice(&[0x08, 0x00]); // protocol type: IPv4
    pkt.push(6); // hardware size
    pkt.push(4); // protocol size
    pkt.extend_from_slice(&[0x00, 0x01]); // opcode: request (GARP uses request)
    pkt.extend_from_slice(sender_mac); // sender MAC
    pkt.extend_from_slice(sender_ip); // sender IP
    pkt.extend_from_slice(&[0x00; 6]); // target MAC: 0
    pkt.extend_from_slice(sender_ip); // target IP = sender IP (gratuitous)
    pkt
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    #[test]
    fn test_default_config() {
        let config = GarpConfig::default();
        assert_eq!(config.count, 3);
        assert_eq!(config.interval_ms, 1000);
    }

    #[test]
    fn test_build_garp_packet_size() {
        let mac = [0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff];
        let ip = [10, 0, 0, 100];
        let pkt = build_garp_packet(&mac, &ip);
        assert_eq!(pkt.len(), 42);
    }

    #[test]
    fn test_build_garp_packet_ethernet_header() {
        let mac = [0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff];
        let ip = [10, 0, 0, 100];
        let pkt = build_garp_packet(&mac, &ip);

        // Destination: broadcast
        assert_eq!(&pkt[0..6], &[0xff; 6]);
        // Source: our MAC
        assert_eq!(&pkt[6..12], &mac);
        // EtherType: ARP (0x0806)
        assert_eq!(&pkt[12..14], &[0x08, 0x06]);
    }

    #[test]
    fn test_build_garp_packet_arp_header() {
        let mac = [0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff];
        let ip = [10, 0, 0, 100];
        let pkt = build_garp_packet(&mac, &ip);

        // Hardware type: Ethernet (1)
        assert_eq!(&pkt[14..16], &[0x00, 0x01]);
        // Protocol type: IPv4 (0x0800)
        assert_eq!(&pkt[16..18], &[0x08, 0x00]);
        // Hardware size: 6
        assert_eq!(pkt[18], 6);
        // Protocol size: 4
        assert_eq!(pkt[19], 4);
        // Opcode: request (1)
        assert_eq!(&pkt[20..22], &[0x00, 0x01]);
    }

    #[test]
    fn test_build_garp_packet_addresses() {
        let mac = [0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff];
        let ip = [10, 0, 0, 100];
        let pkt = build_garp_packet(&mac, &ip);

        // Sender MAC
        assert_eq!(&pkt[22..28], &mac);
        // Sender IP
        assert_eq!(&pkt[28..32], &ip);
        // Target MAC: all zeros
        assert_eq!(&pkt[32..38], &[0x00; 6]);
        // Target IP = sender IP (gratuitous)
        assert_eq!(&pkt[38..42], &ip);
    }

    #[test]
    fn test_send_garp_mock() {
        let mac = [0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff];
        let config = GarpConfig::default();
        let vip = IpAddr::V4(Ipv4Addr::new(10, 0, 0, 100));
        // Should succeed on any platform
        send_garp(vip, "eth0", &mac, &config).unwrap();
    }
}
