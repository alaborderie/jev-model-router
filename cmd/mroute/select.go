package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alaborderie/jev-model-router/internal/selection"
)

// selectDeadline bounds the whole select command; Jev applies its own shorter HTTP deadline.
const selectDeadline = 3 * time.Second

// selectError carries only a closed diagnostic code, never request or provider content.
type selectError struct{ reason string }

func (e *selectError) Error() string { return "select: " + e.reason }

func (e *selectError) exitCode() int {
	if e.reason == "usage" || e.reason == selection.ReasonInvalidRequest {
		return 2
	}
	return 1
}

// selectCommand implements `mroute select`. It never reads config, writes logs or launches engines.
func selectCommand(args []string, in io.Reader, out, errOut io.Writer) error {
	pin, ok := parseSelectArgs(args)
	if !ok {
		return report(errOut, &selectError{"usage"})
	}
	var c selection.Classifier
	if pin != "" {
		c = pinnedJev{model: pin}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, selectDeadline)
	defer cancel()
	return report(errOut, runSelect(ctx, in, out, c))
}

// parseSelectArgs accepts exactly --json plus an optional versioned --jev-model, each at most once.
func parseSelectArgs(args []string) (pin string, ok bool) {
	var sawJSON, sawPin bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json" && !sawJSON:
			sawJSON = true
		case a == "--jev-model" && !sawPin && i+1 < len(args):
			sawPin, pin = true, args[i+1]
			i++
		case strings.HasPrefix(a, "--jev-model=") && !sawPin:
			sawPin, pin = true, strings.TrimPrefix(a, "--jev-model=")
		default:
			return "", false
		}
	}
	if !sawJSON || sawPin && !selection.ValidJevModel(pin) {
		return "", false
	}
	return pin, true
}

// runSelect decodes one request, selects and writes exactly one JSON line for any valid request.
// The stdin read is bounded by ctx: if the deadline or a signal arrives first, nothing is written
// to stdout and the abandoned reader goroutine ends with the process.
func runSelect(ctx context.Context, in io.Reader, out io.Writer, classifier selection.Classifier) *selectError {
	type decoded struct {
		req selection.Request
		err error
	}
	ch := make(chan decoded, 1)
	go func() {
		req, err := selection.Decode(in)
		ch <- decoded{req, err}
	}()
	var d decoded
	select {
	case d = <-ch:
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return &selectError{selection.ReasonCancelled}
		}
		return &selectError{selection.ReasonTimeout}
	}
	if d.err != nil {
		return &selectError{selection.ReasonInvalidRequest}
	}
	resp := selection.Selector{Classifier: classifier}.Select(ctx, d.req)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(resp); err != nil {
		return &selectError{"output"}
	}
	if resp.Status == selection.StatusError {
		return &selectError{resp.Reason}
	}
	return nil
}

// report writes the single stderr line for a select failure and converts it to an exit status.
func report(errOut io.Writer, se *selectError) error {
	if se == nil {
		return nil
	}
	fmt.Fprintf(errOut, "mroute: select: %s\n", se.reason)
	return exitStatus(se.exitCode())
}

// pinnedJev reads TYPESAFE_API_KEY only when inference is actually needed (two or more candidates).
type pinnedJev struct{ model string }

func (p pinnedJev) Choose(ctx context.Context, req selection.Request) (selection.Response, error) {
	return selection.Jev{APIKey: os.Getenv("TYPESAFE_API_KEY"), Model: p.model}.Choose(ctx, req)
}
