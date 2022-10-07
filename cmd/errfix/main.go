// Package main creates an errfix tool.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/yaoguais/errfix"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: errfix [-w] [-q] [-e] [path ...]\n")
	flag.PrintDefaults()
	os.Exit(2)
}

func main() {
	quiet := flag.Bool("q", false, "quiet (no output)")
	write := flag.Bool("w", false, "write result to (source) file instead of stdout")
	setExitStatus := flag.Bool("e", false, "set exit status to 1 if any changes are found")
	flag.Usage = usage
	flag.Parse()

	var r errfix.Reader
	if flag.NArg() == 0 {
		r = errfix.NewReader(os.Stdin)
	} else {
		args := flag.Args()
		inputs := []interface{}{}
		for i := 0; i < len(args); i++ {
			inputs = append(inputs, args[i])
		}
		r = errfix.NewReader(inputs...)
	}

	w := errfix.NewDiffWriter(*write)
	p := errfix.NewProcessor()
	ef := errfix.NewErrFix(r, p, w)
	err := ef.Process(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	diff := w.DiffString()
	if !*quiet {
		fmt.Fprint(os.Stdout, diff)
	}
	if diff != "" && *setExitStatus {
		os.Exit(1)
	}
}
