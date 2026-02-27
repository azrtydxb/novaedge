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

// Package health provides active and passive health checking, circuit breaking,
// outlier detection, and resource limit enforcement for upstream endpoints.
package health

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"go.uber.org/zap"
)

var (
	errUnexpectedCharacter                                                     = errors.New("unexpected character")
	errExpectedTokenKind                                                       = errors.New("expected token kind")
	errEmptyExpression                                                         = errors.New("empty expression")
	errUnexpectedTokenAfterExpression                                          = errors.New("unexpected token after expression")
	errExpectedComparisonOperatorGot                                           = errors.New("expected comparison operator, got")
	errNetworkErrorRatioTakesNoArgumentsGot                                    = errors.New("NetworkErrorRatio() takes no arguments, got")
	errResponseCodeRatioRequires4ArgumentsCodeFromCodeToDividendFromDividendTo = errors.New("ResponseCodeRatio() requires 4 arguments (codeFrom, codeTo, dividendFrom, dividendTo), got")
	errLatencyAtQuantileMSRequires1ArgumentQuantileGot                         = errors.New("LatencyAtQuantileMS() requires 1 argument (quantile), got")
	errUnknownFunction                                                         = errors.New("unknown function")
)

// CBExpression evaluates a circuit breaker expression against cluster statistics.
type CBExpression interface {
	Evaluate(stats *ClusterStats) bool
}

// ClusterStats holds aggregated statistics for a cluster of backends.
type ClusterStats struct {
	TotalRequests  int64
	FailedRequests int64
	NetworkErrors  int64
	ResponseCodes  map[int]int64
	LatencyP50     time.Duration
	LatencyP99     time.Duration
}

// NetworkErrorRatio returns the ratio of network errors to total requests.
// Returns 0 if there are no requests.
func (cs *ClusterStats) NetworkErrorRatio() float64 {
	if cs.TotalRequests == 0 {
		return 0
	}
	return float64(cs.NetworkErrors) / float64(cs.TotalRequests)
}

// ResponseCodeRatio returns the ratio of responses in [codeFrom, codeTo) to responses in [dividendFrom, dividendTo).
// Returns 0 if the dividend range has no responses.
func (cs *ClusterStats) ResponseCodeRatio(codeFrom, codeTo, dividendFrom, dividendTo int) float64 {
	var numerator, denominator int64
	for code, count := range cs.ResponseCodes {
		if code >= codeFrom && code < codeTo {
			numerator += count
		}
		if code >= dividendFrom && code < dividendTo {
			denominator += count
		}
	}
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

// LatencyAtQuantileMS returns the latency at the given quantile in milliseconds.
// Supported quantiles: 0.5 (p50) and 0.99 (p99).
// Returns 0 for unsupported quantiles.
func (cs *ClusterStats) LatencyAtQuantileMS(quantile float64) float64 {
	switch {
	case quantile >= 0.49 && quantile <= 0.51:
		return float64(cs.LatencyP50.Milliseconds())
	case quantile >= 0.98 && quantile <= 1.0:
		return float64(cs.LatencyP99.Milliseconds())
	default:
		return 0
	}
}

// ExpressionCircuitBreaker is a circuit breaker that uses a parsed boolean expression
// evaluated against ClusterStats to decide when to trip.
type ExpressionCircuitBreaker struct {
	mu     sync.RWMutex
	logger *zap.Logger

	// Configuration
	Expression       string
	CheckPeriod      time.Duration
	FallbackDuration time.Duration
	RecoveryDuration time.Duration

	// Parsed expression
	expr CBExpression

	// State
	state          CircuitBreakerState
	stateChangedAt time.Time
}

// NewExpressionCircuitBreaker creates a new expression-based circuit breaker.
func NewExpressionCircuitBreaker(expression string, logger *zap.Logger) (*ExpressionCircuitBreaker, error) {
	expr, err := ParseCBExpression(expression)
	if err != nil {
		return nil, fmt.Errorf("parsing circuit breaker expression: %w", err)
	}

	return &ExpressionCircuitBreaker{
		logger:           logger,
		Expression:       expression,
		CheckPeriod:      100 * time.Millisecond,
		FallbackDuration: 10 * time.Second,
		RecoveryDuration: 10 * time.Second,
		expr:             expr,
		state:            StateClosed,
		stateChangedAt:   time.Now(),
	}, nil
}

// EvaluateAndUpdate evaluates the expression against the provided stats and updates
// the circuit breaker state accordingly.
func (ecb *ExpressionCircuitBreaker) EvaluateAndUpdate(stats *ClusterStats) {
	ecb.mu.Lock()
	defer ecb.mu.Unlock()

	now := time.Now()

	switch ecb.state {
	case StateClosed:
		if ecb.expr.Evaluate(stats) {
			ecb.logger.Warn("Expression circuit breaker tripped",
				zap.String("expression", ecb.Expression),
			)
			ecb.state = StateOpen
			ecb.stateChangedAt = now
		}

	case StateOpen:
		if now.Sub(ecb.stateChangedAt) >= ecb.FallbackDuration {
			ecb.logger.Info("Expression circuit breaker entering half-open state",
				zap.String("expression", ecb.Expression),
			)
			ecb.state = StateHalfOpen
			ecb.stateChangedAt = now
		}

	case StateHalfOpen:
		if ecb.expr.Evaluate(stats) {
			ecb.logger.Warn("Expression circuit breaker re-tripped during recovery",
				zap.String("expression", ecb.Expression),
			)
			ecb.state = StateOpen
			ecb.stateChangedAt = now
		} else if now.Sub(ecb.stateChangedAt) >= ecb.RecoveryDuration {
			ecb.logger.Info("Expression circuit breaker recovered, closing",
				zap.String("expression", ecb.Expression),
			)
			ecb.state = StateClosed
			ecb.stateChangedAt = now
		}
	}
}

// GetState returns the current state of the expression circuit breaker.
func (ecb *ExpressionCircuitBreaker) GetState() CircuitBreakerState {
	ecb.mu.RLock()
	defer ecb.mu.RUnlock()
	return ecb.state
}

// IsOpen returns true if the circuit breaker is in the open state.
func (ecb *ExpressionCircuitBreaker) IsOpen() bool {
	return ecb.GetState() == StateOpen
}

// ---- Expression parsing ----

// tokenKind represents the type of a lexer token.
type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenNumber
	tokenLParen
	tokenRParen
	tokenComma
	tokenGT
	tokenLT
	tokenGTE
	tokenLTE
	tokenAnd
	tokenOr
)

// token represents a single lexical token.
type token struct {
	kind  tokenKind
	value string
}

// lexer tokenizes an expression string.
type lexer struct {
	input  string
	pos    int
	tokens []token
}

func newLexer(input string) *lexer {
	return &lexer{input: input}
}

func (l *lexer) tokenize() ([]token, error) {
	for l.pos < len(l.input) {
		ch := rune(l.input[l.pos])

		if unicode.IsSpace(ch) {
			l.pos++
			continue
		}

		switch {
		case ch == '(':
			l.tokens = append(l.tokens, token{kind: tokenLParen, value: "("})
			l.pos++
		case ch == ')':
			l.tokens = append(l.tokens, token{kind: tokenRParen, value: ")"})
			l.pos++
		case ch == ',':
			l.tokens = append(l.tokens, token{kind: tokenComma, value: ","})
			l.pos++
		case ch == '>' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=':
			l.tokens = append(l.tokens, token{kind: tokenGTE, value: ">="})
			l.pos += 2
		case ch == '<' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '=':
			l.tokens = append(l.tokens, token{kind: tokenLTE, value: "<="})
			l.pos += 2
		case ch == '>':
			l.tokens = append(l.tokens, token{kind: tokenGT, value: ">"})
			l.pos++
		case ch == '<':
			l.tokens = append(l.tokens, token{kind: tokenLT, value: "<"})
			l.pos++
		case ch == '&' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '&':
			l.tokens = append(l.tokens, token{kind: tokenAnd, value: "&&"})
			l.pos += 2
		case ch == '|' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '|':
			l.tokens = append(l.tokens, token{kind: tokenOr, value: "||"})
			l.pos += 2
		case unicode.IsLetter(ch):
			start := l.pos
			for l.pos < len(l.input) && (unicode.IsLetter(rune(l.input[l.pos])) || unicode.IsDigit(rune(l.input[l.pos]))) {
				l.pos++
			}
			l.tokens = append(l.tokens, token{kind: tokenIdent, value: l.input[start:l.pos]})
		case unicode.IsDigit(ch) || ch == '.' || ch == '-':
			start := l.pos
			if ch == '-' {
				l.pos++
			}
			for l.pos < len(l.input) && (unicode.IsDigit(rune(l.input[l.pos])) || l.input[l.pos] == '.') {
				l.pos++
			}
			l.tokens = append(l.tokens, token{kind: tokenNumber, value: l.input[start:l.pos]})
		default:
			return nil, fmt.Errorf("%w: %q at position %d", errUnexpectedCharacter, ch, l.pos)
		}
	}

	l.tokens = append(l.tokens, token{kind: tokenEOF})
	return l.tokens, nil
}

// parser builds a CBExpression AST from tokens.
type parser struct {
	tokens []token
	pos    int
}

func newParser(tokens []token) *parser {
	return &parser{tokens: tokens}
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{kind: tokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) next() token {
	t := p.peek()
	if t.kind != tokenEOF {
		p.pos++
	}
	return t
}

func (p *parser) expect(kind tokenKind) (token, error) {
	t := p.next()
	if t.kind != kind {
		return t, fmt.Errorf("%w: %d, got %d (%q)", errExpectedTokenKind, kind, t.kind, t.value)
	}
	return t, nil
}

// ParseCBExpression parses a circuit breaker expression string into a CBExpression.
// Supported syntax:
//
//	NetworkErrorRatio() > 0.3
//	ResponseCodeRatio(500, 600, 0, 600) > 0.25
//	LatencyAtQuantileMS(0.99) > 200
//	expr && expr
//	expr || expr
//	(expr)
func ParseCBExpression(expr string) (CBExpression, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, errEmptyExpression
	}

	lex := newLexer(expr)
	tokens, err := lex.tokenize()
	if err != nil {
		return nil, fmt.Errorf("tokenizing expression: %w", err)
	}

	p := newParser(tokens)
	result, err := p.parseOr()
	if err != nil {
		return nil, fmt.Errorf("parsing expression: %w", err)
	}

	if p.peek().kind != tokenEOF {
		return nil, fmt.Errorf("%w: %q", errUnexpectedTokenAfterExpression, p.peek().value)
	}

	return result, nil
}

// parseOr handles || (lowest precedence)
func (p *parser) parseOr() (CBExpression, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.peek().kind == tokenOr {
		p.next() // consume ||
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &orExpr{left: left, right: right}
	}

	return left, nil
}

// parseAnd handles && (higher precedence than ||)
func (p *parser) parseAnd() (CBExpression, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}

	for p.peek().kind == tokenAnd {
		p.next() // consume &&
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = &andExpr{left: left, right: right}
	}

	return left, nil
}

// parseComparison handles comparison expressions: funcCall op number
func (p *parser) parseComparison() (CBExpression, error) {
	if p.peek().kind == tokenLParen {
		p.next() // consume (
		expr, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokenRParen); err != nil {
			return nil, fmt.Errorf("missing closing parenthesis: %w", err)
		}
		return expr, nil
	}

	// Expect a function call
	fn, err := p.parseFunctionCall()
	if err != nil {
		return nil, err
	}

	// Expect a comparison operator
	op := p.peek()
	switch op.kind {
	case tokenGT, tokenLT, tokenGTE, tokenLTE:
		p.next()
	default:
		return nil, fmt.Errorf("%w: %q", errExpectedComparisonOperatorGot, op.value)
	}

	// Expect a number
	numTok, err := p.expect(tokenNumber)
	if err != nil {
		return nil, fmt.Errorf("expected number after operator: %w", err)
	}

	threshold, err := strconv.ParseFloat(numTok.value, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q: %w", numTok.value, err)
	}

	return &comparisonExpr{
		fn:        fn,
		op:        op.value,
		threshold: threshold,
	}, nil
}

// parseFunctionCall parses a function call like NetworkErrorRatio() or ResponseCodeRatio(500, 600, 0, 600)
func (p *parser) parseFunctionCall() (statsFunc, error) {
	nameTok, err := p.expect(tokenIdent)
	if err != nil {
		return nil, fmt.Errorf("expected function name: %w", err)
	}

	if _, err := p.expect(tokenLParen); err != nil {
		return nil, fmt.Errorf("expected '(' after function name: %w", err)
	}

	// Parse arguments
	var args []float64
	if p.peek().kind != tokenRParen {
		for {
			argTok, argErr := p.expect(tokenNumber)
			if argErr != nil {
				return nil, fmt.Errorf("expected number argument: %w", argErr)
			}
			val, parseErr := strconv.ParseFloat(argTok.value, 64)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid argument %q: %w", argTok.value, parseErr)
			}
			args = append(args, val)

			if p.peek().kind == tokenComma {
				p.next()
			} else {
				break
			}
		}
	}

	if _, err := p.expect(tokenRParen); err != nil {
		return nil, fmt.Errorf("expected ')' after arguments: %w", err)
	}

	return buildStatsFunc(nameTok.value, args)
}

// statsFunc extracts a float64 value from ClusterStats.
type statsFunc func(stats *ClusterStats) float64

// buildStatsFunc creates a statsFunc for the given function name and arguments.
func buildStatsFunc(name string, args []float64) (statsFunc, error) {
	switch name {
	case "NetworkErrorRatio":
		if len(args) != 0 {
			return nil, fmt.Errorf("%w: %d", errNetworkErrorRatioTakesNoArgumentsGot, len(args))
		}
		return func(stats *ClusterStats) float64 {
			return stats.NetworkErrorRatio()
		}, nil

	case "ResponseCodeRatio":
		if len(args) != 4 {
			return nil, fmt.Errorf("%w: %d", errResponseCodeRatioRequires4ArgumentsCodeFromCodeToDividendFromDividendTo, len(args))
		}
		codeFrom := int(args[0])
		codeTo := int(args[1])
		dividendFrom := int(args[2])
		dividendTo := int(args[3])
		return func(stats *ClusterStats) float64 {
			return stats.ResponseCodeRatio(codeFrom, codeTo, dividendFrom, dividendTo)
		}, nil

	case "LatencyAtQuantileMS":
		if len(args) != 1 {
			return nil, fmt.Errorf("%w: %d", errLatencyAtQuantileMSRequires1ArgumentQuantileGot, len(args))
		}
		quantile := args[0]
		return func(stats *ClusterStats) float64 {
			return stats.LatencyAtQuantileMS(quantile)
		}, nil

	default:
		return nil, fmt.Errorf("%w: %q", errUnknownFunction, name)
	}
}

// ---- AST node types ----

// comparisonExpr evaluates a function against a threshold.
type comparisonExpr struct {
	fn        statsFunc
	op        string
	threshold float64
}

func (c *comparisonExpr) Evaluate(stats *ClusterStats) bool {
	val := c.fn(stats)
	switch c.op {
	case ">":
		return val > c.threshold
	case "<":
		return val < c.threshold
	case ">=":
		return val >= c.threshold
	case "<=":
		return val <= c.threshold
	default:
		return false
	}
}

// andExpr evaluates two expressions with logical AND.
type andExpr struct {
	left, right CBExpression
}

func (a *andExpr) Evaluate(stats *ClusterStats) bool {
	return a.left.Evaluate(stats) && a.right.Evaluate(stats)
}

// orExpr evaluates two expressions with logical OR.
type orExpr struct {
	left, right CBExpression
}

func (o *orExpr) Evaluate(stats *ClusterStats) bool {
	return o.left.Evaluate(stats) || o.right.Evaluate(stats)
}
