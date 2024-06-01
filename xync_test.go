package xync

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/semaphore"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func Test_WithSupervisorBaseContext(t *testing.T) {
	s := &Supervisor{}
	WithSupervisorBaseContext(context.Background())(s)
	assert.Equal(t, context.Background(), s.baseCtx)
}

func Test_WithSupervisorMaxActive(t *testing.T) {
	s := &Supervisor{}
	WithSupervisorMaxActive(5)(s)
	assert.NotNil(t, s.sem)
}

func Test_WithSupervisorRecovery(t *testing.T) {
	s := &Supervisor{}
	WithSupervisorRecovery(func(a any) {})(s)
	assert.NotNil(t, s.recoveryFn)
}

func Test_NewSupervisor(t *testing.T) {
	// no options
	s := NewSupervisor()
	assert.NotNil(t, s.baseCtx)
	assert.NotNil(t, s.baseCancel)

	// ctx option
	ctx := context.WithValue(context.Background(), "hey", 123) //nolint:revive,staticcheck // used only for testing
	s = NewSupervisor(WithSupervisorBaseContext(ctx))
	assert.NotNil(t, s.baseCtx)
	assert.NotNil(t, s.baseCancel)
	assert.Equal(t, 123, s.baseCtx.Value("hey"))
}

func Test_Supervisor_Go(t *testing.T) {
	// restricted
	var (
		s     Supervisor
		mu    sync.RWMutex
		calls int
	)

	closeCh, goDoneCh := make(chan struct{}), make(chan struct{})
	s.activeCtx, s.activeCancel = context.WithCancel(context.Background())
	s.sem = semaphore.NewWeighted(1)

	s.Go(func(ctx context.Context) {
		if err := ctx.Err(); err == nil {
			mu.Lock()
			calls++
			mu.Unlock()
		}

		goDoneCh <- struct{}{}
		<-ctx.Done()
	})
	s.Go(func(ctx context.Context) {
		if err := ctx.Err(); err == nil {
			mu.Lock()
			calls++
			mu.Unlock()
		}

		goDoneCh <- struct{}{}
		<-ctx.Done()
	})

	go func() {
		s.wg.Wait()
		close(closeCh)
	}()

	<-goDoneCh
	s.activeCancel()
	<-goDoneCh

	require.Eventually(t, func() bool {
		select {
		case <-closeCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Equal(t, 1, calls)

	// unrestricted
	calls = 0
	closeCh, goDoneCh = make(chan struct{}), make(chan struct{})
	recCalled := false
	s = Supervisor{
		recoveryFn: func(a any) {
			recCalled = true
			assert.Equal(t, "hello", a)
		},
	}
	s.activeCtx, s.activeCancel = context.WithCancel(context.Background())

	s.Go(func(ctx context.Context) {
		panic("hello")
	})
	s.Go(func(ctx context.Context) {
		if err := ctx.Err(); err == nil {
			mu.Lock()
			calls++
			mu.Unlock()
		}

		goDoneCh <- struct{}{}
		<-ctx.Done()
	})

	go func() {
		s.wg.Wait()
		close(closeCh)
	}()

	<-goDoneCh
	s.activeCancel()

	require.Eventually(t, func() bool {
		select {
		case <-closeCh:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
	assert.Equal(t, 1, calls)
	assert.True(t, recCalled)
}

func Test_Supervisor_BaseContext(t *testing.T) {
	s := Supervisor{
		baseCtx: context.Background(),
	}

	assert.Equal(t, context.Background(), s.BaseContext())
}

func Test_Supervisor_ActiveContext(t *testing.T) {
	s := Supervisor{
		activeCtx: context.Background(),
	}

	assert.Equal(t, context.Background(), s.ActiveContext())
}

func Test_Supervisor_Wait(t *testing.T) {
	var (
		s  Supervisor
		ch = make(chan struct{})
	)

	s.wg.Add(1)

	go func() {
		s.Wait()
		close(ch)
	}()

	assert.Never(t, func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)

	s.wg.Done()

	assert.Eventually(t, func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}, time.Millisecond*200, time.Millisecond*100)
}

func Test_Supervisor_Stop(t *testing.T) {
	activeCtx, activeCancel := context.WithCancel(context.Background())
	s := Supervisor{
		baseCtx:      context.WithValue(context.Background(), "123", 123), //nolint:revive // context is used for tests
		activeCtx:    activeCtx,
		activeCancel: activeCancel,
	}

	s.wg.Add(1)

	go func() {
		<-activeCtx.Done()
		s.wg.Done()
	}()

	s.Stop()
	s.wg.Wait()

	assert.Equal(t, 123, s.activeCtx.Value("123"))
}

func Test_Supervisor_StopAndWait(t *testing.T) {
	activeCtx, activeCancel := context.WithCancel(context.Background())
	s := Supervisor{
		baseCtx:      context.WithValue(context.Background(), "123", 123), //nolint:revive // context is used for tests
		activeCtx:    activeCtx,
		activeCancel: activeCancel,
	}

	s.wg.Add(1)

	go func() {
		<-activeCtx.Done()
		s.wg.Done()
	}()

	s.StopAndWait()

	assert.Equal(t, 123, s.activeCtx.Value("123"))
}

func Test_Supervisor_Close(t *testing.T) {
	s := Supervisor{}
	s.baseCtx, s.baseCancel = context.WithCancel(context.Background())

	s.wg.Add(1)

	go func() {
		<-s.baseCtx.Done()
		s.wg.Done()
	}()

	s.Close()
	s.wg.Wait()
}

func Test_Supervisor_CloseAndWait(t *testing.T) {
	s := Supervisor{}
	s.baseCtx, s.baseCancel = context.WithCancel(context.Background())

	s.wg.Add(1)

	go func() {
		<-s.baseCtx.Done()
		s.wg.Done()
	}()

	s.CloseAndWait()
}
