/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package router

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ExprNode represents a node in the boolean expression AST.
type ExprNode interface {
	Evaluate(r *http.Request) bool
	String() string
}

// AndExpr evaluates to true when both left and right are true.
type AndExpr struct {
	Left  ExprNode
	Right ExprNode
}

// Evaluate performs boolean AND.
func (e *AndExpr) Evaluate(r *http.Request) bool {
	return e.Left.Evaluate(r) && e.Right.Evaluate(r)
}

// String returns a human-readable representation.
func (e *AndExpr) String() string {
	return fmt.Sprintf("(%s AND %s)", e.Left.String(), e.Right.String())
}

// OrExpr evaluates to true when either left or right is true.
type OrExpr struct {
	Left  ExprNode
	Right ExprNode
}

// Evaluate performs boolean OR.
func (e *OrExpr) Evaluate(r *http.Request) bool {
	return e.Left.Evaluate(r) || e.Right.Evaluate(r)
}

// String returns a human-readable representation.
func (e *OrExpr) String() string {
	return fmt.Sprintf("(%s OR %s)", e.Left.String(), e.Right.String())
}

// NotExpr negates the inner expression.
type NotExpr struct {
	Inner ExprNode
}

// Evaluate performs boolean NOT.
func (e *NotExpr) Evaluate(r *http.Request) bool {
	return !e.Inner.Evaluate(r)
}

// String returns a human-readable representation.
func (e *NotExpr) String() string {
	return fmt.Sprintf("NOT(%s)", e.Inner.String())
}

// HeaderExpr matches a request header against a value.
type HeaderExpr struct {
	Name  string
	Op    string // "==" or "!="
	Value string
}

// Evaluate checks the header value.
func (e *HeaderExpr) Evaluate(r *http.Request) bool {
	actual := r.Header.Get(e.Name)
	switch e.Op {
	case "==":
		return actual == e.Value
	case "!=":
		return actual != e.Value
	default:
		return false
	}
}

// String returns a human-readable representation.
func (e *HeaderExpr) String() string {
	return fmt.Sprintf("header:%s %s %q", e.Name, e.Op, e.Value)
}

// PathExpr matches the request path.
type PathExpr struct {
	MatchType string // "exact", "prefix", "contains"
	Value     string
}

// Evaluate checks the path match.
func (e *PathExpr) Evaluate(r *http.Request) bool {
	path := r.URL.Path
	switch e.MatchType {
	case "exact":
		return path == e.Value
	case "prefix":
		return strings.HasPrefix(path, e.Value)
	case "contains":
		return strings.Contains(path, e.Value)
	default:
		return false
	}
}

// String returns a human-readable representation.
func (e *PathExpr) String() string {
	return fmt.Sprintf("path %s %q", e.MatchType, e.Value)
}

// MethodExpr matches the HTTP method.
type MethodExpr struct {
	Method string
}

// Evaluate checks the method.
func (e *MethodExpr) Evaluate(r *http.Request) bool {
	return strings.EqualFold(r.Method, e.Method)
}

// String returns a human-readable representation.
func (e *MethodExpr) String() string {
	return fmt.Sprintf("method == %q", e.Method)
}

// QueryParamExpr matches a query parameter.
type QueryParamExpr struct {
	Name  string
	Op    string // "==" or "!="
	Value string
}

// Evaluate checks the query parameter.
func (e *QueryParamExpr) Evaluate(r *http.Request) bool {
	actual := r.URL.Query().Get(e.Name)
	switch e.Op {
	case "==":
		return actual == e.Value
	case "!=":
		return actual != e.Value
	default:
		return false
	}
}

// String returns a human-readable representation.
func (e *QueryParamExpr) String() string {
	return fmt.Sprintf("query:%s %s %q", e.Name, e.Op, e.Value)
}

// SourceIPExpr matches the client source IP against a CIDR.
type SourceIPExpr struct {
	CIDR    string
	network *net.IPNet
}

// Evaluate checks the source IP.
func (e *SourceIPExpr) Evaluate(r *http.Request) bool {
	if e.network == nil {
		return false
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	// Strip brackets from IPv6
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return e.network.Contains(ip)
}

// String returns a human-readable representation.
func (e *SourceIPExpr) String() string {
	return fmt.Sprintf("source_ip in %q", e.CIDR)
}

// TrueExpr always evaluates to true (used as a default).
type TrueExpr struct{}

// Evaluate always returns true.
func (e *TrueExpr) Evaluate(_ *http.Request) bool { return true }

// String returns "true".
func (e *TrueExpr) String() string { return "true" }

// --- Expression parser ---

// CompileExpression parses a boolean expression string into an ExprNode AST.
// Grammar (simplified):
//
//	expr     = or_expr
//	or_expr  = and_expr ("OR" and_expr)*
//	and_expr = not_expr ("AND" not_expr)*
//	not_expr = "NOT" atom | atom
//	atom     = "(" expr ")" | operand
//	operand  = header_match | path_match | method_match | query_match | source_ip_match
//
// Operand syntax:
//
//	header:X-Env == "value"
//	header:X-Env != "value"
//	path exact "/foo"
//	path prefix "/api"
//	path contains "/v2"
//	method == "GET"
//	query:key == "value"
//	source_ip in "10.0.0.0/8"
func CompileExpression(expr string) (ExprNode, error) {
	if strings.TrimSpace(expr) == "" {
		return &TrueExpr{}, nil
	}
	tokens, err := tokenize(expr)
	if err != nil {
		return nil, fmt.Errorf("tokenize error: %w", err)
	}
	p := &exprParser{tokens: tokens, pos: 0}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.tokens) {
		return nil, fmt.Errorf("unexpected token at position %d: %q", p.pos, p.tokens[p.pos])
	}
	return node, nil
}

// token types
type tokenKind int

const (
	tokenWord   tokenKind = iota // keyword or identifier
	tokenString                  // quoted string
	tokenLParen                  // (
	tokenRParen                  // )
	tokenOp                      // ==, !=
)

type token struct {
	kind  tokenKind
	value string
}

// tokenize splits an expression string into tokens.
func tokenize(expr string) ([]token, error) {
	var tokens []token
	i := 0
	runes := []rune(expr)

	for i < len(runes) {
		ch := runes[i]

		// Skip whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}

		// Parentheses
		if ch == '(' {
			tokens = append(tokens, token{kind: tokenLParen, value: "("})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, token{kind: tokenRParen, value: ")"})
			i++
			continue
		}

		// Operators == !=
		if ch == '=' && i+1 < len(runes) && runes[i+1] == '=' {
			tokens = append(tokens, token{kind: tokenOp, value: "=="})
			i += 2
			continue
		}
		if ch == '!' && i+1 < len(runes) && runes[i+1] == '=' {
			tokens = append(tokens, token{kind: tokenOp, value: "!="})
			i += 2
			continue
		}

		// Quoted string
		if ch == '"' {
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' && j+1 < len(runes) {
					j++ // skip escaped char
				}
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated string at position %d", i)
			}
			tokens = append(tokens, token{kind: tokenString, value: string(runes[i+1 : j])})
			i = j + 1
			continue
		}

		// Word (keywords, identifiers with : and / and -)
		if isWordChar(ch) {
			j := i
			for j < len(runes) && isWordChar(runes[j]) {
				j++
			}
			tokens = append(tokens, token{kind: tokenWord, value: string(runes[i:j])})
			i = j
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at position %d", string(ch), i)
	}

	return tokens, nil
}

func isWordChar(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' || ch == '-' || ch == ':' || ch == '/' || ch == '.' || ch == '*'
}

// exprParser is a recursive-descent parser for boolean expressions.
type exprParser struct {
	tokens []token
	pos    int
}

func (p *exprParser) peek() *token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

func (p *exprParser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *exprParser) expect(kind tokenKind, value string) error {
	if p.pos >= len(p.tokens) {
		return fmt.Errorf("expected %q but got end of expression", value)
	}
	t := p.tokens[p.pos]
	if t.kind != kind || (value != "" && t.value != value) {
		return fmt.Errorf("expected %q but got %q at position %d", value, t.value, p.pos)
	}
	p.pos++
	return nil
}

func (p *exprParser) parseOr() (ExprNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() != nil && p.peek().kind == tokenWord && strings.EqualFold(p.peek().value, "OR") {
		p.advance() // consume OR
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &OrExpr{Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (ExprNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek() != nil && p.peek().kind == tokenWord && strings.EqualFold(p.peek().value, "AND") {
		p.advance() // consume AND
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &AndExpr{Left: left, Right: right}
	}
	return left, nil
}

func (p *exprParser) parseNot() (ExprNode, error) {
	if p.peek() != nil && p.peek().kind == tokenWord && strings.EqualFold(p.peek().value, "NOT") {
		p.advance() // consume NOT
		inner, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &NotExpr{Inner: inner}, nil
	}
	return p.parseAtom()
}

func (p *exprParser) parseAtom() (ExprNode, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	// Parenthesized sub-expression
	if t.kind == tokenLParen {
		p.advance() // consume (
		node, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokenRParen, ")"); err != nil {
			return nil, err
		}
		return node, nil
	}

	// Operands
	if t.kind == tokenWord {
		return p.parseOperand()
	}

	return nil, fmt.Errorf("unexpected token %q at position %d", t.value, p.pos)
}

func (p *exprParser) parseOperand() (ExprNode, error) {
	t := p.advance()

	switch {
	// header:Name == "value" or header:Name != "value"
	case strings.HasPrefix(t.value, "header:"):
		name := strings.TrimPrefix(t.value, "header:")
		opTok := p.peek()
		if opTok == nil || opTok.kind != tokenOp {
			return nil, fmt.Errorf("expected operator after header:%s", name)
		}
		op := p.advance().value

		valTok := p.peek()
		if valTok == nil || valTok.kind != tokenString {
			return nil, fmt.Errorf("expected quoted string after operator")
		}
		val := p.advance().value
		return &HeaderExpr{Name: name, Op: op, Value: val}, nil

	// path exact|prefix|contains "value"
	case strings.EqualFold(t.value, "path"):
		matchTypeTok := p.peek()
		if matchTypeTok == nil || matchTypeTok.kind != tokenWord {
			return nil, fmt.Errorf("expected match type (exact, prefix, contains) after path")
		}
		matchType := strings.ToLower(p.advance().value)
		if matchType != "exact" && matchType != "prefix" && matchType != "contains" {
			return nil, fmt.Errorf("invalid path match type %q", matchType)
		}
		valTok := p.peek()
		if valTok == nil || valTok.kind != tokenString {
			return nil, fmt.Errorf("expected quoted string for path value")
		}
		val := p.advance().value
		return &PathExpr{MatchType: matchType, Value: val}, nil

	// method == "GET"
	case strings.EqualFold(t.value, "method"):
		opTok := p.peek()
		if opTok == nil || opTok.kind != tokenOp {
			return nil, fmt.Errorf("expected operator after method")
		}
		p.advance() // consume operator
		valTok := p.peek()
		if valTok == nil || valTok.kind != tokenString {
			return nil, fmt.Errorf("expected quoted string for method value")
		}
		val := p.advance().value
		return &MethodExpr{Method: val}, nil

	// query:key == "value"
	case strings.HasPrefix(t.value, "query:"):
		name := strings.TrimPrefix(t.value, "query:")
		opTok := p.peek()
		if opTok == nil || opTok.kind != tokenOp {
			return nil, fmt.Errorf("expected operator after query:%s", name)
		}
		op := p.advance().value
		valTok := p.peek()
		if valTok == nil || valTok.kind != tokenString {
			return nil, fmt.Errorf("expected quoted string after operator")
		}
		val := p.advance().value
		return &QueryParamExpr{Name: name, Op: op, Value: val}, nil

	// source_ip in "10.0.0.0/8"
	case strings.EqualFold(t.value, "source_ip"):
		inTok := p.peek()
		if inTok == nil || inTok.kind != tokenWord || !strings.EqualFold(inTok.value, "in") {
			return nil, fmt.Errorf("expected 'in' after source_ip")
		}
		p.advance() // consume 'in'
		valTok := p.peek()
		if valTok == nil || valTok.kind != tokenString {
			return nil, fmt.Errorf("expected quoted CIDR string after 'in'")
		}
		cidr := p.advance().value
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		return &SourceIPExpr{CIDR: cidr, network: network}, nil

	default:
		return nil, fmt.Errorf("unknown operand %q at position %d", t.value, p.pos-1)
	}
}
