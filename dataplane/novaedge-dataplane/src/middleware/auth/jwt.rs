//! JWT (JSON Web Token) validation middleware.
//!
//! Provides claim-level validation (expiration, not-before, issuer, audience)
//! without full cryptographic signature verification (which would require a
//! crypto library such as `ring` or `jsonwebtoken`).

use std::time::{SystemTime, UNIX_EPOCH};

use base64::Engine;

/// JWT validator configuration.
#[derive(Debug, Clone)]
pub struct JwtConfig {
    /// Shared secret for HMAC verification (reserved for future use).
    pub secret: Option<String>,
    /// Expected issuer (`iss` claim).
    pub issuer: Option<String>,
    /// Expected audiences (`aud` claim -- token must match one).
    pub audiences: Vec<String>,
    /// Claims that must be present in the token.
    pub required_claims: Vec<String>,
}

/// Parsed JWT claims.
#[derive(Debug, Clone)]
pub struct JwtClaims {
    /// Subject (`sub`).
    pub sub: Option<String>,
    /// Issuer (`iss`).
    pub iss: Option<String>,
    /// Audience (`aud`).
    pub aud: Option<String>,
    /// Expiration time as Unix timestamp (`exp`).
    pub exp: Option<u64>,
    /// Not-before time as Unix timestamp (`nbf`).
    pub nbf: Option<u64>,
    /// Issued-at time as Unix timestamp (`iat`).
    pub iat: Option<u64>,
    /// Additional claims as key-value pairs.
    pub extra: Vec<(String, String)>,
}

/// JWT token validator.
pub struct JwtValidator {
    config: JwtConfig,
}

impl JwtValidator {
    /// Create a new JWT validator.
    pub fn new(config: JwtConfig) -> Self {
        Self { config }
    }

    /// Parse and validate a JWT token.
    ///
    /// Performs structural validation and claim checks (expiration, not-before,
    /// issuer, audience). Full cryptographic signature verification is not
    /// implemented -- that would require a dependency like `ring`.
    pub fn validate(&self, token: &str) -> Result<JwtClaims, String> {
        let parts: Vec<&str> = token.split('.').collect();
        if parts.len() != 3 {
            return Err("invalid JWT format".into());
        }

        // Decode payload (part 1).
        let payload = base64::engine::general_purpose::URL_SAFE_NO_PAD
            .decode(parts[1])
            .map_err(|e| format!("base64 decode error: {e}"))?;

        let payload_str = String::from_utf8(payload).map_err(|e| format!("UTF-8 error: {e}"))?;

        // Parse claims from JSON payload.
        let claims = parse_jwt_claims(&payload_str)?;

        // Validate expiration.
        if let Some(exp) = claims.exp {
            let now = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_secs();
            if now > exp {
                return Err("token expired".into());
            }
        }

        // Validate not-before.
        if let Some(nbf) = claims.nbf {
            let now = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .unwrap()
                .as_secs();
            if now < nbf {
                return Err("token not yet valid".into());
            }
        }

        // Validate issuer.
        if let Some(expected_iss) = &self.config.issuer {
            if claims.iss.as_deref() != Some(expected_iss.as_str()) {
                return Err("invalid issuer".into());
            }
        }

        // Validate audience.
        if !self.config.audiences.is_empty() {
            if let Some(aud) = &claims.aud {
                if !self.config.audiences.iter().any(|a| a == aud) {
                    return Err("invalid audience".into());
                }
            } else {
                return Err("missing audience".into());
            }
        }

        // Check required claims.
        for claim in &self.config.required_claims {
            let present = match claim.as_str() {
                "sub" => claims.sub.is_some(),
                "iss" => claims.iss.is_some(),
                "aud" => claims.aud.is_some(),
                "exp" => claims.exp.is_some(),
                "nbf" => claims.nbf.is_some(),
                "iat" => claims.iat.is_some(),
                other => claims.extra.iter().any(|(k, _)| k == other),
            };
            if !present {
                return Err(format!("missing required claim: {claim}"));
            }
        }

        Ok(claims)
    }

    /// Check a request for a valid Bearer JWT token.
    pub fn check(&self, req: &super::super::Request) -> super::AuthResult {
        let auth_header = req
            .headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("authorization"))
            .map(|(_, v)| v.as_str());

        match auth_header {
            Some(header) if header.starts_with("Bearer ") => {
                let token = &header[7..];
                match self.validate(token) {
                    Ok(claims) => super::AuthResult::Authenticated {
                        user: claims.sub.unwrap_or_default(),
                        claims: claims.extra,
                    },
                    Err(e) => super::AuthResult::Denied {
                        status: 401,
                        message: e,
                    },
                }
            }
            _ => super::AuthResult::Denied {
                status: 401,
                message: "Bearer token required".into(),
            },
        }
    }
}

/// Parse JWT claims from a JSON payload using serde_json.
fn parse_jwt_claims(json: &str) -> Result<JwtClaims, String> {
    let value: serde_json::Value =
        serde_json::from_str(json).map_err(|e| format!("invalid JSON payload: {e}"))?;

    let obj = value
        .as_object()
        .ok_or_else(|| "JWT payload is not a JSON object".to_string())?;

    let mut extra = Vec::new();
    for (k, v) in obj {
        match k.as_str() {
            "sub" | "iss" | "aud" | "exp" | "nbf" | "iat" => {} // handled below
            _ => {
                let val_str = match v {
                    serde_json::Value::String(s) => s.clone(),
                    other => other.to_string(),
                };
                extra.push((k.clone(), val_str));
            }
        }
    }

    Ok(JwtClaims {
        sub: obj.get("sub").and_then(|v| v.as_str()).map(String::from),
        iss: obj.get("iss").and_then(|v| v.as_str()).map(String::from),
        aud: obj.get("aud").and_then(|v| v.as_str()).map(String::from),
        exp: obj.get("exp").and_then(|v| v.as_u64()),
        nbf: obj.get("nbf").and_then(|v| v.as_u64()),
        iat: obj.get("iat").and_then(|v| v.as_u64()),
        extra,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Helper: build a JWT from a JSON payload (no real signature).
    fn make_jwt(payload_json: &str) -> String {
        let header = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(r#"{"alg":"none"}"#);
        let payload = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(payload_json);
        format!("{header}.{payload}.nosig")
    }

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

    fn future_ts() -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs()
            + 3600
    }

    fn past_ts() -> u64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs()
            - 3600
    }

    #[test]
    fn valid_token() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"user1","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        let result = validator.validate(&token);
        assert!(result.is_ok());
        let claims = result.unwrap();
        assert_eq!(claims.sub, Some("user1".into()));
    }

    #[test]
    fn expired_token() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"user1","exp":{}}}"#, past_ts());
        let token = make_jwt(&payload);
        let result = validator.validate(&token);
        assert_eq!(result.unwrap_err(), "token expired");
    }

    #[test]
    fn not_yet_valid_token() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(
            r#"{{"sub":"user1","nbf":{},"exp":{}}}"#,
            future_ts() + 7200,
            future_ts() + 14400,
        );
        let token = make_jwt(&payload);
        let result = validator.validate(&token);
        assert_eq!(result.unwrap_err(), "token not yet valid");
    }

    #[test]
    fn invalid_issuer() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: Some("expected-issuer".into()),
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(
            r#"{{"sub":"user1","iss":"wrong-issuer","exp":{}}}"#,
            future_ts()
        );
        let token = make_jwt(&payload);
        assert_eq!(validator.validate(&token).unwrap_err(), "invalid issuer");
    }

    #[test]
    fn valid_issuer() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: Some("my-issuer".into()),
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(
            r#"{{"sub":"user1","iss":"my-issuer","exp":{}}}"#,
            future_ts()
        );
        let token = make_jwt(&payload);
        assert!(validator.validate(&token).is_ok());
    }

    #[test]
    fn invalid_audience() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec!["aud-a".into(), "aud-b".into()],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"user1","aud":"aud-c","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        assert_eq!(validator.validate(&token).unwrap_err(), "invalid audience");
    }

    #[test]
    fn valid_audience() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec!["aud-a".into(), "aud-b".into()],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"user1","aud":"aud-b","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        assert!(validator.validate(&token).is_ok());
    }

    #[test]
    fn missing_audience_when_required() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec!["aud-a".into()],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"user1","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        assert_eq!(validator.validate(&token).unwrap_err(), "missing audience");
    }

    #[test]
    fn invalid_jwt_format() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        assert!(validator.validate("not-a-jwt").is_err());
        assert!(validator.validate("only.two").is_err());
    }

    #[test]
    fn check_with_bearer_token() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"alice","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        let req = make_req(Some(&format!("Bearer {token}")));

        match validator.check(&req) {
            super::super::AuthResult::Authenticated { user, .. } => {
                assert_eq!(user, "alice");
            }
            other => panic!("expected Authenticated, got: {other:?}"),
        }
    }

    #[test]
    fn check_missing_bearer() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let req = make_req(None);
        assert!(matches!(
            validator.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn check_expired_bearer() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec![],
        });

        let payload = format!(r#"{{"sub":"alice","exp":{}}}"#, past_ts());
        let token = make_jwt(&payload);
        let req = make_req(Some(&format!("Bearer {token}")));

        assert!(matches!(
            validator.check(&req),
            super::super::AuthResult::Denied { status: 401, .. }
        ));
    }

    #[test]
    fn required_claims_present() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec!["sub".into(), "exp".into()],
        });

        let payload = format!(r#"{{"sub":"user1","exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        assert!(validator.validate(&token).is_ok());
    }

    #[test]
    fn required_claims_missing() {
        let validator = JwtValidator::new(JwtConfig {
            secret: None,
            issuer: None,
            audiences: vec![],
            required_claims: vec!["sub".into()],
        });

        let payload = format!(r#"{{"exp":{}}}"#, future_ts());
        let token = make_jwt(&payload);
        assert_eq!(
            validator.validate(&token).unwrap_err(),
            "missing required claim: sub"
        );
    }

    #[test]
    fn parse_claims_with_serde() {
        let claims = parse_jwt_claims(r#"{"sub":"hello","iss":"world","exp":1234567890}"#).unwrap();
        assert_eq!(claims.sub, Some("hello".into()));
        assert_eq!(claims.iss, Some("world".into()));
        assert_eq!(claims.exp, Some(1234567890));
    }

    #[test]
    fn parse_claims_extra_fields() {
        let claims =
            parse_jwt_claims(r#"{"sub":"u1","role":"admin","exp":9999999999}"#).unwrap();
        assert_eq!(claims.sub, Some("u1".into()));
        assert!(claims.extra.iter().any(|(k, v)| k == "role" && v == "admin"));
    }

    #[test]
    fn parse_claims_invalid_json() {
        assert!(parse_jwt_claims("not json").is_err());
    }
}
