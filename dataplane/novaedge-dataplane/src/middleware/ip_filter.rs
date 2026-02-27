//! IP-based access control middleware with CIDR range support.

use std::net::IpAddr;

/// IP filter configuration.
#[derive(Debug, Clone)]
pub struct IpFilterConfig {
    /// IP ranges that are always allowed (empty = no restriction).
    pub allowlist: Vec<CidrRange>,
    /// IP ranges that are always denied (takes precedence over allowlist).
    pub denylist: Vec<CidrRange>,
    /// Whether to trust X-Forwarded-For headers for extracting client IP.
    pub trust_xff: bool,
    /// How many hops from the right of XFF to use (1 = last entry = closest proxy).
    pub xff_depth: usize,
}

/// A CIDR range (e.g. `10.0.0.0/8` or `::1/128`).
#[derive(Debug, Clone)]
pub struct CidrRange {
    /// Network address.
    pub addr: IpAddr,
    /// Prefix length in bits.
    pub prefix_len: u8,
}

impl CidrRange {
    /// Parse a CIDR string like `10.0.0.0/8` or a bare IP like `192.168.1.1`.
    pub fn parse(s: &str) -> Result<Self, String> {
        if let Some((addr, prefix)) = s.split_once('/') {
            let addr: IpAddr = addr.parse().map_err(|e| format!("{e}"))?;
            let prefix_len: u8 = prefix.parse().map_err(|e| format!("{e}"))?;

            // Validate prefix length.
            let max = match addr {
                IpAddr::V4(_) => 32,
                IpAddr::V6(_) => 128,
            };
            if prefix_len > max {
                return Err(format!(
                    "prefix length {prefix_len} too large for address family (max {max})"
                ));
            }

            Ok(Self { addr, prefix_len })
        } else {
            let addr: IpAddr = s.parse().map_err(|e| format!("{e}"))?;
            let prefix_len = match addr {
                IpAddr::V4(_) => 32,
                IpAddr::V6(_) => 128,
            };
            Ok(Self { addr, prefix_len })
        }
    }

    /// Check whether the given IP falls within this CIDR range.
    pub fn contains(&self, ip: &IpAddr) -> bool {
        match (self.addr, ip) {
            (IpAddr::V4(net), IpAddr::V4(ip)) => {
                if self.prefix_len == 0 {
                    return true;
                }
                let net_bits = u32::from(net);
                let ip_bits = u32::from(*ip);
                let mask = if self.prefix_len >= 32 {
                    u32::MAX
                } else {
                    u32::MAX << (32 - self.prefix_len)
                };
                (net_bits & mask) == (ip_bits & mask)
            }
            (IpAddr::V6(net), IpAddr::V6(ip)) => {
                if self.prefix_len == 0 {
                    return true;
                }
                let net_bits = u128::from(net);
                let ip_bits = u128::from(*ip);
                let mask = if self.prefix_len >= 128 {
                    u128::MAX
                } else {
                    u128::MAX << (128 - self.prefix_len)
                };
                (net_bits & mask) == (ip_bits & mask)
            }
            _ => false, // Mismatched address families.
        }
    }
}

/// IP filter middleware.
pub struct IpFilter {
    config: IpFilterConfig,
}

impl IpFilter {
    /// Create a new IP filter.
    pub fn new(config: IpFilterConfig) -> Self {
        Self { config }
    }

    /// Check whether the request's client IP is allowed.
    ///
    /// Returns `true` if the request should be permitted, `false` if blocked.
    pub fn check(&self, req: &super::Request) -> bool {
        let ip_str = self.extract_ip(req);
        let ip: IpAddr = match ip_str.parse() {
            Ok(ip) => ip,
            Err(_) => return false,
        };

        // Denylist takes precedence.
        if self.config.denylist.iter().any(|r| r.contains(&ip)) {
            return false;
        }

        // If allowlist is non-empty, IP must be in it.
        if !self.config.allowlist.is_empty() {
            return self.config.allowlist.iter().any(|r| r.contains(&ip));
        }

        true // No restrictions.
    }

    /// Extract the effective client IP from a request, optionally trusting XFF.
    fn extract_ip(&self, req: &super::Request) -> String {
        if self.config.trust_xff {
            if let Some((_, xff)) = req
                .headers
                .iter()
                .find(|(k, _)| k.eq_ignore_ascii_case("x-forwarded-for"))
            {
                let ips: Vec<&str> = xff.split(',').map(|s| s.trim()).collect();
                // xff_depth of 1 means the rightmost entry (closest proxy).
                let index = ips.len().saturating_sub(self.config.xff_depth);
                if let Some(ip) = ips.get(index) {
                    return ip.to_string();
                }
            }
        }
        req.client_ip.clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::Ipv4Addr;

    // ---- CidrRange tests ----

    #[test]
    fn cidr_parse_v4_with_prefix() {
        let r = CidrRange::parse("10.0.0.0/8").unwrap();
        assert_eq!(r.prefix_len, 8);
        assert_eq!(r.addr, IpAddr::V4(Ipv4Addr::new(10, 0, 0, 0)));
    }

    #[test]
    fn cidr_parse_v4_bare() {
        let r = CidrRange::parse("192.168.1.1").unwrap();
        assert_eq!(r.prefix_len, 32);
    }

    #[test]
    fn cidr_parse_v6() {
        let r = CidrRange::parse("::1").unwrap();
        assert_eq!(r.prefix_len, 128);
    }

    #[test]
    fn cidr_parse_v6_with_prefix() {
        let r = CidrRange::parse("fe80::/10").unwrap();
        assert_eq!(r.prefix_len, 10);
    }

    #[test]
    fn cidr_parse_invalid() {
        assert!(CidrRange::parse("not-an-ip").is_err());
        assert!(CidrRange::parse("10.0.0.0/33").is_err());
    }

    #[test]
    fn cidr_contains_v4_exact() {
        let r = CidrRange::parse("10.0.0.1").unwrap();
        assert!(r.contains(&"10.0.0.1".parse().unwrap()));
        assert!(!r.contains(&"10.0.0.2".parse().unwrap()));
    }

    #[test]
    fn cidr_contains_v4_subnet() {
        let r = CidrRange::parse("10.0.0.0/24").unwrap();
        assert!(r.contains(&"10.0.0.1".parse().unwrap()));
        assert!(r.contains(&"10.0.0.255".parse().unwrap()));
        assert!(!r.contains(&"10.0.1.0".parse().unwrap()));
    }

    #[test]
    fn cidr_contains_v4_large_subnet() {
        let r = CidrRange::parse("172.16.0.0/12").unwrap();
        assert!(r.contains(&"172.16.0.1".parse().unwrap()));
        assert!(r.contains(&"172.31.255.255".parse().unwrap()));
        assert!(!r.contains(&"172.32.0.0".parse().unwrap()));
    }

    #[test]
    fn cidr_contains_v6() {
        let r = CidrRange::parse("fe80::/10").unwrap();
        assert!(r.contains(&"fe80::1".parse().unwrap()));
        assert!(r.contains(&"feb0::1".parse().unwrap()));
        assert!(!r.contains(&"::1".parse().unwrap()));
    }

    #[test]
    fn cidr_mismatched_families() {
        let r = CidrRange::parse("10.0.0.0/8").unwrap();
        assert!(!r.contains(&"::1".parse().unwrap()));
    }

    #[test]
    fn cidr_v4_zero_prefix() {
        let r = CidrRange::parse("0.0.0.0/0").unwrap();
        assert!(r.contains(&"1.2.3.4".parse().unwrap()));
        assert!(r.contains(&"255.255.255.255".parse().unwrap()));
    }

    // ---- IpFilter tests ----

    fn make_req(client_ip: &str) -> crate::middleware::Request {
        crate::middleware::Request {
            method: "GET".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers: vec![],
            body: None,
            client_ip: client_ip.into(),
        }
    }

    fn make_req_with_xff(client_ip: &str, xff: &str) -> crate::middleware::Request {
        crate::middleware::Request {
            method: "GET".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers: vec![("X-Forwarded-For".into(), xff.into())],
            body: None,
            client_ip: client_ip.into(),
        }
    }

    #[test]
    fn no_restrictions_allows_all() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![],
            denylist: vec![],
            trust_xff: false,
            xff_depth: 1,
        });

        assert!(filter.check(&make_req("1.2.3.4")));
        assert!(filter.check(&make_req("10.0.0.1")));
    }

    #[test]
    fn denylist_blocks() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![],
            denylist: vec![CidrRange::parse("10.0.0.0/8").unwrap()],
            trust_xff: false,
            xff_depth: 1,
        });

        assert!(!filter.check(&make_req("10.0.0.1")));
        assert!(filter.check(&make_req("192.168.1.1")));
    }

    #[test]
    fn allowlist_only_permits_listed() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![CidrRange::parse("192.168.0.0/16").unwrap()],
            denylist: vec![],
            trust_xff: false,
            xff_depth: 1,
        });

        assert!(filter.check(&make_req("192.168.1.1")));
        assert!(!filter.check(&make_req("10.0.0.1")));
    }

    #[test]
    fn denylist_overrides_allowlist() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![CidrRange::parse("10.0.0.0/8").unwrap()],
            denylist: vec![CidrRange::parse("10.0.0.1").unwrap()],
            trust_xff: false,
            xff_depth: 1,
        });

        assert!(!filter.check(&make_req("10.0.0.1"))); // denied
        assert!(filter.check(&make_req("10.0.0.2"))); // allowed
    }

    #[test]
    fn invalid_ip_denied() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![],
            denylist: vec![],
            trust_xff: false,
            xff_depth: 1,
        });

        assert!(!filter.check(&make_req("not-an-ip")));
    }

    #[test]
    fn xff_trusted() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![CidrRange::parse("1.2.3.4").unwrap()],
            denylist: vec![],
            trust_xff: true,
            xff_depth: 1,
        });

        // XFF has 1.2.3.4 as the rightmost entry.
        let req = make_req_with_xff("10.0.0.1", "5.6.7.8, 1.2.3.4");
        assert!(filter.check(&req));
    }

    #[test]
    fn xff_not_trusted() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![CidrRange::parse("1.2.3.4").unwrap()],
            denylist: vec![],
            trust_xff: false,
            xff_depth: 1,
        });

        // Even with XFF, should use client_ip when trust_xff is false.
        let req = make_req_with_xff("10.0.0.1", "1.2.3.4");
        assert!(!filter.check(&req));
    }

    #[test]
    fn xff_depth_selects_correct_entry() {
        let filter = IpFilter::new(IpFilterConfig {
            allowlist: vec![CidrRange::parse("5.6.7.8").unwrap()],
            denylist: vec![],
            trust_xff: true,
            xff_depth: 2,
        });

        // depth=2 => 2nd from right => "5.6.7.8"
        let req = make_req_with_xff("10.0.0.1", "9.9.9.9, 5.6.7.8, 1.2.3.4");
        assert!(filter.check(&req));
    }
}
