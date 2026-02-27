/// Response header modification actions.
#[derive(Debug, Clone)]
pub enum HeaderAction {
    Set(String, String),
    Add(String, String),
    Remove(String),
}

/// Apply header modifications to a set of response headers.
pub fn apply_header_actions(headers: &mut Vec<(String, String)>, actions: &[HeaderAction]) {
    for action in actions {
        match action {
            HeaderAction::Set(name, value) => {
                headers.retain(|(n, _)| !n.eq_ignore_ascii_case(name));
                headers.push((name.clone(), value.clone()));
            }
            HeaderAction::Add(name, value) => {
                headers.push((name.clone(), value.clone()));
            }
            HeaderAction::Remove(name) => {
                headers.retain(|(n, _)| !n.eq_ignore_ascii_case(name));
            }
        }
    }
}

/// Custom error page mapping.
#[derive(Debug, Clone)]
pub struct ErrorPage {
    pub status_codes: Vec<u16>,
    pub body: String,
    pub content_type: String,
}

/// Find custom error page for a given status code.
pub fn find_error_page(status: u16, pages: &[ErrorPage]) -> Option<&ErrorPage> {
    pages.iter().find(|p| p.status_codes.contains(&status))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_set_header_action() {
        let mut headers = vec![
            ("Content-Type".into(), "text/plain".into()),
            ("X-Custom".into(), "old".into()),
        ];
        apply_header_actions(
            &mut headers,
            &[HeaderAction::Set("X-Custom".into(), "new".into())],
        );
        // The old value should be replaced.
        assert_eq!(
            headers,
            vec![
                ("Content-Type".into(), "text/plain".into()),
                ("X-Custom".into(), "new".into()),
            ]
        );
    }

    #[test]
    fn test_set_header_action_case_insensitive() {
        let mut headers = vec![("x-custom".into(), "old".into())];
        apply_header_actions(
            &mut headers,
            &[HeaderAction::Set("X-Custom".into(), "new".into())],
        );
        assert_eq!(headers, vec![("X-Custom".into(), "new".into())]);
    }

    #[test]
    fn test_add_header_action() {
        let mut headers = vec![("X-Existing".into(), "value".into())];
        apply_header_actions(
            &mut headers,
            &[HeaderAction::Add("X-New".into(), "added".into())],
        );
        assert_eq!(
            headers,
            vec![
                ("X-Existing".into(), "value".into()),
                ("X-New".into(), "added".into()),
            ]
        );
    }

    #[test]
    fn test_add_header_does_not_replace() {
        let mut headers = vec![("X-Multi".into(), "first".into())];
        apply_header_actions(
            &mut headers,
            &[HeaderAction::Add("X-Multi".into(), "second".into())],
        );
        assert_eq!(headers.len(), 2);
    }

    #[test]
    fn test_remove_header_action() {
        let mut headers = vec![
            ("Content-Type".into(), "text/html".into()),
            ("X-Remove-Me".into(), "bye".into()),
        ];
        apply_header_actions(&mut headers, &[HeaderAction::Remove("X-Remove-Me".into())]);
        assert_eq!(headers, vec![("Content-Type".into(), "text/html".into())]);
    }

    #[test]
    fn test_remove_header_case_insensitive() {
        let mut headers = vec![("x-remove-me".into(), "bye".into())];
        apply_header_actions(&mut headers, &[HeaderAction::Remove("X-Remove-Me".into())]);
        assert!(headers.is_empty());
    }

    #[test]
    fn test_find_error_page_match() {
        let pages = vec![
            ErrorPage {
                status_codes: vec![500, 502, 503],
                body: "<h1>Server Error</h1>".into(),
                content_type: "text/html".into(),
            },
            ErrorPage {
                status_codes: vec![404],
                body: "<h1>Not Found</h1>".into(),
                content_type: "text/html".into(),
            },
        ];

        let page = find_error_page(502, &pages).unwrap();
        assert_eq!(page.body, "<h1>Server Error</h1>");

        let page = find_error_page(404, &pages).unwrap();
        assert_eq!(page.body, "<h1>Not Found</h1>");
    }

    #[test]
    fn test_find_error_page_no_match() {
        let pages = vec![ErrorPage {
            status_codes: vec![500],
            body: "error".into(),
            content_type: "text/plain".into(),
        }];

        assert!(find_error_page(200, &pages).is_none());
        assert!(find_error_page(404, &pages).is_none());
    }

    #[test]
    fn test_find_error_page_empty_list() {
        assert!(find_error_page(500, &[]).is_none());
    }
}
