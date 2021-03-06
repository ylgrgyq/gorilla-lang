package parser

import (
	"ast"
	"fmt"
	"lexer"
	"strconv"
	"token"
)

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

type ParserError struct {
	errorToken token.Token
	msg        string
	pos        token.Position
}

func (p ParserError) Error() string {
	return fmt.Sprintf("%s at line: %d, column: %d", p.msg, p.pos.Line, p.pos.Column)
}

type Parser struct {
	lex         *lexer.Lexer
	initialized bool

	tracing     bool
	traceIndent int

	currentToken token.Token
	peekToken    token.Token

	prefixFns map[token.TokenType]prefixParseFn
	infixFns  map[token.TokenType]infixParseFn
}

func New(input string) *Parser {
	handler := func(pos token.Position, msg string) {
		panic(fmt.Sprintf("%s at line: %d, column: %d", msg, pos.Line, pos.Column))
	}
	p := Parser{lex: lexer.New(input, handler),
		prefixFns: make(map[token.TokenType]prefixParseFn),
		infixFns:  make(map[token.TokenType]infixParseFn)}

	p.registerPrefixParseFn(token.IDENT, p.parseIdentifier)
	p.registerPrefixParseFn(token.INT, p.parseInteger)
	p.registerPrefixParseFn(token.TRUE, p.parseBoolean)
	p.registerPrefixParseFn(token.FALSE, p.parseBoolean)
	p.registerPrefixParseFn(token.STRING, p.parseString)
	p.registerPrefixParseFn(token.IF, p.parseIfExpression)
	p.registerPrefixParseFn(token.FUNCTION, p.parseFunction)
	p.registerPrefixParseFn(token.LBRACKET, p.parseArrayLiteral)

	p.registerPrefixParseFn(token.LBRACE, p.parseHashLiteral)
	p.registerPrefixParseFn(token.LPAREN, p.parseGroupedExpression)

	p.registerInfixParseFn(token.LPAREN, p.parseCallExpression)
	p.registerInfixParseFn(token.LBRACKET, p.parseIndexExpression)

	for _, tk := range token.GetPrefixOperators() {
		p.registerPrefixParseFn(tk, p.parsePrefix)
	}

	for _, tk := range token.GetInfixOperators() {
		p.registerInfixParseFn(tk, p.parseInfix)
	}

	for _, tk := range token.GetPostfixOperators() {
		p.registerInfixParseFn(tk, p.parsePostfix)
	}

	return &p
}

func (p *Parser) registerPrefixParseFn(tokenType token.TokenType, prefixFn prefixParseFn) {
	p.prefixFns[tokenType] = prefixFn
}

func (p *Parser) registerInfixParseFn(tokenType token.TokenType, infixFn infixParseFn) {
	p.infixFns[tokenType] = infixFn
}

func (p *Parser) nextToken() token.Token {
	p.currentToken = p.peekToken
	p.peekToken = p.lex.NextToken()
	return p.currentToken
}

func (p *Parser) peekTokenPrecedence() int {
	return p.peekToken.Precedence()
}

func (p *Parser) currentTokenPrecedence() int {
	return p.currentToken.Precedence()
}

func (p *Parser) printTrace(a ...interface{}) {
	const dots = ". . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . "
	const n = len(dots)
	pos := p.currentToken.Pos
	fmt.Printf("%5d:%3d: ", pos.Line, pos.Column)
	i := 2 * p.traceIndent
	for i > n {
		fmt.Print(dots)
		i -= n
	}
	// i <= n
	fmt.Print(dots[0:i])
	fmt.Println(a...)
}

func trace(p *Parser, msg string) *Parser {
	p.printTrace(msg, "(")
	p.traceIndent++
	return p
}

func un(p *Parser) {
	p.traceIndent--
	p.printTrace(")")
}

func (p *Parser) parseStatement() ast.Statement {
	var statement ast.Statement

	switch p.currentToken.Type {
	case token.LET:
		statement = p.parseLetStatement()
	case token.RETURN:
		statement = p.parseReturnStatement()
	default:
		statement = p.parseExpressionStatement()
	}

	return statement
}

func (p *Parser) parseLetStatement() *ast.LetStatement {
	if p.tracing {
		defer un(trace(p, "LetStatement"))
	}

	letStatement := &ast.LetStatement{Token: p.currentToken}

	p.assertNextTokenType(token.IDENT)

	letStatement.Name = &ast.Identifier{Token: p.currentToken, Value: p.currentToken.Literal}

	p.assertNextTokenType(token.ASSIGN)

	// skip ASSIGN token and point currentToken to the start of the next expression
	p.nextToken()
	express := p.parseExpression(token.LOWEST_PRECEDENCE)

	letStatement.Value = express

	if fe, ok := express.(*ast.FunctionExpression); ok {
		fe.Name = letStatement.Name
	}

	if p.peekTokenTypeIs(token.SEMICOLON) {
		p.nextToken()
	}

	return letStatement
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	if p.tracing {
		defer un(trace(p, "ReturnStatement"))
	}

	retStatement := &ast.ReturnStatement{Token: p.currentToken}

	if !p.nextTokenTypeIs(token.SEMICOLON) {
		p.nextToken()

		express := p.parseExpression(token.LOWEST_PRECEDENCE)

		retStatement.Value = express
	}

	if p.peekTokenTypeIs(token.SEMICOLON) {
		p.nextToken()
	}

	return retStatement
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	if p.tracing {
		defer un(trace(p, "ExpressionStatement"))
	}

	express := &ast.ExpressionStatement{Token: p.currentToken}

	value := p.parseExpression(token.LOWEST_PRECEDENCE)

	express.Value = value

	if p.peekTokenTypeIs(token.SEMICOLON) {
		p.nextToken()
	}

	return express
}

/*
解析任意一个 expression 时都保证一个不变量，解析开始时 current token 指向 expression 开头，结束时 current token 指向 expression 末尾。
比如 if 表达式开始解析的时候要保证 current token 指向 if，结束时保证 current token 指向 }
*/
func (p *Parser) parseExpression(precedence int) ast.Expression {
	if p.tracing {
		defer un(trace(p, "Expression"))
	}

	prefixFn := p.prefixFns[p.currentToken.Type]
	if prefixFn == nil {
		panic(ParserError{msg: fmt.Sprintf("can not parse token type %q", p.currentToken.Type), errorToken: p.currentToken})
	}

	left := prefixFn()

	// 最难的是这个 for 循环。当执行到这里时候 p.currentToken 指向一个 expression，peekToken 指向空或者下一个 operator
	// precedence 和 p.peekTokenPrecedence 比较实际比较的是 p.currentToken 指向的这个 expression 左右两边的 operator 优先级
	// 拿 A + B + C 来举例，当 p.currentToken 指向 B 时候，比较的就是 B 两侧 + 的优先级，当左边更高时就返回 left，并保持 p.peekToken 依然
	// 指向 B 右边的 + ，从而能在 B 作为 A + B 的 Right 结合为 Expression 整体时候，在上一层的 for 中继续从 p.peekToken 指向 B 右侧的 + 开始解析。
	// 拿 A + B * C 来举例，当 B 右侧优先级更高时，保持 B 去作为 left 和 B 后面的 Expression 结合
	for !p.peekTokenTypeIs(token.SEMICOLON) && precedence < p.peekTokenPrecedence() {
		infixFn := p.infixFns[p.peekToken.Type]
		if infixFn == nil {
			break
		}

		p.nextToken()

		left = infixFn(left)
	}

	return left
}

func (p *Parser) nextTokenTypeIs(expect token.TokenType) bool {
	return p.peekToken.Type == expect
}

func (p *Parser) currentTokenTypeIs(expect token.TokenType) bool {
	return p.currentToken.Type == expect
}

func (p *Parser) assertNextTokenType(expect token.TokenType) {
	p.assertTokenType(expect, p.peekToken)
}

func (p *Parser) assertCurrentTokenType(expect token.TokenType) {
	p.assertTokenType(expect, p.currentToken)
}

func (p *Parser) assertTokenType(expect token.TokenType, actual token.Token) {
	if actual.Type != expect {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", expect, actual.Type), errorToken: actual})
	} else {
		p.nextToken()
	}
}

func (p *Parser) peekTokenTypeIs(expectType token.TokenType) bool {
	return p.peekToken.Type == expectType
}

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.currentToken, Value: p.currentToken.Literal}
}

func (p *Parser) parseInteger() ast.Expression {
	value, err := strconv.ParseInt(p.currentToken.Literal, 0, 64)
	if err != nil {
		panic(ParserError{msg: fmt.Sprintf("could not parse %q as intger", p.currentToken.Literal), errorToken: p.currentToken})
	}

	return &ast.Integer{Token: p.currentToken, Value: value}
}

func (p *Parser) parseBoolean() ast.Expression {
	var value bool
	if p.currentToken.Type == token.TRUE {
		value = true
	} else if p.currentToken.Type == token.FALSE {
		value = false
	} else {
		panic(ParserError{msg: fmt.Sprintf("could not parse %q as boolean", p.currentToken.Literal), errorToken: p.currentToken})
	}

	return &ast.Boolean{Token: p.currentToken, Value: value}
}

func (p *Parser) parseString() ast.Expression {
	return &ast.String{Token: p.currentToken, Value: p.currentToken.Literal}
}

func (p *Parser) parsePrefix() ast.Expression {
	if p.tracing {
		defer un(trace(p, "Prefix"))
	}

	prefix := &ast.PrefixExpression{Token: p.currentToken, Operator: p.currentToken.Literal}

	precedence := p.currentTokenPrecedence()

	p.nextToken()

	value := p.parseExpression(precedence)

	prefix.Value = value

	return prefix
}

// when called p.currentToken must point to a infix operator
func (p *Parser) parseInfix(left ast.Expression) ast.Expression {
	if p.tracing {
		defer un(trace(p, "Infix"))
	}

	infix := &ast.InfixExpression{Token: p.currentToken, Left: left, Operator: p.currentToken.Literal}

	precedence := p.currentTokenPrecedence()
	p.nextToken()

	right := p.parseExpression(precedence)

	infix.Right = right

	return infix
}

func (p *Parser) parsePostfix(left ast.Expression) ast.Expression {
	if p.tracing {
		defer un(trace(p, "Postfix"))
	}

	return &ast.PostfixExpression{Token: p.currentToken, Left: left, Operator: p.currentToken.Literal}
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	if p.tracing {
		defer un(trace(p, "GroupedExpression"))
	}

	p.assertCurrentTokenType(token.LPAREN)

	expression := p.parseExpression(token.LOWEST_PRECEDENCE)

	p.assertNextTokenType(token.RPAREN)

	return expression
}

func (p *Parser) parseIfExpression() ast.Expression {
	if p.tracing {
		defer un(trace(p, "IfExpression"))
	}

	ifExpress := &ast.IfExpression{Token: p.currentToken}

	if !p.currentTokenTypeIs(token.IF) {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", token.IF, p.currentToken.Type), errorToken: p.currentToken})
	}

	p.assertNextTokenType(token.LPAREN)

	condition := p.parseExpression(token.LOWEST_PRECEDENCE)

	ifExpress.Condition = condition

	p.assertCurrentTokenType(token.RPAREN)

	body := p.parseBlockExpression()

	ifExpress.ThenBody = body
	if p.peekTokenTypeIs(token.ELSE) {
		p.nextToken()

		p.assertNextTokenType(token.LBRACE)

		body := p.parseBlockExpression()

		ifExpress.ElseBody = body
	}

	return ifExpress
}

func (p *Parser) parseBlockExpression() *ast.BlockExpression {
	if p.tracing {
		defer un(trace(p, "BlockExpression"))
	}

	block := &ast.BlockExpression{Token: p.currentToken}

	p.assertCurrentTokenType(token.LBRACE)

	for !p.currentTokenTypeIs(token.RBRACE) && !p.currentTokenTypeIs(token.EOF) {
		if statement := p.parseStatement(); statement != nil {
			block.Statements = append(block.Statements, statement)
		}

		p.nextToken()
	}

	if !p.currentTokenTypeIs(token.RBRACE) {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", token.RBRACE, p.currentToken.Type), errorToken: p.currentToken})
	}

	return block
}

func (p *Parser) parseFunction() ast.Expression {
	if p.tracing {
		defer un(trace(p, "Function"))
	}

	function := &ast.FunctionExpression{Token: p.currentToken}

	if p.peekTokenTypeIs(token.IDENT) {
		p.nextToken()
		ident := p.parseIdentifier()

		function.Name = ident.(*ast.Identifier)
	}

	p.assertNextTokenType(token.LPAREN)

	function.Parameters = p.parseParameters()

	p.assertNextTokenType(token.LBRACE)

	function.Body = p.parseBlockExpression()
	return function
}

func (p *Parser) parseArrayLiteral() ast.Expression {
	if p.tracing {
		defer un(trace(p, "ArrayLiteral"))
	}

	array := &ast.ArrayLiteral{Token: p.currentToken}

	p.assertCurrentTokenType(token.LBRACKET)
	if p.peekTokenTypeIs(token.RBRACKET) {
		p.nextToken()
		return array
	}

	for p.currentToken.Type != token.RBRACKET && p.currentToken.Type != token.EOF {
		array.Elements = append(array.Elements, p.parseExpression(token.LOWEST_PRECEDENCE))
		p.nextToken()
		if p.currentTokenTypeIs(token.COMMA) {
			p.nextToken()
		}
	}

	if p.currentToken.Type != token.RBRACKET {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", token.RBRACKET, p.currentToken.Type), errorToken: p.currentToken})
	}
	return array
}

func (p *Parser) parseParameters() []*ast.Identifier {
	if p.tracing {
		defer un(trace(p, "Parameters"))
	}

	p.nextToken()
	params := []*ast.Identifier{}

	for !p.currentTokenTypeIs(token.RPAREN) && !p.currentTokenTypeIs(token.EOF) {
		params = append(params, &ast.Identifier{Token: p.currentToken, Value: p.currentToken.Literal})
		p.nextToken()
		if p.currentToken.Type == token.COMMA {
			p.nextToken()
		}
	}

	if !p.currentTokenTypeIs(token.RPAREN) {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", token.RPAREN, p.currentToken.Type), errorToken: p.currentToken})
	}

	return params
}

func (p *Parser) parseHashLiteral() ast.Expression {
	if p.tracing {
		defer un(trace(p, "HashLiteral"))
	}

	hash := &ast.HashLiteral{Token: p.currentToken, Pair: make(map[ast.Expression]ast.Expression)}

	p.assertCurrentTokenType(token.LBRACE)
	if p.peekTokenTypeIs(token.RBRACE) {
		p.nextToken()
		return hash
	}

	for p.currentToken.Type != token.RBRACE && p.currentToken.Type != token.EOF {
		k := p.parseExpression(token.LOWEST_PRECEDENCE)
		p.assertNextTokenType(token.COLON)
		p.nextToken()
		v := p.parseExpression(token.LOWEST_PRECEDENCE)

		hash.Pair[k] = v
		p.nextToken()
		if p.currentTokenTypeIs(token.COMMA) {
			p.nextToken()
		}
	}

	if p.currentToken.Type != token.RBRACE {
		panic(ParserError{msg: fmt.Sprintf("expectd token type is %q, got %q", token.RBRACE, p.currentToken.Type), errorToken: p.currentToken})
	}
	return hash
}

func (p *Parser) parseCallExpression(fun ast.Expression) ast.Expression {
	if p.tracing {
		defer un(trace(p, "CallExpression"))
	}

	call := &ast.CallExpression{Token: p.currentToken, Function: fun}

	p.assertCurrentTokenType(token.LPAREN)

	args := []ast.Expression{}
	for p.currentToken.Type != token.RPAREN {
		if p.currentToken.Type != token.COMMA {
			arg := p.parseExpression(token.LOWEST_PRECEDENCE)

			args = append(args, arg)
		}
		p.nextToken()
	}
	call.Arguments = args
	return call
}

func (p *Parser) parseIndexExpression(ex ast.Expression) ast.Expression {
	if p.tracing {
		defer un(trace(p, "IndexExpression"))
	}

	indexEx := &ast.InfixExpression{Token: p.currentToken, Left: ex, Operator: p.currentToken.Literal}

	p.assertCurrentTokenType(token.LBRACKET)

	index := p.parseExpression(token.LOWEST_PRECEDENCE)
	indexEx.Right = index

	p.assertNextTokenType(token.RBRACKET)
	return indexEx
}

func (p *Parser) ParseProgram() (program *ast.Program, err error) {
	if p.tracing {
		defer un(trace(p, "Program"))
	}

	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
			program = nil
		}
	}()

	if !p.initialized {
		p.nextToken()
		p.nextToken()
		p.initialized = true
	}

	program = &ast.Program{}

	for !p.currentTokenTypeIs(token.EOF) {
		if statement := p.parseStatement(); statement != nil {
			program.Statements = append(program.Statements, statement)
		}

		p.nextToken()
	}

	return program, nil
}
