// common-iface is a small utility that prints out an interface type representing
// the common interface of the types passed on the command line.
//
// For example, suppose you want to parse some data from either memory
// (contained in a bytes.Reader) or a buffered network connection (bufio.Reader).
// You want to know what methods will be available, so you run:
//
//		$ common-iface bytes.Reader bufio.Reader
//		interface {
//			Read(b []byte) (n int, err error)
//			ReadByte() (byte, error)
//			ReadRune() (ch rune, size int, err error)
//			UnreadByte() error
//			UnreadRune() error
//			WriteTo(w io.Writer) (n int64, err error)
//		}
//
// Or if you want to be able to handle both files and network connections:
//
//		$ common-iface os.File net.Conn
//		interface {
//			Close() error
//			Read(b []byte) (n int, err error)
//			Write(b []byte) (n int, err error)
//		}
//
// This can then e.g. be copy-pasted into a source file to define a local
// interface type (and optionally trimmed down to remove unused methods).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"log"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/go/loader"
)

type argType struct {
	pkg string
	typ string
}

var comments = flag.Bool("comments", false, "Include doc comments from first type")
var header = flag.Bool("header", false, "Include header comment about implementing types")
var private = flag.Bool("private", false, "Include private methods")

func main() {
	flag.Usage = usage

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Figure out what package and type names were passed on the command line.
	argTypes := []argType{}
	conf := &loader.Config{}
	if *comments {
		conf.ParserMode |= parser.ParseComments
	}
	for _, arg := range flag.Args() {
		idx := strings.LastIndexByte(arg, '.')
		if idx < 1 {
			log.Fatalf("Expected [pkg].[type], not %q", arg)
		}
		pkg, typ := arg[:idx], arg[idx+1:]
		argTypes = append(argTypes, argType{pkg, typ})

		// Add to packages to load.
		conf.Import(pkg)
	}

	// Load all relevant packages.
	prog, err := conf.Load()
	if err != nil {
		log.Fatalf("Error loading packages: %v", err)
	}

	// Get a list of relevant types.
	typs := []types.Type{}
	for _, argType := range argTypes {
		pkg := prog.Imported[argType.pkg]
		obj := pkg.Pkg.Scope().Lookup(argType.typ)
		if obj == nil {
			log.Fatalf("Lookup of %q failed", argType.pkg+"."+argType.typ)
		}
		typ, ok := obj.Type().(*types.Named)
		if !ok {
			log.Fatalf("%q is not a declared type, it's a %q", obj, obj.Type())
		}
		typs = append(typs, typ)
	}

	// Get the common methods shared by all specified types.
	var common map[string]fn
	for ti, t := range typs {
		// Wrap the type in a pointer if it might enlarge the method set.
		if _, isPtr := t.(*types.Pointer); !isPtr && !types.IsInterface(t) {
			t = types.NewPointer(t)
			typs[ti] = t
		}

		// Construct a map from name to method signature.
		ms := types.NewMethodSet(t)
		sigs := map[string]fn{}
		for i, n := 0, ms.Len(); i < n; i++ {
			method := ms.At(i)
			obj := method.Obj()
			name := obj.Name()
			if *private || ast.IsExported(name) {
				sigs[name] = fn{method.Type().(*types.Signature), obj}
			}
		}

		if common == nil {
			// The first type provides the initial set of methods.
			common = sigs
		} else {
			// Remove all methods not implemented by the later type.
			for k, v := range common {
				if s, ok := sigs[k]; !ok || !types.Identical(v.Signature, s.Signature) {
					delete(common, k)
				}
			}
		}
	}

	// Create a sorted list of method names.
	names := make([]string, 0, len(common))
	for name := range common {
		names = append(names, name)
	}
	sort.Strings(names)

	// Prepare a buffer for the output.
	buf := &bytes.Buffer{}

	// Add a package header to get a complete package.
	fmt.Fprintln(buf, `package common;type T interface{`)

	// Add the methods.
	for _, name := range names {
		method := common[name]

		// Add doc comment, if requested.
		if *comments {
			pos := method.Obj.Pos()
			_, path, _ := prog.PathEnclosingInterval(pos, pos)

			for _, node := range path[:len(path)-1] {
				var doc *ast.CommentGroup
				switch node := node.(type) {
				case *ast.FuncDecl:
					doc = node.Doc
				case *ast.Field:
					doc = node.Doc
				}
				if doc != nil {
					for _, comment := range doc.List {
						fmt.Fprintln(buf, comment.Text)
					}
					break
				}
			}
		}

		// Add the function signature.
		sig := types.TypeString(method.Signature, (*types.Package).Name)
		fmt.Fprintf(buf, "\t%s%s\n", name, strings.TrimPrefix(sig, "func"))
	}

	fmt.Fprintln(buf, `}`)

	// Pretty-print the buffer, unless we get an error.
	src := buf.Bytes()
	if src2, err := format.Source(src); err == nil {
		src = src2
	}

	// Remove package header again so only the interface type itself remains.
	idx := bytes.Index(src, []byte("interface"))
	if idx >= 0 {
		src = src[idx:]
	}

	// Print a header, if requested.
	if *header {
		// Construct an interface type so we can check whether we can omit pointers.
		funcs := []*types.Func{}
		// Iterating the map directly is fine because order doesn't matter here.
		// (NewInterface sorts the methods)
		for name, method := range common {
			funcs = append(funcs, types.NewFunc(token.NoPos, nil, name, method.Signature))
		}
		iface := types.NewInterface(funcs, nil).Complete()

		// Print the actual header.
		fmt.Println("// Common interface of")
		for _, typ := range typs {
			// Don't print a pointer type if the element type implements the interface.
			if ptr, ok := typ.(*types.Pointer); ok && types.Implements(ptr.Elem(), iface) {
				typ = ptr.Elem()
			}

			fmt.Printf("// %v\n", typ)
		}
	}

	// Print the result.
	fmt.Printf("%s", src)
}

type fn struct {
	Signature *types.Signature
	Obj       types.Object
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [flags] [package].[type] ([package].[type]...)\n", os.Args[0])
	flag.PrintDefaults()
}
