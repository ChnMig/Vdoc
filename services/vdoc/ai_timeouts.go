package vdoc

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// 首次响应计时覆盖响应头和首个响应体字节；收到数据后改为空闲计时。
// 总时长独立计时，只有空闲时限会随网络数据（包括 SSE 心跳）续期。
type aiResponseWatchdog struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelCauseFunc
	cancelTotal context.CancelFunc
	timer       *time.Timer
	deadline    time.Time
	stage       string
	duration    time.Duration
	idle        time.Duration
	closed      bool
}

func newAIResponseWatchdog(parent context.Context, provider *AIProviderConfig) *aiResponseWatchdog {
	var cancelTotal context.CancelFunc
	total := time.Duration(provider.TimeoutMS) * time.Millisecond
	if total > 0 {
		parent, cancelTotal = context.WithTimeoutCause(parent, total, aiTimeoutError("total generation", total))
	} else {
		parent, cancelTotal = context.WithCancel(parent)
	}
	ctx, cancel := context.WithCancelCause(parent)
	w := &aiResponseWatchdog{ctx: ctx, cancel: cancel, cancelTotal: cancelTotal, idle: time.Duration(provider.IdleTimeout()) * time.Millisecond}
	w.mu.Lock()
	w.armLocked("first response", time.Duration(provider.FirstResponseTimeout())*time.Millisecond)
	w.mu.Unlock()
	return w
}

func aiTimeoutError(stage string, duration time.Duration) error {
	return fmt.Errorf("AI %s timeout after %s: %w", stage, duration, context.DeadlineExceeded)
}

func (w *aiResponseWatchdog) armLocked(stage string, duration time.Duration) {
	w.stage, w.duration = stage, duration
	if duration == 0 {
		w.deadline = time.Time{}
		if w.timer != nil {
			w.timer.Stop()
		}
		return
	}
	w.deadline = time.Now().Add(duration)
	if w.timer == nil {
		w.timer = time.AfterFunc(duration, w.expire)
	} else {
		w.timer.Reset(duration)
	}
}

func (w *aiResponseWatchdog) expire() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.deadline.IsZero() || w.ctx.Err() != nil {
		return
	}
	// 已经开始执行的旧回调也必须核对最近一次读取的期限。
	if remaining := time.Until(w.deadline); remaining > 0 {
		w.timer.Reset(remaining)
		return
	}
	w.cancel(aiTimeoutError(w.stage, w.duration))
}

func (w *aiResponseWatchdog) activity() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed && w.ctx.Err() == nil {
		w.armLocked("response idle", w.idle)
	}
}

func (w *aiResponseWatchdog) close() {
	w.mu.Lock()
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	w.cancel(context.Canceled)
	w.cancelTotal()
}

func (w *aiResponseWatchdog) err(err error) error {
	if cause := context.Cause(w.ctx); cause != nil {
		return cause
	}
	return err
}

type aiActivityReader struct {
	reader io.Reader
	watch  *aiResponseWatchdog
}

func (r aiActivityReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.watch.activity()
	}
	return n, err
}
