package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"strings"

	"golang.org/x/tools/go/loader"
)

type argType struct {
	pkg string
	typ string
}

var comment = flag.Bool("comment", false, "Include comment about implementing types")
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
	var common map[string]*types.Signature
	for ti, t := range typs {
		// Wrap the type in a pointer if it might enlarge the method set.
		if _, isPtr := t.(*types.Pointer); !isPtr && !types.IsInterface(t) {
			t = types.NewPointer(t)
			typs[ti] = t
		}

		// Construct a map from name to method signature.
		ms := types.NewMethodSet(t)
		sigs := map[string]*types.Signature{}
		for i, n := 0, ms.Len(); i < n; i++ {
			method := ms.At(i)
			name := method.Obj().Name()
			if *private || ast.IsExported(name) {
				sigs[name] = method.Type().(*types.Signature)
			}
		}

		if common == nil {
			// The first type provides the initial set of methods.
			common = sigs
		} else {
			// Remove all methods not implemented by the later type.
			for k, v := range common {
				if s, ok := sigs[k]; !ok || !types.Identical(v, s) {
					delete(common, k)
				}
			}
		}
	}

	// Construct an interface type.
	funcs := []*types.Func{}
	// Iterating the map directly is fine because order doesn't matter here.
	// (NewInterface sorts the methods)
	for name, sig := range common {
		funcs = append(funcs, types.NewFunc(token.NoPos, nil, name, sig))
	}
	iface := types.NewInterface(funcs, nil).Complete()

	// Add a package header to get a complete package.
	src := []byte(`package common;type T ` +
		types.TypeString(iface, (*types.Package).Name))

	// Pretty-print.
	if src2, err := format.Source(src); err == nil {
		src = src2
	}

	// Remove package header again so only the interface type itself remains.
	idx := bytes.Index(src, []byte("interface"))
	if idx >= 0 {
		src = src[idx:]
	}

	// Print a comment, if requested.
	if *comment {
		fmt.Println("// Common interface of")
		for _, typ := range typs {
			// Remove the pointer if the element type works too.
			if ptr, ok := typ.(*types.Pointer); ok && types.Implements(ptr.Elem(), iface) {
				typ = ptr.Elem()
			}

			fmt.Printf("// %v\n", typ)
		}
	}

	// Print the result.
	fmt.Printf("%s", src)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [flags] [package].[type] ([package].[type]...)\n", os.Args[0])
	flag.PrintDefaults()
}
