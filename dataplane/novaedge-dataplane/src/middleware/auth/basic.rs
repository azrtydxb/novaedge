//! HTTP Basic authentication middleware.

use std::collections::HashMap;

use base64::Engine;

/// HTTP Basic authentication handler.
pub struct BasicAuth {
    /// Realm string for the WWW-Authenticate header.
    pub realm: String,
    /// Mapping from username to password (plaintext comparison).
    pub users: HashMap<String, String>,
}

impl BasicAuth {
    /// Create a new Basic auth handler.
    pub fn new(realm: &str, users: HashMap<String, String>) -> Self {
        Self {
            realm: realm.to_string(),
            users,
        }
    }

    /// Check a request for valid Basic authentication credentials.
    pub fn check(&self, req: &super::super::Request) -> super::AuthResult {
        let auth_header = req
            .headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("authorization"))
            .map(|(_, v)| v.as_str());

        match auth_header {
            Some(header) if header.starts_with("Basic ") => {
                let encoded = &header[6..];
                if let Ok(decoded) = base64::engine::general_purpose::STANDARD.decode(encoded) {
                    if let Ok(creds) = String::from_utf8(decoded) {
                        if let Some((user, pass)) = creds.split_once(':') {
                            if let Some(expected) = self.users.get(user) {
                                if expected == pass {
                                    return super::AuthResult::Authenticated {
                                        user: user.to_string(),
                                        claims: vec![],
                                    };
                                }
                            }
                        }
                    }
                }
                super::AuthResult::Denied {
                    status: 401,
                    message: "Invalid credentials".into(),
                }
            }
            _ => super::AuthResult::Denied {
                status: 401,
                message: "Authentication required".into(),
            },
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_req(auth_header: Option<&str>) -> crate::middleware::Request {
        let mut headers = vec![];
        if let Some(h) = auth_header {
            headers.push(("Authorization".to_string(), h.to_string()));
        }
        crate::middleware::Request {
            method: "GET".into(),
            path: "/".into(),
            host: "example.com".into(),
            headers,
            body: None,
            client_ip: "127.0.0.1".into(),
        }
    }

    fn make_auth() -> BasicAuth {
        let mut users = HashMap::new();
        users.insert("admin".into(), "secret".into());
        users.insert("user".into(), "pass123".into());
        BasicAuth::new("test-realm", users)
    }

    #[test]
    fn valid_credentials() {
        let auth = make_auth();
        // admin:secret => YWRtaW46c2VjcmV0
        let req = make_req(Some("Basic YWRtaW46c2VjcmV0"));
        match auth.check(&req) {
            super::super::AuthResult::Authenticated { user, .. } => {
                assert_eq!(user, "admin");
            }
            other => panic!("expected Authenticated, got: {other:?}"),
        }
    }

    #[test]
    fn valid_credentials_second_user() {
        let auth = make_auth();
        // user:pass123 => dXNlcjpwYXNzMTIz
        let req = make_req(Some("Basic dXNlcjpwYXNzMTIz"));
        match auth.check(&req) {
            super::super::AuthResult::Authenticated { user, .. } => {
                assert_eq!(user, "user");
            }
            other => panic!("expected Authenticated, got: {other:?}"),
        }
    }

    #[test]
    fn invalid_password() {
        let auth = make_auth();
        // admin:wrong => YWRtaW46d3Jvbmc=
        let req = make_req(Some("Basic YWRtaW46d3Jvbmc="));
        assert!(matches!(
            auth.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn unknown_user() {
        let auth = make_auth();
        // nobody:pass => bm9ib2R5OnBhc3M=
        let req = make_req(Some("Basic bm9ib2R5OnBhc3M="));
        assert!(matches!(
            auth.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn missing_auth_header() {
        let auth = make_auth();
        let req = make_req(None);
        match auth.check(&req) {
            super::super::AuthResult::Denied { status, message } => {
                assert_eq!(status, 401);
                assert_eq!(message, "Authentication required");
            }
            other => panic!("expected Denied, got: {other:?}"),
        }
    }

    #[test]
    fn invalid_base64() {
        let auth = make_auth();
        let req = make_req(Some("Basic !!!invalid!!!"));
        assert!(matches!(
            auth.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn wrong_auth_scheme() {
        let auth = make_auth();
        let req = make_req(Some("Bearer some-token"));
        assert!(matches!(
            auth.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn malformed_credentials_no_colon() {
        let auth = make_auth();
        // "nocolon" => bm9jb2xvbg==
        let req = make_req(Some("Basic bm9jb2xvbg=="));
        assert!(matches!(
            auth.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }
}
