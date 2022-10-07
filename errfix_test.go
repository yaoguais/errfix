package errfix

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrFix(t *testing.T) {
	for _, c := range testNormalCases {
		p := NewProcessor()
		f := &File{Name: c.Name, Content: c.Input}
		f2, err := p.Process(context.Background(), f)
		msg := c.Name + " " + c.Desc
		require.Nil(t, err, msg)
		require.Equal(t, c.Output, f2.Content, msg)
	}

}

type normalCase struct {
	Name   string
	Desc   string
	Input  string
	Output string
}

var testNormalCases = []normalCase{
	{
		"Unchanged #1",
		"a simple empty function",
		`package foo

func foo() {

}
`,
		`package foo

func foo() {

}
`,
	},
	{
		"Comment#1",
		"the function foo has a comment inside",
		`package foo

func foo() error {
	var err error
	// Comment A
	if err != nil {
		// Comment B
		return err
	}
	// Comment C
	return err
}
`,
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() error {
	var err error
	// Comment A
	if err != nil {
		// Comment B
		return errors.WithStack(err)
	}
	// Comment C
	return errors.WithStack(err)
}
`,
	},

	{
		"WithStack#1",
		"a function foo which returns error",
		`package main

func main() {

}

func foo() error {
	var err error
	return err
}
`,
		`package main

import (
	"github.com/pkg/errors"
)

func main() {

}

func foo() error {
	var err error
	return errors.WithStack(err)
}
`,
	},
	{
		"WithStack#2",
		"replace the errors package with github.com/pkg/errors",
		`package foo

import (
	"bar"
	"errors"
)

func foo() error {
	err := errors.New("error")
	return err
}
`,
		`package foo

import (
	"bar"
	"github.com/pkg/errors"
)

func foo() error {
	err := errors.New("error")
	return errors.WithStack(err)
}
`,
	},
	{
		"WithStack#3",
		"do not replace package github.com/pkg/errors",
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() error {
	err := errors.New("error")
	return err
}
`,
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() error {
	err := errors.New("error")
	return errors.WithStack(err)
}
`,
	},
	{
		"WithStack#4",
		"a function foo which returns integer and error",
		`package foo

func foo() (int, error) {
	var err error
	return 1, err
}
`,
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() (int, error) {
	var err error
	return 1, errors.WithStack(err)
}
`,
	},
	{
		"WithStack#5",
		"when multiple import keywords exist",
		`package main

import "bar"
import "foo"
import (
	"baz"
)

func main() {

}

func foo() error {
	var err error
	return err
}
`,
		`package main

import (
	"bar"
	"github.com/pkg/errors"
)
import "foo"
import (
	"baz"
)

func main() {

}

func foo() error {
	var err error
	return errors.WithStack(err)
}
`,
	},
	{
		"Cause#1",
		"use Cause to compare errors",
		`package foo

func foo() error {
	var err error
	if err != nil {
		return err
	}
	if err == nil {
		return nil
	}
	if err != ErrNotFound {
		return err
	}
	if err == ErrNotFound {
		return nil
	}
	return err
}
`,
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() error {
	var err error
	if err != nil {
		return errors.WithStack(err)
	}
	if err == nil {
		return nil
	}
	if errors.Cause(err) != ErrNotFound {
		return errors.WithStack(err)
	}
	if errors.Cause(err) == ErrNotFound {
		return nil
	}
	return errors.WithStack(err)
}
`,
	},
	{
		"Cause#2",
		"use Cause to compare type assert",
		`package foo

func foo() error {
	var err error
	if e, ok := err.(CustomError); ok {
		return e
	}
	switch e := err.(type) {
	case CustomError:
		return e
	}
	return err
}
`,
		`package foo

import (
	"github.com/pkg/errors"
)

func foo() error {
	var err error
	if e, ok := errors.Cause(err).(CustomError); ok {
		return e
	}
	switch e := errors.Cause(err).(type) {
	case CustomError:
		return e
	}
	return errors.WithStack(err)
}
`,
	},
	{
		"errors.New#1",
		"replace the errors.New with github.com/pkg/errors.New",
		`package foo

import (
	"bar"
	"errors"
)

var ErrNotFound = errors.New("not found")
`,
		`package foo

import (
	"bar"
	"github.com/pkg/errors"
)

var ErrNotFound = errors.New("not found")
`,
	},
	{
		"fmt.Errorf#1",
		"replace the fmt.Errorf with github.com/pkg/errors.Errorf",
		`package foo

import (
	"bar"
	"errors"
)

var ErrNotFound = fmt.Errorf("not found")
var ErrNotFound2 = fmt.Errorf("not found %d", 1)
var ErrNotFound3 = fmt.Errorf("not found %d %d", 1, 2)
var ErrNotFound4 = fmt.Errorf("not found: %v", err)
var ErrNotFound5 = fmt.Errorf("not found, %v", err)
var ErrNotFound6 = fmt.Errorf("not found %d: %v", 1, err)
var ErrNotFound7 = fmt.Errorf("not found %d %d: %v", 1, 2, err)
`,
		`package foo

import (
	"bar"
	"github.com/pkg/errors"
)

var ErrNotFound = errors.Errorf("not found")
var ErrNotFound2 = errors.Errorf("not found %d", 1)
var ErrNotFound3 = errors.Errorf("not found %d %d", 1, 2)
var ErrNotFound4 = errors.Wrapf(err, "not found")
var ErrNotFound5 = errors.Wrapf(err, "not found")
var ErrNotFound6 = errors.Wrapf(err, "not found %d", 1)
var ErrNotFound7 = errors.Wrapf(err, "not found %d %d", 1, 2)
`,
	},
}
