package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"os"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

/*

migrate to github.com/ilius/is/v2

https://github.com/stretchr/testify/network/dependents


known problems:
1- functions that are clusure or variable or values of maps or slices are not fixed

*/

func main() {
	for _, fpath := range os.Args[1:] {
		fixGoFile(fpath)
	}
}

func fixGoFile(path string) {
	srcBytes, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	src := string(srcBytes)
	fset := token.NewFileSet() // positions are relative to fset
	f, err := parser.ParseFile(
		fset,
		path,
		src,
		parser.ParseComments|parser.AllErrors,
	)
	if err != nil {
		panic(err)
	}

	for _, imp := range f.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(impPath, "github.com/stretchr/") {
			astutil.DeleteNamedImport(fset, f, "", impPath)
		}
	}
	astutil.AddNamedImport(fset, f, "", "github.com/ilius/is/v2")

	for _, obj := range f.Scope.Objects {
		if obj.Kind != ast.Fun {
			continue
		}
		fixTestFunc(obj, srcBytes)
	}

	printConfig := &printer.Config{
		Mode:     printer.TabIndent | printer.UseSpaces,
		Tabwidth: 4,
	}

	var buf bytes.Buffer
	err = printConfig.Fprint(&buf, fset, f)
	if err != nil {
		panic(err)
	}

	newCode, err := format.Source(buf.Bytes())
	if err != nil {
		panic(err)
	}

	err = ioutil.WriteFile(path, newCode, 0o644)
	if err != nil {
		panic(err)
	}
}

func fixTestFunc(obj *ast.Object, srcBytes []byte) {
	decl := obj.Decl.(*ast.FuncDecl)
	typ := decl.Type
	params := typ.Params.List
	t_name := ""
	for _, param := range params {
		if len(param.Names) < 1 {
			continue
		}
		paramType, ok := param.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		x, ok := paramType.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		xx, ok := x.X.(*ast.Ident)
		if !ok {
			continue
		}
		if xx.Name != "testing" {
			continue
		}
		if x.Sel.Name != "T" {
			continue
		}
		t_name = param.Names[0].Name
	}
	if t_name == "" {
		return
	}
	body := decl.Body // *ast.BlockStmt
	fixBlockStatement(body, t_name, srcBytes)
}

func fixBlockStatement(body *ast.BlockStmt, t_name string, srcBytes []byte) {
	hasIsNew := false
	convertedCount := 0
	for i, stmtIn := range body.List {
		// type of stmtIn is ats.Stmt interface
		// underlying struct is either *ast.AssignStmt or *ast.ExprStmt
		pos := stmtIn.Pos()
		end := stmtIn.End()
		stmt := string(srcBytes[pos-1 : end])
		if strings.HasPrefix(stmt, "is := is.New") {
			hasIsNew = true
			continue
		}

		switch stmtTyped := stmtIn.(type) {
		case *ast.AssignStmt:
			break
		case *ast.DeclStmt:
			break
		case *ast.ReturnStmt:
			break
		case *ast.DeferStmt:
			break
		case *ast.RangeStmt:
			fixBlockStatement(stmtTyped.Body, t_name, srcBytes)
		case *ast.IfStmt:
			fixBlockStatement(stmtTyped.Body, t_name, srcBytes)
		case *ast.BlockStmt:
			fixBlockStatement(stmtTyped, t_name, srcBytes)
		case (*ast.ExprStmt):
			callExpr, ok := stmtTyped.X.(*ast.CallExpr)
			if !ok {
				fmt.Printf("ERROR: unknown stmtTyped.X type %T\n", stmtTyped.X)
				continue
			}
			funcSel, ok := callExpr.Fun.(*ast.SelectorExpr)
			if ok {
				x := funcSel.X
				switch xt := x.(type) {
				case *ast.Ident:
					xName := xt.Name
					switch xName {
					case "is":
						continue
					case "require", "assert":
						newFc := convertFuncCall(callExpr, t_name)
						if newFc != nil {
							body.List[i] = &ast.ExprStmt{newFc}
							convertedCount++
						} else {
							fmt.Printf("--- (1) unexpected stmt: %v\n", stmt)
						}
						continue
						// default:
						//	fmt.Printf("--- Func name: %v\n", xName)
					}
				case *ast.CallExpr:
					xFunSel, ok := callExpr.Fun.(*ast.SelectorExpr)
					if ok {
						callExpr2, ok := xFunSel.X.(*ast.CallExpr)
						if ok {
							callExpr2FunSel, ok := callExpr2.Fun.(*ast.SelectorExpr)
							if ok {
								xIdent2, ok := callExpr2FunSel.X.(*ast.Ident)
								if ok {
									if xIdent2.Name == "is" {
										continue
									}
									fmt.Printf("--- xIdent2.Name == %v\n", xIdent2.Name)
								}
							}
						}
						fmt.Printf("--- (2) unexpected stmt: %v\n", stmt)
						continue
					}
					fmt.Printf("--- (3) unexpected stmt: %v\n", stmt)
					continue
				case *ast.SelectorExpr:
					// FIXME
				default:
					fmt.Printf("--- x is %T\n", x)
				}
			}

		default:
			fmt.Printf("fixBlockStatement: unknown statement type %T, statement: %v\n", stmtIn, stmt)

		}

		// fmt.Printf("callExpr.Fun: %T\n", callExpr.Fun)
		//for _, arg := range callExpr.Args {
		//	fmt.Printf("arg: %T, stmt: %#v\n", arg, stmt)
		//}
		//newFc := convertFuncCall(callExpr, t_name)
		//if newFc != nil {
		//	body.List[i] = &ast.ExprStmt{newFc}
		//	convertedCount ++
		//	continue
		//}

	}
	if convertedCount > 0 && !hasIsNew {
		body.List = append([]ast.Stmt{
			newIsStatement(t_name),
		}, body.List...)
	}
}

func parseSelectorExpr(selectorStr string) *ast.SelectorExpr {
	exprIn, err := parser.ParseExpr(selectorStr)
	if err != nil {
		panic(err)
	}
	expr := exprIn.(*ast.SelectorExpr)
	return expr
}

func newMethodCallExpr(x ast.Expr, method string, args []ast.Expr) *ast.CallExpr {
	fun := &ast.SelectorExpr{
		X: x,
		Sel: &ast.Ident{
			Name: method,
		},
	}
	return &ast.CallExpr{
		Fun:  fun,
		Args: args,
		// Lparen
		// Ellipsis
		// Rparen
	}
}

func newFuncCallExpr(name string, args []ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{
		Fun: &ast.Ident{
			Name: name,
		},
		Args: args,
		// Lparen
		// Ellipsis
		// Rparen
	}
}

func msgCallExpr(args []ast.Expr, addMsg bool) *ast.CallExpr {
	method := "Msg"
	if addMsg {
		method = "AddMsg"
	}
	// make sure args[0] is string literal
	firstLit, ok := args[0].(*ast.BasicLit)
	if ok && firstLit.Kind == token.STRING {
		return newMethodCallExpr(&ast.Ident{Name: "is"}, method, args)
	}
	// otherwide, use fmt.Sprint(...)
	sprintExpr := newMethodCallExpr(
		&ast.Ident{Name: "fmt"},
		"Sprint",
		args,
	)
	return newMethodCallExpr(&ast.Ident{Name: "is"}, method, []ast.Expr{sprintExpr})
}

func newIsCallExpr(t_name string) *ast.CallExpr {
	return newMethodCallExpr(
		&ast.Ident{Name: "is"},
		"New",
		[]ast.Expr{
			&ast.Ident{Name: t_name},
		},
	)
}

func newIsStatement(t_name string) ast.Stmt {
	return &ast.AssignStmt{
		Lhs: []ast.Expr{
			&ast.Ident{Name: "is"},
		},
		// TokPos token.Pos   // position of Tok
		Tok: token.DEFINE,
		Rhs: []ast.Expr{
			newIsCallExpr(t_name),
		},
	}
}

func isCallExpr(method string, args []ast.Expr, msg []ast.Expr) *ast.CallExpr {
	var x ast.Expr = &ast.Ident{Name: "is"}
	if len(msg) > 0 {
		x = msgCallExpr(msg, true)
	}
	return newMethodCallExpr(x, method, args)
}

func isLenCallExpr(args []ast.Expr) *ast.CallExpr {
	seq := args[0]
	lenVal := args[1]
	lenCallExpr := newFuncCallExpr("len", []ast.Expr{seq})
	var x ast.Expr = &ast.Ident{Name: "is"}
	if len(args) > 2 {
		x = msgCallExpr(args[2:], true)
	}
	return newMethodCallExpr(x, "Equal", []ast.Expr{lenCallExpr, lenVal})
}

func isEmptyCallExpr(args []ast.Expr) *ast.CallExpr {
	seq := args[0]
	lenVal := &ast.BasicLit{
		Kind:  token.INT,
		Value: "1",
	}
	lenCallExpr := newFuncCallExpr("len", []ast.Expr{seq})
	var x ast.Expr = &ast.Ident{Name: "is"}
	if len(args) > 1 {
		x = msgCallExpr(args[1:], true)
	}
	return newMethodCallExpr(x, "Equal", []ast.Expr{lenCallExpr, lenVal})
}

func isGreaterOrEqualExpr(args []ast.Expr) *ast.CallExpr {
	a := args[0]
	b := args[1]
	var x ast.Expr = &ast.Ident{Name: "is"}
	if len(args) > 2 {
		x = msgCallExpr(args[2:], true)
	}
	x = newMethodCallExpr(
		x,
		"AddMsg",
		[]ast.Expr{
			&ast.BasicLit{
				Kind:  token.STRING,
				Value: "\"expected %v >= %v\"",
			},
			a, b,
		},
	)
	return newMethodCallExpr(x, "True", []ast.Expr{
		&ast.BinaryExpr{
			X:  a,
			Op: token.GEQ,
			Y:  b,
		},
	})
}

func convertFuncCallLow(funcName string, args []ast.Expr) *ast.CallExpr {
	switch funcName {
	case "Equal", "ObjectsAreEqual", "ObjectsAreEqualValues", "EqualValues":
		if len(args) > 2 {
			return isCallExpr("Equal", args[:2], args[2:])
		} else {
			return isCallExpr("Equal", args, nil)
		}
	case "EqualError":
		if len(args) > 2 {
			return isCallExpr("ErrMsg", args[:2], args[2:])
		} else {
			return isCallExpr("ErrMsg", args, nil)
		}
	case "NotEqual":
		if len(args) > 2 {
			return isCallExpr("NotEqual", args[:2], args[2:])
		} else {
			return isCallExpr("Equal", args, nil)
		}
	case "Nil":
		if len(args) > 1 {
			return isCallExpr("Nil", args[:1], args[1:])
		} else {
			return isCallExpr("Nil", args, nil)
		}
	case "NotNil":
		if len(args) > 1 {
			return isCallExpr("NotNil", args[:1], args[1:])
		} else {
			return isCallExpr("NotNil", args, nil)
		}
	case "False":
		if len(args) > 1 {
			return isCallExpr("False", args[:1], args[1:])
		} else {
			return isCallExpr("False", args, nil)
		}
	case "True":
		if len(args) > 1 {
			return isCallExpr("True", args[:1], args[1:])
		} else {
			return isCallExpr("True", args, nil)
		}
	case "Error":
		if len(args) > 1 {
			return isCallExpr("Err", args[:1], args[1:])
		} else {
			return isCallExpr("Err", args, nil)
		}
	case "NoError":
		if len(args) > 1 {
			return isCallExpr("NotErr", args[:1], args[1:])
		} else {
			return isCallExpr("NotErr", args, nil)
		}
	case "Contains":
		if len(args) > 2 {
			return isCallExpr("Contains", args[:2], args[2:])
		} else {
			return isCallExpr("Contains", args, nil)
		}
	case "IsType":
		if len(args) > 2 {
			return isCallExpr("EqualType", args[:2], args[2:])
		} else {
			return isCallExpr("EqualType", args, nil)
		}
	case "Len":
		return isLenCallExpr(args)
	case "Panics":
		// in github.com/ben-turner/gin
	case "NotPanics":
		// in github.com/ben-turner/gin
	case "Empty":
		return isEmptyCallExpr(args)
	case "FailNow":
	case "Fail":
	case "GreaterOrEqual":
		return isGreaterOrEqualExpr(args)
	case "Regexp":
		// in github.com/ben-turner/gin
	case "Exactly":
		// in github.com/ben-turner/gin
	default:
		fmt.Printf("--- convertFuncCallLow: unsupported function %#v\n", funcName)
	}
	return nil
}

func convertFuncCall(fc *ast.CallExpr, t_name string) *ast.CallExpr {
	fun := fc.Fun.(*ast.SelectorExpr)
	funcName := fun.Sel.Name
	args := fc.Args
	if len(args) < 2 {
		return nil
	}
	{
		// args[0] can also be ast.SelectorExpr
		firstArg, ok := args[0].(*ast.Ident)
		if ok {
			// firstArg.Obj is nil
			if firstArg.Name == t_name {
				args = args[1:]
			}
		}
	}
	/*for _, arg := range args {
		fmt.Printf("\t\t\targ: %T\n", arg)
		switch argt := arg.(type) {
		case *ast.TypeAssertExpr:
			fmt.Printf("\t\t\t\tX=%v, Type=%v\n", argt.X, argt.Type)
		}
	}*/
	fcNew := convertFuncCallLow(funcName, args)
	return fcNew
}
