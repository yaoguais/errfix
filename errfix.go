// Package errfix declares the types used to replace the original go error
// with an error with a call stack.
package errfix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/pmezard/go-difflib/difflib"
	"golang.org/x/sync/errgroup"
)

// Reader is an interface that contains only one Read method.
type Reader interface {
	Read(context.Context) (chan *File, error)
}

// NewReader returns a default Reader interface.
// The parameter inputs can be *os.File, io.Reader, file path, directory path.
// When the wrong type is entered, an error will be thrown during actual reading.
func NewReader(inputs ...interface{}) Reader {
	return &reader{inputs: inputs}
}

type reader struct {
	inputs []interface{}
}

// Read returns a file channel when the call succeeds.
// The file channel has a certain buffer, which is used to speed up reading.
func (r *reader) Read(ctx context.Context) (chan *File, error) {
	if len(r.inputs) == 0 {
		return nil, errors.New("no source to read")
	}
	for _, p := range r.inputs {
		switch p := p.(type) {
		case *os.File:
		case io.Reader:
		case string:
			_, err := os.Stat(p)
			if err != nil {
				return nil, fmt.Errorf("the input source is not a valid file or directory, %v", err)
			}
		default:
			return nil, fmt.Errorf("the input source only supports *os.File, io.Reader and string")
		}
	}

	ch := make(chan *File, 8)
	go r.read(ctx, ch)
	return ch, nil
}

func (r *reader) read(ctx context.Context, ch chan *File) {
	defer close(ch)

	i := 0
	formatName := func(s string) string {
		if i > 0 {
			s = fmt.Sprintf("%s#%d", s, i)
		}
		i++
		return s
	}

	for _, p := range r.inputs {
		switch p := p.(type) {
		case *os.File:
			content, err := io.ReadAll(p)
			f := &File{
				Name:    formatName(p.Name()),
				Content: string(content),
				Error:   err,
			}
			ch <- f
		case io.Reader:
			content, err := io.ReadAll(p)
			f := &File{
				Name:    formatName("io.Reader"),
				Content: string(content),
				Error:   err,
			}
			ch <- f
		case string:
			fileInfo, err := os.Stat(p)
			if err != nil {
				f := &File{
					Name:    p,
					Content: "",
					Error:   err,
				}
				ch <- f
			} else if fileInfo.IsDir() {
				r.readDir(ctx, ch, p)
			} else {
				r.readPath(ctx, ch, p)
			}
		}
	}
}

func (r *reader) readPath(ctx context.Context, ch chan *File, p string) {
	isGoFile := strings.HasSuffix(p, ".go")
	if !isGoFile {
		return
	}
	content, err := os.ReadFile(p)
	f := &File{
		Name:    p,
		Content: string(content),
		Error:   err,
	}
	ch <- f
}

func (r *reader) readDir(ctx context.Context, ch chan *File, dir string) {
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			r.readPath(ctx, ch, p)
		}
		return nil
	})
	if err != nil {
		f := &File{
			Name:    dir,
			Content: "",
			Error:   err,
		}
		ch <- f
	}
}

// Processor is an interface that only contains the Process method.
// It converts the input file into a new file through built-in rules.
type Processor interface {
	Process(context.Context, *File) (*File, error)
}

type processor struct {
	fset *token.FileSet
}

// NewProcessor returns a default Processor interface.
func NewProcessor() Processor {
	return &processor{fset: token.NewFileSet()}
}

// Process converts the input file into a new file with built-in rules.
func (p *processor) Process(ctx context.Context, f *File) (*File, error) {
	df, err := decorator.ParseFile(p.fset, f.Name, f.Content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("error parsing ast, %v", err)
	}

	changed := false
	dps := newDstProcessors()
	for _, dp := range dps {
		dst.Inspect(df, func(n dst.Node) bool {
			err = dp.Process(ctx, n)
			return err == nil
		})
		if err != nil {
			return nil, fmt.Errorf("error while traversing ast, %v", err)
		}
		ok, err := dp.EndProcess(ctx, df)
		if err != nil {
			return nil, fmt.Errorf("error ending traversal of ast, %v", err)
		}
		changed = changed || ok
	}

	if !changed {
		f2 := &File{
			Name:    f.Name,
			Content: f.Content,
			Error:   nil,
		}
		return f2, nil
	}

	buf := &bytes.Buffer{}
	err = decorator.Fprint(buf, df)
	if err != nil {
		return nil, fmt.Errorf("error while generating source code based on ast, %v", err)
	}

	f2 := &File{
		Name:    f.Name,
		Content: buf.String(),
		Error:   nil,
	}
	return f2, nil
}

type dstProcessor interface {
	Process(context.Context, dst.Node) error
	EndProcess(context.Context, *dst.File) (bool, error)
}

type dstProcessors []dstProcessor

func newDstProcessors() dstProcessors {
	return dstProcessors{newPkgErrorsDstProcessor()}
}

type pkgErrorsDstProcessor struct {
	pkgPath        string
	errorsIdent    string
	withStackIdent string
	causeIdent     string
	newIdent       string
	errorfIdent    string
	wrapfIdent     string
	errIdent       string
	nilIdent       string
	changed        bool
}

func newPkgErrorsDstProcessor() *pkgErrorsDstProcessor {
	return &pkgErrorsDstProcessor{
		pkgPath:        "github.com/pkg/errors",
		errorsIdent:    "errors",
		withStackIdent: "WithStack",
		causeIdent:     "Cause",
		newIdent:       "New",
		errorfIdent:    "Errorf",
		wrapfIdent:     "Wrapf",
		errIdent:       "err",
		nilIdent:       "nil",
	}
}

func (p *pkgErrorsDstProcessor) Process(ctx context.Context, n dst.Node) (err error) {
	changed := false
	switch n := n.(type) {
	case *dst.ReturnStmt:
		changed = p.fixReturnStmt(n)
	case *dst.IfStmt:
		changed = p.fixIfStmt(n)
	case *dst.TypeAssertExpr:
		changed = p.fixTypeAssertExpr(n)
	case *dst.CallExpr:
		changed = p.fixCallExpr(n)
	}
	p.changed = p.changed || changed
	return
}

func (p *pkgErrorsDstProcessor) EndProcess(ctx context.Context, f *dst.File) (bool, error) {
	if !p.changed {
		return false, nil
	}

	imports := getImports(f)
	imp := findImportByPath(imports, p.pkgPath)
	if imp != nil {
		return true, nil
	}

	imp = findImportByPath(imports, "errors")
	if imp != nil {
		imp.Name = nil
		imp.Path.Value = strconv.Quote(p.pkgPath)
	} else {
		addImport(f, p.pkgPath, "", imports)
	}

	return true, nil
}

func (p pkgErrorsDstProcessor) fixReturnStmt(n *dst.ReturnStmt) (changed bool) {
	// return [..., ]err
	// ->
	// return [..., ]errors.WithStack(err)
	if len(n.Results) == 0 {
		return
	}
	lastResult := &n.Results[len(n.Results)-1]
	if !isName(*lastResult, p.errIdent) {
		return
	}
	*lastResult = &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   dst.NewIdent(p.errorsIdent),
			Sel: dst.NewIdent(p.withStackIdent),
		},
		Args: []dst.Expr{dst.NewIdent(p.errIdent)},
	}
	return true
}

func (p pkgErrorsDstProcessor) fixIfStmt(n *dst.IfStmt) (changed bool) {
	cond, ok := n.Cond.(*dst.BinaryExpr)
	if !ok {
		return
	}

	compareErr := func(cond *dst.BinaryExpr, yIsNil bool) bool {
		ok := isName(cond.X, p.errIdent) && (cond.Op == token.EQL || cond.Op == token.NEQ)
		if !ok {
			return false
		}
		ok = (yIsNil && isName(cond.Y, p.nilIdent)) || (!yIsNil && !isName(cond.Y, p.nilIdent))
		return ok
	}

	// if stmt; err == something-but-not-nil
	// ->
	// if stmt; errors.Cause(err) == something-but-not-nil
	if compareErr(cond, false) {
		cond.X = p.causeExpr()
		return true
	}
	// if stmt; err != nil && err != something-but-not-nil
	// ->
	// if stmt; err != nil && errors.Cause(err) != something-but-not-nil
	condX, okX := cond.X.(*dst.BinaryExpr)
	condY, okY := cond.Y.(*dst.BinaryExpr)
	ok = (cond.Op == token.LAND || cond.Op == token.LOR) &&
		(okX && compareErr(condX, true)) &&
		(okY && compareErr(condY, false))
	if ok {
		condY.X = p.causeExpr()
		return true
	}

	return
}

func (p pkgErrorsDstProcessor) fixTypeAssertExpr(n *dst.TypeAssertExpr) (changed bool) {
	ok := isName(n.X, p.errIdent)
	if !ok {
		return
	}
	n.X = p.causeExpr()
	return true
}

func (p pkgErrorsDstProcessor) fixCallExpr(n *dst.CallExpr) (changed bool) {
	if isPkgSelector(n.Fun, p.errorsIdent, p.newIdent) {
		return true
	}
	if isPkgSelector(n.Fun, "fmt", "Errorf") {
		if len(n.Args) == 0 {
			return
		}
		lit, ok := n.Args[0].(*dst.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return
		}
		format, err := strconv.Unquote(lit.Value)
		if err != nil {
			return
		}
		ok = len(n.Args) >= 2 && isName(n.Args[len(n.Args)-1], p.errIdent) &&
			(strings.HasSuffix(format, "%v"))
		if ok {
			// fmt.Errorf("format: %v", args..., err) ->
			// errors.Wrapf(err, "format", args...)
			newFormat := strings.TrimRight(format[0:len(format)-len("%v")], ` :,`)
			newArgs := []dst.Expr{
				n.Args[len(n.Args)-1],
				&dst.BasicLit{
					Kind:  token.STRING,
					Value: fmt.Sprintf("%q", newFormat),
				},
			}
			newArgs = append(newArgs, n.Args[1:len(n.Args)-1]...)
			n.Args = newArgs
			n.Fun = &dst.SelectorExpr{
				X:   dst.NewIdent(p.errorsIdent),
				Sel: dst.NewIdent(p.wrapfIdent),
			}
			return true
		}
		// fmt.Errorf("foo %s", x) ->
		// errors.Errorf("foo %s", x)
		n.Fun = &dst.SelectorExpr{
			X:   dst.NewIdent(p.errorsIdent),
			Sel: dst.NewIdent(p.errorfIdent),
		}
		return true
	}
	return
}

func (p pkgErrorsDstProcessor) causeExpr() *dst.CallExpr {
	return &dst.CallExpr{
		Fun: &dst.SelectorExpr{
			X:   dst.NewIdent(p.errorsIdent),
			Sel: dst.NewIdent(p.causeIdent),
		},
		Args: []dst.Expr{dst.NewIdent(p.errIdent)},
	}
}

// Writer is an interface that contains only one Write method.
type Writer interface {
	Write(context.Context, *File, *File) error
}

// DiffWriter implements the Writer interface, and it can generate diffs of old and new files.
type DiffWriter struct {
	write bool
	buf   bytes.Buffer
	mu    sync.Mutex
}

// NewDiffWriter returns a DiffWriter structure.
// When write is true and if there is a difference between the old and new files,
// then the content of the new file will overwrite the content of the old file.
func NewDiffWriter(write bool) *DiffWriter {
	return &DiffWriter{write: write}
}

// Write writes the difference between the contents of two files to the buffer,
// and overwrites the old file with the new file when needed.
// If Write is called multiple times, the difference is appended to the buffer without clearing.
func (w *DiffWriter) Write(ctx context.Context, f *File, f2 *File) error {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(f.Content),
		B:        difflib.SplitLines(f2.Content),
		FromFile: f.Name + "#original",
		ToFile:   f2.Name + "#current",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Errorf("error while generating diff, %v", err)
	}
	w.mu.Lock()
	w.buf.WriteString(text)
	w.mu.Unlock()

	if text != "" && w.write {
		fi, err := os.Stat(f.Name)
		if err == nil && !fi.IsDir() {
			return os.WriteFile(f.Name, []byte(f2.Content), 0)
		}
	}

	return nil
}

// DiffString returns the differences of files currently held in the buffer.
func (w *DiffWriter) DiffString() string {
	return w.buf.String()
}

// File represents a go file. The Error field will be set when an error occurs while reading or processing the file.
type File struct {
	Name    string
	Content string
	Error   error
}

// ErrFix converts a simple go error into an error carrying contextual information such as the call stack.
type ErrFix struct {
	r Reader
	p Processor
	w Writer
}

// NewErrFix returns an ErrFix instance.
func NewErrFix(r Reader, p Processor, w Writer) *ErrFix {
	return &ErrFix{r: r, p: p, w: w}
}

// Process will read all the files and then process the files and finally write the files to the buffer
// (or directly overwrite the original files).
func (e *ErrFix) Process(ctx context.Context) error {
	ch, err := e.r.Read(ctx)
	if err != nil {
		return err
	}

	fn := func(f *File) error {
		if f.Error != nil {
			return fmt.Errorf("error while reading from %s, %v", f.Name, f.Error)
		}
		f2, err := e.p.Process(ctx, f)
		if err != nil {
			return err
		}
		err = e.w.Write(ctx, f, f2)
		return err
	}

	wg := &errgroup.Group{}
	wg.SetLimit(8)
	errCh := make(chan error, 1)

LOOP:
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				break LOOP
			}
			wg.Go(func() error {
				return fn(f)
			})
		case err = <-errCh:
			break LOOP
		}
	}
	if err != nil {
		return err
	}

	return wg.Wait()
}
