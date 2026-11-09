package node

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

// ErrSandboxLimit marks work a sandboxed execution refused because it would
// generate more data than the sandbox allows.
var ErrSandboxLimit = errors.New("sandbox limit exceeded")

// Sandbox is carried by the context of a sandboxed execution (a dry-run or a
// node test, see flow.DryRun) and lets the nodes bound what they do there:
//
//   - data: no single value a node generates may exceed the per-value cap,
//     and all of them together (strings an expression builds, rendered
//     templates, outputs and their copies) draw on one budget (Charge,
//     ChargeValue, CheckValue). Once a charge fails the sandbox is Exceeded
//     and the caller ends the execution;
//   - goroutines: work a node runs in a goroutine that may outlive its call
//     (an expression evaluation it stopped waiting for) is started with Go,
//     so the caller can wait for it (Wait) before it lets another sandboxed
//     execution start.
//
// Outside a sandbox (SandboxFrom returns nil) nodes behave as usual.
type Sandbox struct {
	maxValue int64
	budget   atomic.Int64
	exceeded atomic.Bool

	mu     sync.Mutex
	idle   *sync.Cond
	active int
}

// NewSandbox returns a sandbox where no value may exceed maxValue bytes and
// all charges together may not exceed budget bytes.
func NewSandbox(maxValue, budget int64) *Sandbox {
	s := &Sandbox{maxValue: maxValue}
	s.budget.Store(budget)
	s.idle = sync.NewCond(&s.mu)
	return s
}

type sandboxKey struct{}

// WithSandbox returns a context whose nodes run in sb.
func WithSandbox(ctx context.Context, sb *Sandbox) context.Context {
	return context.WithValue(ctx, sandboxKey{}, sb)
}

// SandboxFrom returns the sandbox ctx runs in, or nil outside one (or for a
// nil ctx).
func SandboxFrom(ctx context.Context) *Sandbox {
	if ctx == nil {
		return nil
	}
	sb, _ := ctx.Value(sandboxKey{}).(*Sandbox)
	return sb
}

// InSandbox reports whether ctx is the context of a sandboxed execution.
func InSandbox(ctx context.Context) bool { return SandboxFrom(ctx) != nil }

// Go runs fn in a new goroutine. In a sandbox the goroutine is tracked until
// fn returns (see Sandbox.Wait).
func Go(ctx context.Context, fn func()) {
	if sb := SandboxFrom(ctx); sb != nil {
		sb.Go(fn)
		return
	}
	go fn()
}

// Go runs fn in a goroutine that Wait waits for.
func (s *Sandbox) Go(fn func()) {
	s.mu.Lock()
	s.active++
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			s.active--
			if s.active == 0 {
				s.idle.Broadcast()
			}
			s.mu.Unlock()
		}()
		fn()
	}()
}

// Busy reports whether a goroutine started with Go is still running.
func (s *Sandbox) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active > 0
}

// Wait blocks until every goroutine started with Go has returned.
func (s *Sandbox) Wait() {
	s.mu.Lock()
	for s.active > 0 {
		s.idle.Wait()
	}
	s.mu.Unlock()
}

// MaxValueBytes is the largest value, in bytes, a node may generate.
func (s *Sandbox) MaxValueBytes() int64 { return s.maxValue }

// Exceeded reports whether a charge failed: the execution went over a limit
// and its caller should end it.
func (s *Sandbox) Exceeded() bool { return s.exceeded.Load() }

// Charge spends n bytes of generated data: it fails with ErrSandboxLimit (and
// marks the sandbox exceeded) when n is over the per-value cap or the budget
// is spent. Call it before generating the data.
func (s *Sandbox) Charge(n int64) error {
	if n < 0 || n > s.maxValue {
		s.exceeded.Store(true)
		return fmt.Errorf("%w: a value of more than %d bytes", ErrSandboxLimit, s.maxValue)
	}
	if s.budget.Add(-n) < 0 {
		s.exceeded.Store(true)
		return fmt.Errorf("%w: the data generated went over its budget", ErrSandboxLimit)
	}
	return nil
}

// CheckValue fails with ErrSandboxLimit when v (sized like its JSON, see
// EstimateSize) is over the per-value cap. It spends nothing.
func (s *Sandbox) CheckValue(v any) error {
	if EstimateSize(v, s.maxValue) > s.maxValue {
		return fmt.Errorf("%w: a value of more than %d bytes", ErrSandboxLimit, s.maxValue)
	}
	return nil
}

// ChargeValue is Charge for the estimated size of v.
func (s *Sandbox) ChargeValue(v any) error {
	return s.Charge(EstimateSize(v, s.maxValue))
}

// EstimateSize returns roughly the size of v's JSON encoding, walking maps,
// slices, arrays, pointers and interfaces (a value shared by several
// references counts each time, as an encoding or a deep copy repeats it). It
// stops as soon as the size is over limit and then returns a value greater
// than limit, so the walk is bounded by limit, not by v.
func EstimateSize(v any, limit int64) int64 {
	e := sizeEstimator{limit: limit}
	e.add(v)
	return e.n
}

type sizeEstimator struct {
	n, limit int64
}

func (e *sizeEstimator) over() bool { return e.n > e.limit }

func (e *sizeEstimator) add(v any) {
	if e.over() {
		return
	}
	switch x := v.(type) {
	case nil:
		e.n += 4
	case string:
		e.n += int64(len(x)) + 2
	case bool:
		e.n += 5
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		e.n += 8
	case []byte:
		e.n += int64(len(x))*4/3 + 4
	case map[string]any:
		e.n += 2
		for k, item := range x {
			if e.over() {
				return
			}
			e.n += int64(len(k)) + 4
			e.add(item)
		}
	case []any:
		e.n += 2
		for _, item := range x {
			if e.over() {
				return
			}
			e.n++
			e.add(item)
		}
	default:
		e.addReflect(reflect.ValueOf(v), 0)
	}
}

// maxReflectDepth bounds the reflective walk (pointer cycles).
const maxReflectDepth = 64

func (e *sizeEstimator) addReflect(rv reflect.Value, depth int) {
	if e.over() {
		return
	}
	if depth > maxReflectDepth {
		e.n = e.limit + 1
		return
	}
	switch rv.Kind() {
	case reflect.Invalid:
		e.n += 4
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			e.n += 4
			return
		}
		elem := rv.Elem()
		if elem.Kind() != reflect.Pointer && elem.Kind() != reflect.Interface && elem.CanInterface() {
			e.add(elem.Interface())
			return
		}
		e.addReflect(elem, depth+1)
	case reflect.String:
		e.n += int64(rv.Len()) + 2
	case reflect.Slice, reflect.Array:
		e.n += 2
		for i := 0; i < rv.Len() && !e.over(); i++ {
			e.n++
			e.addValue(rv.Index(i), depth)
		}
	case reflect.Map:
		e.n += 2
		iter := rv.MapRange()
		for iter.Next() && !e.over() {
			e.addValue(iter.Key(), depth)
			e.n += 2
			e.addValue(iter.Value(), depth)
		}
	case reflect.Struct:
		e.n += 2
		for i := 0; i < rv.NumField() && !e.over(); i++ {
			e.n += int64(len(rv.Type().Field(i).Name)) + 4
			e.addValue(rv.Field(i), depth)
		}
	default:
		e.n += 8
	}
}

func (e *sizeEstimator) addValue(rv reflect.Value, depth int) {
	if rv.CanInterface() {
		switch rv.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
			e.addReflect(rv, depth+1)
		default:
			e.add(rv.Interface())
		}
		return
	}
	e.addReflect(rv, depth+1)
}
