use std::io;
use std::net::{IpAddr, SocketAddr};

/// Parsed PROXY protocol header.
#[derive(Debug, Clone, PartialEq)]
pub struct ProxyHeader {
    pub src_addr: SocketAddr,
    pub dst_addr: SocketAddr,
}

/// Parse PROXY protocol v1 header from a text line.
///
/// Format: `PROXY TCP4 src_ip dst_ip src_port dst_port\r\n`
pub fn parse_v1(data: &[u8]) -> Result<(ProxyHeader, usize), io::Error> {
    let line_end = data
        .windows(2)
        .position(|w| w == b"\r\n")
        .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "no CRLF"))?;

    let line = std::str::from_utf8(&data[..line_end])
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "not UTF-8"))?;

    let parts: Vec<&str> = line.split(' ').collect();
    if parts.len() != 6 || parts[0] != "PROXY" {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "invalid PROXY v1 header",
        ));
    }

    let src_ip: IpAddr = parts[2]
        .parse()
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "invalid src IP"))?;
    let dst_ip: IpAddr = parts[3]
        .parse()
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "invalid dst IP"))?;
    let src_port: u16 = parts[4]
        .parse()
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "invalid src port"))?;
    let dst_port: u16 = parts[5]
        .parse()
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "invalid dst port"))?;

    Ok((
        ProxyHeader {
            src_addr: SocketAddr::new(src_ip, src_port),
            dst_addr: SocketAddr::new(dst_ip, dst_port),
        },
        line_end + 2, // consumed bytes including \r\n
    ))
}

/// Generate a PROXY protocol v1 header.
pub fn generate_v1(header: &ProxyHeader) -> Vec<u8> {
    let proto = if header.src_addr.is_ipv4() {
        "TCP4"
    } else {
        "TCP6"
    };
    format!(
        "PROXY {} {} {} {} {}\r\n",
        proto,
        header.src_addr.ip(),
        header.dst_addr.ip(),
        header.src_addr.port(),
        header.dst_addr.port(),
    )
    .into_bytes()
}

/// PROXY protocol v2 magic signature.
const V2_SIGNATURE: [u8; 12] = [
    0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
];

/// Check if data starts with a PROXY protocol v1 or v2 signature.
///
/// Returns `Some(1)` for v1, `Some(2)` for v2, or `None` if neither is
/// detected.
pub fn detect_version(data: &[u8]) -> Option<u8> {
    if data.len() >= 6 && &data[..6] == b"PROXY " {
        Some(1)
    } else if data.len() >= 12 && data[..12] == V2_SIGNATURE {
        Some(2)
    } else {
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_v1_tcp4() {
        let data = b"PROXY TCP4 192.168.1.1 10.0.0.1 56324 443\r\n";
        let (header, consumed) = parse_v1(data).unwrap();
        assert_eq!(
            header.src_addr,
            "192.168.1.1:56324".parse::<SocketAddr>().unwrap()
        );
        assert_eq!(
            header.dst_addr,
            "10.0.0.1:443".parse::<SocketAddr>().unwrap()
        );
        assert_eq!(consumed, data.len());
    }

    #[test]
    fn test_parse_v1_tcp6() {
        let data = b"PROXY TCP6 ::1 ::1 12345 80\r\n";
        let (header, consumed) = parse_v1(data).unwrap();
        assert_eq!(
            header.src_addr,
            "[::1]:12345".parse::<SocketAddr>().unwrap()
        );
        assert_eq!(header.dst_addr, "[::1]:80".parse::<SocketAddr>().unwrap());
        assert_eq!(consumed, data.len());
    }

    #[test]
    fn test_generate_v1_tcp4() {
        let header = ProxyHeader {
            src_addr: "192.168.1.1:56324".parse().unwrap(),
            dst_addr: "10.0.0.1:443".parse().unwrap(),
        };
        let output = generate_v1(&header);
        assert_eq!(output, b"PROXY TCP4 192.168.1.1 10.0.0.1 56324 443\r\n");
    }

    #[test]
    fn test_generate_v1_tcp6() {
        let header = ProxyHeader {
            src_addr: "[::1]:12345".parse().unwrap(),
            dst_addr: "[::1]:80".parse().unwrap(),
        };
        let output = generate_v1(&header);
        assert_eq!(output, b"PROXY TCP6 ::1 ::1 12345 80\r\n");
    }

    #[test]
    fn test_roundtrip_v1() {
        let original = ProxyHeader {
            src_addr: "10.0.0.5:1234".parse().unwrap(),
            dst_addr: "10.0.0.10:8080".parse().unwrap(),
        };
        let encoded = generate_v1(&original);
        let (decoded, _) = parse_v1(&encoded).unwrap();
        assert_eq!(original, decoded);
    }

    #[test]
    fn test_detect_version_v1() {
        let data = b"PROXY TCP4 192.168.1.1 10.0.0.1 56324 443\r\n";
        assert_eq!(detect_version(data), Some(1));
    }

    #[test]
    fn test_detect_version_v2() {
        let mut data = vec![
            0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A,
        ];
        data.extend_from_slice(&[0x21, 0x11, 0x00, 0x0C]); // v2 header continues
        assert_eq!(detect_version(&data), Some(2));
    }

    #[test]
    fn test_detect_version_none() {
        let data = b"GET / HTTP/1.1\r\n";
        assert_eq!(detect_version(data), None);
    }

    #[test]
    fn test_detect_version_short_data() {
        assert_eq!(detect_version(b"PRO"), None);
        assert_eq!(detect_version(b""), None);
    }

    #[test]
    fn test_parse_v1_missing_crlf() {
        let data = b"PROXY TCP4 192.168.1.1 10.0.0.1 56324 443";
        assert!(parse_v1(data).is_err());
    }

    #[test]
    fn test_parse_v1_invalid_prefix() {
        let data = b"PROXZ TCP4 192.168.1.1 10.0.0.1 56324 443\r\n";
        assert!(parse_v1(data).is_err());
    }

    #[test]
    fn test_parse_v1_wrong_field_count() {
        let data = b"PROXY TCP4 192.168.1.1 10.0.0.1 56324\r\n";
        assert!(parse_v1(data).is_err());
    }

    #[test]
    fn test_parse_v1_invalid_ip() {
        let data = b"PROXY TCP4 not_an_ip 10.0.0.1 56324 443\r\n";
        assert!(parse_v1(data).is_err());
    }

    #[test]
    fn test_parse_v1_invalid_port() {
        let data = b"PROXY TCP4 192.168.1.1 10.0.0.1 abc 443\r\n";
        assert!(parse_v1(data).is_err());
    }
}
