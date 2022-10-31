package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/tools/go/ast/astutil"
	"io/ioutil"
	"os"
	"strings"
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
		name := obj.Name
		if !strings.HasPrefix(name, "Test") {
			continue
		}
		fixTestFunc(obj, srcBytes)
	}
	buf := bytes.NewBuffer(nil)
	err = format.Node(buf, fset, f)
	if err != nil {
		panic(err)
	}
	newCode, err := format.Source(buf.Bytes())
	if err != nil {
		panic(err)
	}
	// fmt.Printf(string(newCode))
	err = ioutil.WriteFile(path, newCode, 0644)
	if err != nil {
		panic(err)
	}
}

func fixTestFunc(obj *ast.Object, srcBytes []byte) {
	decl := obj.Decl.(*ast.FuncDecl)
	typ := decl.Type
	if typ.Results != nil && len(typ.Results.List) > 0 {
		return
	}
	params := typ.Params.List
	if len(params) != 1 {
		return
	}
	t_name := params[0].Names[0].Name
	body := decl.Body // *ast.BlockStmt
	fixBlockStatement(body, t_name, srcBytes)
	// TODO:
	doc := decl.Doc // *ast.CommentGroup, associated documentation; or nil
	if doc != nil {
		for _, comment := range doc.List {
			fmt.Printf("Comment: %v\n", comment)
		}
	}
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
		case *ast.RangeStmt:
			fixBlockStatement(stmtTyped.Body, t_name, srcBytes)
		case *ast.IfStmt:
			fixBlockStatement(stmtTyped.Body, t_name, srcBytes)
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
							fmt.Printf("--- unexpected stmt: %v\n", stmt)
						}
						continue
					default:
						fmt.Printf("--- Func name: %v\n", xName)
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
						fmt.Printf("--- unexpected stmt: %v\n", stmt)
						continue
					}
					fmt.Printf("--- unexpected stmt: %v\n", stmt)
					continue
				default:
					fmt.Printf("--- x is %T\n", x)
				}
			}

		default:
			fmt.Printf("stmtIn: %T, stmt: %v\n", stmtIn, stmt)

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

func newCallExpr(x ast.Expr, method string, args []ast.Expr) *ast.CallExpr {
	fun := &ast.SelectorExpr{
		X: x,
		Sel: &ast.Ident{
			Name: method,
		},
	}
	// fmt.Printf("newCallExpr: X=%T, Sel=%T", fun.X, fun.Sel)
	return &ast.CallExpr{
		Fun:  fun,
		Args: args,
		//Lparen
		//Ellipsis
		//Rparen
	}
}

func msgCallExpr(msg []ast.Expr) *ast.CallExpr {
	return newCallExpr(&ast.Ident{Name: "is"}, "Msg", msg)
}

func newIsCallExpr(t_name string) *ast.CallExpr {
	return newCallExpr(
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
		//TokPos token.Pos   // position of Tok
		Tok: token.DEFINE,
		Rhs: []ast.Expr{
			newIsCallExpr(t_name),
		},
	}
}

func isCallExpr(method string, args []ast.Expr, msg []ast.Expr) *ast.CallExpr {
	var x ast.Expr = &ast.Ident{
		Name: "is",
	}
	if len(msg) > 0 {
		x = msgCallExpr(msg)
	}
	return newCallExpr(x, method, args)
}

func convertFuncCallLow(funcName string, args []ast.Expr) *ast.CallExpr {
	switch funcName {
	case "Equal", "ObjectsAreEqual", "ObjectsAreEqualValues":
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
	case "Nil":
		if len(args) > 1 {
			return isCallExpr("Nil", args[:1], args[1:])
		} else {
			return isCallExpr("Nil", args, nil)
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
		//case "FailNow":
		//case "Fail":
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
		firstArg := args[0].(*ast.Ident)
		// firstArg.Obj is nil
		if firstArg.Name == t_name {
			args = args[1:]
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
