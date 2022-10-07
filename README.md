# errfix

[![Build Status](https://github.com/yaoguais/errfix/actions/workflows/ci.yml/badge.svg)](https://github.com/yaoguais/errfix/actions?query=branch%3Amain)
[![codecov](https://codecov.io/gh/yaoguais/errfix/branch/main/graph/badge.svg?token=)](https://codecov.io/gh/yaoguais/errfix)
[![Go Report Card](https://goreportcard.com/badge/github.com/yaoguais/errfix)](https://goreportcard.com/report/github.com/yaoguais/errfix)
[![GoDoc](https://pkg.go.dev/badge/github.com/yaoguais/errfix?status.svg)](https://pkg.go.dev/github.com/yaoguais/errfix?tab=doc)

errfix is a command-line tool that replaces go's native error with the call-stacked error from github.com/pkg/errors.

## Install

    go install github.com/yaoguais/errfix/cmd/errfix@master

## Usage

```
usage: errfix [-w] [-q] [-e] [path ...]
  -e    set exit status to 1 if any changes are found
  -q    quiet (no output)
  -w    write result to (source) file instead of stdout
```

## Replaces

From

```go
package main

import (
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("not found")

func foo() (int, error) {
	err := bar()
	if err != nil {
		return 0, err
	}
	if err := notFound(); err != nil {
		return 0, err
	}
	if err := isExist(); err != ErrNotFound {
		return 0, nil
	}
	return 0, nil
}

func bar() error {
	return errors.New("uncompleted")
}

func notFound() error {
	err := ErrNotFound
	return err
}

func isExist() error {
	err := ErrNotFound
	return fmt.Errorf("Check for existence, %v", err)
}

func main() {

}
```

To

```go
package main

import (
	"fmt"
	"github.com/pkg/errors"
)

var ErrNotFound = errors.New("not found")

func foo() (int, error) {
	err := bar()
	if err != nil {
		return 0, errors.WithStack(err)
	}
	if err := notFound(); err != nil {
		return 0, errors.WithStack(err)
	}
	if err := isExist(); errors.Cause(err) != ErrNotFound {
		return 0, nil
	}
	return 0, nil
}

func bar() error {
	return errors.New("uncompleted")
}

func notFound() error {
	err := ErrNotFound
	return errors.WithStack(err)
}

func isExist() error {
	err := ErrNotFound
	return errors.Wrapf(err, "Check for existence")
}

func main() {

}
```
