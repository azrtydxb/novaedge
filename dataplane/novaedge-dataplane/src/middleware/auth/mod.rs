//! Authentication middleware supporting Basic, JWT, and forward-auth schemes.

pub mod basic;
pub mod forward;
pub mod jwt;

/// Result of an authentication check.
#[derive(Debug, Clone)]
pub enum AuthResult {
    /// The request is authenticated.
    Authenticated {
        /// Authenticated user identifier.
        user: String,
        /// Additional claims/attributes extracted during authentication.
        claims: Vec<(String, String)>,
    },
    /// The request was denied.
    Denied {
        /// HTTP status code to return (e.g. 401, 403).
        status: u16,
        /// Human-readable denial reason.
        message: String,
    },
}
