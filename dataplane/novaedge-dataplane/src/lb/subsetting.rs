//! Deterministic subsetting for endpoint selection.
//!
//! Given a proxy identity (e.g., node name) and endpoint list, uses
//! Rendezvous hashing to deterministically select a subset of K endpoints.
//! This distributes load evenly across endpoints while ensuring that each
//! proxy instance consistently selects the same subset.

use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};

/// Select a deterministic subset of endpoint indices using Rendezvous hashing.
///
/// Each endpoint is scored by hashing `(identity, endpoint_index)`.
/// The top `subset_size` endpoints by score are returned.
///
/// If `subset_size` is 0 or >= the number of candidates, returns all indices.
#[allow(dead_code)]
pub fn deterministic_subset(
    identity: &str,
    candidates: &[usize],
    subset_size: usize,
) -> Vec<usize> {
    if subset_size == 0 || subset_size >= candidates.len() {
        return candidates.to_vec();
    }

    // Score each candidate using Rendezvous hashing.
    let mut scored: Vec<(usize, u64)> = candidates
        .iter()
        .map(|&idx| {
            let mut hasher = DefaultHasher::new();
            identity.hash(&mut hasher);
            idx.hash(&mut hasher);
            (idx, hasher.finish())
        })
        .collect();

    // Sort by score descending; take top subset_size.
    scored.sort_by(|a, b| b.1.cmp(&a.1));
    scored.truncate(subset_size);
    scored.into_iter().map(|(idx, _)| idx).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn subset_selects_correct_count() {
        let candidates: Vec<usize> = (0..10).collect();
        let result = deterministic_subset("node-1", &candidates, 3);
        assert_eq!(result.len(), 3);
    }

    #[test]
    fn subset_is_deterministic() {
        let candidates: Vec<usize> = (0..10).collect();
        let r1 = deterministic_subset("node-1", &candidates, 5);
        let r2 = deterministic_subset("node-1", &candidates, 5);
        assert_eq!(r1, r2);
    }

    #[test]
    fn different_identities_get_different_subsets() {
        let candidates: Vec<usize> = (0..20).collect();
        let r1 = deterministic_subset("node-1", &candidates, 5);
        let r2 = deterministic_subset("node-2", &candidates, 5);
        // With 20 candidates and subset of 5, different identities should
        // (almost certainly) produce different subsets.
        assert_ne!(r1, r2);
    }

    #[test]
    fn subset_zero_returns_all() {
        let candidates: Vec<usize> = (0..5).collect();
        let result = deterministic_subset("node-1", &candidates, 0);
        assert_eq!(result, candidates);
    }

    #[test]
    fn subset_larger_than_candidates_returns_all() {
        let candidates: Vec<usize> = (0..3).collect();
        let result = deterministic_subset("node-1", &candidates, 10);
        assert_eq!(result, candidates);
    }
}
